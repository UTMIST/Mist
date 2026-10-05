package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"mist/docker"
	"mist/multilogger"

	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

// TestContainerJobCPU verifies that when a CPU job is enqueued and a CPU supervisor
// picks it up, a container is actually started using the Docker API.
// Requires: Docker running, Redis running (docker-compose up), pytorch-cpu image built.
func TestContainerJobCPU(t *testing.T) {
	// Skip if Docker is not available
	dockerCli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		t.Skipf("Docker not available, skipping: %v", err)
	}
	defer dockerCli.Close()

	ctx := context.Background()
	if _, err := dockerCli.Ping(ctx); err != nil {
		t.Skipf("Docker daemon not reachable, skipping: %v", err)
	}

	// Ensure pytorch-cpu image exists
	_, _, err = dockerCli.ImageInspectWithRaw(ctx, "pytorch-cpu")
	if err != nil {
		t.Skipf("pytorch-cpu image not found. Build it with: cd src/images/pytorch-cpu && docker build -t pytorch-cpu .")
	}

	redisAddr := "localhost:6379"
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not running at %s, skipping: %v (run: docker-compose up -d)", redisAddr, err)
	}

	// Clean up any stale stream/consumer state for a clean test
	redisClient.FlushDB(ctx)

	config, _ := multilogger.GetLogConfig()
	schedulerLog, err := multilogger.CreateLogger("scheduler", &config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}
	supervisorLog, err := multilogger.CreateLogger("supervisor", &config)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	scheduler := NewScheduler(redisAddr, schedulerLog)
	defer scheduler.Close()

	consumerID := fmt.Sprintf("worker_cpu_test_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, "CPU", supervisorLog)
	if err := supervisor.Start(); err != nil {
		t.Fatalf("supervisor start failed: %v", err)
	}
	defer supervisor.Stop()

	// Enqueue a CPU job
	payload := map[string]interface{}{
		"task": "test_task",
		"data": "test_data",
	}
	jobID, err := scheduler.Enqueue("test_job", "CPU", payload)
	if err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}
	slog.Info("enqueued CPU job", "job_id", jobID)

	// Wait for the job to be processed (supervisor runs container for ~2 sec then cleans up)
	deadline := time.After(15 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-deadline:
			t.Fatal("timeout waiting for job to complete")
		case <-ticker.C:
			metadata, err := redisClient.HGetAll(ctx, "job:"+jobID).Result()
			if err != nil {
				continue
			}
			jobState := metadata["job_state"]
			if jobState == string(JobStateSuccess) {
				t.Logf("job completed successfully: %s", jobID)
				return
			}
			if jobState == string(JobStateFailure) {
				t.Fatalf("job failed: %s", jobID)
			}
		}
	}
}

// TestRunContainerCPU is a unit test in the images package that verifies
// running a CPU container works. We run it here via the images package.
func TestRunContainerCPUIntegration(t *testing.T) {
	dockerCli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer dockerCli.Close()

	ctx := context.Background()
	if _, err := dockerCli.Ping(ctx); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}

	// Check if pytorch-cpu image exists
	_, _, err = dockerCli.ImageInspectWithRaw(ctx, "pytorch-cpu")
	if err != nil {
		t.Skipf("pytorch-cpu image not found. Build with: cd src/images/pytorch-cpu && docker build -t pytorch-cpu .")
	}

	mgr := docker.NewDockerMgr(dockerCli, 10, 100)

	volName := "test_cpu_job_vol"
	vol, err := mgr.CreateVolume(volName)
	if err != nil {
		t.Fatalf("create volume: %v", err)
	}
	if vol.Name != volName {
		t.Errorf("volume name: got %s want %s", vol.Name, volName)
	}
	defer mgr.RemoveVolume(volName, true)

	containerID, err := mgr.RunContainer("pytorch-cpu", "runc", volName, "test_run_cpu_integration")
	if err != nil {
		t.Fatalf("run container: %v", err)
	}
	defer func() {
		_ = mgr.StopContainer(containerID)
		_ = mgr.RemoveContainer(containerID)
	}()

	// Verify container is running
	inspect, err := dockerCli.ContainerInspect(ctx, containerID)
	if err != nil {
		t.Fatalf("Failed to inspect container: %v", err)
	}
	if inspect.State.Status != "running" {
		t.Errorf("container not running: status=%s", inspect.State.Status)
	}

	t.Logf("CPU container started successfully: %s", containerID[:12])
}