package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"mist/images"

	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

// CPUImage and CPURuntime are used when running CPU-only jobs (no GPU).
const (
	CPUImage  = "pytorch-cpu"
	CPURuntime = "runc"
)

type Supervisor struct {
	redisClient   *redis.Client
	ctx           context.Context
	cancel        context.CancelFunc
	consumerID    string
	gpuType       string
	dockerMgr  *images.DockerMgr
	wg            sync.WaitGroup
	log           *slog.Logger
}

func NewSupervisor(redisAddr, consumerID, gpuType string, log *slog.Logger) *Supervisor {
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())

	var dockerMgr *images.DockerMgr
	dockerCli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		log.Warn("Docker client unavailable, containers will not be started", "error", err)
	} else {
		dockerMgr = images.NewDockerMgr(dockerCli, 10, 100)
		log.Info("Docker client initialized for container execution")
	}

	return &Supervisor{
		redisClient:  redisClient,
		ctx:          ctx,
		cancel:       cancel,
		consumerID:   consumerID,
		gpuType:      gpuType,
		dockerMgr: dockerMgr,
		log:          log,
	}
}

func (s *Supervisor) Start() error {
	// Create consumer group if it doesn't exist
	err := s.createConsumerGroup()
	if err != nil {
		return fmt.Errorf("failed to create consumer group: %w", err)
	}

	s.wg.Add(1)
	go s.processJobs()

	s.log.Info("supervisor started", "consumer_id", s.consumerID, "gpu_type", s.gpuType)
	return nil
}

func (s *Supervisor) createConsumerGroup() error {
	result := s.redisClient.XGroupCreateMkStream(s.ctx, StreamName, ConsumerGroup, "$")
	if result.Err() != nil {
		if result.Err().Error() != "BUSYGROUP Consumer Group name already exists" {
			// in this case the group already exists
			return result.Err()
		}
	}
	return nil
}

func (s *Supervisor) processJobs() {
	defer s.wg.Done()
	s.log.Info("job processor started", "consumer_id", s.consumerID)
	defer s.log.Info("job processor stopped", "consumer_id", s.consumerID)

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
			// Read from stream with blocking
			result := s.redisClient.XReadGroup(s.ctx, &redis.XReadGroupArgs{
				Group:    ConsumerGroup,
				Consumer: s.consumerID,
				Streams:  []string{StreamName, ">"},
				Count:    1,
				Block:    time.Second * 5,
			})

			if result.Err() != nil {
				if !errors.Is(result.Err(), redis.Nil) {
					s.log.Error("error reading from stream", "error", result.Err())
				}
				continue
			}

			// Process each message
			for _, stream := range result.Val() {
				for _, message := range stream.Messages {
					s.handleMessage(message)
				}
			}
		}
	}
}

func (s *Supervisor) handleMessage(message redis.XMessage) {
	jobID, ok := message.Values["job_id"].(string)
	if !ok {
		s.log.Error("invalid job_id in message", "message_id", message.ID)
		s.ackMessage(message.ID)
		return
	}

	payloadData, ok := message.Values["payload"].(string)
	if !ok {
		s.log.Error("invalid payload in message", "message_id", message.ID)
		s.ackMessage(message.ID)
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadData), &payload); err != nil {
		s.log.Error("failed to unmarshal payload data", "error", err, "message_id", message.ID)
		s.ackMessage(message.ID)
		return
	}

	jobKey := fmt.Sprintf("job:%s", jobID)
	metadata, err := s.redisClient.HGetAll(s.ctx, jobKey).Result()
	if err != nil {
		s.log.Error("failed to fetch job metadata", "job_id", jobID, "error", err)
		s.ackMessage(message.ID)
		return
	}

	if len(metadata) == 0 {
    s.log.Error("job metadata not found", "job_id", jobID)
    s.ackMessage(message.ID)
    return
	}

	jobType := metadata["type"]
	requiredGPU := metadata["required_gpu"]
	jobState := metadata["job_state"]

	createdTime, _ := time.Parse(time.RFC3339, metadata["created"])
	retries, _ := strconv.Atoi(metadata["retries"])
	
	job := Job{
		ID:          jobID,
		Type:        jobType,
		Payload:     payload,
		Retries:     retries,
		Created:     createdTime,
		RequiredGPU: requiredGPU,
		JobState:    JobState(jobState),
	}

	// certain jobs require a specific GPU
	if !s.canHandleJob(job) {
		s.log.Info("skipping job due to GPU mismatch",
			"job_id", job.ID, "required_gpu", job.RequiredGPU, "supervisor_gpu", s.gpuType)
		// let another supervisor can pick it up
		return
	}

	s.emitJobEvent(job.ID, JobStateInProgress)

	success := s.processJob(job)

	if success {
		s.emitJobEvent(job.ID, JobStateSuccess)
		s.updateJobState(job.ID, JobStateSuccess)
		s.ackMessage(message.ID)
		s.log.Info("job completed successfully", "job_id", job.ID)
	} else {
		s.emitJobEvent(job.ID, JobStateFailure)
		s.updateJobState(job.ID, JobStateFailure)
		s.ackMessage(message.ID)
		s.log.Error("job failed", "job_id", job.ID)
	}
}

// canHandleJob checks if this supervisor can handle the given job based on GPU requirements
func (s *Supervisor) canHandleJob(job Job) bool {
	// If job doesn't specify GPU requirement, any supervisor can handle it
	if job.RequiredGPU == "" {
		return true
	}

	// Job must match supervisor's GPU type
	return job.RequiredGPU == s.gpuType
}

// processJob executes the job by starting a container. For CPU jobs only (no GPU).
// Returns true if the job completed successfully.
func (s *Supervisor) processJob(job Job) bool {
	// Only run CPU containers on this machine (no GPU support)
	if !s.isCPUJob(job) {
		s.log.Info("skipping container start for GPU job on CPU-only machine", "job_id", job.ID)
		return true // Ack without running - let GPU supervisor handle
	}

	if s.dockerMgr == nil {
		s.log.Warn("no container manager, simulating job success", "job_id", job.ID)
		return true
	}

	volumeName := fmt.Sprintf("job_%s_data", job.ID)
	_, err := s.dockerMgr.CreateVolume(volumeName)
	if err != nil {
		s.log.Error("failed to create volume for job", "job_id", job.ID, "error", err)
		return false
	}

	containerID, err := s.dockerMgr.RunContainer(CPUImage, CPURuntime, volumeName)
	if err != nil {
		s.log.Error("failed to run container for job", "job_id", job.ID, "error", err)
		_ = s.dockerMgr.RemoveVolume(volumeName, true)
		return false
	}

	// Run for a short time to simulate work, then clean up
	time.Sleep(2 * time.Second)

	if err := s.dockerMgr.StopContainer(containerID); err != nil {
		s.log.Error("failed to stop container", "job_id", job.ID, "container_id", containerID, "error", err)
	}
	if err := s.dockerMgr.RemoveContainer(containerID); err != nil {
		s.log.Error("failed to remove container", "job_id", job.ID, "container_id", containerID, "error", err)
	}
	if err := s.dockerMgr.RemoveVolume(volumeName, true); err != nil {
		s.log.Warn("failed to remove volume", "job_id", job.ID, "volume", volumeName, "error", err)
	}

	s.log.Info("job container completed", "job_id", job.ID, "container_id", containerID)
	return true
}

// isCPUJob returns true if the job can run on CPU (no GPU required).
func (s *Supervisor) isCPUJob(job Job) bool {
	switch job.RequiredGPU {
	case "", "CPU":
		return true
	default:
		return false
	}
}


func (s *Supervisor) emitJobEvent(jobID string, state JobState) {
	event := map[string]interface{}{
		"job_id":  jobID,
		"state":  string(state),
		"timestamp":  time.Now().Format(time.RFC3339),
		"supervisor": s.consumerID,
		"gpu_type":   s.gpuType,
	}

	if err := s.redisClient.XAdd(s.ctx, &redis.XAddArgs{
		Stream: JobEventStream,
		Values: event,
	}).Err(); err != nil {
		s.log.Error("failed to emit job event", "job_id", jobID, "state", state, "error", err)
	} else {
		s.log.Info("emitted job event", "job_id", jobID, "state", state)
	}
}


func (s *Supervisor) updateJobState(jobID string, state JobState) {
	jobKey := fmt.Sprintf("job:%s", jobID)
	if err := s.redisClient.HSet(s.ctx, jobKey, "job_state", string(state)).Err(); err != nil {
		s.log.Error("failed to update job state", "job_id", jobID, "error", err)
	}
}

func (s *Supervisor) ackMessage(messageID string) {
	result := s.redisClient.XAck(s.ctx, StreamName, ConsumerGroup, messageID)
	if result.Err() != nil {
		s.log.Error("failed to ack message", "message_id", messageID, "error", result.Err())
	}
}

func (s *Supervisor) Stop() {
	s.log.Info("stopping supervisor", "consumer_id", s.consumerID)
	s.cancel()
	s.wg.Wait()
	s.redisClient.Close()
}
