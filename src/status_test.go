package main

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

// Unit tests for StatusRegistry
func TestStatusRegistry_BasicOperations(t *testing.T) {
	redisAddr := "localhost:6379"
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	client.FlushDB(context.Background())

	registry := NewStatusRegistry(client, log)

	now := time.Now()

	// Test adding and retrieving a supervisor
	status := SupervisorStatus{
		ConsumerID: "test_worker_001",
		GPUType:    "AMD",
		Status:     SupervisorStateActive,
		LastSeen:   now,
		StartedAt:  now.Add(-1 * time.Hour),
	}

	// Add status
	err := registry.UpdateStatus(status.ConsumerID, status)
	if err != nil {
		t.Errorf("UpdateStatus failed: %v", err)
	}

	// Retrieve status
	retrievedStatus, err := registry.GetSupervisor(status.ConsumerID)
	if err != nil {
		t.Errorf("GetSupervisor failed: %v", err)
	}

	if retrievedStatus.Status != status.Status {
		t.Errorf("Expected Status %s, got %s", status.Status, retrievedStatus.Status)
	}

	// Test getting all supervisors
	allSupervisors, err := registry.GetAllSupervisors()
	if err != nil {
		t.Errorf("GetAllSupervisors failed: %v", err)
	}

	if len(allSupervisors) != 1 {
		t.Errorf("Expected 1 supervisor, got %d", len(allSupervisors))
	}

	// Test getting active supervisors
	activeSupervisors, err := registry.GetActiveSupervisors()
	if err != nil {
		t.Errorf("GetActiveSupervisors failed: %v", err)
	}

	if len(activeSupervisors) != 1 {
		t.Errorf("Expected 1 active supervisor, got %d", len(activeSupervisors))
	}
}
