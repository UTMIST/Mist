package scheduler

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity"
)

// admit walks pending jobs in priority order (QoS weight, then FIFO) and
// admits every one that fits. M1 lets a lower-priority job start when the
// one ahead of it does not fit; this can starve large jobs until M3.
func (s *Scheduler) admit(ctx context.Context) error {
	var l *ledger
	var pending []*job
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		var err error
		if l, err = loadLedger(ctx, tx); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+jobColumns+jobFrom+
			"WHERE j.state = 'PENDING' ORDER BY q.priority DESC, j.submitted_at, j.id")
		if err != nil {
			return err
		}
		pending, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (*job, error) { return scanJob(r) })
		return err
	})
	if err != nil {
		return err
	}

	for _, j := range pending {
		if err := s.leaderCheck(); err != nil {
			return err
		}
		if j.Held {
			if err := s.setPending(ctx, j, PendingHeld, "held by an administrator"); err != nil {
				return err
			}
			continue
		}
		if reason, detail := l.checkLimits(j); reason != "" {
			if err := s.setPending(ctx, j, reason, detail); err != nil {
				return err
			}
			continue
		}
		node, reason, detail := l.place(j, s.usable)
		if node == nil {
			if err := s.setPending(ctx, j, reason, detail); err != nil {
				return err
			}
			continue
		}
		admitted, err := s.admitOne(ctx, j, node.Name)
		if err != nil {
			return err
		}
		if admitted {
			l.add(allocation{Node: node.Name, Partition: j.Partition, Username: j.Username,
				Team: j.Team, QOS: j.QOS, Res: j.Res})
		}
	}
	return nil
}

func (s *Scheduler) setPending(ctx context.Context, j *job, reason PendingReason, detail string) error {
	if j.PendingReason != nil && *j.PendingReason == string(reason) {
		return nil // pending_detail may lag; the reason is what users act on
	}
	return s.fenced(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			"UPDATE jobs SET pending_reason = $2, pending_detail = $3 WHERE id = $1 AND state = 'PENDING'",
			j.ID, string(reason), detail)
		return err
	})
}

// admitOne commits allocation + LAUNCHING + node + launch intent in one
// transaction. The executor is called later, by dispatch, after commit.
func (s *Scheduler) admitOne(ctx context.Context, j *job, nodeName string) (bool, error) {
	now := s.clock.Now()
	var grantErr error
	admitted, mismatch := false, false
	err := s.fenced(ctx, func(tx pgx.Tx) error {
		// Lock order: account, membership (Identity), then job, then node.
		grant, err := s.identity.LockGrant(ctx, tx, j.Username, j.Team, 0)
		if err != nil {
			grantErr = err
			return nil
		}
		cur, err := lockJob(ctx, tx, j.ID)
		if err != nil {
			return err
		}
		if cur == nil || cur.State != StatePending || cur.Held {
			return nil // cancelled or held since the ledger was read
		}
		var p partitionInfo
		var cap executor.Resources
		var reserveCPU, reserveMem int64
		if err := tx.QueryRow(ctx, `
			SELECT n.milli_cpu, n.memory_bytes, n.gpus, n.tt_boards, n.scratch_bytes, n.hugepages_bytes,
			       n.reserve_milli_cpu, n.reserve_memory_bytes, p.runtime
			FROM nodes n, partitions p WHERE n.name = $1 AND p.name = $2 FOR UPDATE OF n`,
			nodeName, j.Partition).Scan(&cap.MilliCPU, &cap.MemoryBytes, &cap.GPUs, &cap.TTBoards,
			&cap.ScratchBytes, &cap.HugepagesBytes, &reserveCPU, &reserveMem, &p.Runtime); err != nil {
			return err
		}
		// Recheck the node against the database, not only the cycle's copy.
		var used executor.Resources
		if err := tx.QueryRow(ctx, `
			SELECT coalesce(sum(milli_cpu), 0), coalesce(sum(memory_bytes), 0), coalesce(sum(gpus), 0),
			       coalesce(sum(tt_boards), 0), coalesce(sum(scratch_bytes), 0), coalesce(sum(hugepages_bytes), 0)
			FROM allocations WHERE node = $1 AND released_at IS NULL`, nodeName).Scan(
			&used.MilliCPU, &used.MemoryBytes, &used.GPUs, &used.TTBoards, &used.ScratchBytes, &used.HugepagesBytes); err != nil {
			return err
		}
		free := cap.Sub(executor.Resources{MilliCPU: reserveCPU, MemoryBytes: reserveMem}).Sub(used)
		if !free.Fits(j.Res) {
			mismatch = true // the cycle's copy is stale; wait and recompute
			return nil
		}

		var req Request
		var body []byte
		if err := tx.QueryRow(ctx, "SELECT request FROM job_specs WHERE job_id = $1", j.ID).Scan(&body); err != nil {
			return err
		}
		if err := json.Unmarshal(body, &req); err != nil {
			return err
		}
		spec := executor.LaunchSpec{
			JobID:          j.ID,
			ExecutionID:    executor.NewExecutionID(),
			AuthGeneration: grant.Generation,
			LaunchDeadline: now.Add(s.launchTimeout),
			Kind:           req.Kind,
			JobName:        j.Name,
			Partition:      j.Partition,
			QOS:            j.QOS,
			Node:           nodeName,
			User:           executor.User{Name: grant.Account.Username, UID: grant.Account.UID, PersonalGID: grant.Account.PersonalGID},
			TeamSlug:       grant.Team.Slug,
			TeamGID:        grant.Team.GID,
			ExtraGIDs:      grant.ExtraGIDs,
			Resources:      j.Res,
			Mounts: []executor.Mount{
				{VolumeID: grant.Team.VolumeID, Source: "/teams/" + grant.Team.Slug, Target: "/mist/teams/" + grant.Team.Slug},
				{VolumeID: grant.Team.VolumeID, Source: "/software", Target: "/mist/software", ReadOnly: true},
			},
			Workdir:     req.Workdir,
			OutputPath:  req.OutputPath,
			Script:      req.Script,
			Args:        req.Args,
			Argv:        req.Argv,
			Env:         req.Env,
			Runtime:     p.Runtime,
			TimeLimit:   j.TimeLimit,
			SignalGrace: j.Grace,
		}
		spec.Seal()
		if err := spec.Validate(); err != nil {
			// A normalization bug, not a capacity question. Leave it pending
			// with an explanation an admin can act on.
			_, err2 := tx.Exec(ctx,
				"UPDATE jobs SET pending_detail = $2 WHERE id = $1", j.ID, "cannot build launch spec: "+err.Error())
			return err2
		}
		frozen, err := json.Marshal(spec)
		if err != nil {
			return err
		}

		r := j.Res
		if _, err := tx.Exec(ctx, `
			INSERT INTO allocations (job_id, node, partition, username, team, qos,
				milli_cpu, memory_bytes, gpus, tt_boards, scratch_bytes, hugepages_bytes, device_ids, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			j.ID, nodeName, j.Partition, j.Username, j.Team, j.QOS,
			r.MilliCPU, r.MemoryBytes, r.GPUs, r.TTBoards, r.ScratchBytes, r.HugepagesBytes, append([]string{}, spec.DeviceIDs...), now); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, "UPDATE job_specs SET launch_spec = $2 WHERE job_id = $1", j.ID, frozen); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE jobs SET node = $2, execution_id = $3, spec_hash = $4, auth_generation = $5,
				admitted_at = $6, launch_deadline = $7, pending_reason = NULL, pending_detail = ''
			WHERE id = $1`,
			j.ID, nodeName, spec.ExecutionID, spec.SpecHash, grant.Generation, now, spec.LaunchDeadline); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO op_intents (job_id, execution_id, op, status, created_at, updated_at)
			VALUES ($1, $2, 'launch', 'pending', $3, $3)`, j.ID, spec.ExecutionID, now); err != nil {
			return err
		}
		if err := transition(ctx, tx, cur, StateLaunching, now, "admitted to "+nodeName, "mist"); err != nil {
			return err
		}
		admitted = true
		return nil
	})
	if err != nil {
		return false, err
	}

	switch {
	case mismatch:
		s.log.Warn("ledger disagrees with database", "job", j.ID, "node", nodeName)
		err = s.setPending(ctx, j, PendingResources, waitingFor(j.Res))
	case grantErr == nil:
	case errors.Is(grantErr, identity.ErrRevoked):
		err = s.fenced(ctx, func(tx pgx.Tx) error {
			cur, err := lockJob(ctx, tx, j.ID)
			if err != nil || cur == nil {
				return err
			}
			return stop(ctx, tx, cur, StopRevoke, "identity", "access to team "+j.Team+" revoked", now)
		})
	case errors.Is(grantErr, identity.ErrNotProvisioned):
		err = s.setPending(ctx, j, PendingProvisioning, "team folders are not provisioned yet")
	case errors.Is(grantErr, identity.ErrUnavailable):
		err = s.setPending(ctx, j, PendingAuthUnavailable, "cannot confirm current team membership")
	default:
		err = grantErr
	}
	return admitted, err
}
