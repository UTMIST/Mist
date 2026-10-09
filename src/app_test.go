package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
)

func TestAppStartupRejectsUnavailableKubernetes(t *testing.T) {
	e := testExecutor()
	unavailable := errors.New("cluster unavailable")
	e.client.(*fake.Clientset).PrependReactor("list", "jobs", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, unavailable
	})
	app := NewKubernetesApp(e, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := app.Start(); !errors.Is(err, unavailable) {
		t.Fatalf("started gateway despite unavailable executor: %v", err)
	}
}

func TestKubernetesCompatibilityStatusUsesScopedExecutor(t *testing.T) {
	e := testExecutor()
	job, err := e.submit(context.Background(), CreateJobRequest{Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	app := NewKubernetesApp(e, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		method string
		path   string
		want   int
	}{
		{http.MethodGet, "/jobs/status?id=" + job.ID, http.StatusOK},
		{http.MethodGet, "/jobs/status?id=unknown-job", http.StatusNotFound},
		{http.MethodGet, "/jobs/status", http.StatusBadRequest},
		{http.MethodPost, "/jobs/status?id=" + job.ID, http.StatusMethodNotAllowed},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			app.httpServer.Handler.ServeHTTP(response, httptest.NewRequest(tc.method, tc.path, nil))
			if response.Code != tc.want {
				t.Fatalf("status %d, wanted %d: %s", response.Code, tc.want, response.Body.String())
			}
			if tc.want == http.StatusOK && !strings.Contains(response.Body.String(), job.ID) {
				t.Fatal("compatibility endpoint did not return the managed Kubernetes job")
			}
		})
	}
	other := *e
	other.owner = "other-member"
	otherApp := NewKubernetesApp(&other, app.log)
	response := httptest.NewRecorder()
	otherApp.httpServer.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/jobs/status?id="+job.ID, nil))
	if response.Code != http.StatusNotFound {
		t.Fatalf("compatibility route leaked another owner's job: %d", response.Code)
	}
}
