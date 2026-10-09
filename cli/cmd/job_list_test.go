package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJobListFiltersCompleted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jobs":[{"id":"running-id","name":"work","job_state":"InProgress","accelerator":"nvidia"},{"id":"done-id","name":"old","job_state":"Success","accelerator":"cpu"}]}`))
	}))
	defer server.Close()
	ctx := &AppContext{APIBaseURL: server.URL}
	out := CaptureOutput(func() {
		if err := (&ListCmd{}).Run(ctx); err != nil {
			t.Error(err)
		}
	})
	if !contains(out, "running-id") || contains(out, "done-id") {
		t.Fatal(out)
	}
	out = CaptureOutput(func() {
		if err := (&ListCmd{All: true}).Run(ctx); err != nil {
			t.Error(err)
		}
	})
	if !contains(out, "done-id") {
		t.Fatal(out)
	}
}
