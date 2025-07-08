package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
)

type Scheduler struct {
	client *redis.Client
	ctx    context.Context
}

func NewScheduler(redisAddr string) *Scheduler {
	client := redis.NewClient(&redis.Options{
		Addr: redisAddr,
	})

	return &Scheduler{
		client: client,
		ctx:    context.Background(),
	}
}

func (s *Scheduler) Enqueue(jobType string, payload map[string]interface{}) error {
	job := NewJob(jobType, payload)

	jobData, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	result := s.client.XAdd(s.ctx, &redis.XAddArgs{
		Stream: StreamName,
		Values: map[string]interface{}{
			"job_id": job.ID,
			"data":   string(jobData),
		},
	})

	if result.Err() != nil {
		return fmt.Errorf("failed to enqueue job: %w", result.Err())
	}

	log.Printf("Enqueued job %s of type %s", job.ID, job.Type)
	return nil
}

func (s *Scheduler) Close() error {
	return s.client.Close()
}
