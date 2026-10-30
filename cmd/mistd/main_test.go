package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/db/dbtest"
	"github.com/UTMIST/Mist/internal/executor/k3s"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

type instance struct {
	log    *syncBuffer
	cancel context.CancelFunc
	done   chan error
}

func startScheduler(t *testing.T, url string) *instance {
	ctx, cancel := context.WithCancel(context.Background())
	in := &instance{log: &syncBuffer{}, cancel: cancel, done: make(chan error, 1)}
	go func() {
		in.done <- run(ctx, []string{"scheduler", "-database-url", url, "-leader-check", "100ms"}, io.Discard, in.log)
	}()
	t.Cleanup(func() { cancel(); <-in.done })
	return in
}

// M1 step 1 check: the scheduler migrates an empty database and holds the
// advisory lock; a second instance waits until the first stops.
func TestSchedulerMigratesAndHoldsLock(t *testing.T) {
	url := dbtest.URL(t)

	first := startScheduler(t, url)
	waitFor(t, "first instance to lead", func() bool {
		return strings.Contains(first.log.String(), "holding scheduler lock")
	})

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	var n int
	if err := conn.QueryRow(ctx, "SELECT count(*) FROM schema_migrations WHERE owner = 'scheduler'").Scan(&n); err != nil || n == 0 {
		t.Fatalf("scheduler migrations not applied: n=%d err=%v", n, err)
	}

	second := startScheduler(t, url)
	waitFor(t, "second instance to wait", func() bool {
		return strings.Contains(second.log.String(), "waiting for scheduler lock")
	})
	time.Sleep(500 * time.Millisecond)
	if strings.Contains(second.log.String(), "holding scheduler lock") {
		t.Fatal("second instance acquired the lock while the first held it")
	}

	first.cancel()
	if err := <-first.done; err != nil {
		t.Fatalf("first instance: %v", err)
	}
	first.done <- nil // for Cleanup
	waitFor(t, "second instance to take over", func() bool {
		return strings.Contains(second.log.String(), "holding scheduler lock")
	})
}

// Losing the database connection must stop the leader, not leave it
// dispatching without the lock.
func TestSchedulerExitsWhenConnectionLost(t *testing.T) {
	url := dbtest.URL(t)
	in := startScheduler(t, url)
	waitFor(t, "instance to lead", func() bool {
		return strings.Contains(in.log.String(), "holding scheduler lock")
	})

	ctx := context.Background()
	conn, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, `
		SELECT pg_terminate_backend(pid) FROM pg_locks
		WHERE locktype = 'advisory' AND granted AND pid <> pg_backend_pid()`); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-in.done:
		if err == nil || !strings.Contains(err.Error(), "leadership lost") {
			t.Fatalf("want leadership lost error, got %v", err)
		}
		in.done <- nil
	case <-time.After(10 * time.Second):
		t.Fatal("scheduler kept running after losing its lock connection")
	}
}

// cleanup-report -post delivers the receipt with the host's token.
func TestCleanupReportPosts(t *testing.T) {
	kubelet, cgroups := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(kubelet, "pods"), 0o755)
	token := filepath.Join(t.TempDir(), "token")
	os.WriteFile(token, []byte("tt-secret\n"), 0o600)
	const execID = "3f1c2a9e-8b7d-4c6e-9a5b-1d2e3f4a5b6c"
	const uid = "0b6e7f2c-1a2b-4c3d-8e9f-0123456789ab"

	var got k3s.CleanupReceipt
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		json.NewDecoder(r.Body).Decode(&got)
		io.WriteString(w, `{"status":"recorded"}`)
	}))
	defer srv.Close()

	var out bytes.Buffer
	err := run(context.Background(), []string{"cleanup-report", "-kubelet-root", kubelet, "-cgroup-root", cgroups,
		"-node", "utmist-tt", "-execution-id", execID, "-pod-uid", uid, "-post", srv.URL, "-token-file", token}, &out, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tt-secret" || path != "/internal/v1/executions/"+execID+"/cleanup" ||
		got.PodUID != uid || got.Node != "utmist-tt" || got.Evidence == "" {
		t.Fatalf("posted %q %q %+v", auth, path, got)
	}

	// Leftovers mean no receipt at all.
	os.Mkdir(filepath.Join(kubelet, "pods", uid), 0o755)
	auth = ""
	err = run(context.Background(), []string{"cleanup-report", "-kubelet-root", kubelet, "-cgroup-root", cgroups,
		"-node", "utmist-tt", "-execution-id", execID, "-pod-uid", uid, "-post", srv.URL, "-token-file", token}, &out, io.Discard)
	if err == nil || auth != "" {
		t.Fatalf("receipt sent with the Pod directory present: %v", err)
	}
}
