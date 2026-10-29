package main

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/db/dbtest"
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
		in.done <- run(ctx, []string{"scheduler", "-database-url", url, "-leader-check", "100ms"}, in.log)
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
