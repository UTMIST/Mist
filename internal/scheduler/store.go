package scheduler

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UTMIST/Mist/internal/executor"
)

// ErrNotLeader is returned when a write is attempted by a scheduler whose
// leadership epoch is no longer current.
var ErrNotLeader = errors.New("scheduler: not the current leader")

// takeLeadership bumps the epoch. Any transaction still running for an
// older leader holds the epoch row FOR SHARE, so this waits for it, and
// every later fenced transaction from that leader fails.
func takeLeadership(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var epoch int64
	err := pool.QueryRow(ctx,
		"UPDATE scheduler_cursors SET value = value + 1 WHERE name = 'leader_epoch' RETURNING value").Scan(&epoch)
	return epoch, err
}

// fenced runs fn in a transaction that commits only while s is the current
// leader. Every scheduler-side write goes through it. The epoch row is
// share-locked first; it is outside the account→…→node order because only
// takeLeadership ever locks it exclusively, and that takes nothing else.
func (s *Scheduler) fenced(ctx context.Context, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var epoch int64
		if err := tx.QueryRow(ctx,
			"SELECT value FROM scheduler_cursors WHERE name = 'leader_epoch' FOR SHARE").Scan(&epoch); err != nil {
			return err
		}
		if epoch != s.epoch {
			return ErrNotLeader
		}
		return fn(tx)
	})
}

// job is one jobs row.
type job struct {
	ID               int64
	Username, Team   string
	Partition, QOS   string
	Name, Kind       string
	State            State
	PendingReason    *string
	Held             bool
	Res              executor.Resources
	TimeLimit, Grace time.Duration
	Node             *string
	ExecutionID      *string
	SpecHash         *string
	AuthGeneration   *int64
	AdmittedAt       *time.Time
	LaunchDeadline   *time.Time
	StopCause        *string
	ExitCode         *int
	ExitReason       *string
	ExitDetail       string
	SubmittedAt      time.Time
	StartedAt        *time.Time
	FinishedAt       *time.Time
	EndedAt          *time.Time
	CleanupPending   bool
	QOSPriority      int
}

const jobColumns = `j.id, j.username, j.team, j.partition, j.qos, j.name, j.kind, j.state, j.pending_reason, j.held,
	j.milli_cpu, j.memory_bytes, j.gpus, j.tt_boards, j.scratch_bytes, j.time_limit_seconds, j.signal_grace_seconds,
	j.node, j.execution_id::text, j.spec_hash, j.auth_generation, j.admitted_at, j.launch_deadline, j.stop_cause,
	j.exit_code, j.exit_reason, j.exit_detail, j.submitted_at, j.started_at, j.finished_at, j.ended_at,
	j.cleanup_pending, q.priority`

const jobFrom = ` FROM jobs j JOIN qos q ON q.name = j.qos `

func scanJob(row pgx.Row) (*job, error) {
	var j job
	var limit, grace int64
	var state string
	err := row.Scan(&j.ID, &j.Username, &j.Team, &j.Partition, &j.QOS, &j.Name, &j.Kind, &state, &j.PendingReason, &j.Held,
		&j.Res.MilliCPU, &j.Res.MemoryBytes, &j.Res.GPUs, &j.Res.TTBoards, &j.Res.ScratchBytes, &limit, &grace,
		&j.Node, &j.ExecutionID, &j.SpecHash, &j.AuthGeneration, &j.AdmittedAt, &j.LaunchDeadline, &j.StopCause,
		&j.ExitCode, &j.ExitReason, &j.ExitDetail, &j.SubmittedAt, &j.StartedAt, &j.FinishedAt, &j.EndedAt,
		&j.CleanupPending, &j.QOSPriority)
	if err != nil {
		return nil, err
	}
	j.State = State(state)
	j.TimeLimit = time.Duration(limit) * time.Second
	j.Grace = time.Duration(grace) * time.Second
	return &j, nil
}

// lockJob loads and row-locks a job. It returns (nil, nil) if there is none.
func lockJob(ctx context.Context, tx pgx.Tx, id int64) (*job, error) {
	j, err := scanJob(tx.QueryRow(ctx, "SELECT "+jobColumns+jobFrom+"WHERE j.id = $1 FOR UPDATE OF j", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return j, err
}

// transition moves a locked job to state `to` and records the event. Every
// state change goes through here, so job_events is the complete history.
func transition(ctx context.Context, tx pgx.Tx, j *job, to State, at time.Time, reason, actor string) error {
	if _, err := tx.Exec(ctx, "UPDATE jobs SET state = $2 WHERE id = $1", j.ID, string(to)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO job_events (job_id, at, from_state, to_state, reason, actor) VALUES ($1, $2, $3, $4, $5, $6)",
		j.ID, at, string(j.State), string(to), reason, actor); err != nil {
		return err
	}
	j.State = to
	return nil
}

// releaseAllocation frees a job's capacity. Callers must hold proof that
// nothing of the execution remains on the node.
func releaseAllocation(ctx context.Context, tx pgx.Tx, jobID int64, at time.Time, evidence string) error {
	_, err := tx.Exec(ctx,
		"UPDATE allocations SET released_at = $2, release_evidence = $3 WHERE job_id = $1 AND released_at IS NULL",
		jobID, at, evidence)
	return err
}

func setIntent(ctx context.Context, tx pgx.Tx, jobID int64, op OpKind, status OpStatus, at time.Time, lastErr string) error {
	_, err := tx.Exec(ctx,
		"UPDATE op_intents SET status = $3, updated_at = $4, last_error = $5 WHERE job_id = $1 AND op = $2",
		jobID, string(op), string(status), at, lastErr)
	return err
}

// closeOpenIntents marks every unfinished intent of a job done: the
// execution reached an outcome, so there is nothing left to send.
func closeOpenIntents(ctx context.Context, tx pgx.Tx, jobID int64, at time.Time, note string) error {
	_, err := tx.Exec(ctx,
		"UPDATE op_intents SET status = 'done', updated_at = $2, last_error = $3 WHERE job_id = $1 AND status IN ('pending', 'in_flight')",
		jobID, at, note)
	return err
}

func intentStatus(ctx context.Context, tx pgx.Tx, jobID int64, op OpKind) (OpStatus, error) {
	var st string
	err := tx.QueryRow(ctx, "SELECT status FROM op_intents WHERE job_id = $1 AND op = $2", jobID, string(op)).Scan(&st)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return OpStatus(st), err
}

// stop records a stop request for a locked job. The first committed cause
// wins; once a finish result is committed (CLEANING or terminal) nothing
// changes. It is used by API cancellation and by the scheduler itself
// (deadline, revocation, launch timeout), so it must not depend on being
// the leader.
func stop(ctx context.Context, tx pgx.Tx, j *job, cause StopCause, actor, reason string, now time.Time) error {
	switch j.State {
	case StatePending:
		if cause != StopCancel && cause != StopRevoke {
			return nil
		}
		if _, err := tx.Exec(ctx,
			"UPDATE jobs SET stop_cause = $2, stop_actor = $3, stop_requested_at = $4, ended_at = $4, pending_reason = NULL WHERE id = $1",
			j.ID, string(cause), actor, now); err != nil {
			return err
		}
		return transition(ctx, tx, j, StateCancelled, now, reason, actor)

	case StateLaunching, StateRunning:
		if j.StopCause != nil {
			return nil
		}
		if _, err := tx.Exec(ctx,
			"UPDATE jobs SET stop_cause = $2, stop_actor = $3, stop_requested_at = $4 WHERE id = $1",
			j.ID, string(cause), actor, now); err != nil {
			return err
		}
		c := string(cause)
		j.StopCause = &c
		if err := transition(ctx, tx, j, StateStopping, now, reason, actor); err != nil {
			return err
		}

		// A launch intent still 'pending' was never sent, so nothing can
		// exist on the node: finish now and release the allocation.
		if st, err := intentStatus(ctx, tx, j.ID, OpLaunch); err != nil {
			return err
		} else if st == OpPending {
			return finishUnsent(ctx, tx, j, now, "launch was never sent; nothing to clean up")
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO op_intents (job_id, execution_id, op, status, reason, created_at, updated_at)
			VALUES ($1, $2, 'terminate', 'pending', $3, $4, $4)
			ON CONFLICT (job_id, op) DO UPDATE SET status = 'pending', reason = $3, updated_at = $4`,
			j.ID, *j.ExecutionID, reason, now)
		return err
	}
	return nil // STOPPING keeps its first cause; CLEANING and terminal already have an outcome
}

// finishUnsent ends a STOPPING job whose execution provably never reached
// the executor.
func finishUnsent(ctx context.Context, tx pgx.Tx, j *job, now time.Time, why string) error {
	if err := closeOpenIntents(ctx, tx, j.ID, now, why); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		"UPDATE jobs SET finished_at = $2, exit_detail = $3 WHERE id = $1", j.ID, now, why); err != nil {
		return err
	}
	if err := transition(ctx, tx, j, StateCleaning, now, why, "mist"); err != nil {
		return err
	}
	if err := releaseAllocation(ctx, tx, j.ID, now, why); err != nil {
		return err
	}
	return endJob(ctx, tx, j, decide(j), now, "mist")
}

// endJob moves a CLEANING job to its terminal state.
func endJob(ctx context.Context, tx pgx.Tx, j *job, to State, now time.Time, actor string) error {
	if _, err := tx.Exec(ctx,
		"UPDATE jobs SET ended_at = $2, cleanup_pending = false WHERE id = $1", j.ID, now); err != nil {
		return err
	}
	return transition(ctx, tx, j, to, now, exitSummary(j, to), actor)
}

// decide picks the terminal state for a job whose cleanup is confirmed.
// A committed stop cause outranks the process result, because stop()
// refuses to set one after a finish result is committed.
func decide(j *job) State {
	reason := ""
	if j.ExitReason != nil {
		reason = *j.ExitReason
	}
	cause := ""
	if j.StopCause != nil {
		cause = *j.StopCause
	}
	switch {
	case cause == string(StopLaunchTimeout):
		return StateLaunchFailed
	case cause == string(StopCancel), cause == string(StopRevoke):
		return StateCancelled
	case cause == string(StopDeadline):
		return StateTimeout
	case reason == string(executor.ExitLaunchError), reason == string(executor.ExitLaunchTimeout):
		return StateLaunchFailed
	case reason == string(executor.ExitOutOfMemory):
		return StateOutOfMemory
	case reason == string(executor.ExitDeadline):
		return StateTimeout
	case reason == string(executor.ExitNormal) && j.ExitCode != nil && *j.ExitCode == 0:
		return StateCompleted
	}
	return StateFailed
}

func exitSummary(j *job, to State) string {
	switch to {
	case StateTimeout:
		return fmt.Sprintf("time limit %s reached", j.TimeLimit)
	case StateOutOfMemory:
		return fmt.Sprintf("exceeded %d MiB memory limit", j.Res.MemoryBytes>>20)
	case StateCompleted:
		return "exit 0"
	case StateFailed:
		if j.ExitCode != nil {
			return fmt.Sprintf("exit %d %s", *j.ExitCode, j.ExitDetail)
		}
	}
	return j.ExitDetail
}
