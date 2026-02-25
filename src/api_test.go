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

// addTestSession creates a session in Redis and returns a cookie value for use in requests.
func addTestSession(redisClient *redis.Client, userID string) string {
	sessionID := "test-session-" + userID
	redisClient.Set(context.Background(), "session:"+sessionID, userID, time.Hour)
	return sessionID
}

func TestGetJobLogs_RequiresAuth(t *testing.T) {
	redisAddr := "localhost:6379"
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}
	redisClient.FlushDB(context.Background())

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)

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

	sessionID := addTestSession(redisClient, "admin")

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/job_123", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
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
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}
	redisClient.FlushDB(context.Background())

	sessionID := addTestSession(redisClient, "admin")

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/nonexistent_job", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing logs, got %d", rr.Code)
	}
}

func TestGetJobLogs_InvalidSession(t *testing.T) {
	redisAddr := "localhost:6379"
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}
	redisClient.FlushDB(context.Background())

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/job_123", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: "invalid-session-not-in-redis"})
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for invalid session, got %d", rr.Code)
	}
}

func TestGetJobLogs_NoSessionCookie(t *testing.T) {
	redisAddr := "localhost:6379"
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := redisClient.Ping(context.Background()).Err(); err != nil {
		t.Skipf("Redis not running, skipping: %v", err)
	}

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/job_123", nil)
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without session cookie, got %d", rr.Code)
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

	sessionID := addTestSession(redisClient, "admin")

	log := slog.New(slog.NewJSONHandler(io.Discard, nil))
	app := NewApp(redisAddr, "AMD", log)

	req := httptest.NewRequest(http.MethodGet, "/jobs/logs/?id=job_456", nil)
	req.AddCookie(&http.Cookie{Name: "session_id", Value: sessionID})
	rr := httptest.NewRecorder()

	app.requireAuth(app.getJobLogs)(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "hello-from-container") {
		t.Errorf("expected logs to contain 'hello-from-container', got %q", rr.Body.String())
	}
}
