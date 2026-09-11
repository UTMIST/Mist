package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLocalJobValidation(t *testing.T) {
	app := NewApp("localhost:1", "NVIDIA", slog.Default())
	defer app.redisClient.Close()
	defer app.scheduler.Close()
	defer app.supervisor.Stop()
	for _, body := range []string{
		`{"type":"cuda_benchmark","gpu":"AMD"}`,
		`{"type":"shell","gpu":"NVIDIA"}`,
		`{"type":"cuda_benchmark","gpu":"NVIDIA","payload":{"matrix_size":100000}}`,
		`{"type":"cuda_benchmark","gpu":"NVIDIA","payload":{"matrix_size":2048.5}}`,
		`{"type":"cuda_benchmark","gpu":"NVIDIA","payload":{"matrix_size":"2048"}}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		app.httpServer.Handler.ServeHTTP(w, r)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400 for %s; got %d: %s", body, w.Code, w.Body.String())
		}
	}
	r := httptest.NewRequest(http.MethodPost, "/api/jobs", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(w, r)
	if w.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected JSON content-type rejection; got %d", w.Code)
	}
}

func TestBenchmarkSize(t *testing.T) {
	for _, size := range []float64{1024, 2048, 4096} {
		got, err := benchmarkSize(map[string]interface{}{"matrix_size": size})
		if err != nil || got != int(size) {
			t.Fatalf("size %v: %d, %v", size, got, err)
		}
	}
	if got, err := benchmarkSize(nil); got != 2048 || err != nil {
		t.Fatalf("default: %d, %v", got, err)
	}
}

func TestJobMetadataResult(t *testing.T) {
	job, err := jobFromMetadata("job_test", map[string]string{
		"type": "cuda_benchmark", "created": "2026-09-11T20:00:00Z", "job_state": "Success",
		"payload": `{"matrix_size":2048}`, "result": `{"gpu_ms":0.8,"verified":true}`,
		"logs": "real output", "completed": "2026-09-11T20:00:05Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(job)
	if job.Result["verified"] != true || job.Logs != "real output" || job.TimeCompleted == nil {
		t.Fatalf("result not restored: %s", encoded)
	}
}
