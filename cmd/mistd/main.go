// Command mistd is the Mist control plane: one binary, several roles (D16).
//
//	mistd api        HTTP API
//	mistd scheduler  queue, admission and executor calls (one active leader)
//	mistd cleanup-report  on a k3s node, as root: check that nothing of a
//	                 deleted Pod remains, then print or post the receipt
//
// api and scheduler connect to MIST_DATABASE_URL (or -database-url) and
// apply pending migrations at start.
//
// Binaries built with -tags mistdev also accept -dev-fixture: the
// identitytest accounts and the X-Mist-Dev-User header, for local
// development only. Production builds reject both.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/UTMIST/Mist/internal/api"
	"github.com/UTMIST/Mist/internal/clock"
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
		return cleanupReport(ctx, args, stdout)
	}
	fs := flag.NewFlagSet("mistd "+cmd, flag.ContinueOnError)
	fs.SetOutput(stderr)
	dbURL := fs.String("database-url", os.Getenv("MIST_DATABASE_URL"), "Postgres URL")
	listen := fs.String("listen", "127.0.0.1:8080", "API listen address (api)")
	tick := fs.Duration("tick", 10*time.Second, "scheduling cycle period (scheduler)")
	leaderCheck := fs.Duration("leader-check", 2*time.Second, "leadership check period (scheduler)")
	hostTokens := fs.String("host-tokens", "", "file of hashed host agent tokens (api)")
	kubeconfig := fs.String("kubeconfig", "", "kubeconfig for job logs and cleanup receipts; empty disables them (api)")
	kubeContext := fs.String("kube-context", "", "kubeconfig context (api)")
	namespace := fs.String("namespace", "mist-jobs", "job namespace (api)")
	origins := fs.String("allowed-origins", "", "comma-separated browser origins allowed to open WebSockets (api)")
	devFixture := fs.Bool("dev-fixture", false, "use the identitytest accounts (development builds only)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var dev *devIdentity
	if *devFixture {
		var err error
		if dev, err = newDevIdentity(); err != nil {
			return err
		}
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
		// Until Identity's implementation lands, fail closed: nothing is
		// admitted (AUTH_UNAVAILABLE). No executors are wired yet either.
		var admitter identity.Admitter = identity.Unavailable{}
		if dev != nil {
			admitter = dev.admit
		}
		return scheduler.Run(ctx, scheduler.Config{
			DatabaseURL: *dbURL,
			Pool:        pool,
			Identity:    admitter,
			Log:         log,
			Tick:        *tick,
			LeaderCheck: *leaderCheck,
		})
	case "api":
		s := &api.Server{
			Jobs: scheduler.Jobs{Pool: pool, Clock: clock.Real{}},
			// Fail closed until Identity provides the session and relay
			// authenticators and the real Authorizer.
			Authz: closedAuthorizer{},
			Log:   log,
			Ping:  pool.Ping,
		}
		if dev != nil {
			s.Authz, s.Accounts = dev.authz, []api.Authenticator{dev.header}
			log.Warn("development identity fixture in use; never run this in production")
		}
		if *origins != "" {
			s.AllowedOrigins = strings.Split(*origins, ",")
		}
		if *hostTokens != "" {
			ht, err := api.LoadHostTokens(*hostTokens, "cleanup")
			if err != nil {
				return err
			}
			s.Hosts = ht
		}
		if *kubeconfig != "" {
			cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				&clientcmd.ClientConfigLoadingRules{ExplicitPath: *kubeconfig},
				&clientcmd.ConfigOverrides{CurrentContext: *kubeContext}).ClientConfig()
			if err != nil {
				return err
			}
			client, err := kubernetes.NewForConfig(cfg)
			if err != nil {
				return err
			}
			// Not started: the API only reads logs and records cleanup
			// receipts. The scheduler's executor sees recorded cleanups
			// through List; the events this one emits are not needed.
			ex := k3s.New(k3s.Options{Config: k3s.Config{Namespace: *namespace}, Client: client,
				Pool: pool, Clock: clock.Real{}, Log: log})
			go func() {
				for range ex.Events() {
				}
			}()
			s.Logs, s.Cleanup = ex, ex
		}
		srv := &http.Server{Addr: *listen, Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second}
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

// devIdentity is the development identity (dev.go); production builds
// cannot construct one.
type devIdentity struct {
	authz  identity.Authorizer
	admit  identity.Admitter
	header api.Authenticator
}

// closedAuthorizer refuses everything as unavailable, so a server without
// a real Identity backend never grants access.
type closedAuthorizer struct{}

func (closedAuthorizer) Authorize(context.Context, identity.Principal, identity.Action, identity.Target) error {
	return identity.ErrUnavailable
}

// cleanupReport checks a node for leftovers of one Pod and prints the
// receipt, or with -post delivers it to the API using this host's cleanup
// token. The receipt is evidence for the executor to check, never a
// request it must obey.
func cleanupReport(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mistd cleanup-report", flag.ContinueOnError)
	kubelet := fs.String("kubelet-root", "/var/lib/kubelet", "kubelet root directory")
	cgroups := fs.String("cgroup-root", "/sys/fs/cgroup", "cgroup v2 mount")
	node := fs.String("node", "", "this node's name")
	execID := fs.String("execution-id", "", "execution ID")
	podUID := fs.String("pod-uid", "", "recorded Pod UID")
	post := fs.String("post", "", "API base URL to deliver the receipt to, e.g. http://192.168.240.1:8080")
	tokenFile := fs.String("token-file", "/etc/mist/cleanup-token", "this host's cleanup token (with -post)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	evidence, err := k3s.VerifyCleanup(*kubelet, *cgroups, *podUID)
	if err != nil {
		return err
	}
	receipt := k3s.CleanupReceipt{ExecutionID: *execID, PodUID: *podUID, Node: *node, Evidence: evidence}
	if *post == "" {
		return json.NewEncoder(stdout).Encode(receipt)
	}
	token, err := os.ReadFile(*tokenFile)
	if err != nil {
		return err
	}
	body, _ := json.Marshal(receipt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(*post, "/")+"/internal/v1/executions/"+url.PathEscape(*execID)+"/cleanup", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	reply, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cleanup receipt refused: %s: %s", resp.Status, bytes.TrimSpace(reply))
	}
	fmt.Fprintf(stdout, "%s\n", bytes.TrimSpace(reply))
	return nil
}
