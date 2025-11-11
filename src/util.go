package main

import (
	"fmt"
	"os"
	"time"
)

const (
	StreamName          = "jobs:stream"
	ConsumerGroup       = "workers"
	JobEventStream		= "jobs:events"
	SupervisorStatusKey = "supervisors:status"
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
	ID           	 string                 `json:"id"`
	Type         	 string                 `json:"type"`
	Payload      	 map[string]interface{} `json:"payload"`
	Retries      	 int                    `json:"retries"`
	Created      	 time.Time              `json:"created"`
	RequiredGPU  	 string                 `json:"required_gpu,omitempty"`
	JobState     	 JobState               `json:"job_state"`
	ConsumerID	     *string				`json:"consumer_id,omitempty"`
	TimeAssigned     *time.Time				`json:"time_assigned,omitempty"`			
	TimeStarted		 *time.Time				`json:"time_started,omitempty"` 
	TimeCompleted	 *time.Time				`json:"time_completed,omitempty"`
	Result		     map[string]interface{}	`json:"result,omitempty"`
	Error		     *string				`json:"error,omitempty"`
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
