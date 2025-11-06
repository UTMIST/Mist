package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

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
			"job_state": job.JobState,
		},
	})

	// store metadata in a redis hash
	metadataKey := fmt.Sprintf("job:%s", job.ID)
	pipe.HSet(s.ctx, metadataKey, map[string]interface{}{
		"type":         job.Type,
		"retries":      job.Retries,
		"created":      "created": job.Created.Format(time.RFC3339),
		"required_gpu": job.RequiredGPU,
		"job_state":    job.JobState,
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
