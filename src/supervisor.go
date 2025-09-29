package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type Supervisor struct {
	redisClient *redis.Client
	ctx         context.Context
	cancel      context.CancelFunc
	consumerID  string
	gpuType     string
	wg          sync.WaitGroup
	log         *slog.Logger
	metrics     *Metrics
}

func NewSupervisor(redisAddr, consumerID, gpuType string, log *slog.Logger, metrics *Metrics) *Supervisor {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	ctx, cancel := context.WithCancel(context.Background())

	return &Supervisor{
		redisClient: client,
		ctx:         ctx,
		cancel:      cancel,
		consumerID:  consumerID,
		gpuType:     gpuType,
		log:         log,
		metrics:     metrics,
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
	jobData, ok := message.Values["data"].(string)
	if !ok {
		s.log.Error("invalid job data in message", "message_id", message.ID)
		s.ackMessage(message.ID)
		return
	}

	var job Job
	if err := json.Unmarshal([]byte(jobData), &job); err != nil {
		s.log.Error("failed to unmarshal job data", "error", err, "message_id", message.ID)
		s.ackMessage(message.ID)
		return
	}

	// certain jobs require a specific GPU
	if !s.canHandleJob(job) {
		s.log.Info("skipping job due to GPU mismatch",
			"job_id", job.ID, "required_gpu", job.RequiredGPU, "supervisor_gpu", s.gpuType)
		// let another supervisor can pick it up
		return
	}

	s.log.Info("processing job", "job_id", job.ID, "job_type", job.Type)

	// Simulate job processing
	gpuLabel := s.gpuType // e.g. "AMD" or "NVIDIA"
	err := s.metrics.TrackJob(context.Background(), job.Type, gpuLabel, func(ctx context.Context) error {
		if s.processJob(job) {
			return nil
		}
		return fmt.Errorf("job failed")
	})

	if err == nil {
		s.ackMessage(message.ID)
		s.log.Info("job completed successfully", "job_id", job.ID)
	} else {
		s.log.Error("job failed", "job_id", job.ID)
		s.ackMessage(message.ID) // TODO: change this once we have docker support
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

// TODO: Actually schedule a container here
func (s *Supervisor) processJob(job Job) bool {
	return true
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
