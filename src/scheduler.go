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

func (s *Scheduler) Enqueue(jobType string, payload map[string]interface{}, requiredGPU string) (string, error) {
	job := Job{
		ID:          generateJobID(),
		Type:        jobType,
		Payload:     payload,
		Retries:     0,
		Created:     time.Now(),
		RequiredGPU: requiredGPU,
		JobState:    JobStateScheduled,
	}

	jobData, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("failed to marshal job: %w", err)
	}

	statusJSON, err := json.Marshal(job)
	if err != nil {
		return "", fmt.Errorf("failed to marshal job status: %w", err)
	}

	if err := s.client.HSet(s.ctx, JobStatusKey, job.ID, string(statusJSON)).Err(); err != nil {
		return "", fmt.Errorf("failed to store job status: %w", err)
	}

	result := s.client.XAdd(s.ctx, &redis.XAddArgs{
		Stream: StreamName,
		Values: map[string]interface{}{
			"job_id": job.ID,
			"data":   string(jobData),
		},
	})

	if result.Err() != nil {
		s.client.HDel(s.ctx, JobStatusKey, job.ID)
		return "", fmt.Errorf("failed to enqueue job: %w", result.Err())
	}

	s.log.Info("enqueued job", "job_id", job.ID, "job_type", job.Type, "gpu", requiredGPU)
	return job.ID, nil
}

func (s *Scheduler) Close() error {
	return s.client.Close()
}
