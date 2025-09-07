package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"

	"github.com/redis/go-redis/v9"
)

func TestIntegration(t *testing.T) {
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

	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, "AMD", supervisorLog)

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
