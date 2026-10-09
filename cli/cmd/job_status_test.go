package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJobStatusPropagatesFailureAndExitCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":"failed-id","name":"failed","job_state":"Failure","exit_code":17}`))
	}))
	defer server.Close()
	out := CaptureOutput(func() {
		if err := (&JobStatusCmd{ID: "failed-id"}).Run(&AppContext{APIBaseURL: server.URL}); err != nil {
			t.Error(err)
		}
	})
	if !contains(out, "Failure") || !contains(out, "Exit code: 17") {
		t.Fatal(out)
	}
}
func TestJobStatusDoesNotInventSuccessForMissingJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"job not found"}`))
	}))
	defer server.Close()
	if err := (&JobStatusCmd{ID: "missing"}).Run(&AppContext{APIBaseURL: server.URL}); err == nil || !contains(err.Error(), "job not found") {
		t.Fatal(err)
	}
}
