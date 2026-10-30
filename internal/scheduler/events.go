package scheduler

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/executor"
)

// HandleEvent consumes one executor notification. The event ID is recorded
// in the same transaction as the state change, so a redelivered event is
// skipped. Handlers are also idempotent by state, because reconciliation
// replays observations under its own IDs, and order is not guaranteed.
func (s *Scheduler) HandleEvent(ctx context.Context, ev executor.Event) error {
	return s.fenced(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			"INSERT INTO consumed_events (event_id, job_id, consumed_at) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING",
			ev.EventID, ev.JobID, s.clock.Now())
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return s.apply(ctx, tx, ev)
	})
}

// observe applies an observation from reconciliation, without dedupe.
func (s *Scheduler) observe(ctx context.Context, ev executor.Event) error {
	return s.fenced(ctx, func(tx pgx.Tx) error { return s.apply(ctx, tx, ev) })
}

func (s *Scheduler) apply(ctx context.Context, tx pgx.Tx, ev executor.Event) error {
	j, err := lockJob(ctx, tx, ev.JobID)
	if err != nil || j == nil {
		return err
	}
	if j.ExecutionID == nil || *j.ExecutionID != ev.ExecutionID {
		s.log.Warn("ignoring event for another execution", "job", ev.JobID, "event", ev.EventID)
		return nil
	}
	switch ev.Kind {
	case executor.EventStarted:
		return s.onStarted(ctx, tx, j, ev)
	case executor.EventFinished, executor.EventLaunchFailed:
		return s.onFinished(ctx, tx, j, ev)
	case executor.EventExited:
		return s.onExited(ctx, tx, j, ev)
	case executor.EventLost:
		return s.onLost(ctx, tx, j, ev)
	}
	return nil
}

func (s *Scheduler) onStarted(ctx context.Context, tx pgx.Tx, j *job, ev executor.Event) error {
	// A fast job may have finished before this arrived: adopt the real
	// start time whatever the state, but only move LAUNCHING forward.
	if j.StartedAt == nil {
		if _, err := tx.Exec(ctx, "UPDATE jobs SET started_at = $2 WHERE id = $1", j.ID, ev.At); err != nil {
			return err
		}
		j.StartedAt = &ev.At
	}
	if j.State != StateLaunching {
		return nil
	}
	if err := setIntent(ctx, tx, j.ID, OpLaunch, OpDone, s.clock.Now(), ""); err != nil {
		return err
	}
	return transition(ctx, tx, j, StateRunning, s.clock.Now(), "started on "+ev.Node, "executor")
}

func (s *Scheduler) onFinished(ctx context.Context, tx pgx.Tx, j *job, ev executor.Event) error {
	switch j.State {
	case StateLaunching, StateRunning, StateStopping:
	case StateNodeFail:
		// A lost execution reported back: keep the result for display.
		if j.ExitReason == nil {
			return recordResult(ctx, tx, j, ev)
		}
		return nil
	default:
		return nil // CLEANING or terminal: the first result stands
	}
	if err := recordResult(ctx, tx, j, ev); err != nil {
		return err
	}
	if err := closeOpenIntents(ctx, tx, j.ID, s.clock.Now(), ""); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE jobs SET cleanup_pending = true WHERE id = $1", j.ID); err != nil {
		return err
	}
	return transition(ctx, tx, j, StateCleaning, s.clock.Now(), describe(ev), "executor")
}

func recordResult(ctx context.Context, tx pgx.Tx, j *job, ev executor.Event) error {
	reason := string(ev.Reason)
	if _, err := tx.Exec(ctx,
		"UPDATE jobs SET exit_code = $2, exit_reason = $3, exit_detail = $4, finished_at = $5 WHERE id = $1",
		j.ID, ev.ExitCode, reason, ev.Detail, ev.At); err != nil {
		return err
	}
	j.ExitCode, j.ExitReason, j.ExitDetail, j.FinishedAt = ev.ExitCode, &reason, ev.Detail, &ev.At
	return nil
}

func (s *Scheduler) onExited(ctx context.Context, tx pgx.Tx, j *job, ev executor.Event) error {
	now := s.clock.Now()
	switch j.State {
	case StateLaunching, StateRunning, StateStopping:
		// The Finished notification was lost or is late; Exited carries
		// the same outcome.
		if err := s.onFinished(ctx, tx, j, ev); err != nil {
			return err
		}
	case StateCleaning:
	default:
		// Terminal. A quarantined allocation (NODE_FAIL) is released by
		// this matching cleanup receipt; a repeated receipt changes nothing.
		if _, err := tx.Exec(ctx,
			"UPDATE jobs SET cleanup_pending = false, cleanup_detail = $2 WHERE id = $1 AND cleanup_pending",
			j.ID, ev.Detail); err != nil {
			return err
		}
		return releaseAllocation(ctx, tx, j.ID, now, ev.Detail)
	}
	if err := releaseAllocation(ctx, tx, j.ID, now, ev.Detail); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE jobs SET cleanup_detail = $2 WHERE id = $1", j.ID, ev.Detail); err != nil {
		return err
	}
	return endJob(ctx, tx, j, decide(j), now, "executor")
}

func (s *Scheduler) onLost(ctx context.Context, tx pgx.Tx, j *job, ev executor.Event) error {
	switch j.State {
	case StateLaunching, StateRunning, StateStopping, StateCleaning:
	default:
		return nil
	}
	now := s.clock.Now()
	// Keep the allocation, marked quarantined: the devices may still be in
	// use until recovery or fencing proves the old execution stopped.
	if _, err := tx.Exec(ctx,
		"UPDATE allocations SET quarantined = true WHERE job_id = $1 AND released_at IS NULL", j.ID); err != nil {
		return err
	}
	if err := closeOpenIntents(ctx, tx, j.ID, now, "execution lost"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE jobs SET ended_at = $2, cleanup_pending = true, cleanup_detail = $3,
			exit_reason = coalesce(exit_reason, $4), exit_detail = CASE WHEN exit_reason IS NULL THEN $3 ELSE exit_detail END
		WHERE id = $1`, j.ID, now, ev.Detail, string(executor.ExitNodeLost)); err != nil {
		return err
	}
	return transition(ctx, tx, j, StateNodeFail, now, ev.Detail, "executor")
}

func describe(ev executor.Event) string {
	if ev.Detail != "" {
		return string(ev.Reason) + ": " + ev.Detail
	}
	return string(ev.Reason)
}
