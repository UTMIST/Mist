package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// App owns the HTTP gateway and the single team admission controller.
// Execution and persisted job state live in Kubernetes.
type App struct {
	httpServer *http.Server
	wg         sync.WaitGroup
	log        *slog.Logger
	executor   *KubernetesExecutor
	auth       *AuthGateway
	teams      *TeamService
}

func NewKubernetesApp(executor *KubernetesExecutor, log *slog.Logger) *App {
	mux := http.NewServeMux()
	a := &App{executor: executor, log: log, httpServer: &http.Server{
		Addr: envOr("MIST_HTTP_ADDR", "127.0.0.1:3000"), Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}}
	mux.HandleFunc("/session", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]interface{}{"enabled": false, "user": nil})
	})
	mux.HandleFunc("/datasets", a.datasets)
	mux.HandleFunc("/datasets/", a.dataset)
	mux.HandleFunc("GET /storage", a.storageInfo)
	mux.HandleFunc("GET /storage/folders", a.storageFolders)
	mux.HandleFunc("GET /storage/files", a.storageFolderFiles)
	if raw := os.Getenv("MIST_AUTH_URL"); raw != "" {
		var err error
		a.auth, err = newAuthGateway(raw)
		if err != nil {
			panic(err)
		}
		if os.Getenv("MIST_TEAMS_ENABLED") == "true" {
			if executor.storage == nil {
				panic("team workspaces require shared storage")
			}
			a.teams = newTeamService(a)
			mux.HandleFunc("/teams", a.teamEndpoint)
			mux.HandleFunc("/teams/", a.teamEndpoint)
			a.httpServer.Handler = a.auth.middleware(a.teams.middleware(mux))
		} else {
			a.httpServer.Handler = a.auth.middleware(mux)
		}
	}
	mux.HandleFunc("/jobs", a.kubernetesJobs)
	mux.HandleFunc("/jobs/status", a.getJobStatus)
	mux.HandleFunc("/jobs/", a.kubernetesJob)
	mux.HandleFunc("GET /hardware", a.hardware)
	mux.HandleFunc("GET /images", a.images)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		_, err := executor.client.BatchV1().Jobs(executor.namespace).List(r.Context(), metav1.ListOptions{Limit: 1, LabelSelector: managedLabel + "=mist"})
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok", "executor": "kubernetes"})
	})
	return a
}

func (a *App) Start() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := a.executor.client.BatchV1().Jobs(a.executor.namespace).List(ctx, metav1.ListOptions{Limit: 1}); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", a.httpServer.Addr)
	if err != nil {
		return err
	}
	if a.teams != nil {
		a.teams.start()
	}
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		a.log.Info("http server started", "address", listener.Addr().String())
		if err := a.httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			a.log.Error("HTTP server error", "error", err)
		}
	}()
	return nil
}

func (a *App) Shutdown(ctx context.Context) error {
	err := a.httpServer.Shutdown(ctx)
	a.wg.Wait()
	if a.teams != nil {
		a.teams.shutdown()
	}
	return err
}
