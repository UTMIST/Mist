// Command mistd is the Mist control plane: one binary, several roles (D16).
//
//	mistd api        HTTP API
//	mistd scheduler  queue, admission and executor calls (one active leader)
//	mistd cleanup-report  on a k3s node, as root: print a cleanup receipt
//	                 for a deleted Pod, or fail if anything of it remains
//
// Both connect to MIST_DATABASE_URL (or -database-url) and apply pending
// migrations at start.
package main

import (
	"context"
	"encoding/json"
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
	"github.com/UTMIST/Mist/internal/executor/k3s"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/scheduler"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "mistd:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: mistd <api|scheduler|cleanup-report> [flags]")
	}
	cmd, args := args[0], args[1:]
	if cmd == "cleanup-report" {
		return cleanupReport(args, stdout)
	}
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

// cleanupReport checks a node for leftovers of one Pod and prints the
// receipt. Until the internal API exists (M1 step 6) the receipt is
// delivered by whoever runs this; it is evidence, never a request.
func cleanupReport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mistd cleanup-report", flag.ContinueOnError)
	kubelet := fs.String("kubelet-root", "/var/lib/kubelet", "kubelet root directory")
	cgroups := fs.String("cgroup-root", "/sys/fs/cgroup", "cgroup v2 mount")
	node := fs.String("node", "", "this node's name")
	execID := fs.String("execution-id", "", "execution ID")
	podUID := fs.String("pod-uid", "", "recorded Pod UID")
	if err := fs.Parse(args); err != nil {
		return err
	}
	evidence, err := k3s.VerifyCleanup(*kubelet, *cgroups, *podUID)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(k3s.CleanupReceipt{
		ExecutionID: *execID, PodUID: *podUID, Node: *node, Evidence: evidence,
	})
}
