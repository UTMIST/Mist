// Command mistd is the Mist control plane: one binary, several roles (D16).
//
//	mistd api        HTTP API
//	mistd scheduler  queue, admission and executor calls (one active leader)
//
// Both connect to MIST_DATABASE_URL (or -database-url) and apply pending
// migrations at start.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/UTMIST/Mist/internal/db"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/scheduler"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "mistd:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: mistd <api|scheduler> [flags]")
	}
	cmd, args := args[0], args[1:]
	fs := flag.NewFlagSet("mistd "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbURL := fs.String("database-url", os.Getenv("MIST_DATABASE_URL"), "Postgres URL")
	listen := fs.String("listen", "127.0.0.1:8080", "API listen address (api)")
	tick := fs.Duration("tick", 10*time.Second, "scheduling cycle period (scheduler)")
	leaderCheck := fs.Duration("leader-check", 2*time.Second, "leadership check period (scheduler)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *dbURL == "" {
		return errors.New("set MIST_DATABASE_URL or -database-url")
	}
	log := slog.New(slog.NewTextHandler(stderr, nil)).With("role", cmd)

	pool, err := db.Open(ctx, *dbURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	log.Info("database migrated")

	switch cmd {
	case "scheduler":
		return scheduler.Run(ctx, scheduler.Config{
			DatabaseURL: *dbURL,
			Pool:        pool,
			// Until Identity's implementation lands, fail closed: nothing is
			// admitted (AUTH_UNAVAILABLE). No executors are wired yet either.
			Identity:    identity.Unavailable{},
			Log:         log,
			Tick:        *tick,
			LeaderCheck: *leaderCheck,
		})
	case "api":
		mux := http.NewServeMux()
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
			if err := pool.Ping(r.Context()); err != nil {
				http.Error(w, "database unavailable", http.StatusServiceUnavailable)
				return
			}
			io.WriteString(w, "ok\n")
		})
		srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			<-ctx.Done()
			shut, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			srv.Shutdown(shut)
		}()
		log.Info("listening", "addr", *listen)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	default:
		return fmt.Errorf("unknown subcommand %q", cmd)
	}
}
