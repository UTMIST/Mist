package main

import (
	"context"
	"log/slog"
	"os"
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
			LastSeen:   now,
			StartedAt:  now.Add(-2 * time.Hour),
		},
		{
			ConsumerID: "worker_nvidia_002",
			GPUType:    "NVIDIA",
			Status:     SupervisorStateActive,
			LastSeen:   now.Add(-30 * time.Second),
			StartedAt:  now.Add(-1 * time.Hour),
		},
		{
			ConsumerID: "worker_tt_003",
			GPUType:    "TT",
			Status:     SupervisorStateInactive,
			LastSeen:   now.Add(-5 * time.Minute),
			StartedAt:  now.Add(-3 * time.Hour),
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

func TestDummySupervisors(t *testing.T) {
	redisAddr := "localhost:6379"
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	os.Setenv("ENV", "test")
	defer os.Unsetenv("ENV")

	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	client.FlushDB(context.Background())

	app := NewApp(redisAddr, "AMD", log)
	defer app.redisClient.Close()

	addDummySupervisors(app.statusRegistry, log)

	supervisors, err := app.statusRegistry.GetAllSupervisors()
	if err != nil {
		t.Errorf("Failed to get supervisors: %v", err)
	}
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
