// Package executor defines how the scheduler asks for work to run, and how
// it learns what happened. The contract is docs/agents/architecture/executors.md.
//
// The scheduler and API import only this package. Kubernetes and systemd
// types stay inside the implementation subpackages.
package executor

import (
	"context"
	"errors"
	"io"
	"time"
)

// Executor runs admitted jobs on the nodes it manages. Only the scheduler
// calls Launch and Terminate; the API uses Logs and Attach.
type Executor interface {
	Name() string

	// Nodes reports schedulable capacity and health for nodes this executor
	// manages. Capacity is net of the executor's own host reservations.
	Nodes(ctx context.Context) ([]Node, error)

	// Launch ensures the one execution for spec.ExecutionID exists on
	// spec.Node. Repeating it with the same ID and SpecHash is safe. A
	// different SpecHash for a known ID returns ErrSpecConflict. It never
	// recreates an execution that has a recorded native object or any
	// evidence of having started; it returns ErrExecutionStarted instead.
	Launch(ctx context.Context, spec LaunchSpec) error

	// Terminate sends SIGTERM, then SIGKILL after grace. Idempotent.
	Terminate(ctx context.Context, jobID int64, grace time.Duration, reason string) error

	// Attach connects a terminal to a running interactive job and returns
	// when the session ends. Ending the session detaches; it never cancels.
	Attach(ctx context.Context, jobID int64, term Terminal) error

	// Logs streams the job's live output, as written to the .out file.
	Logs(ctx context.Context, jobID int64, follow bool) (io.ReadCloser, error)

	// List returns every execution this executor knows about, merged with
	// durable receipts. An incomplete observation is an error wrapping
	// ErrIncompleteObservation, never a shorter successful list.
	List(ctx context.Context) ([]Execution, error)

	// Events is an at-least-once notification stream. Consumers persist and
	// dedupe by EventID; List and receipts recover missed notifications.
	Events() <-chan Event
}

var (
	// ErrSpecConflict: the ExecutionID exists with a different SpecHash.
	ErrSpecConflict = errors.New("executor: execution exists with a different spec")
	// ErrExecutionStarted: the execution may have run already, so it cannot
	// be created again. The caller reports loss rather than replaying.
	ErrExecutionStarted = errors.New("executor: execution already started or recorded")
	// ErrIncompleteObservation: the executor could not see everything, so
	// absence from its answer proves nothing.
	ErrIncompleteObservation = errors.New("executor: observation incomplete")
	// ErrNotFound: no such execution, and the executor is sure of it.
	ErrNotFound = errors.New("executor: execution not found")
)

// Kind is what the job runs.
type Kind string

const (
	KindBatch       Kind = "batch"       // bash Script Args...
	KindCommand     Kind = "command"     // Argv, without a shell
	KindInteractive Kind = "interactive" // persistent PTY holder (M2)
)

// Resources is an amount of node capacity. Accelerators are whole devices.
type Resources struct {
	MilliCPU       int64 `json:"milli_cpu"`
	MemoryBytes    int64 `json:"memory_bytes"`
	GPUs           int64 `json:"gpus"`
	TTBoards       int64 `json:"tt_boards"`
	ScratchBytes   int64 `json:"scratch_bytes"`
	HugepagesBytes int64 `json:"hugepages_bytes"` // M4
}

// Add returns r + o.
func (r Resources) Add(o Resources) Resources {
	return Resources{
		MilliCPU:       r.MilliCPU + o.MilliCPU,
		MemoryBytes:    r.MemoryBytes + o.MemoryBytes,
		GPUs:           r.GPUs + o.GPUs,
		TTBoards:       r.TTBoards + o.TTBoards,
		ScratchBytes:   r.ScratchBytes + o.ScratchBytes,
		HugepagesBytes: r.HugepagesBytes + o.HugepagesBytes,
	}
}

// Sub returns r - o. Results may be negative; check with Fits.
func (r Resources) Sub(o Resources) Resources {
	return r.Add(Resources{
		MilliCPU:       -o.MilliCPU,
		MemoryBytes:    -o.MemoryBytes,
		GPUs:           -o.GPUs,
		TTBoards:       -o.TTBoards,
		ScratchBytes:   -o.ScratchBytes,
		HugepagesBytes: -o.HugepagesBytes,
	})
}

// Fits reports whether want fits in r on every dimension.
func (r Resources) Fits(want Resources) bool {
	return want.MilliCPU <= r.MilliCPU &&
		want.MemoryBytes <= r.MemoryBytes &&
		want.GPUs <= r.GPUs &&
		want.TTBoards <= r.TTBoards &&
		want.ScratchBytes <= r.ScratchBytes &&
		want.HugepagesBytes <= r.HugepagesBytes
}

// User is the job's Linux identity (D4).
type User struct {
	Name        string `json:"name"`
	UID         int    `json:"uid"`
	PersonalGID int    `json:"personal_gid"`
}

// Mount is one resolved durable mount.
type Mount struct {
	VolumeID string `json:"volume_id"`
	Source   string `json:"source"` // path on the volume
	Target   string `json:"target"` // absolute path inside the job
	ReadOnly bool   `json:"read_only"`
}

// Node is one node as the executor sees it.
type Node struct {
	Name       string
	Partitions []string
	Capacity   Resources // allocatable, net of executor/system reservations
	Reserved   Resources // what the executor already held back, for display only
	Devices    []Device  // optional per-device inventory
	Healthy    bool
	Detail     string // why it is unhealthy, shown to admins
}

// Device is one accelerator.
type Device struct {
	ID      string // GPU UUID or TT board ID
	Kind    string // "gpu" | "tt"
	Healthy bool
}

// EventKind is what an Event reports.
type EventKind string

const (
	EventStarted      EventKind = "started"       // work began; At is the actual start
	EventFinished     EventKind = "finished"      // process ended; cleanup not yet proven
	EventExited       EventKind = "exited"        // cleanup confirmed; resources may be released
	EventLaunchFailed EventKind = "launch_failed" // definitive failure before work started
	EventLost         EventKind = "lost"          // execution vanished or node unreachable
)

// ExitReason explains a Finished, LaunchFailed or Lost event.
type ExitReason string

const (
	ExitNormal          ExitReason = "normal" // see ExitCode
	ExitOutOfMemory     ExitReason = "out_of_memory"
	ExitScratchExceeded ExitReason = "scratch_exceeded"
	ExitDeadline        ExitReason = "deadline" // executor backstop fired
	ExitSignaled        ExitReason = "signaled" // Terminate or external signal
	ExitLaunchTimeout   ExitReason = "launch_timeout"
	ExitLaunchError     ExitReason = "launch_error" // image, mount, path or admission error
	ExitEvicted         ExitReason = "evicted"
	ExitNodeLost        ExitReason = "node_lost"
)

// Event is one at-least-once notification.
type Event struct {
	EventID     string // stable across redelivery
	JobID       int64
	ExecutionID string
	Kind        EventKind
	Node        string
	ExitCode    *int
	Reason      ExitReason
	Detail      string // human-readable, shown to users
	At          time.Time
}

// Phase is an execution's observed lifecycle position.
type Phase string

const (
	PhaseCreating Phase = "creating" // create intent recorded, outcome unknown
	PhasePending  Phase = "pending"  // object exists, work not started
	PhaseRunning  Phase = "running"
	PhaseFinished Phase = "finished" // process ended, cleanup in progress
	PhaseCleaned  Phase = "cleaned"  // cleanup receipt recorded
	PhaseLost     Phase = "lost"
)

// CleanupStatus tracks node-side cleanup separately from process outcome.
type CleanupStatus string

const (
	CleanupNone      CleanupStatus = ""          // not started
	CleanupPending   CleanupStatus = "pending"   // deletion requested, no node receipt
	CleanupConfirmed CleanupStatus = "confirmed" // node receipt matches this execution
	CleanupFailed    CleanupStatus = "failed"    // keep capacity reserved and alert
)

// Result is a finished process's outcome.
type Result struct {
	ExitCode         *int       `json:"exit_code"`
	Reason           ExitReason `json:"reason"`
	Detail           string     `json:"detail"`
	PeakMemoryBytes  int64      `json:"peak_memory_bytes"`
	OutputIncomplete bool       `json:"output_incomplete"`
}

// Execution is what an executor knows about one execution, from live
// objects merged with durable receipts.
type Execution struct {
	JobID           int64
	ExecutionID     string
	SpecHash        string
	Node            string
	Phase           Phase
	ObjectUID       string // native object identity (Pod UID, unit invocation ID)
	StartedAt       *time.Time
	FinishedAt      *time.Time
	Result          *Result
	Cleanup         CleanupStatus
	CleanupEvidence string
}

// ReceiptKind names a durable execution fact.
type ReceiptKind string

const (
	ReceiptCreateIntent ReceiptKind = "create_intent" // written before the native create call
	ReceiptCreated      ReceiptKind = "created"       // native object UID observed
	ReceiptStarted      ReceiptKind = "started"
	ReceiptFinished     ReceiptKind = "finished" // with Result and log tail
	ReceiptCleaned      ReceiptKind = "cleaned"  // node-confirmed cleanup
	ReceiptTombstone    ReceiptKind = "tombstone"
)

// Receipt is an append-only durable fact about an execution, written by the
// executor package before it acts on or reports that fact. Receipts outlive
// native objects, so a deleted Pod never erases execution history.
type Receipt struct {
	ExecutionID string
	Kind        ReceiptKind
	ObjectUID   string
	At          time.Time
	Result      *Result // ReceiptFinished only
	Evidence    string  // ReceiptCleaned: what the node verified
}

// Terminal is the client side of an attached interactive session (M2).
type Terminal interface {
	io.Reader // keyboard bytes
	io.Writer // screen bytes
	// Controls delivers resize and allowed-signal requests. It closes when
	// the client goes away, which detaches.
	Controls() <-chan Control
}

// Control is a terminal control message.
type Control struct {
	Cols, Rows int    // resize when nonzero
	Signal     string // "SIGINT", "SIGQUIT", "SIGTSTP" or ""
}
