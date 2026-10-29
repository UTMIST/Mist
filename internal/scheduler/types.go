package scheduler

import "time"

// State is a job's position in the state machine (scheduler.md#job-states).
type State string

const (
	StatePending   State = "PENDING"
	StateLaunching State = "LAUNCHING"
	StateRunning   State = "RUNNING"
	StateStopping  State = "STOPPING"
	StateCleaning  State = "CLEANING" // process ended; allocation held until cleanup receipt

	StateCompleted    State = "COMPLETED"
	StateFailed       State = "FAILED"
	StateOutOfMemory  State = "OUT_OF_MEMORY"
	StateLaunchFailed State = "LAUNCH_FAILED"
	StateCancelled    State = "CANCELLED"
	StateTimeout      State = "TIMEOUT"
	StateNodeFail     State = "NODE_FAIL" // may still hold quarantined capacity
)

// Terminal reports whether no further transition is possible. A terminal
// job can still hold a quarantined allocation (NODE_FAIL).
func (s State) Terminal() bool {
	switch s {
	case StatePending, StateLaunching, StateRunning, StateStopping, StateCleaning:
		return false
	}
	return true
}

// PendingReason explains why a PENDING job is not running. A pending job
// without one is a bug.
type PendingReason string

const (
	PendingPriority        PendingReason = "PRIORITY"
	PendingResources       PendingReason = "RESOURCES"
	PendingUserLimit       PendingReason = "USER_LIMIT"
	PendingTeamLimit       PendingReason = "TEAM_LIMIT"
	PendingQOSLimit        PendingReason = "QOS_LIMIT"
	PendingPartitionDown   PendingReason = "PARTITION_DOWN"
	PendingProvisioning    PendingReason = "PROVISIONING"
	PendingAuthUnavailable PendingReason = "AUTH_UNAVAILABLE"
	PendingHeld            PendingReason = "HELD"
)

// StopCause is why Mist asked a job to stop. The first committed cause wins,
// unless a finish result was committed first.
type StopCause string

const (
	StopNone          StopCause = ""
	StopCancel        StopCause = "cancel"
	StopRevoke        StopCause = "revoke"
	StopDeadline      StopCause = "deadline"       // work walltime reached -> TIMEOUT
	StopLaunchTimeout StopCause = "launch_timeout" // never started -> LAUNCH_FAILED
)

// OpKind is an external call the scheduler makes to an executor.
type OpKind string

const (
	OpLaunch    OpKind = "launch"
	OpTerminate OpKind = "terminate"
)

// OpStatus is how far an operation intent got.
type OpStatus string

const (
	// OpPending: committed, call not yet sent.
	OpPending OpStatus = "pending"
	// OpInFlight: call sent, no answer recorded. After a crash or timeout
	// this is ambiguous: reconcile by observation, never assume it failed.
	OpInFlight OpStatus = "in_flight"
	OpDone     OpStatus = "done"
	// OpFailed: the executor gave a definitive refusal.
	OpFailed OpStatus = "failed"
)

// OpIntent is a durable record, committed before an executor call, of what
// the scheduler is about to ask for. A new leader resolves every unfinished
// intent before admitting more work on that node. Retries reuse the same
// ExecutionID and SpecHash; they never create a second execution.
type OpIntent struct {
	ID          int64
	JobID       int64
	ExecutionID string
	SpecHash    string
	Op          OpKind
	Status      OpStatus
	Attempts    int
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
