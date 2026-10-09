package main

import "time"

type JobState string

const (
	JobStateScheduled  JobState = "Scheduled"
	JobStateInProgress JobState = "InProgress"
	JobStateSuccess    JobState = "Success"
	JobStateFailure    JobState = "Failure"
	JobStateCancelled  JobState = "Cancelled"
)

type Job struct {
	ID            string                 `json:"id"`
	Type          string                 `json:"type"`
	Payload       map[string]interface{} `json:"payload"`
	Retries       int                    `json:"retries"`
	Created       time.Time              `json:"created"`
	RequiredGPU   string                 `json:"required_gpu,omitempty"`
	JobState      JobState               `json:"job_state"`
	ConsumerID    *string                `json:"consumer_id,omitempty"`
	TimeAssigned  *time.Time             `json:"time_assigned,omitempty"`
	TimeStarted   *time.Time             `json:"time_started,omitempty"`
	TimeCompleted *time.Time             `json:"time_completed,omitempty"`
	Result        map[string]interface{} `json:"result,omitempty"`
	Error         *string                `json:"error,omitempty"`
}

type CreateJobRequest struct {
	Type             string                 `json:"type"`
	DatasetID        string                 `json:"dataset_id,omitempty"`
	StorageScope     string                 `json:"storage_scope,omitempty"`
	TTRuntime        string                 `json:"tt_runtime,omitempty"`
	Payload          map[string]interface{} `json:"payload"`
	RequiredGPU      string                 `json:"gpu,omitempty"`
	Name             string                 `json:"name,omitempty"`
	Image            string                 `json:"image,omitempty"`
	Command          []string               `json:"command,omitempty"`
	Args             []string               `json:"args,omitempty"`
	WorkingDirectory string                 `json:"working_directory,omitempty"`
	Script           string                 `json:"script,omitempty"`
	ScriptName       string                 `json:"script_name,omitempty"`
	Env              map[string]string      `json:"env,omitempty"`
	CPU              string                 `json:"cpu,omitempty"`
	Memory           string                 `json:"memory,omitempty"`
	Accelerator      string                 `json:"accelerator,omitempty"`
	DeviceCount      int                    `json:"device_count,omitempty"`
	TimeoutSeconds   int64                  `json:"timeout_seconds,omitempty"`
}

type KubernetesJobStatus struct {
	Job
	Name                string            `json:"name"`
	Image               string            `json:"image"`
	Accelerator         string            `json:"accelerator"`
	DeviceCount         int               `json:"device_count"`
	CPU                 string            `json:"cpu"`
	Memory              string            `json:"memory"`
	Owner               string            `json:"owner"`
	Node                string            `json:"node,omitempty"`
	Pod                 string            `json:"pod,omitempty"`
	ExitCode            *int32            `json:"exit_code,omitempty"`
	Message             string            `json:"message,omitempty"`
	Devices             []AllocatedDevice `json:"devices,omitempty"`
	CheckpointDirectory string            `json:"checkpoint_directory"`
	OutputDirectory     string            `json:"output_directory"`
	WorkingDirectory    string            `json:"working_directory,omitempty"`
	TimeoutSeconds      int64             `json:"timeout_seconds"`
	DatasetID           string            `json:"dataset_id,omitempty"`
	TeamID              string            `json:"team_id,omitempty"`
	CreatorID           string            `json:"creator_id,omitempty"`
	CreatorName         string            `json:"creator_name,omitempty"`
	StorageScope        string            `json:"storage_scope,omitempty"`
	QueuePosition       int               `json:"queue_position,omitempty"`
	CanCancel           *bool             `json:"can_cancel,omitempty"`
}

type AllocatedDevice struct {
	Driver string `json:"driver"`
	Pool   string `json:"pool"`
	Device string `json:"device"`
}
