package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/executor"
)

// Request is a normalized submission. Filling defaults and validating
// paths, sizes and authorization happen in the API (M1 step 6); the
// scheduler stores what it is given and freezes it at admission.
type Request struct {
	Username    string             `json:"username"`
	Team        string             `json:"team"`
	Partition   string             `json:"partition"`
	QOS         string             `json:"qos"`
	Name        string             `json:"name"`
	Kind        executor.Kind      `json:"kind"`
	Resources   executor.Resources `json:"resources"`
	TimeLimit   time.Duration      `json:"time_limit"`
	SignalGrace time.Duration      `json:"signal_grace"`
	Workdir     string             `json:"workdir"`
	OutputPath  string             `json:"output_path"`
	Script      string             `json:"script"`
	Args        []string           `json:"args"`
	Argv        []string           `json:"argv"`
	Env         map[string]string  `json:"env"`
}

// ErrLimitExceeded is a submit-time limit (queue length) refusal.
var ErrLimitExceeded = errors.New("scheduler: limit exceeded")

// ErrNotFound: no such job.
var ErrNotFound = errors.New("scheduler: job not found")

// Jobs is the API-side handle: submit and cancel. It does not need
// leadership; row locks serialize it with the scheduler.
type Jobs struct {
	Pool  *pgxpool.Pool
	Clock clock.Clock
}

// notifyChannel wakes the leader for an early cycle.
const notifyChannel = "mist_scheduler"

// Submit inserts a PENDING job and returns its ID.
func (js Jobs) Submit(ctx context.Context, r Request) (int64, error) {
	if r.Resources.MilliCPU <= 0 || r.Resources.MemoryBytes <= 0 || r.Resources.ScratchBytes <= 0 ||
		r.TimeLimit < time.Second || r.SignalGrace < executor.MinSignalGrace || r.SignalGrace > executor.MaxSignalGrace {
		return 0, fmt.Errorf("scheduler: request is not normalized")
	}
	body, err := json.Marshal(r)
	if err != nil {
		return 0, err
	}
	now := js.Clock.Now()
	var id int64
	err = pgx.BeginFunc(ctx, js.Pool, func(tx pgx.Tx) error {
		// Serialize one user's submissions so the queue-length check holds.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('submit:' || $1))", r.Username); err != nil {
			return err
		}
		var max *int64
		err := tx.QueryRow(ctx, `
			SELECT max FROM limits
			WHERE scope = 'user' AND resource = 'submitted_jobs'
			  AND subject IN ($1, '*') AND partition IN ($2, '*') AND qos IN ($3, '*')
			ORDER BY (subject <> '*') DESC, (partition <> '*') DESC, (qos <> '*') DESC LIMIT 1`,
			r.Username, r.Partition, r.QOS).Scan(&max)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if max != nil {
			var n int64
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM jobs
				WHERE username = $1 AND state IN ('PENDING', 'LAUNCHING', 'RUNNING', 'STOPPING', 'CLEANING')`,
				r.Username).Scan(&n); err != nil {
				return err
			}
			if n >= *max {
				return fmt.Errorf("%w: %s already has %d queued or running jobs", ErrLimitExceeded, r.Username, n)
			}
		}
		res := r.Resources
		if err := tx.QueryRow(ctx, `
			INSERT INTO jobs (username, team, partition, qos, name, kind, state, pending_reason,
				milli_cpu, memory_bytes, gpus, tt_boards, scratch_bytes, time_limit_seconds, signal_grace_seconds, submitted_at)
			VALUES ($1, $2, $3, $4, $5, $6, 'PENDING', 'PRIORITY', $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING id`,
			r.Username, r.Team, r.Partition, r.QOS, r.Name, string(r.Kind),
			res.MilliCPU, res.MemoryBytes, res.GPUs, res.TTBoards, res.ScratchBytes,
			int64(r.TimeLimit/time.Second), int64(r.SignalGrace/time.Second), now).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO job_specs (job_id, request) VALUES ($1, $2)", id, body); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO job_events (job_id, at, to_state, reason, actor) VALUES ($1, $2, 'PENDING', 'submitted', $3)",
			id, now, r.Username); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "SELECT pg_notify($1, '')", notifyChannel)
		return err
	})
	return id, err
}

// Cancel records a stop request and returns the job's state afterwards.
// It never waits for termination and never frees an allocation that might
// still be in use. Repeating it is harmless.
func (js Jobs) Cancel(ctx context.Context, jobID int64, actor string) (State, error) {
	var state State
	err := pgx.BeginFunc(ctx, js.Pool, func(tx pgx.Tx) error {
		j, err := lockJob(ctx, tx, jobID)
		if err != nil {
			return err
		}
		if j == nil {
			return ErrNotFound
		}
		if err := stop(ctx, tx, j, StopCancel, actor, "cancelled by "+actor, js.Clock.Now()); err != nil {
			return err
		}
		state = j.State
		_, err = tx.Exec(ctx, "SELECT pg_notify($1, '')", notifyChannel)
		return err
	})
	return state, err
}
