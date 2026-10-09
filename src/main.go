package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if mode := envOr("MIST_EXECUTOR", "kubernetes"); mode != "kubernetes" {
		log.Error("unsupported executor; the Docker/Redis executor has been retired", "executor", mode)
		os.Exit(1)
	}
	executor, err := NewKubernetesExecutor()
	if err != nil {
		log.Error("Kubernetes configuration failed", "error", err)
		os.Exit(1)
	}
	app := NewKubernetesApp(executor, log)
	if err := app.Start(); err != nil {
		log.Error("API startup failed", "error", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := app.Shutdown(shutdown); err != nil {
		log.Error("API shutdown failed", "error", err)
	}
}
