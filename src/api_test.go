package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"mist/docker"

	"github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

func TestGetJobLogs_RequiresAuth(t *testing.T) {
	redisAddr := "localhost:6379"
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}
	client.FlushDB(context.Background())

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)
	app.authToken = "secret-token"

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/job_123", nil)
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without auth, got %d", rr.Code)
	}
}

func TestGetJobLogs_ValidAuth(t *testing.T) {
	redisAddr := "localhost:6379"
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}
	redisClient.FlushDB(context.Background())

	dockerCli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer dockerCli.Close()
	if _, err := dockerCli.Ping(context.Background()); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}
	_, _, err = dockerCli.ImageInspectWithRaw(context.Background(), "pytorch-cpu")
	if err != nil {
		t.Skipf("pytorch-cpu image not found: %v", err)
	}

	// Start a running container named with job ID (simulating supervisor)
	mgr := docker.NewDockerMgr(dockerCli, 10, 100)
	volName := "test_logs_vol"
	_, _ = mgr.CreateVolume(volName)
	defer mgr.RemoveVolume(volName, true)

	containerID, err := mgr.RunContainer("pytorch-cpu", "runc", volName, "job_123")
	if err != nil {
		t.Fatalf("failed to run container: %v", err)
	}
	defer func() {
		_ = mgr.StopContainer(containerID)
		_ = mgr.RemoveContainer(containerID)
	}()

	time.Sleep(500 * time.Millisecond) // let container produce output

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)
	app.authToken = "secret-token"

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/job_123", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with valid auth, got %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "hello-from-container") {
		t.Errorf("expected logs to contain 'hello-from-container', got %q", rr.Body.String())
	}
}

func TestGetJobLogs_NotFound(t *testing.T) {
	redisAddr := "localhost:6379"
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}
	client.FlushDB(context.Background())

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)
	app.authToken = "secret-token"

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/nonexistent_job", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing logs, got %d", rr.Code)
	}
}

func TestGetJobLogs_NoAuthConfigured(t *testing.T) {
	redisAddr := "localhost:6379"
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)
	app.authToken = "" // no auth configured

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/job_123", nil)
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 when auth not configured, got %d", rr.Code)
	}
}

func TestGetJobLogs_InvalidToken(t *testing.T) {
	redisAddr := "localhost:6379"
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)
	app.authToken = "correct-token"

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/job_123", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid token, got %d", rr.Code)
	}
}

func TestGetJobLogs_QueryParam(t *testing.T) {
	redisAddr := "localhost:6379"
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}
	redisClient.FlushDB(context.Background())

	dockerCli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	defer dockerCli.Close()
	if _, err := dockerCli.Ping(context.Background()); err != nil {
		t.Skipf("Docker daemon not reachable: %v", err)
	}
	_, _, err = dockerCli.ImageInspectWithRaw(context.Background(), "pytorch-cpu")
	if err != nil {
		t.Skipf("pytorch-cpu image not found: %v", err)
	}

	mgr := docker.NewDockerMgr(dockerCli, 10, 100)
	volName := "test_logs_query_vol"
	_, _ = mgr.CreateVolume(volName)
	defer mgr.RemoveVolume(volName, true)

	containerID, err := mgr.RunContainer("pytorch-cpu", "runc", volName, "job_456")
	if err != nil {
		t.Fatalf("failed to run container: %v", err)
	}
	defer func() {
		_ = mgr.StopContainer(containerID)
		_ = mgr.RemoveContainer(containerID)
	}()

	time.Sleep(500 * time.Millisecond)

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)
	app.authToken = "token"

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/?id=job_456", nil)
	req.Header.Set("Authorization", "Bearer token")
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "hello-from-container") {
		t.Errorf("expected logs to contain 'hello-from-container', got %q", rr.Body.String())
	}
}
