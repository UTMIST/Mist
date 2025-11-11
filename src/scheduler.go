package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
	"errors"

	"github.com/redis/go-redis/v9"
)

type Scheduler struct {
	client *redis.Client
	ctx    context.Context
	log    *slog.Logger
}

func NewScheduler(redisAddr string, log *slog.Logger) *Scheduler {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	return &Scheduler{
		client: client,
		ctx:    context.Background(),
		log:    log,
	}
}

func (s *Scheduler) Enqueue(jobType string, requiredGPU string, payload map[string]interface{}) error {
	// create a new job
	job := Job{
		ID:          generateJobID(),
		Type:        jobType,
		Payload:     payload,
		Retries:     0,
		Created:     time.Now(),
		RequiredGPU: requiredGPU,
		JobState:    JobStateScheduled,
	}

	if ok, err := s.JobExists(job.ID); err != nil {
    return err
	} else if ok {
		s.log.Warn("duplicate job skipped", "job_id", job.ID)
		return nil
	}

	// marshal the payload
	payloadJSON, err := json.Marshal(job.Payload)
	if err != nil {
		s.log.Error("failed to marshal job payload", "error", err)
		return err
	}

	// start redis pipeline
	pipe := s.client.Pipeline()

	// add payload to redis stream
	pipe.XAdd(s.ctx, &redis.XAddArgs{
		Stream: StreamName,
		Values: map[string]interface{}{
			"job_id":  job.ID,
			"payload": string(payloadJSON),
			"job_state": string(job.JobState),
		},
	})

	// store metadata in a redis hash
	metadataKey := fmt.Sprintf("job:%s", job.ID)
	pipe.HSet(s.ctx, metadataKey, map[string]interface{}{
		"type":         job.Type,
		"retries":      job.Retries,
		"created": job.Created.Format(time.RFC3339),
		"required_gpu": job.RequiredGPU,
		"job_state":    string(job.JobState),
	})

	// execute pipeline
	if _, err := pipe.Exec(s.ctx); err != nil {
		s.log.Error("failed to enqueue job", "error", err)
		return err
	}

	s.log.Info("enqueued job", "job_id", job.ID, "job_type", job.Type)
	return nil
}

func (s *Scheduler) Close() error {
	return s.client.Close()
}

func (s *Scheduler) JobExists(jobID string) (bool, error) {
    exists, err := s.client.Exists(s.ctx, "job:"+jobID).Result()
    if err != nil {
        return false, err
    }
    return exists > 0, nil
}

func (s *Scheduler) ListenForEvents() {
    s.log.Info("listening for job events...", "stream", JobEventStream)

    lastID := "$"

    for {
        result, err := s.client.XRead(s.ctx, &redis.XReadArgs{
            Streams: []string{JobEventStream, lastID},
            Count:   10,
            Block:   5 * time.Second,
        }).Result()

        if err != nil {
            if errors.Is(err, redis.Nil) {
                continue // no new messages
            }
            s.log.Error("error reading from event stream", "error", err)
			time.Sleep(time.Second)
            continue
        }

        for _, stream := range result {
            for _, msg := range stream.Messages {
                s.handleEventMessage(msg)
                lastID = msg.ID
            }
        }
    }
}

func (s *Scheduler) handleEventMessage(msg redis.XMessage) {
    jobID, _ := msg.Values["job_id"].(string)
    state, _ := msg.Values["state"].(string)
    timestamp, _ := msg.Values["timestamp"].(string)
    supervisor, _ := msg.Values["supervisor"].(string)

    if jobID == "" {
        s.log.Warn("received event with missing job_id", "message_id", msg.ID)
        return
    }

    metadataKey := fmt.Sprintf("job:%s", jobID)

    // Update job state in Redis
    if err := s.client.HSet(s.ctx, metadataKey, "job_state", state, "updated_at", timestamp).Err(); err != nil {
        s.log.Error("failed to update job metadata", "job_id", jobID, "error", err)
        return
    }

    s.log.Info("job state updated",
        "job_id", jobID,
        "state", state,
        "supervisor", supervisor)
}
