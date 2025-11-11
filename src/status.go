package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

type StatusRegistry struct {
	redisClient *redis.Client
	log         *slog.Logger
}

func NewStatusRegistry(redisClient *redis.Client, log *slog.Logger) *StatusRegistry {
	return &StatusRegistry{
		redisClient: redisClient,
		log:         log,
	}
}

func (sr *StatusRegistry) GetAllSupervisors() ([]SupervisorStatus, error) {
	ctx := context.Background()
	result := sr.redisClient.HGetAll(ctx, SupervisorStatusKey)
	if result.Err() != nil {
		return nil, fmt.Errorf("failed to get supervisor status: %w", result.Err())
	}

	var supervisors []SupervisorStatus
	for consumerID, statusJSON := range result.Val() {
		var status SupervisorStatus
		if err := json.Unmarshal([]byte(statusJSON), &status); err != nil {
			sr.log.Error("failed to unmarshal supervisor status", "consumer_id", consumerID, "error", err)
			continue
		}
		supervisors = append(supervisors, status)
	}

	return supervisors, nil
}

func (sr *StatusRegistry) GetSupervisor(consumerID string) (*SupervisorStatus, error) {
	ctx := context.Background()
	result := sr.redisClient.HGet(ctx, SupervisorStatusKey, consumerID)
	if result.Err() != nil {
		return nil, fmt.Errorf("failed to get supervisor status: %w", result.Err())
	}

	var status SupervisorStatus
	if err := json.Unmarshal([]byte(result.Val()), &status); err != nil {
		return nil, fmt.Errorf("failed to unmarshal supervisor status: %w", err)
	}

	return &status, nil
}

func (sr *StatusRegistry) GetActiveSupervisors() ([]SupervisorStatus, error) {
	allSupervisors, err := sr.GetAllSupervisors()
	if err != nil {
		return nil, err
	}

	var activeSupervisors []SupervisorStatus
	for _, supervisor := range allSupervisors {
		if supervisor.Status == SupervisorStateActive {
			activeSupervisors = append(activeSupervisors, supervisor)
		}
	}

	return activeSupervisors, nil
}

func (sr *StatusRegistry) UpdateStatus(consumerID string, status SupervisorStatus) error {
	ctx := context.Background()
	statusJSON, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("failed to marshal supervisor status: %w", err)
	}

	result := sr.redisClient.HSet(ctx, SupervisorStatusKey, consumerID, string(statusJSON))
	if result.Err() != nil {
		return fmt.Errorf("failed to update supervisor status: %w", result.Err())
	}

	sr.log.Info("supervisor status updated", "consumer_id", consumerID, "status", status.Status)
	return nil
}

func (sr *StatusRegistry) GetJobStatus(jobID string) (*Job, error) {
	ctx := context.Background()
	result := sr.redisClient.HGet(ctx, JobStatusKey, jobID)
	if result.Err() != nil {
		if errors.Is(result.Err(), redis.Nil) {
			return nil, fmt.Errorf("job not found: %s", jobID)
		}
		return nil, fmt.Errorf("failed to get job status: %w", result.Err())
	}

	var job Job
	if err := json.Unmarshal([]byte(result.Val()), &job); err != nil {
		return nil, fmt.Errorf("failed to unmarshal job status: %w", err)
	}

	return &job, nil
}

func (sr *StatusRegistry) UpdateJobStatus(jobID string, job Job) error {
	ctx := context.Background()
	statusJSON, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job status: %w", err)
	}

	result := sr.redisClient.HSet(ctx, JobStatusKey, jobID, string(statusJSON))
	if result.Err() != nil {
		return fmt.Errorf("failed to update job status: %w", result.Err())
	}

	sr.log.Info("job status updated", "job_id", jobID, "status", job.JobState)
	return nil
}
