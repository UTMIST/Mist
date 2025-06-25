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

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), os.Getpid())
}
