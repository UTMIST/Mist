package scheduler

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/db"
	"github.com/UTMIST/Mist/internal/db/dbtest"
	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/executor/fake"
	"github.com/UTMIST/Mist/internal/identity/identitytest"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

const (
	gpuNode = "utmist-z1opa08"
	cpuNode = "utmist-tt"
)

// harness wires a real Postgres, the fake executor, the fake clock and
// the identity fixture. Nothing touches a cluster.
type harness struct {
	t     *testing.T
	ctx   context.Context
	pool  *pgxpool.Pool
	clock *clock.Fake
	exec  *fake.Fake
	id    *identitytest.Fixture
	s     *Scheduler
	jobs  Jobs
}

func newHarness(t *testing.T) *harness {
	ctx := context.Background()
	pool, err := db.Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"INSERT INTO nodes (name, executor) VALUES ($1, 'k3s'), ($2, 'k3s')",
		"INSERT INTO partition_nodes VALUES ('gpu', $1), ('cpu', $2)",
	} {
		if _, err := pool.Exec(ctx, q, gpuNode, cpuNode); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE partitions SET runtime = 'runtime@sha256:test'"); err != nil {
		t.Fatal(err)
	}
	c := clock.NewFake(t0)
	ex := fake.New(fake.Config{Clock: c, Nodes: []executor.Node{
		{Name: gpuNode, Partitions: []string{"gpu"}, Healthy: true, Capacity: executor.Resources{
			MilliCPU: 32000, MemoryBytes: 128 << 30, GPUs: 2, ScratchBytes: 400 << 30}},
		{Name: cpuNode, Partitions: []string{"cpu"}, Healthy: true, Capacity: executor.Resources{
			MilliCPU: 32000, MemoryBytes: 256 << 30, ScratchBytes: 1 << 40}},
	}})
	t.Cleanup(ex.Close)
	h := &harness{t: t, ctx: ctx, pool: pool, clock: c, exec: ex, id: identitytest.New(),
		jobs: Jobs{Pool: pool, Clock: c}}
	h.s = h.newScheduler()
	return h
}

// newScheduler takes leadership, fencing any earlier scheduler.
func (h *harness) newScheduler() *Scheduler {
	s, err := New(h.ctx, Config{
		Pool:      h.pool,
		Identity:  h.id,
		Executors: map[string]executor.Executor{"k3s": h.exec},
		Clock:     h.clock,
	}, nil)
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h *harness) submit(user, team, partition string, mutate ...func(*Request)) int64 {
	h.t.Helper()
	r := Request{
		Username: user, Team: team, Partition: partition, QOS: "normal", Name: "test",
		Kind:        executor.KindBatch,
		TimeLimit:   time.Hour,
		SignalGrace: time.Minute,
		Workdir:     "/mist/teams/" + team + "/" + user,
		OutputPath:  "/mist/teams/" + team + "/" + user + "/mist.out",
		Script:      "python train.py\n",
	}
	if partition == "gpu" {
		r.Resources = executor.Resources{MilliCPU: 8000, MemoryBytes: 32 << 30, GPUs: 1, ScratchBytes: 100 << 30}
	} else {
		r.Resources = executor.Resources{MilliCPU: 2000, MemoryBytes: 8 << 30, ScratchBytes: 50 << 30}
	}
	for _, m := range mutate {
		m(&r)
	}
	id, err := h.jobs.Submit(h.ctx, r)
	if err != nil {
		h.t.Fatal(err)
	}
	return id
}

func (h *harness) cycle() {
	h.t.Helper()
	if err := h.s.Cycle(h.ctx); err != nil {
		h.t.Fatalf("cycle: %v", err)
	}
}

// drain handles every event the fake has delivered, and returns how many.
func (h *harness) drain() int {
	h.t.Helper()
	n := 0
	for {
		select {
		case ev := <-h.exec.Events():
			if err := h.s.HandleEvent(h.ctx, ev); err != nil {
				h.t.Fatalf("event %s: %v", ev.EventID, err)
			}
			n++
		case <-time.After(20 * time.Millisecond):
			return n
		}
	}
}

// settle cycles and handles events until nothing more happens at this
// instant of virtual time.
func (h *harness) settle() {
	h.t.Helper()
	for range 10 {
		h.cycle()
		if h.drain() == 0 {
			return
		}
	}
	h.t.Fatal("did not settle")
}

func (h *harness) advance(d time.Duration) {
	h.t.Helper()
	h.clock.Advance(d)
	h.settle()
}

func (h *harness) job(id int64) *job {
	h.t.Helper()
	j, err := scanJob(h.pool.QueryRow(h.ctx, "SELECT "+jobColumns+jobFrom+"WHERE j.id = $1", id))
	if err != nil {
		h.t.Fatal(err)
	}
	return j
}

func (h *harness) want(id int64, state State, reason ...PendingReason) {
	h.t.Helper()
	j := h.job(id)
	if j.State != state {
		h.t.Fatalf("job %d: state %s, want %s (history %s)", id, j.State, state, h.history(id))
	}
	if len(reason) > 0 && (j.PendingReason == nil || *j.PendingReason != string(reason[0])) {
		got := "<nil>"
		if j.PendingReason != nil {
			got = *j.PendingReason
		}
		h.t.Fatalf("job %d: pending reason %s, want %s", id, got, reason[0])
	}
}

// history returns transitions like ">PENDING PENDING>LAUNCHING ...".
func (h *harness) history(id int64) string {
	h.t.Helper()
	rows, err := h.pool.Query(h.ctx,
		"SELECT coalesce(from_state, ''), to_state FROM job_events WHERE job_id = $1 ORDER BY id", id)
	if err != nil {
		h.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var from, to string
		rows.Scan(&from, &to)
		out = append(out, from+">"+to)
	}
	return strings.Join(out, " ")
}

// allocation reports whether job id has an allocation and whether it is
// released and quarantined.
func (h *harness) allocation(id int64) (exists, released, quarantined bool) {
	h.t.Helper()
	var rel *time.Time
	err := h.pool.QueryRow(h.ctx,
		"SELECT released_at, quarantined FROM allocations WHERE job_id = $1", id).Scan(&rel, &quarantined)
	if err != nil {
		return false, false, false
	}
	return true, rel != nil, quarantined
}

func (h *harness) sql(sql string, args ...any) {
	h.t.Helper()
	if _, err := h.pool.Exec(h.ctx, sql, args...); err != nil {
		h.t.Fatalf("%s: %v", sql, err)
	}
}

func (h *harness) intent(id int64, op OpKind) (OpStatus, string) {
	h.t.Helper()
	var st, last string
	if err := h.pool.QueryRow(h.ctx, "SELECT status, last_error FROM op_intents WHERE job_id = $1 AND op = $2",
		id, string(op)).Scan(&st, &last); err != nil {
		return "", fmt.Sprint(err)
	}
	return OpStatus(st), last
}

const lifecycle = ">PENDING PENDING>LAUNCHING LAUNCHING>RUNNING RUNNING>CLEANING CLEANING>COMPLETED"
