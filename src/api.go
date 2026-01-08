package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	log2 "mist/multilogger"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	dockerClient "github.com/docker/docker/client"
	"github.com/redis/go-redis/v9"
)

type App struct {
	redisClient    *redis.Client
	scheduler      *Scheduler
	supervisor     *Supervisor
	httpServer     *http.Server
	wg             sync.WaitGroup
	log            *slog.Logger
	statusRegistry *StatusRegistry
  dockerClient   *dockerClient.Client
	containerMgr   *ContainerMgr
}

func NewApp(redisAddr, gpuType string, log *slog.Logger) (*App, error) {
	redisClient := redis.NewClient(&redis.Options{Addr: redisAddr})
	scheduler := NewScheduler(redisAddr, log)
	statusRegistry := NewStatusRegistry(client, log)

	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, gpuType, log)

	// Initialize Docker client with explicit API version 1.41 for compatibility
	// (Docker daemon supports up to 1.41, but client defaults to 1.50)
	dockerClient, err := dockerClient.NewClientWithOpts(dockerClient.FromEnv, dockerClient.WithVersion("1.41"))
	if err != nil {
		return nil, fmt.Errorf("failed to create docker client: %w", err)
	}

	// Initialize container manager with reasonable defaults
	containerMgr := NewContainerMgr(dockerClient, 100, 50)

	mux := http.NewServeMux()
	a := &App{
		redisClient:    redisClient,
		scheduler:      scheduler,
		supervisor:     supervisor,
		httpServer:     &http.Server{Addr: ":3000", Handler: mux},
		log:            log,
		statusRegistry: statusRegistry,
    dockerClient:   dockerClient,
		containerMgr:   containerMgr,
	}

	mux.HandleFunc("/auth/login", a.login)
	mux.HandleFunc("/auth/refresh", a.refresh)
	mux.HandleFunc("/jobs", a.handleJobs)
	mux.HandleFunc("/jobs/status", a.getJobStatus)
	mux.HandleFunc("/containers/", a.handleContainerLogs)
	mux.HandleFunc("/supervisors/status", a.getSupervisorStatus)
	mux.HandleFunc("/supervisors/status/", a.getSupervisorStatusByID)
	mux.HandleFunc("/supervisors", a.getAllSupervisors)

	a.log.Info("new app initialized", "redis_address", redisAddr,
		"gpu_type", gpuType, "http_address", a.httpServer.Addr)

	return a, nil
}

func (a *App) Start() error {
	// Connect to redis
	if err := a.redisClient.Ping(context.Background()).Err(); err != nil {
		a.log.Error("redis ping failed", "err", err)
		return err
	}

	// Start supervisor
	if err := a.supervisor.Start(); err != nil {
		a.log.Error("supervisor start failed", "err", err)
		return err
	}

	// Launch HTTP server
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		slog.Info("http server started", "address", a.httpServer.Addr)
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.log.Error("HTTP server error", "err", err)
		}
	}()

	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	if err := a.httpServer.Shutdown(ctx); err != nil {
		a.log.Error("error shutting down HTTP server", "err", err)
	}

	// Wait for ListenAndServe goroutine to finish
	a.wg.Wait()

	a.supervisor.Stop()

	if err := a.scheduler.Close(); err != nil {
		a.log.Error("error closing scheduler", "err", err)

	} else {
		a.log.Info("scheduler closed successfully")
	}

	if err := a.redisClient.Close(); err != nil {
		a.log.Error("error closing redis client", "err", err)
	} else {
		a.log.Info("redis client closed successfully")
	}

	if a.dockerClient != nil {
		if err := a.dockerClient.Close(); err != nil {
			a.log.Error("error closing docker client", "err", err)
		} else {
			a.log.Info("docker client closed successfully")
		}
	}

	a.log.Info("shutdown completed")

	return nil
}

func main() {
	cfg, err := log2.GetLogConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get log config: %v\n", err)
	}
	log, err := log2.CreateLogger("app", &cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	app, err := NewApp("localhost:6379", "AMD", log)
	if err != nil {
		log.Error("failed to create app", "err", err)
		os.Exit(1)
	}

	if err := app.Start(); err != nil {
		log.Error("failed to start app", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Info("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown error", "err", err)
	}

	log.Info("all services stopped cleanly")
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	a.log.Info("login handler accessed", "remote_address", r.RemoteAddr)
	val, err := a.redisClient.Get(ctx, "some:key").Result()
	if errors.Is(err, redis.Nil) {
		a.log.Info("redis key not found")
		http.Error(w, "redis key not found", http.StatusNotFound)
		return
	}
	if err != nil {
		a.log.Error("redis error on login", "err", err)
		http.Error(w, "redis error", http.StatusInternalServerError)
		return
	}
	a.log.Info("login success", "remote_address", r.RemoteAddr)
	fmt.Fprintf(w, "login page; redis says: %q\n", val)
}

func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!\n")
}

type CreateJobRequest struct {
	Type        string                 `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	RequiredGPU string                 `json:"gpu,omitempty"`
}

type CreateJobResponse struct {
	JobID string `json:"job_id"`
}

func (a *App) handleJobs(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		a.createJob(w, r)
		return
	}
	http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
}

func (a *App) createJob(w http.ResponseWriter, r *http.Request) {

	a.log.Info("createJob handler accessed", "remote_address", r.RemoteAddr)

	var req CreateJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		a.log.Error("failed to decode request body", "err", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Type == "" {
		http.Error(w, "Job type is required", http.StatusBadRequest)
		return
	}
	if err := a.scheduler.Enqueue("jobType", "gpuType", payload); err != nil {
		a.log.Error("enqueue failed", "err", err, "payload", payload)
		http.Error(w, "enqueue failed", http.StatusInternalServerError)
		return
	}

	a.log.Info("job created", "job_id", jobID, "type", req.Type, "gpu", req.RequiredGPU)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	response := CreateJobResponse{JobID: jobID}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		a.log.Error("failed to encode response", "err", err)
	}
}

func (a *App) getJobStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		// Try to get from path if query param not provided
		path := strings.TrimPrefix(r.URL.Path, "/jobs/status/")
		if path != "" && path != "/jobs/status" {
			jobID = path
		}
	}

	if jobID == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	a.log.Info("getJobStatus handler accessed", "job_id", jobID, "remote_address", r.RemoteAddr)

	job, err := a.statusRegistry.GetJobStatus(jobID)
	if err != nil {
		a.log.Error("failed to get job status", "job_id", jobID, "error", err)
		http.Error(w, fmt.Sprintf("Job not found: %s", jobID), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(job); err != nil {
		a.log.Error("failed to encode job status response", "error", err)
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (a *App) getSupervisorStatus(w http.ResponseWriter, r *http.Request) {
	supervisors, err := a.statusRegistry.GetAllSupervisors()
	if err != nil {
		a.log.Error("failed to get supervisor status", "error", err)
		http.Error(w, "failed to get supervisor status", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"supervisors": supervisors,
		"count":       len(supervisors),
	}); err != nil {
		a.log.Error("failed to encode supervisor status response", "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (a *App) getSupervisorStatusByID(w http.ResponseWriter, r *http.Request) {
	// extract consumer ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/supervisors/status/")
	if path == "" {
		http.Error(w, "consumer ID required", http.StatusBadRequest)
		return
	}

	supervisor, err := a.statusRegistry.GetSupervisor(path)
	if err != nil {
		a.log.Error("failed to get supervisor status", "consumer_id", path, "error", err)
		http.Error(w, "supervisor not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(supervisor); err != nil {
		a.log.Error("failed to encode supervisor status response", "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (a *App) getAllSupervisors(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true"

	var supervisors []SupervisorStatus
	var err error

	if activeOnly {
		supervisors, err = a.statusRegistry.GetActiveSupervisors()
	} else {
		supervisors, err = a.statusRegistry.GetAllSupervisors()
	}

	if err != nil {
		a.log.Error("failed to get supervisors", "active_only", activeOnly, "error", err)
		http.Error(w, "failed to get supervisors", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"supervisors": supervisors,
		"count":       len(supervisors),
		"active_only": activeOnly,
	}); err != nil {
		a.log.Error("failed to encode supervisors response", "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (a *App) getSupervisorStatus(w http.ResponseWriter, r *http.Request) {
	supervisors, err := a.statusRegistry.GetAllSupervisors()
	if err != nil {
		a.log.Error("failed to get supervisor status", "error", err)
		http.Error(w, "failed to get supervisor status", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"supervisors": supervisors,
		"count":       len(supervisors),
	}); err != nil {
		a.log.Error("failed to encode supervisor status response", "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (a *App) getSupervisorStatusByID(w http.ResponseWriter, r *http.Request) {
	// extract consumer ID from URL path
	path := strings.TrimPrefix(r.URL.Path, "/supervisors/status/")
	if path == "" {
		http.Error(w, "consumer ID required", http.StatusBadRequest)
		return
	}

	supervisor, err := a.statusRegistry.GetSupervisor(path)
	if err != nil {
		a.log.Error("failed to get supervisor status", "consumer_id", path, "error", err)
		http.Error(w, "supervisor not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(supervisor); err != nil {
		a.log.Error("failed to encode supervisor status response", "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

func (a *App) getAllSupervisors(w http.ResponseWriter, r *http.Request) {
	activeOnly := r.URL.Query().Get("active") == "true"

	var supervisors []SupervisorStatus
	var err error

	if activeOnly {
		supervisors, err = a.statusRegistry.GetActiveSupervisors()
	} else {
		supervisors, err = a.statusRegistry.GetAllSupervisors()
	}

	if err != nil {
		a.log.Error("failed to get supervisors", "active_only", activeOnly, "error", err)
		http.Error(w, "failed to get supervisors", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"supervisors": supervisors,
		"count":       len(supervisors),
		"active_only": activeOnly,
	}); err != nil {
		a.log.Error("failed to encode supervisors response", "error", err)
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
		return
	}
}

// AssociateContainerWithUser stores the container-user association in Redis.
// This should be called when a container is created to track ownership for authorization.
func (a *App) AssociateContainerWithUser(ctx context.Context, containerID, userID string) error {
	key := fmt.Sprintf("container:%s:owner", containerID)
	return a.redisClient.Set(ctx, key, userID, 0).Err()
}

// getContainerOwner retrieves the owner user ID for a container from Redis
func (a *App) getContainerOwner(ctx context.Context, containerID string) (string, error) {
	key := fmt.Sprintf("container:%s:owner", containerID)
	userID, err := a.redisClient.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", fmt.Errorf("container not found or not associated with any user")
		}
		return "", fmt.Errorf("failed to get container owner: %w", err)
	}
	return userID, nil
}

// getCurrentUser extracts the current user ID from the request
// This is a placeholder - in a real implementation, this would extract from JWT token, session, etc.
func (a *App) getCurrentUser(r *http.Request) (string, error) {
	// For now, we'll use a simple Authorization header or user query parameter
	// In a production system, this would validate JWT tokens, session cookies, etc.
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		// Extract user from "Bearer <token>" or similar
		parts := strings.Split(authHeader, " ")
		if len(parts) == 2 && parts[0] == "Bearer" {
			// In a real implementation, decode and validate the token
			// For now, we'll use the token as a simple user identifier
			return parts[1], nil
		}
	}

	// Fallback: check for user query parameter (for testing)
	userID := r.URL.Query().Get("user")
	if userID != "" {
		return userID, nil
	}

	return "", fmt.Errorf("authentication required")
}

// authorizeContainerAccess checks if the current user has access to the specified container
func (a *App) authorizeContainerAccess(ctx context.Context, containerID string, userID string) error {
	ownerID, err := a.getContainerOwner(ctx, containerID)
	if err != nil {
		return err
	}

	if ownerID != userID {
		return fmt.Errorf("unauthorized: user %s does not have access to container %s", userID, containerID)
	}

	return nil
}

// handleContainerLogs handles requests to /containers/{containerID}/logs
func (a *App) handleContainerLogs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Extract container ID from path
	// Path format: /containers/{containerID}/logs
	path := strings.TrimPrefix(r.URL.Path, "/containers/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[1] != "logs" {
		http.Error(w, "Invalid path. Expected /containers/{containerID}/logs", http.StatusBadRequest)
		return
	}

	containerID := parts[0]
	if containerID == "" {
		http.Error(w, "Container ID is required", http.StatusBadRequest)
		return
	}

	// Get current user
	userID, err := a.getCurrentUser(r)
	if err != nil {
		a.log.Warn("authentication failed", "error", err, "remote_address", r.RemoteAddr)
		http.Error(w, "Authentication required", http.StatusUnauthorized)
		return
	}

	// Authorize access to container
	if err := a.authorizeContainerAccess(ctx, containerID, userID); err != nil {
		a.log.Warn("authorization failed", "error", err, "user_id", userID, "container_id", containerID)
		http.Error(w, "Unauthorized: "+err.Error(), http.StatusForbidden)
		return
	}

	// Parse query parameters for log options
	tailStr := r.URL.Query().Get("tail")
	tail := 0
	if tailStr != "" {
		var err error
		tail, err = strconv.Atoi(tailStr)
		if err != nil || tail < 0 {
			http.Error(w, "Invalid tail parameter. Must be a non-negative integer", http.StatusBadRequest)
			return
		}
	}

	followStr := r.URL.Query().Get("follow")
	follow := followStr == "true" || followStr == "1"
	since := r.URL.Query().Get("since")
	until := r.URL.Query().Get("until")

	// Fetch container logs
	logsReader, err := a.containerMgr.GetContainerLogs(containerID, tail, follow, since, until)
	if err != nil {
		a.log.Error("failed to get container logs", "error", err, "container_id", containerID)
		http.Error(w, fmt.Sprintf("Failed to fetch container logs: %v", err), http.StatusInternalServerError)
		return
	}
	defer logsReader.Close()

	// Set appropriate headers for streaming logs
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if follow {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
	}

	// Stream logs to response
	_, err = io.Copy(w, logsReader)
	if err != nil && !errors.Is(err, io.EOF) {
		a.log.Error("error streaming logs", "error", err, "container_id", containerID)
		// Don't send error to client if we've already started streaming
		return
	}

	a.log.Info("container logs retrieved", "container_id", containerID, "user_id", userID)
}
