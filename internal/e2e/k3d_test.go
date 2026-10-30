//go:build k3d

// Package e2e runs the M1 path through the public API on a k3d cluster:
// run scripts/k3d-test.sh. HTTP submit with an idempotency key, scheduler
// admission, the K3s executor, the job's log through the API, and the
// node's cleanup reporter posting its receipt to /internal with a host
// token. Identity is the identitytest fixture.
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/UTMIST/Mist/internal/api"
	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/db"
	"github.com/UTMIST/Mist/internal/db/dbtest"
	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/executor/k3s"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/identity/identitytest"
	"github.com/UTMIST/Mist/internal/scheduler"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

type user struct{}

func (user) Authenticate(r *http.Request) (identity.Principal, error) {
	if u := r.Header.Get("X-Test-User"); u != "" {
		return identity.Principal{Kind: identity.PrincipalAccount, Username: u}, nil
	}
	return identity.Principal{}, api.ErrNoCredentials
}

func TestK3dAPIEndToEnd(t *testing.T) {
	kctx, node := os.Getenv("MIST_K3D_CONTEXT"), os.Getenv("MIST_K3D_NODE")
	if kctx == "" || node == "" {
		t.Skip("run through scripts/k3d-test.sh")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kctx}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Impersonate.UserName = "system:serviceaccount:mist-system:mistd"
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO nodes (name, executor) VALUES ($1, 'k3s')",
		"INSERT INTO partition_nodes VALUES ('cpu', $1)",
		"UPDATE partitions SET runtime = 'mist-test-runtime:dev'",
		"SELECT setval('jobs_id_seq', (extract(epoch FROM now())::bigint % 1000000) * 100 + 90)",
	} {
		args := []any{node}
		if !strings.Contains(q, "$1") {
			args = nil
		}
		if _, err := pool.Exec(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}

	ex := k3s.New(k3s.Options{
		Config: k3s.Config{
			Namespace: "mist-jobs", EntryPath: "/usr/local/bin/mist-job-entry",
			LaunchTimeout: 10 * time.Minute, BackstopSlack: time.Minute, Nameservers: []string{"1.1.1.1"},
			Volumes: map[string]k3s.Volume{"vol1": {HostNode: node, HostPath: "/mist"}},
		},
		Client: client, Pool: pool, Resync: 5 * time.Second,
	})
	if err := ex.Start(ctx); err != nil {
		t.Fatal(err)
	}
	fixture := identitytest.New()
	sched, err := scheduler.New(ctx, scheduler.Config{
		Pool: pool, Identity: fixture, Executors: map[string]executor.Executor{"k3s": ex},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sched.Cycle(ctx); err != nil { // reports the node, so submit can check fit
		t.Fatal(err)
	}

	tokens := filepath.Join(t.TempDir(), "tokens")
	os.WriteFile(tokens, []byte(node+" cleanup "+api.HashToken("node-secret")+"\n"), 0o600)
	hosts, err := api.LoadHostTokens(tokens, "cleanup")
	if err != nil {
		t.Fatal(err)
	}
	// The API's own executor, like `mistd api`: not started, logs and
	// cleanup only. The scheduler learns of the cleanup through List.
	apiEx := k3s.New(k3s.Options{Config: k3s.Config{Namespace: "mist-jobs"}, Client: client, Pool: pool})
	go func() {
		for range apiEx.Events() {
		}
	}()
	srv := httptest.NewUnstartedServer((&api.Server{
		Jobs: scheduler.Jobs{Pool: pool, Clock: clock.Real{}}, Authz: fixture,
		Accounts: []api.Authenticator{user{}}, Hosts: hosts, Logs: apiEx, Cleanup: apiEx,
	}).Handler())
	l, err := net.Listen("tcp", "0.0.0.0:0") // reachable from the node as host.k3d.internal
	if err != nil {
		t.Fatal(err)
	}
	srv.Listener = l
	srv.Start()
	defer srv.Close()
	port := l.Addr().(*net.TCPAddr).Port

	call := func(method, path, who string, body any, into any) int {
		t.Helper()
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = bytes.NewReader(b)
		}
		req, _ := http.NewRequestWithContext(ctx, method, srv.URL+path, rd)
		req.Header.Set("X-Test-User", who)
		req.Header.Set("Idempotency-Key", "e2e-1")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		if into != nil {
			json.Unmarshal(b, into)
		}
		if resp.StatusCode >= 300 {
			t.Logf("%s %s: %d %s", method, path, resp.StatusCode, b)
		}
		return resp.StatusCode
	}

	submit := map[string]any{
		"partition": "cpu", "kind": "batch", "workdir": "/mist/teams/team-a/alice",
		"script":             "#!/bin/bash\necho \"hello from $MIST_JOB_ID\"\n",
		"resources":          map[string]any{"cpus": 1, "memory_bytes": 256 << 20, "scratch_bytes": 1 << 30},
		"time_limit_seconds": 300, "signal_grace_seconds": 5,
	}
	var job api.Job
	if code := call("POST", "/v1/jobs", "alice", submit, &job); code != http.StatusCreated {
		t.Fatalf("submit: %d", code)
	}
	var again api.Job
	if code := call("POST", "/v1/jobs", "alice", submit, &again); code != http.StatusOK || again.ID != job.ID {
		t.Fatalf("retry made job %d (%d)", again.ID, code)
	}
	jobPath := fmt.Sprintf("/v1/jobs/%d", job.ID)

	handled, cleaned := 0, false
	events := ex.Events()
	var pending []executor.Event
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if err := sched.Cycle(ctx); err != nil {
			t.Fatal(err)
		}
	drain:
		for {
			select {
			case ev := <-events:
				pending = append(pending, ev)
			default:
				break drain
			}
		}
		for ; handled < len(pending); handled++ {
			if err := sched.HandleEvent(ctx, pending[handled]); err != nil {
				t.Fatal(err)
			}
		}
		call("GET", jobPath, "bob", nil, &job) // a teammate watches it
		if job.State == "CLEANING" && !cleaned {
			if code := call("GET", jobPath, "carol", nil, nil); code != http.StatusNotFound {
				t.Fatalf("other team sees the job: %d", code)
			}
			var execID, uid string
			pool.QueryRow(ctx, "SELECT execution_id::text FROM jobs WHERE id = $1", job.ID).Scan(&execID)
			pool.QueryRow(ctx, "SELECT object_uid FROM k3s_executions WHERE execution_id = $1", execID).Scan(&uid)
			cleaned = postCleanup(t, node, execID, uid, port)
		}
		if job.State == "COMPLETED" {
			var released *time.Time
			pool.QueryRow(ctx, "SELECT released_at FROM allocations WHERE job_id = $1", job.ID).Scan(&released)
			if !cleaned || released == nil {
				t.Fatalf("COMPLETED with cleaned=%v released=%v", cleaned, released)
			}
			var log api.Log
			call("GET", jobPath+"/log", "alice", nil, &log)
			if !strings.Contains(log.Text, fmt.Sprintf("hello from %d", job.ID)) {
				t.Fatalf("log through the API: %+v", log)
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("job ended in %s", job.State)
}

// postCleanup runs the reporter on the node, which posts its receipt to
// the API itself, retrying while kubelet is still tearing the Pod down.
func postCleanup(t *testing.T, node, execID, uid string, port int) bool {
	t.Helper()
	if err := exec.Command("docker", "exec", node, "sh", "-c",
		"printf node-secret > /tmp/cleanup-token").Run(); err != nil {
		t.Fatal(err)
	}
	var last []byte
	for range 60 {
		out, err := exec.Command("docker", "exec", node, "/mistd", "cleanup-report",
			"-pod-uid", uid, "-execution-id", execID, "-node", node,
			"-post", fmt.Sprintf("http://host.k3d.internal:%d", port), "-token-file", "/tmp/cleanup-token").CombinedOutput()
		if err == nil {
			return true
		}
		last = out
		time.Sleep(time.Second)
	}
	t.Fatalf("cleanup receipt never accepted: %s", last)
	return false
}
