package main

import (
	"fmt"
	"os"
	"time"
)

const (
	StreamName    = "jobs:stream"
	ConsumerGroup = "workers"
	MaxRetries    = 3
	RetryDelay    = 5 * time.Second
)

type Job struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	Retries     int                    `json:"retries"`
	Created     time.Time              `json:"created"`
	RequiredGPU string                 `json:"gpu"`
}

func NewJob(jobType string, payload map[string]interface{}) Job {
	return Job{
		ID:      generateJobID(),
		Type:    jobType,
		Payload: payload,
		Retries: 0,
		Created: time.Now(),
	}
}

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), os.Getpid())
}
