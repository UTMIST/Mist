package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/executor/fake"
)

// Two 1-GPU jobs run side by side; a third waits with RESOURCES until a
// cleanup receipt, not process exit, frees a GPU.
func TestTwoGPUJobsThirdWaits(t *testing.T) {
	h := newHarness(t)
	a := h.submit("alice", "team-a", "gpu")
	c := h.submit("carol", "team-b", "gpu")
	b := h.submit("bob", "team-a", "gpu")
	h.settle()
	h.want(a, StateLaunching)
	h.want(c, StateLaunching)
	h.want(b, StatePending, PendingResources)

	h.advance(time.Second)
	h.want(a, StateRunning)
	h.advance(time.Minute)
	h.want(a, StateCleaning)
	if _, released, _ := h.allocation(a); released {
		t.Fatal("allocation released on process exit")
	}
	h.want(b, StatePending, PendingResources)

	h.advance(time.Second)
	h.want(a, StateCompleted)
	h.want(c, StateCompleted)
	h.want(b, StateLaunching)
	if got := h.history(a); got != lifecycle {
		t.Fatalf("history %s", got)
	}
}

func TestUserAndTeamLimits(t *testing.T) {
	h := newHarness(t)
	h.sql("INSERT INTO limits VALUES ('team', 'team-a', 'gpu', '*', 'gpus', 1)")
	a1 := h.submit("alice", "team-a", "gpu")
	a2 := h.submit("alice", "team-a", "gpu")
	b1 := h.submit("bob", "team-a", "gpu")
	h.settle()
	h.want(a1, StateLaunching)
	h.want(a2, StatePending, PendingUserLimit)
	h.want(b1, StatePending, PendingTeamLimit)

	// A user override cannot bypass the team cap.
	h.sql("INSERT INTO limits VALUES ('user', 'alice', 'gpu', '*', 'gpus', 2)")
	h.settle()
	h.want(a2, StatePending, PendingTeamLimit)
}

func TestPartitionCapAndSharedNode(t *testing.T) {
	h := newHarness(t)
	eight := func(r *Request) { r.Resources.MilliCPU = 8000 }
	j1 := h.submit("alice", "team-a", "cpu", eight)
	j2 := h.submit("bob", "team-a", "cpu", eight)
	j3 := h.submit("carol", "team-b", "cpu", eight)
	h.settle()
	h.want(j1, StateLaunching)
	h.want(j2, StateLaunching)
	h.want(j3, StatePending, PendingResources) // 16-CPU partition cap, node has 32

	// A second partition on the same node draws from the same pool.
	h.sql(`INSERT INTO partitions SELECT 'cpu2', executor, state, accelerator, runtime, min_accelerators,
		max_accelerators, default_milli_cpu, default_memory_bytes, max_milli_cpu, max_memory_bytes,
		default_scratch_bytes, max_scratch_bytes, default_time_limit_seconds, max_time_limit_seconds, NULL, NULL
		FROM partitions WHERE name = 'cpu'`)
	h.sql("INSERT INTO partition_nodes VALUES ('cpu2', $1)", cpuNode)
	big := h.submit("carol", "team-b", "cpu2", func(r *Request) { r.Resources.MilliCPU = 16000 })
	small := h.submit("bob", "team-a", "cpu2", func(r *Request) { r.Resources.MilliCPU = 2000 })
	h.settle()
	h.want(big, StateLaunching)
	h.want(small, StatePending, PendingResources)
}

func TestOutcomes(t *testing.T) {
	cases := []struct {
		name   string
		script fake.Script
		after  time.Duration
		want   State
	}{
		{"completed", fake.DefaultScript, 2 * time.Minute, StateCompleted},
		{"failed", fake.Script{StartDelay: time.Second, RunFor: time.Minute, ExitCode: 3}, 2 * time.Minute, StateFailed},
		{"oom", fake.Script{StartDelay: time.Second, RunFor: time.Minute, ExitCode: 137, Reason: executor.ExitOutOfMemory},
			2 * time.Minute, StateOutOfMemory},
		{"launch error", fake.Script{StartDelay: time.Second, LaunchError: "image pull failed"}, time.Minute, StateLaunchFailed},
		{"launch timeout", fake.Script{StartDelay: time.Hour}, 11 * time.Minute, StateLaunchFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			id := h.submit("alice", "team-a", "gpu")
			h.exec.SetScript(id, c.script)
			h.settle()
			for elapsed := time.Duration(0); elapsed < c.after; elapsed += 30 * time.Second {
				h.advance(30 * time.Second)
			}
			h.want(id, c.want)
			if _, released, _ := h.allocation(id); !released {
				t.Fatal("allocation not released after cleanup")
			}
		})
	}
}

// The time limit runs from started_at, and the stop cause decides the
// terminal state even though the process exits by signal.
func TestTimeLimit(t *testing.T) {
	h := newHarness(t)
	id := h.submit("alice", "team-a", "gpu")
	h.exec.SetScript(id, fake.Script{StartDelay: time.Minute, RunFor: 10 * time.Hour, StopDelay: time.Second, CleanupDelay: time.Second})
	h.settle()
	h.advance(time.Minute)
	h.want(id, StateRunning)
	h.advance(time.Hour - time.Second)
	h.want(id, StateRunning) // an hour from submit, but not from start
	h.advance(time.Second)
	h.want(id, StateStopping)
	h.advance(time.Second)
	h.want(id, StateCleaning)
	h.advance(time.Second)
	h.want(id, StateTimeout)
}

func TestCancelInEachState(t *testing.T) {
	t.Run("pending", func(t *testing.T) {
		h := newHarness(t)
		id := h.submit("alice", "team-a", "gpu")
		if st, err := h.jobs.Cancel(h.ctx, id, "alice"); err != nil || st != StateCancelled {
			t.Fatalf("cancel: %s %v", st, err)
		}
		h.settle()
		h.want(id, StateCancelled)
		if exists, _, _ := h.allocation(id); exists {
			t.Fatal("cancelled pending job was allocated")
		}
	})

	t.Run("launching, never sent", func(t *testing.T) {
		h := newHarness(t)
		id := h.submit("alice", "team-a", "gpu")
		if err := h.s.refreshNodes(h.ctx); err != nil {
			t.Fatal(err)
		}
		if err := h.s.admit(h.ctx); err != nil { // committed, not yet dispatched
			t.Fatal(err)
		}
		h.want(id, StateLaunching)
		h.jobs.Cancel(h.ctx, id, "alice")
		h.want(id, StateCancelled)
		if _, released, _ := h.allocation(id); !released {
			t.Fatal("unsent launch kept its allocation")
		}
		h.settle()
		if n := h.exec.Creates(id); n != 0 {
			t.Fatalf("cancelled unsent launch created %d objects", n)
		}
	})

	t.Run("launching, sent", func(t *testing.T) {
		h := newHarness(t)
		id := h.submit("alice", "team-a", "gpu")
		h.settle()
		h.jobs.Cancel(h.ctx, id, "alice")
		h.want(id, StateStopping)
		h.settle()
		h.want(id, StateCleaning)
		h.advance(time.Second)
		h.want(id, StateCancelled)
	})

	t.Run("running", func(t *testing.T) {
		h := newHarness(t)
		id := h.submit("alice", "team-a", "gpu")
		h.settle()
		h.advance(time.Second)
		h.jobs.Cancel(h.ctx, id, "alice")
		h.jobs.Cancel(h.ctx, id, "alice") // repeat is harmless
		h.settle()
		h.advance(time.Second)
		h.advance(time.Second)
		h.want(id, StateCancelled)
	})

	t.Run("stopping keeps first cause", func(t *testing.T) {
		h := newHarness(t)
		id := h.submit("alice", "team-a", "gpu")
		h.exec.SetScript(id, fake.Script{StartDelay: time.Second, RunFor: 10 * time.Hour, IgnoreSIGTERM: true, CleanupDelay: time.Second})
		h.settle()
		h.advance(time.Second)
		h.advance(time.Hour)
		h.want(id, StateStopping)
		if st, _ := h.jobs.Cancel(h.ctx, id, "alice"); st != StateStopping {
			t.Fatalf("cancel during stop: %s", st)
		}
		h.advance(time.Minute)
		h.advance(time.Second)
		h.want(id, StateTimeout)
	})

	t.Run("cleaning and terminal", func(t *testing.T) {
		h := newHarness(t)
		id := h.submit("alice", "team-a", "gpu")
		h.exec.SetScript(id, fake.Script{StartDelay: time.Second, RunFor: time.Minute, CleanupDelay: time.Minute})
		h.settle()
		h.advance(time.Second)
		h.advance(time.Minute)
		h.want(id, StateCleaning)
		h.jobs.Cancel(h.ctx, id, "alice")
		h.want(id, StateCleaning)
		h.advance(time.Minute)
		h.want(id, StateCompleted)
		before := h.history(id)
		h.jobs.Cancel(h.ctx, id, "alice")
		h.settle()
		h.want(id, StateCompleted)
		if h.history(id) != before {
			t.Fatal("cancel changed a terminal job's history")
		}
	})
}

// A lost node keeps the allocation quarantined until recovery proves the
// execution stopped; repeated cleanup receipts change nothing.
func TestNodeFailRetainsAllocation(t *testing.T) {
	h := newHarness(t)
	id := h.submit("alice", "team-a", "gpu")
	h.settle()
	h.advance(time.Second)
	h.exec.LoseNode(gpuNode, "kubelet NotReady")
	h.settle()
	h.want(id, StateNodeFail)
	if _, released, quarantined := h.allocation(id); released || !quarantined {
		t.Fatalf("lost allocation: released=%v quarantined=%v", released, quarantined)
	}
	if !h.job(id).CleanupPending {
		t.Fatal("NODE_FAIL without cleanup_pending")
	}
	next := h.submit("carol", "team-b", "gpu")
	h.advance(time.Hour)
	h.want(next, StatePending, PendingPartitionDown)
	if n := h.exec.Creates(id); n != 1 {
		t.Fatalf("lost work recreated: %d objects", n)
	}

	h.exec.RecoverNode(gpuNode)
	h.settle()
	if _, released, _ := h.allocation(id); !released {
		t.Fatal("recovered node did not release the allocation")
	}
	h.want(id, StateNodeFail)
	h.want(next, StateLaunching)

	before := h.history(id)
	ev := executor.Event{EventID: "repeat", JobID: id, ExecutionID: *h.job(id).ExecutionID,
		Kind: executor.EventExited, Detail: "again", At: h.clock.Now()}
	if err := h.s.HandleEvent(h.ctx, ev); err != nil {
		t.Fatal(err)
	}
	if h.history(id) != before {
		t.Fatal("repeated cleanup receipt changed history")
	}
}

// An execution that vanishes from complete observations, with no receipt,
// is a loss after confirmation, never a reason to launch again.
func TestMissingExecutionIsNodeFail(t *testing.T) {
	h := newHarness(t)
	id := h.submit("alice", "team-a", "gpu")
	h.settle()
	h.advance(time.Second)
	h.exec.Forget(id)
	h.cycle()
	h.want(id, StateRunning) // one absent list is not proof
	h.cycle()
	h.want(id, StateNodeFail)
	h.settle()
	if n := h.exec.Creates(id); n != 1 {
		t.Fatalf("missing execution relaunched: %d", n)
	}
}

// A job can finish before Started is delivered; events arriving in reverse
// order still converge on the right outcome and real start time.
func TestFastExitOutOfOrder(t *testing.T) {
	h := newHarness(t)
	id := h.submit("alice", "team-a", "gpu")
	h.settle()
	h.exec.HoldEvents()
	h.clock.Advance(time.Second)
	h.clock.Advance(time.Minute)
	h.clock.Advance(time.Second)
	h.exec.ReleaseEvents(true)
	h.drain()
	h.want(id, StateCompleted)
	if j := h.job(id); j.StartedAt == nil || !j.StartedAt.Equal(t0.Add(time.Second)) {
		t.Fatalf("started_at %v", j.StartedAt)
	}
	if got := h.history(id); got != ">PENDING PENDING>LAUNCHING LAUNCHING>CLEANING CLEANING>COMPLETED" {
		t.Fatalf("history %s", got)
	}
	h.settle()
	h.want(id, StateCompleted)
}

func TestDuplicateEvents(t *testing.T) {
	h := newHarness(t)
	h.exec.DuplicateEvents(true)
	id := h.submit("alice", "team-a", "gpu")
	h.settle()
	h.advance(time.Second)
	h.advance(time.Minute)
	h.advance(time.Second)
	if got := h.history(id); got != lifecycle {
		t.Fatalf("history %s", got)
	}
}

// Lost notifications are recovered from List; a restarted scheduler picks
// up where the old one stopped.
func TestDroppedEventsAndRestart(t *testing.T) {
	h := newHarness(t)
	id := h.submit("alice", "team-a", "gpu")
	h.settle()
	h.exec.HoldEvents()
	h.clock.Advance(time.Second)
	h.exec.DropHeldEvents()
	h.exec.HoldEvents()
	h.s = h.newScheduler()
	h.cycle()
	h.want(id, StateRunning)
	h.clock.Advance(time.Minute + time.Second)
	h.exec.Restart()
	h.cycle()
	h.want(id, StateCompleted)
}

func TestAmbiguousLaunchRetriesSameExecution(t *testing.T) {
	for _, fault := range []fake.LaunchFault{fake.FaultTimeoutBeforeCreate, fake.FaultTimeoutAfterCreate, fake.FaultLateCreate} {
		h := newHarness(t)
		id := h.submit("alice", "team-a", "gpu")
		h.exec.FailNextLaunch(id, fault, 500*time.Millisecond)
		h.cycle()
		if st, last := h.intent(id, OpLaunch); st != OpInFlight || last == "" {
			t.Fatalf("fault %d: intent %s %q", fault, st, last)
		}
		h.settle() // retries the same ExecutionID
		h.advance(time.Second)
		h.advance(time.Minute)
		h.advance(time.Second)
		h.want(id, StateCompleted)
		if n := h.exec.Creates(id); n != 1 {
			t.Fatalf("fault %d: %d objects", fault, n)
		}
	}
}

// A scheduler that lost leadership cannot commit anything; the new one
// dispatches the admission the old one committed, with the same identity.
func TestSplitLeadershipAndCrashBeforeDispatch(t *testing.T) {
	h := newHarness(t)
	id := h.submit("alice", "team-a", "gpu")
	old := h.s
	if err := old.refreshNodes(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := old.admit(h.ctx); err != nil {
		t.Fatal(err)
	}
	execID := *h.job(id).ExecutionID

	h.s = h.newScheduler()
	if err := old.Cycle(h.ctx); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("old leader cycle: %v", err)
	}
	other := h.submit("carol", "team-b", "gpu")
	if err := old.admit(h.ctx); !errors.Is(err, ErrNotLeader) {
		t.Fatalf("old leader admit: %v", err)
	}
	h.want(other, StatePending)

	h.settle()
	h.advance(time.Second)
	if j := h.job(id); *j.ExecutionID != execID || j.State != StateRunning {
		t.Fatalf("after takeover: %s %s", *j.ExecutionID, j.State)
	}
	if n := h.exec.Creates(id); n != 1 {
		t.Fatalf("%d objects", n)
	}
}

func TestRevocation(t *testing.T) {
	h := newHarness(t)
	running := h.submit("alice", "team-a", "gpu")
	otherTeam := h.submit("alice", "team-b", "cpu")
	h.settle()
	h.advance(time.Second)
	queued := h.submit("alice", "team-a", "cpu")
	h.id.RemoveMember("alice", "team-a")
	h.settle()
	h.want(queued, StateCancelled)
	h.want(running, StateStopping)
	h.want(otherTeam, StateRunning)
	h.advance(time.Second)
	h.advance(time.Second)
	h.want(running, StateCancelled)
	h.want(otherTeam, StateRunning)
}

func TestAuthorizationNotReady(t *testing.T) {
	h := newHarness(t)
	h.id.SetProvisioned("team-a", false)
	id := h.submit("alice", "team-a", "gpu")
	h.settle()
	h.want(id, StatePending, PendingProvisioning)
	h.id.SetProvisioned("team-a", true)
	h.id.Unavailable = true
	h.settle()
	h.want(id, StatePending, PendingAuthUnavailable)
	h.id.Unavailable = false
	h.settle()
	h.want(id, StateLaunching)
}

// An unstarted launch is never replayed under an older authorization.
func TestStaleGenerationCancelsUnsentLaunch(t *testing.T) {
	h := newHarness(t)
	id := h.submit("alice", "team-a", "gpu")
	if err := h.s.refreshNodes(h.ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.s.admit(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.id.AddMember("bob", "team-b", "member") // bumps the generation
	if err := h.s.dispatch(h.ctx); err != nil {
		t.Fatal(err)
	}
	h.want(id, StateCancelled)
	if n := h.exec.Creates(id); n != 0 {
		t.Fatalf("stale launch sent: %d", n)
	}
}

func TestSubmitQueueLimit(t *testing.T) {
	h := newHarness(t)
	h.sql("UPDATE limits SET max = 2 WHERE resource = 'submitted_jobs'")
	h.submit("alice", "team-a", "gpu")
	h.submit("alice", "team-a", "gpu")
	_, err := h.jobs.Submit(h.ctx, Request{Username: "alice", Team: "team-a", Partition: "gpu", QOS: "normal",
		Kind: executor.KindBatch, Script: "true", TimeLimit: time.Hour, SignalGrace: time.Minute,
		Resources: executor.Resources{MilliCPU: 1000, MemoryBytes: 1 << 30, GPUs: 1, ScratchBytes: 1 << 30}})
	if !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("want ErrLimitExceeded, got %v", err)
	}
}
