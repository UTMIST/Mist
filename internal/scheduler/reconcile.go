package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity"
)

// refreshNodes copies each executor's node report into the nodes table.
// Nodes must already be registered by an admin; unknown ones are ignored.
func (s *Scheduler) refreshNodes(ctx context.Context) error {
	for name, ex := range s.executors {
		nodes, err := ex.Nodes(ctx)
		if err != nil {
			s.observed[name] = false
			s.log.Warn("executor nodes unavailable", "executor", name, "err", err)
			continue
		}
		s.observed[name] = true
		err = s.fenced(ctx, func(tx pgx.Tx) error {
			for _, n := range nodes {
				c := n.Capacity
				if _, err := tx.Exec(ctx, `
					UPDATE nodes SET healthy = $3, health_detail = $4, observed_at = $5,
						milli_cpu = $6, memory_bytes = $7, gpus = $8, tt_boards = $9, scratch_bytes = $10, hugepages_bytes = $11
					WHERE name = $1 AND executor = $2`,
					n.Name, name, n.Healthy, n.Detail, s.clock.Now(),
					c.MilliCPU, c.MemoryBytes, c.GPUs, c.TTBoards, c.ScratchBytes, c.HugepagesBytes); err != nil {
					return err
				}
				s.nodeExec[n.Name] = name
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// usable reports whether admission may place work on n this cycle: its
// executor answered completely, and it hosts no unexplained execution.
func (s *Scheduler) usable(n *nodeInfo) bool {
	return s.observed[n.Executor] && !s.unknownOn[n.Name]
}

func (s *Scheduler) executorFor(node string) executor.Executor {
	name, ok := s.nodeExec[node]
	if !ok || !s.observed[name] {
		return nil
	}
	return s.executors[name]
}

// reconcile compares each executor's complete List with the database,
// following the matrix in scheduler.md#reconciliation.
func (s *Scheduler) reconcile(ctx context.Context) error {
	for name, ex := range s.executors {
		if !s.observed[name] {
			continue
		}
		xs, err := ex.List(ctx)
		if err != nil {
			// No absence inference from a failed or partial list, and no new
			// admissions on this executor's nodes this cycle.
			s.observed[name] = false
			s.log.Warn("executor list incomplete", "executor", name, "err", err)
			continue
		}
		if err := s.reconcileExecutor(ctx, name, xs); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) reconcileExecutor(ctx context.Context, name string, xs []executor.Execution) error {
	seen := map[int64]bool{}
	unknown := map[string]bool{}
	for _, x := range xs {
		seen[x.JobID] = true
		known, err := s.reconcileOne(ctx, name, x)
		if err != nil {
			return err
		}
		if !known && x.Cleanup != executor.CleanupConfirmed {
			unknown[x.Node] = true
		}
	}
	for node, ex := range s.nodeExec {
		if ex == name {
			s.unknownOn[node] = unknown[node]
		}
	}

	// Jobs this executor should have but did not report.
	var active []*job
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, "SELECT "+jobColumns+jobFrom+`
			JOIN nodes n ON n.name = j.node
			WHERE n.executor = $1 AND j.state IN ('LAUNCHING', 'RUNNING', 'STOPPING', 'CLEANING')`, name)
		if err != nil {
			return err
		}
		active, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*job, error) { return scanJob(r) })
		return err
	})
	if err != nil {
		return err
	}
	for _, j := range active {
		if seen[j.ID] {
			delete(s.missing, j.ID)
			continue
		}
		if j.State == StateLaunching {
			continue // dispatch sends or resends the same launch
		}
		// Require two consecutive complete lists before treating absence
		// as real, so one racy read never ends a job.
		s.missing[j.ID]++
		if s.missing[j.ID] < 2 {
			continue
		}
		delete(s.missing, j.ID)
		if err := s.resolveMissing(ctx, j); err != nil {
			return err
		}
	}
	return nil
}

// reconcileOne turns one observed execution into the events it implies.
// It reports whether the execution belongs to a job the scheduler knows.
func (s *Scheduler) reconcileOne(ctx context.Context, exName string, x executor.Execution) (bool, error) {
	var j *job
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		var err error
		j, err = scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+jobFrom+"WHERE j.id = $1", x.JobID))
		if errors.Is(err, pgx.ErrNoRows) {
			j, err = nil, nil
		}
		return err
	})
	if err != nil {
		return false, err
	}
	if j == nil || j.ExecutionID == nil || *j.ExecutionID != x.ExecutionID || j.State == StatePending {
		// Not ours to adopt: stop it and keep its node out of admission.
		if x.Phase != executor.PhaseCleaned {
			s.log.Warn("stopping unexpected execution", "job", x.JobID, "execution", x.ExecutionID, "node", x.Node)
			if err := s.executors[exName].Terminate(ctx, x.JobID, 10*time.Second, "unexpected execution"); err != nil {
				s.log.Warn("terminate unexpected execution", "err", err)
			}
		}
		return false, nil
	}

	base := executor.Event{JobID: x.JobID, ExecutionID: x.ExecutionID, Node: x.Node}
	emit := func(kind executor.EventKind, at time.Time, r *executor.Result) error {
		ev := base
		ev.Kind, ev.At = kind, at
		if r != nil {
			ev.ExitCode, ev.Reason, ev.Detail = r.ExitCode, r.Reason, r.Detail
		}
		return s.observe(ctx, ev)
	}
	if x.StartedAt != nil {
		if err := emit(executor.EventStarted, *x.StartedAt, nil); err != nil {
			return true, err
		}
	}
	if x.Result != nil && x.FinishedAt != nil {
		kind := executor.EventFinished
		if x.Result.Reason == executor.ExitLaunchError || x.Result.Reason == executor.ExitLaunchTimeout {
			kind = executor.EventLaunchFailed
		}
		if err := emit(kind, *x.FinishedAt, x.Result); err != nil {
			return true, err
		}
	}
	switch {
	case x.Cleanup == executor.CleanupConfirmed:
		r := x.Result
		if r == nil {
			r = &executor.Result{Reason: executor.ExitNodeLost}
		}
		r2 := *r
		r2.Detail = x.CleanupEvidence
		return true, emit(executor.EventExited, s.clock.Now(), &r2)
	case x.Phase == executor.PhaseLost:
		return true, emit(executor.EventLost, s.clock.Now(),
			&executor.Result{Reason: executor.ExitNodeLost, Detail: "node " + x.Node + " unreachable"})
	case j.State == StateStopping && (x.Phase == executor.PhaseRunning || x.Phase == executor.PhasePending):
		return true, s.reissueStop(ctx, j)
	case j.State.Terminal() && x.Phase != executor.PhaseFinished:
		// Terminal in the database but still alive on the node.
		return true, s.executors[exName].Terminate(ctx, j.ID, j.Grace, "job already ended")
	}
	return true, nil
}

// reissueStop resends Terminate if the last one was answered but the
// execution is still alive well after its grace period.
func (s *Scheduler) reissueStop(ctx context.Context, j *job) error {
	return s.fenced(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE op_intents SET status = 'pending', updated_at = $2
			WHERE job_id = $1 AND op = 'terminate' AND status = 'done'
			  AND updated_at + make_interval(secs => $3) <= $2`,
			j.ID, s.clock.Now(), (j.Grace + s.stopRetry).Seconds())
		return err
	})
}

// resolveMissing handles a started or stopping job whose execution is
// confirmed absent from complete observations.
func (s *Scheduler) resolveMissing(ctx context.Context, j *job) error {
	return s.fenced(ctx, func(tx pgx.Tx) error {
		cur, err := lockJob(ctx, tx, j.ID)
		if err != nil || cur == nil {
			return err
		}
		now := s.clock.Now()
		if cur.State == StateStopping && cur.StartedAt == nil {
			// The executor has no record of the create at all, including no
			// create intent, so nothing was ever started.
			st, err := intentStatus(ctx, tx, cur.ID, OpLaunch)
			if err != nil {
				return err
			}
			if st == OpPending || st == OpInFlight {
				return finishUnsent(ctx, tx, cur, now, "executor has no record of the launch")
			}
		}
		switch cur.State {
		case StateRunning, StateStopping, StateCleaning:
			return s.onLost(ctx, tx, cur, executor.Event{
				Detail: fmt.Sprintf("execution missing from node %s", *cur.Node),
			})
		}
		return nil
	})
}

// enforceDeadlines stops RUNNING jobs at started_at + time limit, and
// LAUNCHING jobs that have not started by their launch deadline.
func (s *Scheduler) enforceDeadlines(ctx context.Context) error {
	now := s.clock.Now()
	var ids []int64
	var causes []StopCause
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, CASE WHEN state = 'RUNNING' THEN 'deadline' ELSE 'launch_timeout' END FROM jobs
			WHERE stop_cause IS NULL AND (
			    (state = 'RUNNING' AND started_at + make_interval(secs => time_limit_seconds) <= $1)
			 OR (state = 'LAUNCHING' AND started_at IS NULL AND launch_deadline <= $1))
			ORDER BY id`, now)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var c string
			if err := rows.Scan(&id, &c); err != nil {
				return err
			}
			ids, causes = append(ids, id), append(causes, StopCause(c))
		}
		return rows.Err()
	})
	if err != nil {
		return err
	}
	for i, id := range ids {
		err := s.fenced(ctx, func(tx pgx.Tx) error {
			j, err := lockJob(ctx, tx, id)
			if err != nil || j == nil {
				return err
			}
			reason := fmt.Sprintf("time limit %s reached", j.TimeLimit)
			if causes[i] == StopLaunchTimeout {
				reason = "work did not start before the launch deadline"
			}
			return stop(ctx, tx, j, causes[i], "mist", reason, now)
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// applyRevocations stops every nonterminal job affected by a durable
// Identity revocation, then advances the cursor in the same transaction.
func (s *Scheduler) applyRevocations(ctx context.Context) error {
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		var cursor int64
		if err := tx.QueryRow(ctx,
			"SELECT value FROM scheduler_cursors WHERE name = 'revocation_seq' FOR UPDATE").Scan(&cursor); err != nil {
			return err
		}
		revs, last, err := s.identity.Revocations(ctx, tx, cursor)
		if err != nil {
			return err
		}
		now := s.clock.Now()
		for _, r := range revs {
			rows, err := tx.Query(ctx, `
				SELECT id FROM jobs WHERE username = $1 AND ($2 = '' OR team = $2)
				  AND state IN ('PENDING', 'LAUNCHING', 'RUNNING') ORDER BY id`, r.Username, r.Team)
			if err != nil {
				return err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
			if err != nil {
				return err
			}
			for _, id := range ids {
				j, err := lockJob(ctx, tx, id)
				if err != nil {
					return err
				}
				if err := stop(ctx, tx, j, StopRevoke, "identity", "access revoked", now); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(ctx, "UPDATE scheduler_cursors SET value = $1 WHERE name = 'revocation_seq'", last)
		return err
	})
	if errors.Is(err, identity.ErrUnavailable) {
		return nil // try again next cycle; admission reports AUTH_UNAVAILABLE meanwhile
	}
	return err
}
