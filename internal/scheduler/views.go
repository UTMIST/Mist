package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/executor"
)

// Read-only views for the API. They need no leadership and take no row
// locks; the API decides who may see what, and redacts.

// JobInfo is one job as the API shows it.
type JobInfo struct {
	ID               int64
	Username         string
	Team             string
	Partition        string
	QOS              string
	Name             string
	Kind             executor.Kind
	State            State
	PendingReason    *string
	PendingDetail    string
	Held             bool
	Resources        executor.Resources
	TimeLimit        time.Duration
	SignalGrace      time.Duration
	Node             *string
	AuthGeneration   *int64
	StopCause        *string
	ExitCode         *int
	ExitReason       *string
	ExitDetail       string
	OutputIncomplete bool
	SubmittedAt      time.Time
	StartedAt        *time.Time
	EndedAt          *time.Time
	CleanupPending   bool
	CleanupDetail    string
	Workdir          string
	OutputPath       string
}

const infoColumns = `j.id, j.username, j.team, j.partition, j.qos, j.name, j.kind, j.state, j.pending_reason,
	j.pending_detail, j.held, j.milli_cpu, j.memory_bytes, j.gpus, j.tt_boards, j.scratch_bytes,
	j.time_limit_seconds, j.signal_grace_seconds, j.node, j.auth_generation, j.stop_cause, j.exit_code,
	j.exit_reason, j.exit_detail, j.output_incomplete, j.submitted_at, j.started_at, j.ended_at,
	j.cleanup_pending, j.cleanup_detail, s.request->>'workdir', s.request->>'output_path'
	FROM jobs j JOIN job_specs s ON s.job_id = j.id JOIN qos q ON q.name = j.qos `

func scanInfo(row pgx.Row) (*JobInfo, error) {
	var j JobInfo
	var kind, state string
	var limit, grace int64
	err := row.Scan(&j.ID, &j.Username, &j.Team, &j.Partition, &j.QOS, &j.Name, &kind, &state, &j.PendingReason,
		&j.PendingDetail, &j.Held, &j.Resources.MilliCPU, &j.Resources.MemoryBytes, &j.Resources.GPUs,
		&j.Resources.TTBoards, &j.Resources.ScratchBytes, &limit, &grace, &j.Node, &j.AuthGeneration,
		&j.StopCause, &j.ExitCode, &j.ExitReason, &j.ExitDetail, &j.OutputIncomplete, &j.SubmittedAt,
		&j.StartedAt, &j.EndedAt, &j.CleanupPending, &j.CleanupDetail, &j.Workdir, &j.OutputPath)
	if err != nil {
		return nil, err
	}
	j.Kind, j.State = executor.Kind(kind), State(state)
	j.TimeLimit, j.SignalGrace = time.Duration(limit)*time.Second, time.Duration(grace)*time.Second
	return &j, nil
}

func collectInfos(rows pgx.Rows, err error) ([]*JobInfo, error) {
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (*JobInfo, error) { return scanInfo(r) })
}

// Get returns one job, or ErrNotFound.
func (js Jobs) Get(ctx context.Context, id int64) (*JobInfo, error) {
	j, err := scanInfo(js.Pool.QueryRow(ctx, "SELECT "+infoColumns+"WHERE j.id = $1", id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	return j, err
}

// ListFilter selects jobs, newest first. Empty fields match everything.
type ListFilter struct {
	Username string
	Team     string
	States   []State
	Before   int64 // only IDs below this (the pagination cursor)
	Limit    int
}

// List returns jobs matching f, newest first.
func (js Jobs) List(ctx context.Context, f ListFilter) ([]*JobInfo, error) {
	states := make([]string, len(f.States))
	for i, s := range f.States {
		states[i] = string(s)
	}
	return collectInfos(js.Pool.Query(ctx, "SELECT "+infoColumns+`
		WHERE ($1 = '' OR j.username = $1) AND ($2 = '' OR j.team = $2)
		  AND (cardinality($3::text[]) = 0 OR j.state = ANY($3)) AND ($4 = 0 OR j.id < $4)
		ORDER BY j.id DESC LIMIT $5`, f.Username, f.Team, states, f.Before, f.Limit))
}

// Queue returns every nonterminal job in admission order (QoS priority,
// then FIFO), optionally for one partition.
func (js Jobs) Queue(ctx context.Context, partition string) ([]*JobInfo, error) {
	return collectInfos(js.Pool.Query(ctx, "SELECT "+infoColumns+`
		WHERE j.state IN ('PENDING', 'LAUNCHING', 'RUNNING', 'STOPPING', 'CLEANING')
		  AND ($1 = '' OR j.partition = $1)
		ORDER BY q.priority DESC, j.submitted_at, j.id`, partition))
}

// Spec returns the normalized submission a job was created from.
func (js Jobs) Spec(ctx context.Context, id int64) (Request, error) {
	var r Request
	var body []byte
	err := js.Pool.QueryRow(ctx, "SELECT request FROM job_specs WHERE job_id = $1", id).Scan(&body)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	return r, json.Unmarshal(body, &r)
}

// JobEvent is one recorded state change.
type JobEvent struct {
	At     time.Time
	From   *string
	To     string
	Reason string
	Actor  string
}

// Events returns a job's state history, oldest first.
func (js Jobs) Events(ctx context.Context, id int64) ([]JobEvent, error) {
	rows, err := js.Pool.Query(ctx,
		"SELECT at, from_state, to_state, reason, actor FROM job_events WHERE job_id = $1 ORDER BY id", id)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (JobEvent, error) {
		var e JobEvent
		return e, r.Scan(&e.At, &e.From, &e.To, &e.Reason, &e.Actor)
	})
}

// Partition is a partition's configuration, defaults and live capacity.
// CPU and memory defaults and maximums are per accelerator when the
// partition has accelerators, otherwise per job.
type Partition struct {
	Name, State, Accelerator, Runtime string
	IsDefault                         bool
	MinAccelerators, MaxAccelerators  int64
	DefaultMilliCPU, MaxMilliCPU      int64
	DefaultMemory, MaxMemory          int64
	DefaultScratch, MaxScratch        int64
	DefaultTimeLimit, MaxTimeLimit    time.Duration
	DefaultSignalGrace                time.Duration
	CapMilliCPU, CapMemory            *int64
	Nodes                             []NodeStatus
}

// NodeStatus is one node of a partition. Total is the last reported
// capacity net of the Mist host reserve; Free also subtracts unreleased
// allocations from every partition sharing the node.
type NodeStatus struct {
	Name, AdminState, Detail string
	Healthy, Quarantined     bool
	Reported                 bool // the executor has reported this node at least once
	Total, Free              executor.Resources
}

// QOS is one quality-of-service class.
type QOS struct {
	Name         string
	Priority     int
	MaxTimeLimit *time.Duration
}

// Limit is one limits row.
type Limit struct {
	Scope, Subject, Partition, QOS, Resource string
	Max                                      int64
}

// Catalog is everything the API needs to normalize a submission and show
// partitions, read in one transaction so it is consistent.
type Catalog struct {
	Partitions []Partition
	QOS        []QOS
	Limits     []Limit
}

func (c *Catalog) Partition(name string) *Partition {
	for i := range c.Partitions {
		if c.Partitions[i].Name == name {
			return &c.Partitions[i]
		}
	}
	return nil
}

func (c *Catalog) QoS(name string) *QOS {
	for i := range c.QOS {
		if c.QOS[i].Name == name {
			return &c.QOS[i]
		}
	}
	return nil
}

// Catalog reads partitions, nodes, QoS and limits.
func (js Jobs) Catalog(ctx context.Context) (*Catalog, error) {
	c := &Catalog{}
	err := pgx.BeginTxFunc(ctx, js.Pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT name, state, accelerator, runtime, is_default, min_accelerators, max_accelerators,
			       default_milli_cpu, max_milli_cpu, default_memory_bytes, max_memory_bytes,
			       default_scratch_bytes, max_scratch_bytes, default_time_limit_seconds, max_time_limit_seconds,
			       default_signal_grace_seconds, cap_milli_cpu, cap_memory_bytes
			FROM partitions ORDER BY name`)
		if err != nil {
			return err
		}
		c.Partitions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (Partition, error) {
			var p Partition
			var dt, mt, dg int64
			err := r.Scan(&p.Name, &p.State, &p.Accelerator, &p.Runtime, &p.IsDefault, &p.MinAccelerators,
				&p.MaxAccelerators, &p.DefaultMilliCPU, &p.MaxMilliCPU, &p.DefaultMemory, &p.MaxMemory,
				&p.DefaultScratch, &p.MaxScratch, &dt, &mt, &dg, &p.CapMilliCPU, &p.CapMemory)
			p.DefaultTimeLimit, p.MaxTimeLimit = time.Duration(dt)*time.Second, time.Duration(mt)*time.Second
			p.DefaultSignalGrace = time.Duration(dg) * time.Second
			return p, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx, `
			SELECT pn.partition, n.name, n.admin_state, n.health_detail, n.healthy, n.quarantined,
			       n.observed_at IS NOT NULL,
			       n.milli_cpu - n.reserve_milli_cpu, n.memory_bytes - n.reserve_memory_bytes,
			       n.gpus, n.tt_boards, n.scratch_bytes, n.hugepages_bytes,
			       coalesce(sum(a.milli_cpu), 0), coalesce(sum(a.memory_bytes), 0), coalesce(sum(a.gpus), 0),
			       coalesce(sum(a.tt_boards), 0), coalesce(sum(a.scratch_bytes), 0), coalesce(sum(a.hugepages_bytes), 0)
			FROM partition_nodes pn JOIN nodes n ON n.name = pn.node
			LEFT JOIN allocations a ON a.node = n.name AND a.released_at IS NULL
			GROUP BY pn.partition, n.name ORDER BY n.name`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var part string
			var n NodeStatus
			var used executor.Resources
			t := &n.Total
			if err := rows.Scan(&part, &n.Name, &n.AdminState, &n.Detail, &n.Healthy, &n.Quarantined, &n.Reported,
				&t.MilliCPU, &t.MemoryBytes, &t.GPUs, &t.TTBoards, &t.ScratchBytes, &t.HugepagesBytes,
				&used.MilliCPU, &used.MemoryBytes, &used.GPUs, &used.TTBoards, &used.ScratchBytes, &used.HugepagesBytes); err != nil {
				return err
			}
			n.Free = n.Total.Sub(used)
			if p := c.Partition(part); p != nil {
				p.Nodes = append(p.Nodes, n)
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}

		rows, err = tx.Query(ctx, "SELECT name, priority, max_time_limit_seconds FROM qos ORDER BY priority, name")
		if err != nil {
			return err
		}
		c.QOS, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (QOS, error) {
			var q QOS
			var max *int64
			err := r.Scan(&q.Name, &q.Priority, &max)
			if max != nil {
				d := time.Duration(*max) * time.Second
				q.MaxTimeLimit = &d
			}
			return q, err
		})
		if err != nil {
			return err
		}

		rows, err = tx.Query(ctx,
			"SELECT scope, subject, partition, qos, resource, max FROM limits ORDER BY scope, subject, partition, qos, resource")
		if err != nil {
			return err
		}
		c.Limits, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Limit])
		return err
	})
	return c, err
}
