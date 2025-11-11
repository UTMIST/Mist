package main

import (
	"fmt"
	"os"
	"time"
)

const (
	StreamName          = "jobs:stream"
	ConsumerGroup       = "workers"
	SupervisorStatusKey = "supervisors:status"
	JobStatusKey        = "jobs:status"
	MaxRetries          = 3
	RetryDelay          = 5 * time.Second
)

type JobState string

const (
	JobStateScheduled  JobState = "Scheduled"
	JobStateInProgress JobState = "InProgress"
	JobStateSuccess    JobState = "Success"
	JobStateError      JobState = "Error"
	JobStateFailure    JobState = "Failure"
)

type Job struct {
	ID          string                 `json:"id"`
	Type        string                 `json:"type"`
	Payload     map[string]interface{} `json:"payload"`
	Retries     int                    `json:"retries"`
	Created     time.Time              `json:"created"`
	RequiredGPU string                 `json:"gpu"`
	JobState    JobState               `json:"job_state"`
}

type SupervisorState string

const (
	SupervisorStateActive   SupervisorState = "active"
	SupervisorStateInactive SupervisorState = "inactive"
	SupervisorStateFailed   SupervisorState = "failed"
)

type SupervisorStatus struct {
	ConsumerID string          `json:"consumer_id"`
	GPUType    string          `json:"gpu_type"`
	Status     SupervisorState `json:"status"`
	LastSeen   time.Time       `json:"last_seen"`
	StartedAt  time.Time       `json:"started_at"`
}

func generateJobID() string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), os.Getpid())
}
