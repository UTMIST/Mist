package main

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestJobEnqueueAndSupervisor(t *testing.T) {
	redisAddr := "localhost:6379"
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Fatalf("Failed to connect to Redis: %v", err)
	}

	client.FlushDB(context.Background())

	// Scheduler
	scheduler := NewScheduler(redisAddr, log)
	defer scheduler.Close()

	// Supervisor
	supervisor := NewSupervisor(redisAddr, "test_worker_001", "AMD", log)
	if err := supervisor.Start(); err != nil {
		t.Fatalf("Failed to start supervisor: %v", err)
	}
	defer supervisor.Stop()

	// Enqueue jobs
	for i := 0; i < 3; i++ {
		payload := map[string]interface{}{"task": i}
		if err := scheduler.Enqueue("test_job_type", "AMD", payload); err != nil {
			t.Errorf("Failed to enqueue job %d: %v", i, err)
		}
	}

	time.Sleep(3 * time.Second)
}
