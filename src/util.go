package main

import (
	"fmt"
	"os"
	"time"
)

const (
	StreamName          = "jobs:stream"
	ConsumerGroup       = "workers"
	MaxRetries          = 3
	RetryDelay          = 5 * time.Second
	SupervisorRegistry  = "supervisors:registry"
	SupervisorStatusKey = "supervisors:status"
	HeartbeatInterval   = 10 * time.Second
	HeartbeatTimeout    = 30 * time.Second
)

type Job struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	Retries     int                    `json:"retries"`
	Created     time.Time              `json:"created"`
	RequiredGPU string                 `json:"gpu"`
}

type SupervisorStatus struct {
	ConsumerID string    `json:"consumer_id"`
	GPUType    string    `json:"gpu_type"`
	Status     string    `json:"status"` // "active", "inactive", "failed"
	LastSeen   time.Time `json:"last_seen"`
	StartedAt  time.Time `json:"started_at"`
}

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), os.Getpid())
}
