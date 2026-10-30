package k3s

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UTMIST/Mist/internal/executor"
)

// record is one k3s_executions row: everything durable about an execution.
type record struct {
	ExecutionID, SpecHash, Node string
	JobID                       int64
	CreateIntentAt              time.Time
	ObjectUID                   string
	StartedAt, FinishedAt       *time.Time
	Result                      *executor.Result
	LogTail                     []byte
	TerminateRequestedAt        *time.Time
	LostAt                      *time.Time
	LostDetail                  string
	PodDeletedAt, CleanedAt     *time.Time
	CleanupEvidence             string
}

type store struct{ pool *pgxpool.Pool }

const recordColumns = `execution_id::text, spec_hash, node, job_id, create_intent_at, coalesce(object_uid, ''),
	started_at, finished_at, result, log_tail, terminate_requested_at, lost_at, lost_detail,
	pod_deleted_at, cleaned_at, cleanup_evidence`

func scanRecord(row pgx.Row) (*record, error) {
	var r record
	var result []byte
	err := row.Scan(&r.ExecutionID, &r.SpecHash, &r.Node, &r.JobID, &r.CreateIntentAt, &r.ObjectUID,
		&r.StartedAt, &r.FinishedAt, &result, &r.LogTail, &r.TerminateRequestedAt, &r.LostAt, &r.LostDetail,
		&r.PodDeletedAt, &r.CleanedAt, &r.CleanupEvidence)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if result != nil {
		r.Result = new(executor.Result)
		if err := json.Unmarshal(result, r.Result); err != nil {
			return nil, err
		}
	}
	return &r, nil
}

func (s store) byJob(ctx context.Context, jobID int64) (*record, error) {
	return scanRecord(s.pool.QueryRow(ctx, "SELECT "+recordColumns+" FROM k3s_executions WHERE job_id = $1", jobID))
}

func (s store) all(ctx context.Context) ([]*record, error) {
	rows, err := s.pool.Query(ctx, "SELECT "+recordColumns+" FROM k3s_executions ORDER BY job_id")
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*record, error) { return scanRecord(r) })
}

// createIntent records that a create is about to be attempted. It is a
// no-op if the same execution already has a row.
func (s store) createIntent(ctx context.Context, spec executor.LaunchSpec, at time.Time) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO k3s_executions (execution_id, job_id, spec_hash, node, create_intent_at)
			VALUES ($1, $2, $3, $4, $5) ON CONFLICT DO NOTHING`,
			spec.ExecutionID, spec.JobID, spec.SpecHash, spec.Node, at)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		return receipt(ctx, tx, spec.ExecutionID, executor.ReceiptCreateIntent, at, "", "")
	})
}

// update applies one change to a row and appends its receipt, atomically.
// cond guards the change (for example "started_at IS NULL"), so only the
// first observation of a fact is recorded. It reports whether it applied.
func (s store) update(ctx context.Context, execID string, kind executor.ReceiptKind, at time.Time,
	objectUID, detail, set, cond string, args ...any) (bool, error) {
	applied := false
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		q := "UPDATE k3s_executions SET " + set + " WHERE execution_id = $1"
		if cond != "" {
			q += " AND " + cond
		}
		tag, err := tx.Exec(ctx, q, append([]any{execID}, args...)...)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		applied = true
		if kind == "" {
			return nil
		}
		return receipt(ctx, tx, execID, kind, at, objectUID, detail)
	})
	return applied, err
}

func receipt(ctx context.Context, tx pgx.Tx, execID string, kind executor.ReceiptKind, at time.Time, uid, detail string) error {
	_, err := tx.Exec(ctx,
		"INSERT INTO k3s_receipts (execution_id, kind, at, object_uid, detail) VALUES ($1, $2, $3, $4, $5)",
		execID, string(kind), at, uid, detail)
	return err
}

func (s store) receipts(ctx context.Context, execID string) ([]executor.Receipt, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT kind, at, object_uid, detail FROM k3s_receipts WHERE execution_id = $1 ORDER BY id", execID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (executor.Receipt, error) {
		var x executor.Receipt
		var kind string
		err := r.Scan(&kind, &x.At, &x.ObjectUID, &x.Evidence)
		x.Kind, x.ExecutionID = executor.ReceiptKind(kind), execID
		return x, err
	})
}
