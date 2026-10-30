package api

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/scheduler"
)

func jobID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id < 1 {
		return 0, notFound("job not found")
	}
	return id, nil
}

func target(j *scheduler.JobInfo) identity.Target {
	return identity.Target{Team: j.Team, Owner: j.Username}
}

// viewableJob loads a job the caller may view under current membership.
// Anything else, including a job that does not exist, is not_found.
func (s *Server) viewableJob(ctx context.Context, p identity.Principal, id int64) (*scheduler.JobInfo, error) {
	j, err := s.Jobs.Get(ctx, id)
	if errors.Is(err, scheduler.ErrNotFound) {
		return nil, notFound("job not found")
	}
	if err != nil {
		return nil, err
	}
	if err := s.Authz.Authorize(ctx, p, identity.ActionViewJob, target(j)); err != nil {
		return nil, authzError(err, true, "job")
	}
	return j, nil
}

// cancellable: the job can still be stopped by a cancel request.
func cancellable(st scheduler.State) bool {
	return st == scheduler.StatePending || st == scheduler.StateLaunching || st == scheduler.StateRunning
}

// jobObject builds the job object, computing can_cancel from state and
// current authorization so clients never decide it.
func (s *Server) jobObject(ctx context.Context, p identity.Principal, j *scheduler.JobInfo) (Job, error) {
	can := false
	if cancellable(j.State) {
		err := s.Authz.Authorize(ctx, p, identity.ActionCancelJob, target(j))
		if errors.Is(err, identity.ErrUnavailable) {
			return Job{}, authzError(err, false, "")
		}
		can = err == nil
	}
	return wireJob(j, can), nil
}

// getJob handles GET /v1/jobs/{id}[?script=1].
func (s *Server) getJob(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	id, err := jobID(r)
	if err != nil {
		return err
	}
	j, err := s.viewableJob(r.Context(), p, id)
	if err != nil {
		return err
	}
	out, err := s.jobObject(r.Context(), p, j)
	if err != nil {
		return err
	}
	events, err := s.Jobs.Events(r.Context(), id)
	if err != nil {
		return err
	}
	out.Events = make([]Event, len(events))
	for i, e := range events {
		out.Events[i] = Event{At: e.At.UTC(), From: e.From, To: e.To, Reason: e.Reason, Actor: e.Actor}
	}
	if r.URL.Query().Get("script") == "1" {
		spec, err := s.Jobs.Spec(r.Context(), id)
		if err != nil {
			return err
		}
		out.Script, out.Args, out.Argv, out.Env = &spec.Script, spec.Args, spec.Argv, spec.Env
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// listJobs handles GET /v1/jobs. Without parameters it lists the caller's
// own jobs in teams they still belong to. ?team= lists a team's jobs for
// its members; ?all=1 lists every job for admins. ?state= takes a comma
// list of states, or "active".
func (s *Server) listJobs(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	q := r.URL.Query()
	f := scheduler.ListFilter{Team: q.Get("team"), Limit: 50}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			return invalid("limit", "limit must be 1–200")
		}
		f.Limit = n
	}
	if v := q.Get("cursor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 {
			return invalid("cursor", "invalid cursor")
		}
		f.Before = n
	}
	for _, st := range strings.Split(q.Get("state"), ",") {
		switch {
		case st == "":
		case st == "active":
			f.States = append(f.States, scheduler.StatePending, scheduler.StateLaunching,
				scheduler.StateRunning, scheduler.StateStopping, scheduler.StateCleaning)
		case knownState(scheduler.State(st)):
			f.States = append(f.States, scheduler.State(st))
		default:
			return invalid("state", "unknown state "+st)
		}
	}

	switch {
	case q.Get("all") == "1":
		if err := s.Authz.Authorize(r.Context(), p, identity.ActionAdmin, identity.Target{}); err != nil {
			return authzError(err, false, "")
		}
	case f.Team != "":
		if err := s.Authz.Authorize(r.Context(), p, identity.ActionViewJob, identity.Target{Team: f.Team}); err != nil {
			return authzError(err, false, "")
		}
	default:
		f.Username = p.Username
	}

	rows, err := s.Jobs.List(r.Context(), f)
	if err != nil {
		return err
	}
	out := List[Job]{Items: []Job{}}
	viewable := map[string]bool{} // own jobs: only teams the caller still belongs to
	for _, j := range rows {
		if f.Username != "" {
			ok, seen := viewable[j.Team]
			if !seen {
				err := s.Authz.Authorize(r.Context(), p, identity.ActionViewJob, target(j))
				if errors.Is(err, identity.ErrUnavailable) {
					return authzError(err, false, "")
				}
				ok = err == nil
				viewable[j.Team] = ok
			}
			if !ok {
				continue
			}
		}
		job, err := s.jobObject(r.Context(), p, j)
		if err != nil {
			return err
		}
		out.Items = append(out.Items, job)
	}
	if len(rows) == f.Limit {
		c := strconv.FormatInt(rows[len(rows)-1].ID, 10)
		out.NextCursor = &c
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

func knownState(st scheduler.State) bool {
	return slices.Contains([]scheduler.State{
		scheduler.StatePending, scheduler.StateLaunching, scheduler.StateRunning, scheduler.StateStopping,
		scheduler.StateCleaning, scheduler.StateCompleted, scheduler.StateFailed, scheduler.StateOutOfMemory,
		scheduler.StateLaunchFailed, scheduler.StateCancelled, scheduler.StateTimeout, scheduler.StateNodeFail,
	}, st)
}

// cancelJob handles POST /v1/jobs/{id}/cancel. It records the stop
// request and returns the job; it never waits for the job to end.
// Cancelling an ended job is not an error.
func (s *Server) cancelJob(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	id, err := jobID(r)
	if err != nil {
		return err
	}
	j, err := s.viewableJob(r.Context(), p, id)
	if err != nil {
		return err
	}
	if err := s.Authz.Authorize(r.Context(), p, identity.ActionCancelJob, target(j)); err != nil {
		if !errors.Is(err, identity.ErrUnavailable) {
			return &Error{http.StatusForbidden, CodeForbidden,
				"only the job's owner, a lead of team " + j.Team + " or an admin can cancel it", nil}
		}
		return authzError(err, false, "")
	}
	if !j.State.Terminal() {
		if _, err := s.Jobs.Cancel(r.Context(), id, p.Username); err != nil {
			return err
		}
		if j, err = s.Jobs.Get(r.Context(), id); err != nil {
			return err
		}
	}
	out, err := s.jobObject(r.Context(), p, j)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// queue handles GET /v1/queue[?partition=]. Every caller sees every
// queued and running job, but only as an allowlisted summary; identities
// are filled in only for jobs the caller could view anyway (D15).
func (s *Server) queue(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	rows, err := s.Jobs.Queue(r.Context(), r.URL.Query().Get("partition"))
	if err != nil {
		return err
	}
	now := s.Clock.Now()
	viewable := map[string]bool{}
	out := List[QueueEntry]{Items: make([]QueueEntry, 0, len(rows))}
	for _, j := range rows {
		ok, seen := viewable[j.Team]
		if !seen {
			err := s.Authz.Authorize(r.Context(), p, identity.ActionViewJob, identity.Target{Team: j.Team})
			if errors.Is(err, identity.ErrUnavailable) {
				return authzError(err, false, "")
			}
			ok = err == nil
			viewable[j.Team] = ok
		}
		e := QueueEntry{
			ID: j.ID, Partition: j.Partition, QOS: j.QOS, State: string(j.State),
			Resources:     wireResources(j.Resources),
			AgeSeconds:    max(int64(now.Sub(j.SubmittedAt)/time.Second), 0),
			PendingReason: publicReason(j.PendingReason),
		}
		if ok {
			e.User, e.Team, e.Name = &j.Username, &j.Team, &j.Name
		}
		out.Items = append(out.Items, e)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}

// publicReason passes only known pending-reason codes, never free text.
func publicReason(r *string) *string {
	if r == nil {
		return nil
	}
	switch scheduler.PendingReason(*r) {
	case scheduler.PendingPriority, scheduler.PendingResources, scheduler.PendingUserLimit,
		scheduler.PendingTeamLimit, scheduler.PendingQOSLimit, scheduler.PendingPartitionDown,
		scheduler.PendingProvisioning, scheduler.PendingAuthUnavailable, scheduler.PendingHeld:
		return r
	}
	return nil
}

// partitions handles GET /v1/partitions: configuration, defaults, live
// free capacity and the limits that apply to the caller.
func (s *Server) partitions(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	cat, err := s.Jobs.Catalog(r.Context())
	if err != nil {
		return err
	}
	admin := s.Authz.Authorize(r.Context(), p, identity.ActionAdmin, identity.Target{}) == nil
	member := map[string]bool{}
	applies := func(l scheduler.Limit) (bool, error) {
		switch {
		case admin || l.Subject == "*":
			return true, nil
		case l.Scope == "user":
			return l.Subject == p.Username, nil
		case l.Scope == "team":
			ok, seen := member[l.Subject]
			if !seen {
				err := s.Authz.Authorize(r.Context(), p, identity.ActionViewJob, identity.Target{Team: l.Subject})
				if errors.Is(err, identity.ErrUnavailable) {
					return false, authzError(err, false, "")
				}
				ok = err == nil
				member[l.Subject] = ok
			}
			return ok, nil
		}
		return false, nil
	}

	out := List[Partition]{Items: []Partition{}}
	for _, c := range cat.Partitions {
		pw := Partition{
			Name: c.Name, State: c.State, Default: c.IsDefault, Accelerator: c.Accelerator,
			MinAccelerators: c.MinAccelerators, MaxAccelerators: c.MaxAccelerators,
			PerAccelerator: c.Accelerator != "none",
			DefaultCPUs:    c.DefaultMilliCPU / 1000, MaxCPUs: c.MaxMilliCPU / 1000,
			DefaultMemoryBytes: c.DefaultMemory, MaxMemoryBytes: c.MaxMemory,
			DefaultScratchBytes: c.DefaultScratch, MaxScratchBytes: c.MaxScratch,
			DefaultTimeLimitSeconds:   int64(c.DefaultTimeLimit / time.Second),
			MaxTimeLimitSeconds:       int64(c.MaxTimeLimit / time.Second),
			DefaultSignalGraceSeconds: int64(c.DefaultSignalGrace / time.Second),
			CapMemoryBytes:            c.CapMemory,
			Nodes:                     []Node{},
			Limits:                    []Limit{},
		}
		if c.CapMilliCPU != nil {
			v := *c.CapMilliCPU / 1000
			pw.CapCPUs = &v
		}
		var free executor.Resources
		for _, n := range c.Nodes {
			pw.Nodes = append(pw.Nodes, Node{Name: n.Name, State: n.AdminState, Healthy: n.Healthy,
				Quarantined: n.Quarantined, Total: wireResources(n.Total), Free: wireResources(n.Free)})
			if n.AdminState == "up" && n.Healthy && !n.Quarantined && c.State == "up" {
				free = free.Add(n.Free)
			}
		}
		pw.Free = wireResources(free)
		for _, l := range cat.Limits {
			if l.Partition != "*" && l.Partition != c.Name {
				continue
			}
			ok, err := applies(l)
			if err != nil {
				return err
			}
			if ok {
				pw.Limits = append(pw.Limits, Limit{Scope: l.Scope, Subject: l.Subject, QOS: l.QOS,
					Resource: l.Resource, Max: l.Max})
			}
		}
		out.Items = append(out.Items, pw)
	}
	writeJSON(w, http.StatusOK, out)
	return nil
}
