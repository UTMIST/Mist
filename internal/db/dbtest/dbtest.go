// Package dbtest gives tests a fresh, empty Postgres database.
//
// If MIST_TEST_DATABASE_URL is set (a superuser URL), databases are created
// on that server. Otherwise one embedded Postgres per test binary is started
// as the current user; the first run downloads it to ~/.embedded-postgres-go.
package dbtest

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5"
)

var (
	mu       sync.Mutex
	server   *embeddedpostgres.EmbeddedPostgres
	adminURL string
	runDir   string
	counter  atomic.Int64
)

// Main wraps a package's TestMain so the embedded server is stopped.
func Main(m *testing.M) {
	code := m.Run()
	mu.Lock()
	if server != nil {
		server.Stop()
		os.RemoveAll(runDir)
	}
	mu.Unlock()
	os.Exit(code)
}

// URL creates an empty database for this test and returns its URL.
func URL(t testing.TB) string {
	t.Helper()
	admin, err := start()
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	name := fmt.Sprintf("mist_test_%d_%d", os.Getpid(), counter.Add(1))
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	t.Cleanup(func() {
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			return
		}
		defer conn.Close(ctx)
		conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, _ := url.Parse(admin)
	u.Path = "/" + name
	return u.String()
}

func start() (string, error) {
	mu.Lock()
	defer mu.Unlock()
	if adminURL != "" {
		return adminURL, nil
	}
	if env := os.Getenv("MIST_TEST_DATABASE_URL"); env != "" {
		adminURL = env
		return adminURL, nil
	}
	port, err := freePort()
	if err != nil {
		return "", err
	}
	runDir, err = os.MkdirTemp("", "mist-pg-")
	if err != nil {
		return "", err
	}
	server = embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Version(embeddedpostgres.V17).
		Port(uint32(port)).
		RuntimePath(filepath.Join(runDir, "run")).
		DataPath(filepath.Join(runDir, "data")).
		Logger(io.Discard))
	if err := server.Start(); err != nil {
		server = nil
		return "", fmt.Errorf("start embedded postgres: %w", err)
	}
	adminURL = fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/postgres?sslmode=disable", port)
	return adminURL, nil
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
