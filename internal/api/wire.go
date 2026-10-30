package api

import (
	"time"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/scheduler"
)

// Wire types (api.md#job-wire-types). Times are RFC 3339 UTC, durations
// integer seconds, sizes integer bytes.

// SubmitResources is the requested size. Omitted fields take partition
// defaults. cpus is a whole-core count.
type SubmitResources struct {
	CPUs         *int64 `json:"cpus,omitempty"`
	MemoryBytes  *int64 `json:"memory_bytes,omitempty"`
	GPUs         *int64 `json:"gpus,omitempty"`
	TTBoards     *int64 `json:"tt_boards,omitempty"`
	ScratchBytes *int64 `json:"scratch_bytes,omitempty"`
}

// SubmitRequest is the body of POST /v1/jobs.
type SubmitRequest struct {
	Team               string            `json:"team,omitempty"`
	Partition          string            `json:"partition,omitempty"`
	QOS                string            `json:"qos,omitempty"`
	Name               string            `json:"name,omitempty"`
	Kind               string            `json:"kind"`
	Script             string            `json:"script,omitempty"`
	Args               []string          `json:"args,omitempty"`
	Argv               []string          `json:"argv,omitempty"`
	Workdir            string            `json:"workdir,omitempty"`
	Output             string            `json:"output,omitempty"`
	Env                map[string]string `json:"env,omitempty"`
	Resources          SubmitResources   `json:"resources"`
	TimeLimitSeconds   *int64            `json:"time_limit_seconds,omitempty"`
	SignalGraceSeconds *int64            `json:"signal_grace_seconds,omitempty"`
	Runtime            string            `json:"runtime,omitempty"`
}

// Resources is a job's resolved size.
type Resources struct {
	CPUs         int64 `json:"cpus"`
	MemoryBytes  int64 `json:"memory_bytes"`
	GPUs         int64 `json:"gpus"`
	TTBoards     int64 `json:"tt_boards"`
	ScratchBytes int64 `json:"scratch_bytes"`
}

func wireResources(r executor.Resources) Resources {
	return Resources{CPUs: r.MilliCPU / 1000, MemoryBytes: r.MemoryBytes, GPUs: r.GPUs,
		TTBoards: r.TTBoards, ScratchBytes: r.ScratchBytes}
}

// Job is the job object, for callers allowed to view the job.
type Job struct {
	ID                      int64      `json:"id"`
	Name                    string     `json:"name"`
	User                    string     `json:"user"`
	Team                    string     `json:"team"`
	Partition               string     `json:"partition"`
	QOS                     string     `json:"qos"`
	Kind                    string     `json:"kind"`
	State                   string     `json:"state"`
	PendingReason           *string    `json:"pending_reason"`
	PendingDetail           *string    `json:"pending_detail"`
	ExitReason              *string    `json:"exit_reason"`
	ExitCode                *int       `json:"exit_code"`
	ExitDetail              *string    `json:"exit_detail"`
	Resources               Resources  `json:"resources"`
	TimeLimitSeconds        int64      `json:"time_limit_seconds"`
	SignalGraceSeconds      int64      `json:"signal_grace_seconds"`
	Node                    *string    `json:"node"`
	Workdir                 string     `json:"workdir"`
	OutputPath              string     `json:"output_path"`
	OutputIncomplete        bool       `json:"output_incomplete"`
	SubmittedAt             time.Time  `json:"submitted_at"`
	StartedAt               *time.Time `json:"started_at"`
	EndedAt                 *time.Time `json:"ended_at"`
	CleanupPending          bool       `json:"cleanup_pending"`
	CleanupDetail           *string    `json:"cleanup_detail"`
	AuthorizationGeneration *int64     `json:"authorization_generation"`
	CanCancel               bool       `json:"can_cancel"`
	CanAttach               bool       `json:"can_attach"`

	// GET /v1/jobs/{id} only.
	Events []Event           `json:"events,omitempty"`
	Script *string           `json:"script,omitempty"` // ?script=1
	Args   []string          `json:"args,omitempty"`
	Argv   []string          `json:"argv,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
}

// Event is one state change in a job's history.
type Event struct {
	At     time.Time `json:"at"`
	From   *string   `json:"from"`
	To     string    `json:"to"`
	Reason string    `json:"reason"`
	Actor  string    `json:"actor"`
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func wireJob(j *scheduler.JobInfo, canCancel bool) Job {
	out := Job{
		ID: j.ID, Name: j.Name, User: j.Username, Team: j.Team, Partition: j.Partition, QOS: j.QOS,
		Kind: string(j.Kind), State: string(j.State), PendingReason: j.PendingReason,
		ExitReason: j.ExitReason, ExitCode: j.ExitCode, ExitDetail: nonEmpty(j.ExitDetail),
		Resources:          wireResources(j.Resources),
		TimeLimitSeconds:   int64(j.TimeLimit / time.Second),
		SignalGraceSeconds: int64(j.SignalGrace / time.Second),
		Node:               j.Node, Workdir: j.Workdir, OutputPath: j.OutputPath,
		OutputIncomplete: j.OutputIncomplete, SubmittedAt: j.SubmittedAt.UTC(),
		StartedAt: utc(j.StartedAt), EndedAt: utc(j.EndedAt),
		CleanupPending: j.CleanupPending, CleanupDetail: nonEmpty(j.CleanupDetail),
		AuthorizationGeneration: j.AuthGeneration,
		CanCancel:               canCancel,
	}
	if j.State == scheduler.StatePending {
		out.PendingDetail = nonEmpty(j.PendingDetail)
	}
	return out
}

// QueueEntry is the allowlisted summary of a queued or running job.
// User, team and name are filled only for jobs the caller may view;
// workdir and output path are never shown here.
type QueueEntry struct {
	ID            int64     `json:"id"`
	Partition     string    `json:"partition"`
	QOS           string    `json:"qos"`
	State         string    `json:"state"`
	Resources     Resources `json:"resources"`
	AgeSeconds    int64     `json:"age_seconds"`
	PendingReason *string   `json:"pending_reason"`
	User          *string   `json:"user"`
	Team          *string   `json:"team"`
	Name          *string   `json:"name"`
	Workdir       *string   `json:"workdir"`
	OutputPath    *string   `json:"output_path"`
}

// Partition is one entry of GET /v1/partitions. CPU and memory defaults
// and maximums are per accelerator when PerAccelerator is set.
type Partition struct {
	Name                      string    `json:"name"`
	State                     string    `json:"state"`
	Default                   bool      `json:"default"`
	Accelerator               string    `json:"accelerator"`
	MinAccelerators           int64     `json:"min_accelerators"`
	MaxAccelerators           int64     `json:"max_accelerators"`
	PerAccelerator            bool      `json:"per_accelerator"`
	DefaultCPUs               int64     `json:"default_cpus"`
	MaxCPUs                   int64     `json:"max_cpus"`
	DefaultMemoryBytes        int64     `json:"default_memory_bytes"`
	MaxMemoryBytes            int64     `json:"max_memory_bytes"`
	DefaultScratchBytes       int64     `json:"default_scratch_bytes"`
	MaxScratchBytes           int64     `json:"max_scratch_bytes"`
	DefaultTimeLimitSeconds   int64     `json:"default_time_limit_seconds"`
	MaxTimeLimitSeconds       int64     `json:"max_time_limit_seconds"`
	DefaultSignalGraceSeconds int64     `json:"default_signal_grace_seconds"`
	CapCPUs                   *int64    `json:"cap_cpus"`
	CapMemoryBytes            *int64    `json:"cap_memory_bytes"`
	Free                      Resources `json:"free"` // on schedulable nodes
	Nodes                     []Node    `json:"nodes"`
	Limits                    []Limit   `json:"limits"`
}

// Node is one node of a partition.
type Node struct {
	Name        string    `json:"name"`
	State       string    `json:"state"` // up, drain, down
	Healthy     bool      `json:"healthy"`
	Quarantined bool      `json:"quarantined"`
	Total       Resources `json:"total"`
	Free        Resources `json:"free"`
}

// Limit is one limit that applies to the caller in a partition.
type Limit struct {
	Scope    string `json:"scope"`
	Subject  string `json:"subject"`
	QOS      string `json:"qos"`
	Resource string `json:"resource"`
	Max      int64  `json:"max"`
}

// List is the shape of every list reply.
type List[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
