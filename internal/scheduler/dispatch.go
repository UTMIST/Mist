package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity"
)

type openIntent struct {
	JobID  int64
	Op     OpKind
	Node   string
	Reason string
}

// dispatch sends every open intent to its executor. Launch intents that
// were sent but not answered are resent with the same ExecutionID and
// SpecHash; the executor's repeat-safety makes that harmless.
func (s *Scheduler) dispatch(ctx context.Context) error {
	var open []openIntent
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT i.job_id, i.op, j.node, i.reason FROM op_intents i JOIN jobs j ON j.id = i.job_id
			WHERE i.status IN ('pending', 'in_flight') ORDER BY i.id`)
		if err != nil {
			return err
		}
		open, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (openIntent, error) {
			var o openIntent
			var op string
			err := r.Scan(&o.JobID, &op, &o.Node, &o.Reason)
			o.Op = OpKind(op)
			return o, err
		})
		return err
	})
	if err != nil {
		return err
	}
	for _, o := range open {
		if err := s.leaderCheck(); err != nil {
			return err
		}
		ex := s.executorFor(o.Node)
		if ex == nil {
			continue // executor not observed; the intent waits
		}
		switch o.Op {
		case OpLaunch:
			err = s.dispatchLaunch(ctx, ex, o)
		case OpTerminate:
			err = s.dispatchTerminate(ctx, ex, o)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Scheduler) dispatchLaunch(ctx context.Context, ex executor.Executor, o openIntent) error {
	now := s.clock.Now()
	var spec *executor.LaunchSpec
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		// Read the job without locking to learn who to authorize, then take
		// locks in order: Identity's rows first, then the job.
		var user, team string
		var gen int64
		if err := tx.QueryRow(ctx, "SELECT username, team, auth_generation FROM jobs WHERE id = $1",
			o.JobID).Scan(&user, &team, &gen); err != nil {
			return err
		}
		_, grantErr := s.identity.LockGrant(ctx, tx, user, team, gen)
		j, err := lockJob(ctx, tx, o.JobID)
		if err != nil {
			return err
		}
		if j.State != StateLaunching || j.StopCause != nil {
			return nil // a stop owns this execution now
		}
		switch {
		case errors.Is(grantErr, identity.ErrRevoked), errors.Is(grantErr, identity.ErrStaleGeneration):
			// Never replay an unstarted launch under an older authorization.
			return stop(ctx, tx, j, StopRevoke, "identity", "authorization changed before launch", now)
		case grantErr != nil:
			return nil // unavailable or not provisioned: retry; the launch deadline bounds it
		case !now.Before(*j.LaunchDeadline):
			return stop(ctx, tx, j, StopLaunchTimeout, "mist", "launch deadline passed", now)
		}
		var body []byte
		if err := tx.QueryRow(ctx, "SELECT launch_spec FROM job_specs WHERE job_id = $1", j.ID).Scan(&body); err != nil {
			return err
		}
		spec = new(executor.LaunchSpec)
		if err := json.Unmarshal(body, spec); err != nil {
			return err
		}
		spec.SpecHash = *j.SpecHash
		_, err = tx.Exec(ctx, `
			UPDATE op_intents SET status = 'in_flight', attempts = attempts + 1, updated_at = $2
			WHERE job_id = $1 AND op = 'launch'`, j.ID, now)
		return err
	})
	if err != nil || spec == nil {
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, s.callTimeout)
	callErr := ex.Launch(callCtx, *spec)
	cancel()

	return s.fenced(ctx, func(tx pgx.Tx) error {
		now := s.clock.Now()
		switch {
		case callErr == nil, errors.Is(callErr, executor.ErrExecutionStarted):
			// It exists, or existed: events and List say what happened.
			_, err := tx.Exec(ctx,
				"UPDATE op_intents SET status = 'done', updated_at = $2, last_error = '' WHERE job_id = $1 AND op = 'launch' AND status = 'in_flight'",
				o.JobID, now)
			return err
		case errors.Is(callErr, executor.ErrSpecConflict):
			// A different execution claims this job: a bug or a stale writer.
			// Stop using the node until an admin looks.
			if err := setIntent(ctx, tx, o.JobID, OpLaunch, OpFailed, now, callErr.Error()); err != nil {
				return err
			}
			_, err := tx.Exec(ctx,
				"UPDATE nodes SET quarantined = true, quarantine_detail = $2 WHERE name = $1",
				spec.Node, "spec conflict for job execution "+spec.ExecutionID)
			return err
		default:
			// Ambiguous: it may or may not exist. Stay in_flight and retry.
			_, err := tx.Exec(ctx,
				"UPDATE op_intents SET last_error = $2, updated_at = $3 WHERE job_id = $1 AND op = 'launch'",
				o.JobID, callErr.Error(), now)
			return err
		}
	})
}

func (s *Scheduler) dispatchTerminate(ctx context.Context, ex executor.Executor, o openIntent) error {
	var grace time.Duration
	send := false
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		j, err := lockJob(ctx, tx, o.JobID)
		if err != nil {
			return err
		}
		if j.State != StateStopping {
			return setIntent(ctx, tx, j.ID, OpTerminate, OpDone, s.clock.Now(), "no longer stopping")
		}
		grace, send = j.Grace, true
		_, err = tx.Exec(ctx, `
			UPDATE op_intents SET status = 'in_flight', attempts = attempts + 1, updated_at = $2
			WHERE job_id = $1 AND op = 'terminate'`, j.ID, s.clock.Now())
		return err
	})
	if err != nil || !send {
		return err
	}

	callCtx, cancel := context.WithTimeout(ctx, s.callTimeout)
	callErr := ex.Terminate(callCtx, o.JobID, grace, o.Reason)
	cancel()

	return s.fenced(ctx, func(tx pgx.Tx) error {
		if callErr != nil {
			return setIntent(ctx, tx, o.JobID, OpTerminate, OpPending, s.clock.Now(), callErr.Error())
		}
		_, err := tx.Exec(ctx,
			"UPDATE op_intents SET status = 'done', updated_at = $2 WHERE job_id = $1 AND op = 'terminate' AND status = 'in_flight'",
			o.JobID, s.clock.Now())
		return err
	})
}
