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

	"github.com/go-oauth2/oauth2/v4/manage"
	"github.com/go-oauth2/oauth2/v4/models"
	"github.com/go-oauth2/oauth2/v4/server"
	"github.com/go-oauth2/oauth2/v4/store"
	"github.com/redis/go-redis/v9"
)

type App struct {
	redisClient *redis.Client
	scheduler   *Scheduler
	httpServer  *http.Server
	manager     *manage.Manager
	srv         *server.Server
	wg          sync.WaitGroup
}

func NewApp(redisAddr, gpuType string) *App {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	scheduler := NewScheduler(redisAddr)

	manager := manage.NewDefaultManager()
	// token memory store
	manager.MustTokenStorage(store.NewMemoryTokenStore())

	// client memory store
	clientStore := store.NewClientStore()
	clientStore.Set("000000", &models.Client{
		ID:     "000000",
		Secret: "999999",
		Domain: "http://localhost:3000", // replace with environment domain
	})
	manager.MapClientStorage(clientStore)
	srv := CreateServer(manager)

	mux := http.NewServeMux()
	a := &App{
		redisClient: client,
		scheduler:   scheduler,
		manager:     manager,
		srv:         srv,
		httpServer:  &http.Server{Addr: ":3000", Handler: mux},
	}

	// auth routes
	mux.HandleFunc("/oauth/authorize", a.authorize)
	mux.HandleFunc("/oauth/token", a.token)
	
	mux.HandleFunc("/jobs", a.enqueueJob)
	mux.HandleFunc("/jobs/status", a.getJobStatus)

	return a
}

func (a *App) Start() error {
	// Connect to redis
	if err := a.redisClient.Ping(context.Background()).Err(); err != nil {
		return fmt.Errorf("redis ping failed: %w", err)
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

func (a *App) authorize(w http.ResponseWriter, r *http.Request) {
	err := a.srv.HandleAuthorizeRequest(w, r)
	if err != nil {
		log.Printf("Authorize error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (a *App) token(w http.ResponseWriter, r *http.Request) {
	err := a.srv.HandleTokenRequest(w, r)
	if err != nil {
		log.Printf("Token error: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
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
