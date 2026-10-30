//go:build !mistdev

package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/UTMIST/Mist/internal/db/dbtest"
)

// M1 step 6 check: a production build refuses the development fixture,
// and its API rejects the development principal header.
func TestProductionRejectsDevIdentity(t *testing.T) {
	url := dbtest.URL(t)
	err := run(context.Background(), []string{"api", "-database-url", url, "-dev-fixture"}, io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "development build") {
		t.Fatalf("-dev-fixture in a production build: %v", err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, []string{"api", "-database-url", url, "-listen", addr}, io.Discard, io.Discard)
	}()
	t.Cleanup(func() { cancel(); <-done })

	waitFor(t, "api to listen", func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err == nil {
			resp.Body.Close()
		}
		return err == nil && resp.StatusCode == 200
	})
	req, _ := http.NewRequest("GET", "http://"+addr+"/v1/jobs", nil)
	req.Header.Set("X-Mist-Dev-User", "root-admin")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dev header on a production API: %s", resp.Status)
	}
}
