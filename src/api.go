package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
)

type App struct {
	redisClient *redis.Client
	scheduler   *Scheduler
	supervisor  *Supervisor
	httpServer  *http.Server
	wg          sync.WaitGroup
}

func NewApp(redisAddr, gpuType string) *App {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	scheduler := NewScheduler(redisAddr)

	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, gpuType)

	mux := http.NewServeMux()
	a := &App{
		redisClient: client,
		scheduler:   scheduler,
		supervisor:  supervisor,
		httpServer:  &http.Server{Addr: ":3000", Handler: mux},
	}

	mux.HandleFunc("/auth/login", a.login)
	mux.HandleFunc("/auth/refresh", a.refresh)
	mux.HandleFunc("/jobs", a.enqueueJob)
	mux.HandleFunc("/jobs/status", a.getJobStatus)

	return a
}

func (a *App) Start() error {
	// Connect to redis
	if err := a.redisClient.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
	}

	// Start Supervisor
	if err := a.supervisor.Start(); err != nil {
		return fmt.Errorf("supervisor start failed: %w", err)
	}

	// Launch HTTP server
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		log.Println("HTTP server listening on", a.httpServer.Addr)
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("HTTP server error: %v", err)
		}
	}()

	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	if err := a.httpServer.Shutdown(ctx); err != nil {
		log.Printf("error shutting down HTTP server: %v", err)
	}

	// Wait for ListenAndServe goroutine to finish
	a.wg.Wait()

	a.supervisor.Stop()

	if err := a.scheduler.Close(); err != nil {
		log.Printf("error closing scheduler: %v", err)
	}

	if err := a.redisClient.Close(); err != nil {
		log.Printf("error closing redis client: %v", err)
	}

	return nil
}

func main() {
	app := NewApp("localhost:6379", "AMD")

	if err := app.Start(); err != nil {
		log.Fatalf("failed to start app: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	log.Println("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := app.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown error: %v", err)
	}

	log.Println("all services stopped cleanly")
}

func (a *App) login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	val, err := a.redisClient.Get(ctx, "some:key").Result()
	if err != nil && err != redis.Nil {
		http.Error(w, "redis error", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "login page; redis says: %q\n", val)
}

func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!\n")
}

func (a *App) enqueueJob(w http.ResponseWriter, r *http.Request) {
	payload := map[string]interface{}{
		"task_id": 123,
		"data":    "test_data_123",
	}
	if err := a.scheduler.Enqueue("jobType", payload); err != nil {
		http.Error(w, "enqueue failed", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	fmt.Fprint(w, "enqueued")
}

func (a *App) getJobStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	fmt.Fprintln(w, "job id=", id)
}
