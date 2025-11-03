package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

type App struct {
	redisClient    *redis.Client
	scheduler      *Scheduler
	supervisor     *Supervisor
	httpServer     *http.Server
	wg             sync.WaitGroup
	log            *slog.Logger
	metrics        *Metrics
	statusRegistry *StatusRegistry
}

func NewApp(redisAddr, gpuType string, log *slog.Logger, metrics *Metrics) *App {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	scheduler := NewScheduler(redisAddr, log)
	statusRegistry := NewStatusRegistry(client, log)

	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, gpuType, log, metrics)

	mux := http.NewServeMux()

	mux.Handle("/metrics", promhttp.Handler())

	a := &App{
		redisClient:    client,
		scheduler:      scheduler,
		supervisor:     supervisor,
		httpServer:     &http.Server{Addr: ":3000", Handler: mux},
		log:            log,
		metrics:        metrics,
		statusRegistry: statusRegistry,
	}

	mux.Handle("/auth/login", a.metrics.WrapHTTP("auth_login", http.HandlerFunc(a.login)))
	mux.Handle("/auth/refresh", a.metrics.WrapHTTP("auth_refresh", http.HandlerFunc(a.refresh)))
	mux.Handle("/jobs", a.metrics.WrapHTTP("jobs", http.HandlerFunc(a.enqueueJob)))
	mux.Handle("/jobs/status", a.metrics.WrapHTTP("jobs_status", http.HandlerFunc(a.getJobStatus)))
	mux.Handle("/supervisors/status", a.metrics.WrapHTTP("supervisors_status", http.HandlerFunc(a.getSupervisorStatus)))
	mux.Handle("/supervisors/status/", a.metrics.WrapHTTP("supervisors_status_by_id", http.HandlerFunc(a.getSupervisorStatusByID)))
	mux.Handle("/supervisors", a.metrics.WrapHTTP("supervisors", http.HandlerFunc(a.getAllSupervisors)))

	a.log.Info("new app initialized", "redis_address", redisAddr,
		"gpu_type", gpuType, "http_address", a.httpServer.Addr)

	return a
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

	a.log.Info("shutdown completed")

	return nil
}

func main() {
	log, err := createLogger("app")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}

	metrics := NewMetrics()
	app := NewApp("localhost:6379", "AMD", log, metrics)

	if err := app.Start(); err != nil {
		log.Error("failed to start app", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go app.metrics.StartCollecting(ctx)

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

func (a *App) enqueueJob(w http.ResponseWriter, r *http.Request) {
	a.log.Info("enqueueJob handler accessed", "remote_address", r.RemoteAddr)
	payload := map[string]interface{}{
		"task_id": 123,
		"data":    "test_data_123",
	}
	if err := a.scheduler.Enqueue("jobType", payload); err != nil {
		a.log.Error("enqueue failed", "err", err, "payload", payload)
		http.Error(w, "enqueue failed", http.StatusInternalServerError)
		return
	}
	a.log.Info("job enqueued", "payload", payload)
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, "enqueued")
}

func (a *App) getJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	fmt.Fprintln(w, "job id=", id)
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
