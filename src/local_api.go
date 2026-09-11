package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func jobFromMetadata(id string, fields map[string]string) (*Job, error) {
	created, err := time.Parse(time.RFC3339Nano, fields["created"])
	if err != nil {
		return nil, err
	}
	job := &Job{ID: id, Type: fields["type"], Created: created,
		RequiredGPU: fields["required_gpu"], JobState: JobState(fields["job_state"]), Logs: fields["logs"]}
	job.Retries, _ = strconv.Atoi(fields["retries"])
	for field, target := range map[string]*map[string]interface{}{"payload": &job.Payload, "result": &job.Result} {
		if fields[field] != "" {
			if err := json.Unmarshal([]byte(fields[field]), target); err != nil {
				return nil, err
			}
		}
	}
	if value := fields["error"]; value != "" {
		job.Error = &value
	}
	if value := fields["consumer_id"]; value != "" {
		job.ConsumerID = &value
	}
	if value, err := time.Parse(time.RFC3339Nano, fields["started"]); err == nil {
		job.TimeStarted = &value
	}
	if value, err := time.Parse(time.RFC3339Nano, fields["completed"]); err == nil {
		job.TimeCompleted = &value
	}
	return job, nil
}

func (a *App) listJobs(w http.ResponseWriter, r *http.Request) {
	ids, err := a.redisClient.ZRevRange(r.Context(), "jobs:recent", 0, 49).Result()
	if err != nil {
		http.Error(w, "Cannot read job history", http.StatusServiceUnavailable)
		return
	}
	jobs := make([]*Job, 0, len(ids))
	for _, id := range ids {
		job, err := a.statusRegistry.GetJobStatus(id)
		if err != nil {
			http.Error(w, "Cannot read job status", http.StatusServiceUnavailable)
			return
		}
		jobs = append(jobs, job)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(map[string]interface{}{"jobs": jobs})
}

func (a *App) getHardware(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.gpuMu.Lock()
	defer a.gpuMu.Unlock()
	if a.gpuInfo == nil || time.Since(a.gpuChecked) > 10*time.Second {
		info := map[string]interface{}{"available": false, "worker_type": a.supervisor.gpuType}
		var output string
		var err error
		if a.supervisor.gpuType != "NVIDIA" {
			err = fmt.Errorf("No NVIDIA worker is configured")
		} else {
			ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
			output, err = a.supervisor.runGPUCommand(ctx, []string{
				"nvidia-smi", "--query-gpu=name,memory.total,memory.used,utilization.gpu,driver_version", "--format=csv,noheader,nounits",
			})
			cancel()
		}
		if err == nil {
			var rows [][]string
			rows, err = csv.NewReader(strings.NewReader(strings.TrimSpace(output))).ReadAll()
			if err == nil && len(rows) > 0 && len(rows[0]) == 5 {
				row := rows[0]
				info["available"] = true
				info["name"] = strings.TrimSpace(row[0])
				for index, name := range []string{"memory_total_mib", "memory_used_mib", "utilization_percent"} {
					if value, parseErr := strconv.ParseFloat(strings.TrimSpace(row[index+1]), 64); parseErr == nil {
						info[name] = value
					}
				}
				info["driver_version"] = strings.TrimSpace(row[4])
			} else if err == nil {
				err = fmt.Errorf("No NVIDIA GPU was detected")
			}
		}
		if err != nil {
			info["error"] = err.Error()
		}
		a.gpuInfo, a.gpuChecked = info, time.Now()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(a.gpuInfo)
}

// Serve the built SPA and let client-side routes fall back to index.html.
func spaHandler(directory string) http.Handler {
	files := http.FileServer(http.Dir(directory))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/jobs", http.StatusFound)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if info, err := os.Stat(filepath.Join(directory, clean)); err == nil && !info.IsDir() {
			files.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, filepath.Join(directory, "index.html"))
	})
}
