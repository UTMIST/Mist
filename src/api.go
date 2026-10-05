package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	log2 "mist/multilogger"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"crypto/rand"
	"encoding/hex"
	"html/template"

	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
)

type App struct {
	redisClient    *redis.Client
	scheduler      *Scheduler
	supervisor     *Supervisor
	httpServer     *http.Server
	wg             sync.WaitGroup
	log            *slog.Logger
	statusRegistry *StatusRegistry
	oauthServer    *OAuthServer
	userStore      UserStore
}

func NewApp(redisAddr, gpuType string, log *slog.Logger) *App {
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	scheduler := NewScheduler(redisAddr, log)
	statusRegistry := NewStatusRegistry(client, log)
	userStore := NewRedisUserStore(client)

	ctx := context.Background()
	_, err := userStore.GetByUsername(ctx, "admin")
	if err != nil {
		// Create admin user if not exists
		// TODO: REMOVE THIS
		hash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
		admin := &User{
			ID:           "admin",
			Username:     "admin",
			PasswordHash: string(hash),
			Role:         "admin",
		}
		if err := userStore.Create(ctx, admin); err != nil {
			log.Error("failed to seed admin user", "err", err)
		} else {
			log.Info("seeded admin user")
		}
	}

	consumerID := fmt.Sprintf("worker_%d", os.Getpid())
	supervisor := NewSupervisor(redisAddr, consumerID, gpuType, log)

	oauthServer, err := NewOAuthServer(redisAddr, client, log, userStore)
	if err != nil {
		log.Error("failed to initialize oauth server", "err", err)
		// For now, we don't exit, but we should probably handle this better
	}

	mux := http.NewServeMux()
	a := &App{
		redisClient:    client,
		scheduler:      scheduler,
		supervisor:     supervisor,
		httpServer:     &http.Server{Addr: ":3000", Handler: mux},
		log:            log,
		statusRegistry: statusRegistry,
		oauthServer:    oauthServer,
		userStore:      userStore,
	}

	mux.HandleFunc("/auth/login", a.handleLogin)
	mux.HandleFunc("/auth/refresh", a.refresh)
	mux.HandleFunc("/jobs", a.requireAuth(a.handleJobs))
	mux.HandleFunc("/jobs/status", a.requireAuth(a.getJobStatus))
	mux.HandleFunc("/jobs/logs/", a.requireAuth(a.getJobLogs))
	mux.HandleFunc("/supervisors/status", a.requireAuth(a.getSupervisorStatus))
	mux.HandleFunc("/supervisors/status/", a.requireAuth(a.getSupervisorStatusByID))
	mux.HandleFunc("/supervisors", a.requireAuth(a.getAllSupervisors))

	// OAuth
	mux.HandleFunc("/oauth/authorize", a.handleAuthorize)
	mux.HandleFunc("/oauth/token", a.handleToken)
	mux.HandleFunc("/oauth/callback", a.handleOAuthCallback)

	a.log.Info("new app initialized", "redis_address", redisAddr,
		"gpu_type", gpuType, "http_address", a.httpServer.Addr)

	return a
}

func (a *App) handleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Code not found", http.StatusBadRequest)
		return
	}

	tmpl := `
	<!DOCTYPE html>
	<html>
	<body>
		Copy this code into the cli: {{ .Code }}
	</body>
	</html>
	`
	t, _ := template.New("callback").Parse(tmpl)
	t.Execute(w, map[string]string{"Code": code})
}

func (a *App) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		_, err = a.redisClient.Get(r.Context(), "session:"+cookie.Value).Result()
		if err != nil {
			http.Error(w, "Unauthorized session", http.StatusUnauthorized)
			return
		}

		next(w, r)
	}
}

func (a *App) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session_id")
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		username, err := a.redisClient.Get(r.Context(), "session:"+cookie.Value).Result()
		if err != nil {
			http.Error(w, "Unauthorized session", http.StatusUnauthorized)
			return
		}

		user, err := a.userStore.GetByUsername(r.Context(), username)
		if err != nil {
			http.Error(w, "User not found", http.StatusUnauthorized)
			return
		}

		if user.Role != "admin" {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next(w, r)
	}
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
	cfg, err := log2.GetLogConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to get log config: %v\n", err)
	}
	log, err := log2.CreateLogger("app", &cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create logger: %v\n", err)
		os.Exit(1)
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}
	gpuType := os.Getenv("GPU_TYPE")
	if gpuType == "" {
		gpuType = "CPU" // default CPU so local dev/smoke test can run container jobs
	}
	app := NewApp(redisAddr, gpuType, log)

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

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		// Render login form
		tmpl := `
		<!DOCTYPE html>
		<html>
		<head><title>Login</title></head>
		<body>
			<h2>Login</h2>
			<form method="POST" action="/auth/login">
				<input type="hidden" name="return_url" value="{{ .ReturnURL }}">
				<label>Username: <input type="text" name="username"></label><br>
				<label>Password: <input type="password" name="password"></label><br>
				<button type="submit">Login</button>
			</form>
		</body>
		</html>
		`
		t, _ := template.New("login").Parse(tmpl)
		returnURL := r.URL.Query().Get("return_url")
		t.Execute(w, map[string]string{"ReturnURL": returnURL})
		return
	}

	if r.Method == http.MethodPost {
		username := r.FormValue("username")
		password := r.FormValue("password")
		returnURL := r.FormValue("return_url")

		user, err := a.userStore.GetByUsername(r.Context(), username)
		if err != nil || !a.userStore.VerifyPassword(user, password) {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}

		// Generate session
		b := make([]byte, 32)
		rand.Read(b)
		sessionID := hex.EncodeToString(b)

		// Store session
		a.redisClient.Set(r.Context(), "session:"+sessionID, user.ID, 24*time.Hour)

		http.SetCookie(w, &http.Cookie{
			Name:     "session_id",
			Value:    sessionID,
			Path:     "/",
			HttpOnly: true,
			Expires:  time.Now().Add(24 * time.Hour),
		})

		if returnURL == "" {
			returnURL = "/"
		}
		http.Redirect(w, r, returnURL, http.StatusFound)
		return
	}
}

func (a *App) refresh(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, world!\n")
}

func (a *App) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	err := a.oauthServer.Server.HandleAuthorizeRequest(w, r)
	if err != nil {
		a.log.Error("authorization request failed", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
	}
}

func (a *App) handleToken(w http.ResponseWriter, r *http.Request) {
	err := a.oauthServer.Server.HandleTokenRequest(w, r)
	if err != nil {
		a.log.Error("token request failed", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
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
	jobID, err := a.scheduler.Enqueue(req.Type, req.RequiredGPU, req.Payload)
	if err != nil {
		a.log.Error("enqueue failed", "err", err, "payload", req.Payload)
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

func (a *App) getJobLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/jobs/logs/")
	jobID := strings.Trim(path, "/")
	if jobID == "" {
		jobID = r.URL.Query().Get("id")
	}
	if jobID == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}

	a.log.Info("getJobLogs handler accessed", "job_id", jobID, "remote_address", r.RemoteAddr)

	logs, err := a.supervisor.GetContainerLogsForJob(jobID)
	if err != nil {
		a.log.Error("failed to get job logs", "job_id", jobID, "error", err)
		http.Error(w, fmt.Sprintf("Logs not available for job: %s (container must be running)", jobID), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(logs); err != nil {
		a.log.Error("failed to write job logs response", "job_id", jobID, "error", err)
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
