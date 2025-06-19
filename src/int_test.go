package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestIntegration(t *testing.T) {
	redisAddr := "localhost:6379"

	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Errorf("Failed to connect to Redis: %v", err)
	}

	scheduler := NewScheduler(redisAddr)
	defer scheduler.Close()

	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor, err := NewSupervisor(redisAddr, consumerID)
	if err != nil {
		t.Errorf("Failed to create supervisor: %v", err)
	}

	if err := supervisor.Start(); err != nil {
		t.Errorf("Failed to start supervisor: %v", err)
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// test jobs
	go func() {
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
	supervisor.Stop()
}
