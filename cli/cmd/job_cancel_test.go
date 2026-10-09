package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestJobCancelCallsAPIWithoutConfirmation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/jobs/cancel-id/cancel" {
			t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"cancel-id","job_state":"Cancelled"}`))
	}))
	defer server.Close()
	out := CaptureOutput(func() {
		if err := (&JobCancelCmd{ID: "cancel-id"}).Run(&AppContext{APIBaseURL: server.URL}); err != nil {
			t.Error(err)
		}
	})
	if !contains(out, "Cancelled") || contains(out, "Are you sure") {
		t.Fatal(out)
	}
}
