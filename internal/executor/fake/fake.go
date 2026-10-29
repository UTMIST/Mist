// Package fake is an in-memory Executor driven by a clock, for scheduler
// tests and the simulator. Jobs "run" for scripted durations; tests inject
// the failures that the real executors must survive: lost or late create
// responses, OOM, launch failures, lost nodes, failed cleanup, observation
// outages, and lost, duplicated or reordered notifications.
//
// Like a real executor it keeps two separate things:
//
//   - durable receipts and execution records, which survive Restart and
//     are what List reports, and
//   - the at-least-once Events stream, which tests may drop, hold,
//     duplicate or reorder without losing any receipt.
//
// With clock.Fake every transition runs synchronously inside Advance, so
// tests are deterministic.
package fake

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/executor"
)

// Script is what one job does once launched.
type Script struct {
	StartDelay       time.Duration       // admission to work start (image pull, mounts)
	RunFor           time.Duration       // work duration if not stopped
	ExitCode         int                 // when RunFor elapses
	Reason           executor.ExitReason // Normal, OutOfMemory, ScratchExceeded...
	Detail           string
	OutputIncomplete bool
	LaunchError      string        // nonempty: definitive failure at StartDelay, before work starts
	StopDelay        time.Duration // SIGTERM to exit; capped at the grace period
	IgnoreSIGTERM    bool          // exit only on SIGKILL after grace
	CleanupDelay     time.Duration // finish to node-confirmed cleanup
	CleanupFails     bool          // cleanup never confirms until FixCleanup
}

// DefaultScript is used for jobs without their own script.
var DefaultScript = Script{
	StartDelay:   time.Second,
	RunFor:       time.Minute,
	Reason:       executor.ExitNormal,
	StopDelay:    time.Second,
	CleanupDelay: time.Second,
}

// LaunchFault is an injected failure of one create call.
type LaunchFault int

const (
	// FaultTimeoutBeforeCreate: the request is lost; nothing was created.
	FaultTimeoutBeforeCreate LaunchFault = iota + 1
	// FaultTimeoutAfterCreate: the object was created but the response lost.
	FaultTimeoutAfterCreate
	// FaultLateCreate: the response is lost and the create lands later
	// (after the duration passed to FailNextLaunch). A retry in between
	// still yields exactly one object, as a deterministic name would.
	FaultLateCreate
)

// ErrTimeout is returned for injected lost responses. The outcome is
// ambiguous: callers must observe, not assume failure.
var ErrTimeout = fmt.Errorf("fake: create response lost: %w", context.DeadlineExceeded)

// ErrUnavailable is returned while the fake API or node is unreachable.
var ErrUnavailable = errors.New("fake: executor API unavailable")

// Config sets up a Fake.
type Config struct {
	Clock         clock.Clock
	Nodes         []executor.Node
	Default       *Script       // nil means DefaultScript
	BackstopSlack time.Duration // beyond time limit + grace; default 60 s
}

// Fake implements executor.Executor.
type Fake struct {
	clock  clock.Clock
	slack  time.Duration
	events *delivery

	mu       sync.Mutex
	def      Script
	nodes    map[string]*executor.Node
	order    []string // node names in config order
	scripts  map[int64]Script
	faults   map[int64]fault
	execs    map[int64]*execution
	receipts map[string][]executor.Receipt
	creates  map[int64]int
	uids     int
	outage   bool
}

type fault struct {
	kind   LaunchFault
	lateBy time.Duration
}

type execution struct {
	spec          executor.LaunchSpec
	script        Script
	created       bool
	lateCreate    bool // a lost create will still land
	uid           string
	phase         executor.Phase
	startedAt     *time.Time
	finishedAt    *time.Time
	result        *executor.Result
	cleanup       executor.CleanupStatus
	evidence      string
	stopRequested string // reason, when stopped before the object existed
	terminating   bool
	timers        []clock.Timer
	frozen        bool // node lost: nothing progresses
}

// New returns a Fake. Close it to stop event delivery.
func New(cfg Config) *Fake {
	f := &Fake{
		clock:    cfg.Clock,
		slack:    cfg.BackstopSlack,
		events:   newDelivery(),
		def:      DefaultScript,
		nodes:    map[string]*executor.Node{},
		scripts:  map[int64]Script{},
		faults:   map[int64]fault{},
		execs:    map[int64]*execution{},
		receipts: map[string][]executor.Receipt{},
		creates:  map[int64]int{},
	}
	if f.clock == nil {
		f.clock = clock.Real{}
	}
	if f.slack == 0 {
		f.slack = 60 * time.Second
	}
	if cfg.Default != nil {
		f.def = *cfg.Default
	}
	for _, n := range cfg.Nodes {
		n := n
		f.nodes[n.Name] = &n
		f.order = append(f.order, n.Name)
	}
	return f
}

// Close stops event delivery.
func (f *Fake) Close() { f.events.close() }

func (f *Fake) Name() string { return "fake" }

// SetScript sets what jobID does when launched.
func (f *Fake) SetScript(jobID int64, s Script) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scripts[jobID] = s
}

// FailNextLaunch makes the next create for jobID fail with fault. lateBy
// applies to FaultLateCreate only.
func (f *Fake) FailNextLaunch(jobID int64, kind LaunchFault, lateBy time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.faults[jobID] = fault{kind, lateBy}
}

// SetOutage makes Nodes, List and Launch fail as if the API were down.
func (f *Fake) SetOutage(down bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outage = down
}

// Creates counts native objects created for jobID. Tests assert it is
// never more than one.
func (f *Fake) Creates(jobID int64) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates[jobID]
}

// Receipts returns the durable receipts for an execution.
func (f *Fake) Receipts(executionID string) []executor.Receipt {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.receipts[executionID])
}

// Restart simulates the executor process restarting: notifications not yet
// delivered are lost. Receipts and running work are unaffected.
func (f *Fake) Restart() { f.events.dropQueued() }

// HoldEvents queues notifications instead of delivering them.
func (f *Fake) HoldEvents() { f.events.hold() }

// ReleaseEvents delivers held notifications, optionally newest first.
func (f *Fake) ReleaseEvents(reversed bool) { f.events.release(reversed) }

// DropHeldEvents discards held notifications; receipts remain.
func (f *Fake) DropHeldEvents() { f.events.dropHeld() }

// DuplicateEvents makes every later notification arrive twice.
func (f *Fake) DuplicateEvents(on bool) { f.events.setDuplicate(on) }

func (f *Fake) Events() <-chan executor.Event { return f.events.ch }

func (f *Fake) Nodes(context.Context) ([]executor.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outage {
		return nil, ErrUnavailable
	}
	out := make([]executor.Node, 0, len(f.order))
	for _, name := range f.order {
		n := *f.nodes[name]
		n.Partitions = slices.Clone(n.Partitions)
		n.Devices = slices.Clone(n.Devices)
		out = append(out, n)
	}
	return out, nil
}

func (f *Fake) Launch(_ context.Context, spec executor.LaunchSpec) error {
	if err := spec.Validate(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outage {
		return ErrUnavailable
	}
	node := f.nodes[spec.Node]
	if node == nil {
		return fmt.Errorf("fake: unknown node %q", spec.Node)
	}

	e := f.execs[spec.JobID]
	if e == nil {
		script, ok := f.scripts[spec.JobID]
		if !ok {
			script = f.def
		}
		e = &execution{spec: spec, script: script, phase: executor.PhaseCreating}
		f.execs[spec.JobID] = e
		f.receipt(e, executor.ReceiptCreateIntent, nil, "")
	} else if e.spec.ExecutionID != spec.ExecutionID || e.spec.SpecHash != spec.SpecHash {
		return executor.ErrSpecConflict
	}

	switch e.phase {
	case executor.PhasePending, executor.PhaseRunning:
		return nil // already exists
	case executor.PhaseCreating:
		// No object yet: creating is safe, and a pending late create will
		// find it already exists.
	default:
		return executor.ErrExecutionStarted
	}
	if !node.Healthy {
		return ErrUnavailable
	}

	ft, faulted := f.faults[spec.JobID]
	delete(f.faults, spec.JobID)
	switch {
	case !faulted:
		f.create(e)
		return nil
	case ft.kind == FaultTimeoutBeforeCreate:
		return ErrTimeout
	case ft.kind == FaultTimeoutAfterCreate:
		f.create(e)
		return ErrTimeout
	case ft.kind == FaultLateCreate:
		e.lateCreate = true
		f.clock.AfterFunc(ft.lateBy, func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			e.lateCreate = false
			if e.phase == executor.PhaseCreating && !e.frozen {
				f.create(e)
			}
		})
		return ErrTimeout
	}
	return fmt.Errorf("fake: unknown fault %d", ft.kind)
}

// create makes the native object and schedules its lifecycle. f.mu held.
func (f *Fake) create(e *execution) {
	f.uids++
	f.creates[e.spec.JobID]++
	e.created = true
	e.uid = fmt.Sprintf("fake-uid-%d", f.uids)
	e.phase = executor.PhasePending
	f.receipt(e, executor.ReceiptCreated, nil, "")

	if e.stopRequested != "" {
		f.finish(e, executor.EventFinished, executor.Result{Reason: executor.ExitSignaled,
			Detail: "stopped before start: " + e.stopRequested})
		return
	}
	now := f.clock.Now()
	s := e.script
	switch {
	case s.LaunchError != "":
		f.after(e, s.StartDelay, func() {
			f.finish(e, executor.EventLaunchFailed, executor.Result{Reason: executor.ExitLaunchError, Detail: s.LaunchError})
		})
	case now.Add(s.StartDelay).After(e.spec.LaunchDeadline):
		f.after(e, max(e.spec.LaunchDeadline.Sub(now), 0), func() {
			f.finish(e, executor.EventLaunchFailed, executor.Result{Reason: executor.ExitLaunchTimeout,
				Detail: "work did not start before the launch deadline"})
		})
	default:
		f.after(e, s.StartDelay, func() { f.start(e) })
	}
}

func (f *Fake) start(e *execution) {
	if e.phase != executor.PhasePending {
		return
	}
	now := f.clock.Now()
	e.phase = executor.PhaseRunning
	e.startedAt = &now
	f.receipt(e, executor.ReceiptStarted, nil, "")
	f.emit(e, executor.EventStarted, nil, "", "", now)

	s := e.script
	if s.Reason == "" {
		s.Reason = executor.ExitNormal
	}
	f.after(e, s.RunFor, func() {
		f.finish(e, executor.EventFinished, executor.Result{ExitCode: ptr(s.ExitCode), Reason: s.Reason,
			Detail: s.Detail, OutputIncomplete: s.OutputIncomplete})
	})
	f.after(e, e.spec.TimeLimit+e.spec.SignalGrace+f.slack, func() {
		f.finish(e, executor.EventFinished, executor.Result{ExitCode: ptr(137), Reason: executor.ExitDeadline,
			Detail: "killed by executor backstop", OutputIncomplete: true})
	})
}

// finish records the process outcome and begins cleanup. The first outcome
// wins; later timers for the same execution are no-ops.
func (f *Fake) finish(e *execution, kind executor.EventKind, r executor.Result) {
	if e.phase != executor.PhasePending && e.phase != executor.PhaseRunning && e.phase != executor.PhaseCreating {
		return
	}
	f.stopTimers(e)
	now := f.clock.Now()
	e.phase = executor.PhaseFinished
	e.finishedAt = &now
	e.result = &r
	e.cleanup = executor.CleanupPending
	f.receipt(e, executor.ReceiptFinished, &r, "")
	f.emit(e, kind, r.ExitCode, r.Reason, r.Detail, now)

	f.after(e, e.script.CleanupDelay, func() {
		if e.script.CleanupFails {
			e.cleanup = executor.CleanupFailed
			e.evidence = "fake: cleanup reporter did not confirm"
			return
		}
		f.cleaned(e, "fake: processes gone, scratch removed, devices free")
	})
}

func (f *Fake) cleaned(e *execution, evidence string) {
	e.phase = executor.PhaseCleaned
	e.cleanup = executor.CleanupConfirmed
	e.evidence = evidence
	f.receipt(e, executor.ReceiptCleaned, nil, evidence)
	var code *int
	reason := executor.ExitNodeLost
	if e.result != nil {
		code, reason = e.result.ExitCode, e.result.Reason
	}
	f.emit(e, executor.EventExited, code, reason, evidence, f.clock.Now())
}

// FixCleanup lets a failed cleanup confirm now.
func (f *Fake) FixCleanup(jobID int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.execs[jobID]
	if e == nil {
		return
	}
	e.script.CleanupFails = false
	if e.cleanup == executor.CleanupFailed {
		f.cleaned(e, "fake: cleanup confirmed on retry")
	}
}

func (f *Fake) Terminate(_ context.Context, jobID int64, grace time.Duration, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outage {
		return ErrUnavailable
	}
	e := f.execs[jobID]
	if e == nil || e.frozen {
		return nil
	}
	switch e.phase {
	case executor.PhaseCreating:
		if e.lateCreate {
			e.stopRequested = reason // the late object stops as soon as it lands
			return nil
		}
		// Nothing exists and nothing will: record a stopped outcome.
		f.finish(e, executor.EventFinished, executor.Result{Reason: executor.ExitSignaled,
			Detail: "stopped before creation: " + reason})
	case executor.PhasePending:
		f.finish(e, executor.EventFinished, executor.Result{Reason: executor.ExitSignaled,
			Detail: "stopped before start: " + reason})
	case executor.PhaseRunning:
		if e.terminating {
			return nil
		}
		e.terminating = true
		if e.script.IgnoreSIGTERM {
			f.after(e, grace, func() {
				f.finish(e, executor.EventFinished, executor.Result{ExitCode: ptr(137), Reason: executor.ExitSignaled,
					Detail: fmt.Sprintf("killed after %s grace: %s", grace, reason), OutputIncomplete: true})
			})
		} else {
			f.after(e, min(e.script.StopDelay, grace), func() {
				f.finish(e, executor.EventFinished, executor.Result{ExitCode: ptr(143), Reason: executor.ExitSignaled,
					Detail: reason})
			})
		}
	}
	return nil
}

// LoseNode makes a node unreachable. Every unfinished or uncleaned
// execution on it is frozen and reported Lost; nothing on it progresses.
func (f *Fake) LoseNode(name, detail string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[name]
	n.Healthy, n.Detail = false, detail
	for _, e := range f.sortedExecs() {
		if e.spec.Node != name || e.phase == executor.PhaseCleaned || e.phase == executor.PhaseLost {
			continue
		}
		f.stopTimers(e)
		e.frozen = true
		e.phase = executor.PhaseLost
		f.emit(e, executor.EventLost, nil, executor.ExitNodeLost, "node "+name+" unreachable: "+detail, f.clock.Now())
	}
}

// RecoverNode brings a node back. Recovery proves its lost executions are
// gone, so each gets a cleanup receipt and an Exited event.
func (f *Fake) RecoverNode(name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := f.nodes[name]
	n.Healthy, n.Detail = true, ""
	for _, e := range f.sortedExecs() {
		if e.spec.Node == name && e.phase == executor.PhaseLost {
			e.frozen = false
			f.cleaned(e, "fake: node "+name+" recovered; execution not running")
		}
	}
}

func (f *Fake) List(context.Context) ([]executor.Execution, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.outage {
		return nil, fmt.Errorf("fake: %w", executor.ErrIncompleteObservation)
	}
	var out []executor.Execution
	for _, e := range f.sortedExecs() {
		x := executor.Execution{
			JobID:           e.spec.JobID,
			ExecutionID:     e.spec.ExecutionID,
			SpecHash:        e.spec.SpecHash,
			Node:            e.spec.Node,
			Phase:           e.phase,
			ObjectUID:       e.uid,
			StartedAt:       e.startedAt,
			FinishedAt:      e.finishedAt,
			Cleanup:         e.cleanup,
			CleanupEvidence: e.evidence,
		}
		if e.result != nil {
			r := *e.result
			x.Result = &r
		}
		out = append(out, x)
	}
	return out, nil
}

func (f *Fake) Logs(_ context.Context, jobID int64, _ bool) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e := f.execs[jobID]
	if e == nil || !e.created {
		return nil, executor.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(fmt.Sprintf("fake output for job %d\n", jobID))), nil
}

func (f *Fake) Attach(context.Context, int64, executor.Terminal) error {
	return errors.New("fake: attach is not implemented until M2")
}

// after runs fn under f.mu after d unless the execution is frozen.
func (f *Fake) after(e *execution, d time.Duration, fn func()) {
	e.timers = append(e.timers, f.clock.AfterFunc(d, func() {
		f.mu.Lock()
		defer f.mu.Unlock()
		if !e.frozen {
			fn()
		}
	}))
}

func (f *Fake) stopTimers(e *execution) {
	for _, t := range e.timers {
		t.Stop()
	}
	e.timers = nil
}

func (f *Fake) sortedExecs() []*execution {
	ids := make([]int64, 0, len(f.execs))
	for id := range f.execs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	out := make([]*execution, len(ids))
	for i, id := range ids {
		out[i] = f.execs[id]
	}
	return out
}

func (f *Fake) receipt(e *execution, kind executor.ReceiptKind, r *executor.Result, evidence string) {
	f.receipts[e.spec.ExecutionID] = append(f.receipts[e.spec.ExecutionID], executor.Receipt{
		ExecutionID: e.spec.ExecutionID,
		Kind:        kind,
		ObjectUID:   e.uid,
		At:          f.clock.Now(),
		Result:      r,
		Evidence:    evidence,
	})
}

func (f *Fake) emit(e *execution, kind executor.EventKind, code *int, reason executor.ExitReason, detail string, at time.Time) {
	f.events.push(executor.Event{
		EventID:     e.spec.ExecutionID + "/" + string(kind),
		JobID:       e.spec.JobID,
		ExecutionID: e.spec.ExecutionID,
		Kind:        kind,
		Node:        e.spec.Node,
		ExitCode:    code,
		Reason:      reason,
		Detail:      detail,
		At:          at,
	})
}

func ptr[T any](v T) *T { return &v }

var _ executor.Executor = (*Fake)(nil)
