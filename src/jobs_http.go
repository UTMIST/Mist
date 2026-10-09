package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (a *App) kubernetesJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req CreateJobRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid submission: " + err.Error()})
			return
		}
		if err := decoder.Decode(new(interface{})); err != io.EOF {
			writeJSON(w, 400, map[string]string{"error": "request must contain one JSON object"})
			return
		}
		status, err := a.requestExecutor(r).submit(r.Context(), req)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{"job_id": status.ID, "job": status})
	case http.MethodGet:
		jobs, err := a.requestExecutor(r).list(r.Context())
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]interface{}{"jobs": jobs, "count": len(jobs)})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) kubernetesJob(w http.ResponseWriter, r *http.Request) {
	if strings.Contains(r.URL.Path, "/files") {
		a.jobFiles(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "logs" && r.Method == http.MethodGet {
		logs, err := a.requestExecutor(r).logs(r.Context(), id)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"job_id": id, "logs": logs})
		return
	}
	if (len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost) || (len(parts) == 1 && r.Method == http.MethodDelete) {
		status, err := a.requestExecutor(r).cancel(r.Context(), id)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		status, err := a.requestExecutor(r).get(r.Context(), id)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	writeJSON(w, 405, map[string]string{"error": "method not allowed"})
}

func (a *App) getJobStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := r.URL.Query().Get("id")
	if jobID == "" {
		// Try to get from path if query param not provided
		path := strings.TrimPrefix(r.URL.Path, "/jobs/status/")
		if path != "" && path != "/jobs/status" {
			jobID = path
		}
	}

	if jobID == "" {
		http.Error(w, "Job ID is required", http.StatusBadRequest)
		return
	}
	status, err := a.requestExecutor(r).get(r.Context(), jobID)
	if err != nil {
		executorError(w, err)
		return
	}
	writeJSON(w, 200, status)
}
