package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func addDummySupervisors(statusRegistry *StatusRegistry, log *slog.Logger) {
	now := time.Now()

	dummySupervisors := []SupervisorStatus{
		{
			ConsumerID: "worker_amd_001",
			GPUType:    "AMD",
			Status:     SupervisorStateActive,
			LastSeen:   now,                     // now
			StartedAt:  now.Add(-2 * time.Hour), // 2hours ago
		},
		{
			ConsumerID: "worker_nvidia_002",
			GPUType:    "NVIDIA",
			Status:     SupervisorStateActive,
			LastSeen:   now.Add(-30 * time.Second), // 30 seconds ago
			StartedAt:  now.Add(-1 * time.Hour),    // 1 hour ago
		},
		{
			ConsumerID: "worker_tt_003",
			GPUType:    "TT",
			Status:     SupervisorStateInactive,
			LastSeen:   now.Add(-5 * time.Minute), // seen 5 minutes ago
			StartedAt:  now.Add(-3 * time.Hour),   // 3 hours ago
		},
	}

	for _, supervisor := range dummySupervisors {
		if err := statusRegistry.UpdateStatus(supervisor.ConsumerID, supervisor); err != nil {
			log.Error("failed to add dummy supervisor", "consumer_id", supervisor.ConsumerID, "error", err)
		} else {
			log.Info("added dummy supervisor", "consumer_id", supervisor.ConsumerID, "gpu_type", supervisor.GPUType, "status", supervisor.Status)
		}
	}
}

func TestIntegration(t *testing.T) {
	os.Setenv("ENV", "test")
	defer os.Unsetenv("ENV")

	redisAddr := "localhost:6379"
	schedulerLog, err := createLogger("scheduler")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Errorf("Failed to connect to Redis: %v", err)
	}

	scheduler := NewScheduler(redisAddr, schedulerLog)
	defer scheduler.Close()

	supervisorLog, err := createLogger("supervisor")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	metrics := NewMetrics()
	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, "AMD", supervisorLog, metrics)

	if err := supervisor.Start(); err != nil {
		t.Errorf("Failed to start supervisor: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// test jobs
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		jobTypes := []string{"a", "b", "c"}
		for i := 0; i < 10; i++ {
			jobType := jobTypes[i%len(jobTypes)]
			payload := map[string]interface{}{
				"task_id": i,
				"data":    fmt.Sprintf("test_data_%d", i),
			}

			if err := scheduler.Enqueue(jobType, payload); err != nil {
				t.Errorf("Failed to enqueue job: %v", err)
			}
		}
	}()

	wg.Wait()
	supervisor.Stop()
}

func TestDummySupervisors(t *testing.T) {
	redisAddr := "localhost:6379"
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// Test 1: Dummy supervisors should be added in test environment
	os.Setenv("ENV", "test")
	defer os.Unsetenv("ENV")

	// Clean up Redis data before test
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	client.FlushDB(context.Background())

	app := NewApp(redisAddr, "AMD", log)
	defer app.redisClient.Close()

	// Manually add dummy supervisors for testing
	addDummySupervisors(app.statusRegistry, log)

	supervisors, err := app.statusRegistry.GetAllSupervisors()
	if err != nil {
		t.Errorf("Failed to get supervisors: %v", err)
	}
	// Verify dummy supervisor IDs exist
	dummyIDs := []string{"worker_amd_001", "worker_nvidia_002", "worker_tt_003"}
	for _, dummyID := range dummyIDs {
		found := false
		for _, supervisor := range supervisors {
			if supervisor.ConsumerID == dummyID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected dummy supervisor %s not found", dummyID)
		}
	}
}

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
