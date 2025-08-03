package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

const (
	StreamName    = "jobs:stream"
	ConsumerGroup = "workers"
	MaxRetries    = 3
	RetryDelay    = 5 * time.Second
)

type APIResponse struct {
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

type Job struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	Retries     int                    `json:"retries"`
	Created     time.Time              `json:"created"`
	RequiredGPU string                 `json:"gpu"`
}

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), os.Getpid())
}

func (a *App) jsonResponse(w http.ResponseWriter, statusCode int, response APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(response)
}
