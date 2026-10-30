package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/executor"
)

// Request is a normalized submission. Filling defaults and validating
// paths, sizes and authorization happen in the API (internal/api); the
// scheduler stores what it is given and freezes it at admission.
// OutputPath may contain %j, which Submit replaces with the job ID.
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

// ErrIdempotencyConflict: the idempotency key was already used for a
// different request.
var ErrIdempotencyConflict = errors.New("scheduler: idempotency key reused with a different request")

// Idempotency identifies one client submission: the key the client chose
// and a hash of the request exactly as the client sent it.
type Idempotency struct {
	Key         string
	RequestHash string
}

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
	id, _, err := js.SubmitOnce(ctx, r, Idempotency{})
	return id, err
}

// LookupSubmission returns the job a previous submission with this key
// created, before the caller spends effort normalizing a retry.
func (js Jobs) LookupSubmission(ctx context.Context, username, key string) (jobID int64, requestHash string, found bool, err error) {
	err = js.Pool.QueryRow(ctx,
		"SELECT job_id, request_hash FROM submissions WHERE username = $1 AND idempotency_key = $2",
		username, key).Scan(&jobID, &requestHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", false, nil
	}
	return jobID, requestHash, err == nil, err
}

// SubmitOnce is Submit with an idempotency key. If the key was already
// used with the same request hash it returns the original job and
// replayed = true; with a different hash it returns ErrIdempotencyConflict.
// An empty key disables the check.
func (js Jobs) SubmitOnce(ctx context.Context, r Request, idem Idempotency) (id int64, replayed bool, err error) {
	if r.Resources.MilliCPU <= 0 || r.Resources.MemoryBytes <= 0 || r.Resources.ScratchBytes <= 0 ||
		r.TimeLimit < time.Second || r.SignalGrace < executor.MinSignalGrace || r.SignalGrace > executor.MaxSignalGrace {
		return 0, false, fmt.Errorf("scheduler: request is not normalized")
	}
	now := js.Clock.Now()
	err = pgx.BeginFunc(ctx, js.Pool, func(tx pgx.Tx) error {
		// Serialize one user's submissions so the queue-length check and
		// the idempotency lookup hold.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext('submit:' || $1))", r.Username); err != nil {
			return err
		}
		if idem.Key != "" {
			var hash string
			err := tx.QueryRow(ctx,
				"SELECT job_id, request_hash FROM submissions WHERE username = $1 AND idempotency_key = $2",
				r.Username, idem.Key).Scan(&id, &hash)
			switch {
			case err == nil && hash == idem.RequestHash:
				replayed = true
				return nil
			case err == nil:
				return ErrIdempotencyConflict
			case !errors.Is(err, pgx.ErrNoRows):
				return err
			}
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
		r.OutputPath = strings.ReplaceAll(r.OutputPath, "%j", strconv.FormatInt(id, 10))
		body, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO job_specs (job_id, request) VALUES ($1, $2)", id, body); err != nil {
			return err
		}
		if idem.Key != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO submissions (username, idempotency_key, request_hash, job_id, created_at)
				VALUES ($1, $2, $3, $4, $5)`, r.Username, idem.Key, idem.RequestHash, id, now); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx,
			"INSERT INTO job_events (job_id, at, to_state, reason, actor) VALUES ($1, $2, 'PENDING', 'submitted', $3)",
			id, now, r.Username); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "SELECT pg_notify($1, '')", notifyChannel)
		return err
	})
	return id, replayed, err
}

// Wake asks the leader for an early cycle, for example after a cleanup
// receipt was recorded. It is only a hint; the next tick does the same.
func (js Jobs) Wake(ctx context.Context) error {
	_, err := js.Pool.Exec(ctx, "SELECT pg_notify($1, '')", notifyChannel)
	return err
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
