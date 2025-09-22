package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

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

func addDummySupervisors(statusRegistry *StatusRegistry, log *slog.Logger) {
	now := time.Now()

	// three dummy supervisors with different statuses
	dummySupervisors := []SupervisorStatus{
		{
			ConsumerID: "worker_amd_001",
			GPUType:    "AMD",
			Status:     "active",
			LastSeen:   now,                     // now
			StartedAt:  now.Add(-2 * time.Hour), // 2hours ago
		},
		{
			ConsumerID: "worker_nvidia_002",
			GPUType:    "NVIDIA",
			Status:     "active",
			LastSeen:   now.Add(-30 * time.Second), // 30 seconds ago
			StartedAt:  now.Add(-1 * time.Hour),    // 1 hour ago
		},
		{
			ConsumerID: "worker_tt_003",
			GPUType:    "TT",
			Status:     "inactive",
			LastSeen:   now.Add(-5 * time.Minute), // seen 5 minutes ago
			StartedAt:  now.Add(-3 * time.Hour),   // 3 hours ago
		},
	}

	// Add each dummy supervisor to the registry
	for _, supervisor := range dummySupervisors {
		if err := statusRegistry.UpdateStatus(supervisor.ConsumerID, supervisor); err != nil {
			log.Error("failed to add dummy supervisor", "consumer_id", supervisor.ConsumerID, "error", err)
		} else {
			log.Info("added dummy supervisor", "consumer_id", supervisor.ConsumerID, "gpu_type", supervisor.GPUType, "status", supervisor.Status)
		}
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
		if supervisor.Status == "active" {
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
