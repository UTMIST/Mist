package scheduler

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/executor"
)

// The ledger is the scheduler's view of capacity: what each node reports,
// minus the Mist host reserve, minus every unreleased allocation. It never
// asks Kubernetes whether something fits.

type nodeInfo struct {
	Name, Executor, AdminState string
	Healthy, Quarantined       bool
	Capacity, Reserve, Used    executor.Resources
}

func (n *nodeInfo) free() executor.Resources { return n.Capacity.Sub(n.Reserve).Sub(n.Used) }

type partitionInfo struct {
	Name, Executor, State, Accelerator, Runtime string
	CapMilliCPU, CapMemory                      *int64
	Nodes                                       []string
	Used                                        executor.Resources
}

type allocation struct {
	Node, Partition, Username, Team, QOS string
	Res                                  executor.Resources
}

type limitRow struct {
	Scope, Subject, Partition, QOS, Resource string
	Max                                      int64
}

type ledger struct {
	nodes      map[string]*nodeInfo
	partitions map[string]*partitionInfo
	allocs     []allocation
	limits     []limitRow
}

func loadLedger(ctx context.Context, tx pgx.Tx) (*ledger, error) {
	l := &ledger{nodes: map[string]*nodeInfo{}, partitions: map[string]*partitionInfo{}}

	rows, err := tx.Query(ctx, `
		SELECT name, executor, admin_state, healthy, quarantined,
		       milli_cpu, memory_bytes, gpus, tt_boards, scratch_bytes, hugepages_bytes,
		       reserve_milli_cpu, reserve_memory_bytes
		FROM nodes`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n nodeInfo
		c := &n.Capacity
		if err := rows.Scan(&n.Name, &n.Executor, &n.AdminState, &n.Healthy, &n.Quarantined,
			&c.MilliCPU, &c.MemoryBytes, &c.GPUs, &c.TTBoards, &c.ScratchBytes, &c.HugepagesBytes,
			&n.Reserve.MilliCPU, &n.Reserve.MemoryBytes); err != nil {
			return nil, err
		}
		l.nodes[n.Name] = &n
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.Query(ctx, `
		SELECT p.name, p.executor, p.state, p.accelerator, p.runtime, p.cap_milli_cpu, p.cap_memory_bytes,
		       coalesce(array_agg(pn.node ORDER BY pn.node) FILTER (WHERE pn.node IS NOT NULL), '{}')
		FROM partitions p LEFT JOIN partition_nodes pn ON pn.partition = p.name
		GROUP BY p.name`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p partitionInfo
		if err := rows.Scan(&p.Name, &p.Executor, &p.State, &p.Accelerator, &p.Runtime,
			&p.CapMilliCPU, &p.CapMemory, &p.Nodes); err != nil {
			return nil, err
		}
		l.partitions[p.Name] = &p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Every unreleased allocation counts: launching, running, stopping,
	// cleaning and quarantined terminal jobs alike.
	rows, err = tx.Query(ctx, `
		SELECT node, partition, username, team, qos,
		       milli_cpu, memory_bytes, gpus, tt_boards, scratch_bytes, hugepages_bytes
		FROM allocations WHERE released_at IS NULL`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var a allocation
		r := &a.Res
		if err := rows.Scan(&a.Node, &a.Partition, &a.Username, &a.Team, &a.QOS,
			&r.MilliCPU, &r.MemoryBytes, &r.GPUs, &r.TTBoards, &r.ScratchBytes, &r.HugepagesBytes); err != nil {
			return nil, err
		}
		l.add(a)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = tx.Query(ctx, "SELECT scope, subject, partition, qos, resource, max FROM limits")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var r limitRow
		if err := rows.Scan(&r.Scope, &r.Subject, &r.Partition, &r.QOS, &r.Resource, &r.Max); err != nil {
			return nil, err
		}
		l.limits = append(l.limits, r)
	}
	return l, rows.Err()
}

// add charges an allocation to its node and partition.
func (l *ledger) add(a allocation) {
	l.allocs = append(l.allocs, a)
	if n := l.nodes[a.Node]; n != nil {
		n.Used = n.Used.Add(a.Res)
	}
	if p := l.partitions[a.Partition]; p != nil {
		p.Used = p.Used.Add(a.Res)
	}
}

// effectiveLimits picks, for each scope and resource, the most specific
// matching row: exact subject over '*', then exact partition, then exact
// QoS. User, team and partition results all apply together.
func (l *ledger) effectiveLimits(j *job) []limitRow {
	best := map[[2]string]limitRow{}
	score := map[[2]string]int{}
	for _, r := range l.limits {
		if r.Resource == "submitted_jobs" {
			continue // checked at submit
		}
		var subject string
		switch r.Scope {
		case "user":
			subject = j.Username
		case "team":
			subject = j.Team
		case "partition":
			subject = j.Partition
		}
		if (r.Subject != subject && r.Subject != "*") ||
			(r.Partition != j.Partition && r.Partition != "*") ||
			(r.QOS != j.QOS && r.QOS != "*") {
			continue
		}
		s := 0
		if r.Subject != "*" {
			s += 4
		}
		if r.Partition != "*" {
			s += 2
		}
		if r.QOS != "*" {
			s++
		}
		key := [2]string{r.Scope, r.Resource}
		if old, ok := score[key]; !ok || s > old {
			best[key], score[key] = r, s
		}
	}
	out := make([]limitRow, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b limitRow) int { // deterministic reason order
		return scopeRank(a.Scope) - scopeRank(b.Scope)
	})
	return out
}

func scopeRank(scope string) int {
	return map[string]int{"user": 0, "team": 1, "partition": 2}[scope]
}

func amount(r executor.Resources, resource string) int64 {
	switch resource {
	case "gpus":
		return r.GPUs
	case "tt_boards":
		return r.TTBoards
	case "milli_cpu":
		return r.MilliCPU
	case "memory_bytes":
		return r.MemoryBytes
	case "jobs":
		return 1
	}
	return 0
}

// checkLimits returns the pending reason and detail if j would exceed a
// concurrent limit, or "" if it is within all of them.
func (l *ledger) checkLimits(j *job) (PendingReason, string) {
	for _, r := range l.effectiveLimits(j) {
		var used int64
		for _, a := range l.allocs {
			if (r.Partition != "*" && a.Partition != r.Partition) || (r.QOS != "*" && a.QOS != r.QOS) {
				continue
			}
			switch r.Scope {
			case "user":
				if a.Username != j.Username {
					continue
				}
			case "team":
				if a.Team != j.Team {
					continue
				}
			}
			used += amount(a.Res, r.Resource)
		}
		want := amount(j.Res, r.Resource)
		if want == 0 || used+want <= r.Max {
			continue
		}
		detail := fmt.Sprintf("%s limit: %d of %d %s in use", r.Scope, used, r.Max, r.Resource)
		switch r.Scope {
		case "user":
			return PendingUserLimit, detail
		case "team":
			return PendingTeamLimit, detail
		default:
			return PendingQOSLimit, detail
		}
	}
	return "", ""
}

// place finds a node for j, or explains why there is none. usable reports
// whether the executor observed the node completely this cycle.
func (l *ledger) place(j *job, usable func(*nodeInfo) bool) (*nodeInfo, PendingReason, string) {
	p := l.partitions[j.Partition]
	if p == nil || p.State != "up" {
		return nil, PendingPartitionDown, "partition " + j.Partition + " is down"
	}
	if p.CapMilliCPU != nil && p.Used.MilliCPU+j.Res.MilliCPU > *p.CapMilliCPU ||
		p.CapMemory != nil && p.Used.MemoryBytes+j.Res.MemoryBytes > *p.CapMemory {
		return nil, PendingResources, "partition " + j.Partition + " is at its CPU or memory cap"
	}
	anyUp := false
	for _, name := range p.Nodes {
		n := l.nodes[name]
		if n == nil || n.AdminState != "up" || !n.Healthy || n.Quarantined || !usable(n) {
			continue
		}
		anyUp = true
		if n.free().Fits(j.Res) {
			return n, "", ""
		}
	}
	if !anyUp {
		return nil, PendingPartitionDown, "no schedulable node in partition " + j.Partition
	}
	return nil, PendingResources, waitingFor(j.Res)
}

func waitingFor(r executor.Resources) string {
	switch {
	case r.GPUs > 0:
		return fmt.Sprintf("waiting for %d GPU(s)", r.GPUs)
	case r.TTBoards > 0:
		return fmt.Sprintf("waiting for %d TT board(s)", r.TTBoards)
	}
	return fmt.Sprintf("waiting for %d CPU(s) and %d GiB memory", r.MilliCPU/1000, r.MemoryBytes>>30)
}
