package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestJobSubmitUsesRealAPI(t *testing.T) {
	script := filepath.Join(t.TempDir(), "train.py")
	_ = os.WriteFile(script, []byte("print('train')"), 0600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/jobs" {
			t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["accelerator"] != "tenstorrent" || body["device_count"] != float64(2) || body["script"] != "print('train')" {
			t.Errorf("wrong submission: %+v", body)
		}
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"job_id":"real-id"}`))
	}))
	defer server.Close()
	var err error
	output := CaptureOutput(func() {
		err = (&JobSubmitCmd{Script: script, Compute: "TT", Devices: 2}).Run(&AppContext{APIBaseURL: server.URL})
	})
	if err != nil || !contains(output, "real-id") || contains(output, "Are you sure") {
		t.Fatalf("%v: %s", err, output)
	}
}
func TestJobSubmitRejectsMissingFile(t *testing.T) {
	if err := (&JobSubmitCmd{Script: "/does-not-exist.py", Compute: "CPU"}).Run(&AppContext{}); err == nil {
		t.Fatal("missing file accepted")
	}
}
