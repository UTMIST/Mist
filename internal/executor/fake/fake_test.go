package fake

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/executor"
)

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

const gpuNode = "utmist-z1opa08"

type env struct {
	t     *testing.T
	clock *clock.Fake
	fake  *Fake
	ctx   context.Context
}

func newEnv(t *testing.T) *env {
	c := clock.NewFake(t0)
	f := New(Config{Clock: c, Nodes: []executor.Node{{
		Name:       gpuNode,
		Partitions: []string{"gpu"},
		Capacity:   executor.Resources{MilliCPU: 48000, MemoryBytes: 240 << 30, GPUs: 2, ScratchBytes: 400 << 30},
		Healthy:    true,
	}}})
	t.Cleanup(f.Close)
	return &env{t: t, clock: c, fake: f, ctx: context.Background()}
}

func (e *env) spec(jobID int64) executor.LaunchSpec {
	s := executor.LaunchSpec{
		JobID:          jobID,
		ExecutionID:    executor.NewExecutionID(),
		AuthGeneration: 1,
		LaunchDeadline: e.clock.Now().Add(10 * time.Minute),
		Kind:           executor.KindBatch,
		Node:           gpuNode,
		User:           executor.User{Name: "alice", UID: 20001, PersonalGID: 20001},
		TeamSlug:       "team-a",
		TeamGID:        40001,
		ExtraGIDs:      []int{19000},
		Resources:      executor.Resources{MilliCPU: 8000, MemoryBytes: 32 << 30, GPUs: 1, ScratchBytes: 100 << 30},
		Workdir:        "/mist/teams/team-a/alice",
		OutputPath:     "/mist/teams/team-a/alice/mist.out",
		Script:         "true\n",
		Runtime:        "gpu@sha256:0",
		TimeLimit:      time.Hour,
		SignalGrace:    60 * time.Second,
	}
	s.Seal()
	return s
}

func (e *env) launch(s executor.LaunchSpec) {
	e.t.Helper()
	if err := e.fake.Launch(e.ctx, s); err != nil {
		e.t.Fatalf("launch: %v", err)
	}
}

// next returns the next delivered event.
func (e *env) next() executor.Event {
	e.t.Helper()
	select {
	case ev := <-e.fake.Events():
		return ev
	case <-time.After(2 * time.Second):
		e.t.Fatal("no event delivered")
		return executor.Event{}
	}
}

func (e *env) expect(kind executor.EventKind, reason executor.ExitReason) executor.Event {
	e.t.Helper()
	ev := e.next()
	if ev.Kind != kind || (reason != "" && ev.Reason != reason) {
		e.t.Fatalf("got %s/%s (%s), want %s/%s", ev.Kind, ev.Reason, ev.Detail, kind, reason)
	}
	return ev
}

func (e *env) quiet() {
	e.t.Helper()
	select {
	case ev := <-e.fake.Events():
		e.t.Fatalf("unexpected event %s/%s", ev.Kind, ev.Reason)
	case <-time.After(50 * time.Millisecond):
	}
}

func (e *env) execution(jobID int64) executor.Execution {
	e.t.Helper()
	xs, err := e.fake.List(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, x := range xs {
		if x.JobID == jobID {
			return x
		}
	}
	e.t.Fatalf("job %d not listed", jobID)
	return executor.Execution{}
}

func receiptKinds(rs []executor.Receipt) []executor.ReceiptKind {
	var out []executor.ReceiptKind
	for _, r := range rs {
		out = append(out, r.Kind)
	}
	return out
}

func TestLifecycle(t *testing.T) {
	e := newEnv(t)
	s := e.spec(1)
	e.launch(s)
	e.launch(s) // repeat-safe
	if x := e.execution(1); x.Phase != executor.PhasePending || x.ObjectUID == "" {
		t.Fatalf("after launch: %+v", x)
	}

	e.clock.Advance(time.Second)
	if ev := e.expect(executor.EventStarted, ""); !ev.At.Equal(t0.Add(time.Second)) {
		t.Fatalf("started at %v", ev.At)
	}
	e.clock.Advance(time.Minute)
	if ev := e.expect(executor.EventFinished, executor.ExitNormal); *ev.ExitCode != 0 {
		t.Fatalf("exit %d", *ev.ExitCode)
	}
	if x := e.execution(1); x.Phase != executor.PhaseFinished || x.Cleanup != executor.CleanupPending {
		t.Fatalf("finish must not imply cleanup: %+v", x)
	}
	e.clock.Advance(time.Second)
	e.expect(executor.EventExited, executor.ExitNormal)

	want := []executor.ReceiptKind{executor.ReceiptCreateIntent, executor.ReceiptCreated,
		executor.ReceiptStarted, executor.ReceiptFinished, executor.ReceiptCleaned}
	if got := receiptKinds(e.fake.Receipts(s.ExecutionID)); !slices.Equal(got, want) {
		t.Fatalf("receipts %v, want %v", got, want)
	}
	if err := e.fake.Launch(e.ctx, s); !errors.Is(err, executor.ErrExecutionStarted) {
		t.Fatalf("relaunch after finish: %v", err)
	}
	if n := e.fake.Creates(1); n != 1 {
		t.Fatalf("%d objects created", n)
	}
}

func TestSpecConflict(t *testing.T) {
	e := newEnv(t)
	s := e.spec(1)
	e.launch(s)
	other := s
	other.Resources.GPUs = 2
	other.Seal()
	if err := e.fake.Launch(e.ctx, other); !errors.Is(err, executor.ErrSpecConflict) {
		t.Fatalf("different spec: %v", err)
	}
	newID := e.spec(1)
	if err := e.fake.Launch(e.ctx, newID); !errors.Is(err, executor.ErrSpecConflict) {
		t.Fatalf("second execution for job: %v", err)
	}
}

func TestOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		script Script
		kind   executor.EventKind
		reason executor.ExitReason
		after  time.Duration
	}{
		{"oom", Script{StartDelay: time.Second, RunFor: time.Minute, ExitCode: 137, Reason: executor.ExitOutOfMemory},
			executor.EventFinished, executor.ExitOutOfMemory, time.Minute},
		{"nonzero", Script{StartDelay: time.Second, RunFor: time.Minute, ExitCode: 1, Reason: executor.ExitNormal},
			executor.EventFinished, executor.ExitNormal, time.Minute},
		{"backstop", Script{StartDelay: time.Second, RunFor: 10 * time.Hour},
			executor.EventFinished, executor.ExitDeadline, time.Hour + 2*time.Minute},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			e.fake.SetScript(1, c.script)
			e.launch(e.spec(1))
			e.clock.Advance(time.Second)
			e.expect(executor.EventStarted, "")
			e.clock.Advance(c.after - time.Second)
			e.quiet()
			e.clock.Advance(time.Second)
			e.expect(c.kind, c.reason)
		})
	}
}

func TestLaunchFailures(t *testing.T) {
	e := newEnv(t)
	e.fake.SetScript(1, Script{StartDelay: 5 * time.Second, LaunchError: "image pull failed"})
	e.fake.SetScript(2, Script{StartDelay: 20 * time.Minute, RunFor: time.Minute})
	e.launch(e.spec(1))
	e.launch(e.spec(2))

	e.clock.Advance(5 * time.Second)
	e.expect(executor.EventLaunchFailed, executor.ExitLaunchError)
	e.expect(executor.EventExited, executor.ExitLaunchError) // zero cleanup delay
	e.clock.Advance(10*time.Minute - 5*time.Second)
	ev := e.expect(executor.EventLaunchFailed, executor.ExitLaunchTimeout)
	if ev.JobID != 2 || !ev.At.Equal(t0.Add(10*time.Minute)) {
		t.Fatalf("launch timeout %+v", ev)
	}
	if x := e.execution(2); x.StartedAt != nil {
		t.Fatal("timed-out launch has a start time")
	}
}

func TestTerminate(t *testing.T) {
	e := newEnv(t)
	e.fake.SetScript(2, Script{StartDelay: time.Second, RunFor: time.Hour, IgnoreSIGTERM: true})
	e.launch(e.spec(1))
	e.launch(e.spec(2))
	e.launch(e.spec(3))
	e.fake.Terminate(e.ctx, 3, time.Minute, "cancelled by alice") // before start
	e.expect(executor.EventFinished, executor.ExitSignaled)

	e.clock.Advance(time.Second)
	e.expect(executor.EventStarted, "")
	e.expect(executor.EventStarted, "")
	e.expect(executor.EventExited, executor.ExitSignaled) // job 3 cleaned up

	for range 2 { // idempotent
		e.fake.Terminate(e.ctx, 1, time.Minute, "cancelled")
		e.fake.Terminate(e.ctx, 2, time.Minute, "cancelled")
	}
	e.clock.Advance(time.Second)
	if ev := e.expect(executor.EventFinished, executor.ExitSignaled); ev.JobID != 1 || *ev.ExitCode != 143 {
		t.Fatalf("SIGTERM exit %+v", ev)
	}
	e.clock.Advance(time.Minute)
	e.expect(executor.EventExited, executor.ExitSignaled)
	if ev := e.expect(executor.EventFinished, executor.ExitSignaled); ev.JobID != 2 || *ev.ExitCode != 137 {
		t.Fatalf("SIGKILL exit %+v", ev)
	}
}

func TestAmbiguousCreates(t *testing.T) {
	e := newEnv(t)

	// Lost request: nothing created, retry creates exactly once.
	s1 := e.spec(1)
	e.fake.FailNextLaunch(1, FaultTimeoutBeforeCreate, 0)
	if err := e.fake.Launch(e.ctx, s1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want timeout, got %v", err)
	}
	if x := e.execution(1); x.Phase != executor.PhaseCreating || e.fake.Creates(1) != 0 {
		t.Fatalf("lost request: %+v creates=%d", x, e.fake.Creates(1))
	}
	e.launch(s1)

	// Lost response: object exists, retry is a no-op.
	s2 := e.spec(2)
	e.fake.FailNextLaunch(2, FaultTimeoutAfterCreate, 0)
	if err := e.fake.Launch(e.ctx, s2); err == nil {
		t.Fatal("want timeout")
	}
	e.launch(s2)

	// Late create: retry wins, the late one finds it exists.
	s3 := e.spec(3)
	e.fake.FailNextLaunch(3, FaultLateCreate, 5*time.Second)
	if err := e.fake.Launch(e.ctx, s3); err == nil {
		t.Fatal("want timeout")
	}
	e.launch(s3)
	e.clock.Advance(time.Minute)

	for job := int64(1); job <= 3; job++ {
		if n := e.fake.Creates(job); n != 1 {
			t.Errorf("job %d: %d objects", job, n)
		}
	}
}

func TestStopBeforeLateCreateLands(t *testing.T) {
	e := newEnv(t)
	e.fake.FailNextLaunch(1, FaultLateCreate, 5*time.Second)
	e.fake.Launch(e.ctx, e.spec(1))
	e.fake.Terminate(e.ctx, 1, time.Minute, "cancelled")
	e.quiet()
	e.clock.Advance(5 * time.Second)
	e.expect(executor.EventFinished, executor.ExitSignaled)
	e.clock.Advance(time.Hour)
	e.expect(executor.EventExited, executor.ExitSignaled)
	if x := e.execution(1); x.StartedAt != nil || e.fake.Creates(1) != 1 {
		t.Fatalf("stopped late create ran: %+v", x)
	}
}

func TestLostNode(t *testing.T) {
	e := newEnv(t)
	s := e.spec(1)
	e.launch(s)
	e.clock.Advance(time.Second)
	e.expect(executor.EventStarted, "")

	e.fake.LoseNode(gpuNode, "kubelet NotReady")
	e.expect(executor.EventLost, executor.ExitNodeLost)
	nodes, _ := e.fake.Nodes(e.ctx)
	if nodes[0].Healthy {
		t.Fatal("lost node reported healthy")
	}
	e.clock.Advance(24 * time.Hour) // nothing progresses, including cleanup
	e.quiet()
	if err := e.fake.Launch(e.ctx, s); !errors.Is(err, executor.ErrExecutionStarted) {
		t.Fatalf("relaunch of lost execution: %v", err)
	}
	if x := e.execution(1); x.Cleanup == executor.CleanupConfirmed {
		t.Fatal("lost execution reported cleaned")
	}

	e.fake.RecoverNode(gpuNode)
	e.expect(executor.EventExited, executor.ExitNodeLost)
	if n := e.fake.Creates(1); n != 1 {
		t.Fatalf("%d objects", n)
	}
}

func TestFailedCleanupHolds(t *testing.T) {
	e := newEnv(t)
	e.fake.SetScript(1, Script{StartDelay: time.Second, RunFor: time.Minute, CleanupDelay: time.Second, CleanupFails: true})
	e.launch(e.spec(1))
	e.clock.Advance(time.Second)
	e.expect(executor.EventStarted, "")
	e.clock.Advance(time.Minute)
	e.expect(executor.EventFinished, executor.ExitNormal)
	e.clock.Advance(time.Hour)
	e.quiet()
	if x := e.execution(1); x.Cleanup != executor.CleanupFailed {
		t.Fatalf("cleanup %q", x.Cleanup)
	}
	e.fake.FixCleanup(1)
	e.expect(executor.EventExited, executor.ExitNormal)
}

func TestOutage(t *testing.T) {
	e := newEnv(t)
	e.fake.SetOutage(true)
	if _, err := e.fake.List(e.ctx); !errors.Is(err, executor.ErrIncompleteObservation) {
		t.Fatalf("List during outage: %v", err)
	}
	if _, err := e.fake.Nodes(e.ctx); err == nil {
		t.Fatal("Nodes during outage succeeded")
	}
	if err := e.fake.Launch(e.ctx, e.spec(1)); err == nil {
		t.Fatal("Launch during outage succeeded")
	}
	e.fake.SetOutage(false)
	if xs, err := e.fake.List(e.ctx); err != nil || len(xs) != 0 {
		t.Fatalf("after outage: %v %v", xs, err)
	}
}

func TestDelivery(t *testing.T) {
	run := func(e *env, job int64) {
		e.launch(e.spec(job))
		e.clock.Advance(time.Second)
		e.clock.Advance(time.Minute)
		e.clock.Advance(time.Second)
	}

	t.Run("reordered", func(t *testing.T) {
		e := newEnv(t)
		e.fake.HoldEvents()
		run(e, 1)
		e.quiet()
		e.fake.ReleaseEvents(true)
		e.expect(executor.EventExited, "")
		e.expect(executor.EventFinished, "")
		e.expect(executor.EventStarted, "")
	})

	t.Run("duplicated", func(t *testing.T) {
		e := newEnv(t)
		e.fake.DuplicateEvents(true)
		e.launch(e.spec(1))
		e.clock.Advance(time.Second)
		a, b := e.next(), e.next()
		if a.EventID != b.EventID || a.Kind != executor.EventStarted {
			t.Fatalf("duplicate has different identity: %s vs %s", a.EventID, b.EventID)
		}
	})

	t.Run("dropped", func(t *testing.T) {
		e := newEnv(t)
		e.fake.HoldEvents()
		run(e, 1)
		e.fake.DropHeldEvents()
		e.quiet()
		// Receipts and List still recover the outcome.
		if x := e.execution(1); x.Phase != executor.PhaseCleaned || x.Result == nil || *x.Result.ExitCode != 0 {
			t.Fatalf("not recoverable from List: %+v", x)
		}
	})

	t.Run("restart", func(t *testing.T) {
		e := newEnv(t)
		e.fake.HoldEvents()
		run(e, 1)
		e.fake.Restart()
		e.quiet()
		if len(e.fake.Receipts(e.execution(1).ExecutionID)) != 5 {
			t.Fatal("restart lost receipts")
		}
	})
}
