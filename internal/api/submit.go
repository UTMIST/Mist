package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity"
	"github.com/UTMIST/Mist/internal/jobentry"
	"github.com/UTMIST/Mist/internal/scheduler"
)

// Submission bounds (api.md#submitting-post-v1jobs).
const (
	maxBody       = 4 << 20
	maxScript     = 1 << 20
	maxEnvTotal   = 64 << 10
	maxArgsTotal  = 128 << 10
	maxNameRunes  = 128
	maxPath       = 4096
	maxKey        = 200
	maxCPUs       = 1 << 20
	teamsRoot     = "/mist/teams/"
	defaultOutput = "mist-%j.out"
)

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	envPattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// submit handles POST /v1/jobs.
func (s *Server) submit(w http.ResponseWriter, r *http.Request, p identity.Principal) error {
	key := r.Header.Get("Idempotency-Key")
	if key == "" || len(key) > maxKey || strings.IndexFunc(key, func(c rune) bool { return c < 0x21 || c > 0x7e }) >= 0 {
		return invalid("Idempotency-Key", "an Idempotency-Key header of 1–200 visible ASCII characters is required")
	}
	in, err := decodeSubmit(w, r)
	if err != nil {
		return err
	}
	canon, _ := json.Marshal(in)
	sum := sha256.Sum256(canon)
	idem := scheduler.Idempotency{Key: key, RequestHash: hex.EncodeToString(sum[:])}

	// A retry returns the original job, even if defaults have changed or
	// the request would no longer be valid.
	if id, hash, found, err := s.Jobs.LookupSubmission(r.Context(), p.Username, key); err != nil {
		return err
	} else if found {
		if hash != idem.RequestHash {
			return conflictKey()
		}
		return s.replySubmitted(w, r, p, id, true)
	}

	cat, err := s.Jobs.Catalog(r.Context())
	if err != nil {
		return err
	}
	req, err := s.normalize(r.Context(), p, in, cat)
	if err != nil {
		return err
	}
	id, replayed, err := s.Jobs.SubmitOnce(r.Context(), req, idem)
	switch {
	case errors.Is(err, scheduler.ErrIdempotencyConflict):
		return conflictKey()
	case errors.Is(err, scheduler.ErrLimitExceeded):
		return limitExceeded("", strings.TrimPrefix(err.Error(), scheduler.ErrLimitExceeded.Error()+": "))
	case err != nil:
		return err
	}
	return s.replySubmitted(w, r, p, id, replayed)
}

func conflictKey() error {
	return &Error{http.StatusConflict, CodeConflict,
		"this Idempotency-Key was already used for a different request; use a new key to submit another job", nil}
}

func (s *Server) replySubmitted(w http.ResponseWriter, r *http.Request, p identity.Principal, id int64, replayed bool) error {
	j, err := s.viewableJob(r.Context(), p, id)
	if err != nil {
		return err
	}
	out, err := s.jobObject(r.Context(), p, j)
	if err != nil {
		return err
	}
	status := http.StatusCreated
	if replayed {
		w.Header().Set("Idempotent-Replayed", "true")
		status = http.StatusOK
	}
	writeJSON(w, status, out)
	return nil
}

func decodeSubmit(w http.ResponseWriter, r *http.Request) (SubmitRequest, error) {
	var in SubmitRequest
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return in, invalid("", fmt.Sprintf("request body is larger than %d bytes", maxBody))
		}
		return in, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, invalid("", "request body: "+err.Error())
	}
	if dec.More() {
		return in, invalid("", "request body has trailing data")
	}
	return in, nil
}

// normalize validates a submission and fills every default from the
// catalog, giving the complete request the scheduler stores. Checks that
// depend on current state (membership, capacity) are repeated at
// admission; these ones make bad requests fail early and clearly.
func (s *Server) normalize(ctx context.Context, p identity.Principal, in SubmitRequest, cat *scheduler.Catalog) (scheduler.Request, error) {
	var req scheduler.Request

	// What runs.
	switch executor.Kind(in.Kind) {
	case executor.KindBatch:
		if in.Script == "" {
			return req, invalid("script", "a batch job needs a script")
		}
		if len(in.Argv) > 0 {
			return req, invalid("argv", "a batch job takes script and args, not argv")
		}
	case executor.KindCommand:
		if len(in.Argv) == 0 || in.Argv[0] == "" {
			return req, invalid("argv", "a command job needs argv, starting with the program to run")
		}
		if in.Script != "" || len(in.Args) > 0 {
			return req, invalid("script", "a command job takes argv, not script or args")
		}
	case executor.KindInteractive:
		return req, invalid("kind", "interactive jobs are not available yet (milestone M2)")
	default:
		return req, invalid("kind", `kind must be "batch" or "command"`)
	}
	if len(in.Script) > maxScript {
		return req, invalid("script", fmt.Sprintf("script is larger than %d bytes", maxScript))
	}
	if strings.ContainsRune(in.Script, 0) {
		return req, invalid("script", "script contains a NUL byte")
	}
	total := 0
	for _, a := range append(append([]string{}, in.Args...), in.Argv...) {
		if strings.ContainsRune(a, 0) {
			return req, invalid("args", "arguments cannot contain NUL bytes")
		}
		total += len(a) + 1
	}
	if total > maxArgsTotal {
		return req, invalid("args", fmt.Sprintf("arguments total more than %d bytes", maxArgsTotal))
	}

	// Environment. Protected names are rejected, not silently dropped.
	total = 0
	for k, v := range in.Env {
		if !envPattern.MatchString(k) {
			return req, invalid("env", fmt.Sprintf("%q is not a valid environment variable name", k))
		}
		if jobentry.ProtectedEnv(k) {
			return req, invalid("env", k+" is set by Mist and cannot be overridden")
		}
		if strings.ContainsRune(v, 0) {
			return req, invalid("env", k+" contains a NUL byte")
		}
		total += len(k) + len(v) + 2
	}
	if total > maxEnvTotal {
		return req, invalid("env", fmt.Sprintf("environment totals more than %d bytes", maxEnvTotal))
	}

	// Name.
	name := in.Name
	if name == "" {
		name = string(executor.KindBatch)
		if len(in.Argv) > 0 {
			name = path.Base(in.Argv[0])
		}
	}
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > maxNameRunes ||
		strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return req, invalid("name", fmt.Sprintf("name must be at most %d printable characters", maxNameRunes))
	}

	// Charged team, working directory and output path.
	if in.Workdir != "" {
		if err := checkPath("workdir", in.Workdir); err != nil {
			return req, err
		}
	}
	team := in.Team
	if team == "" {
		rest, ok := strings.CutPrefix(in.Workdir, teamsRoot)
		if !ok {
			if in.Workdir == "" {
				return req, invalid("team", "give a team, or a workdir inside a team folder")
			}
			return req, invalid("workdir", "workdir must be inside a team folder (/mist/teams/<team>/...); pass a team to use your personal folder")
		}
		team, _, _ = strings.Cut(rest, "/")
	}
	if !slugPattern.MatchString(team) {
		return req, invalid("team", fmt.Sprintf("%q is not a team name", team))
	}
	if err := s.Authz.Authorize(ctx, p, identity.ActionSubmitJob, identity.Target{Team: team}); err != nil {
		if errors.Is(err, identity.ErrForbidden) || errors.Is(err, identity.ErrRevoked) {
			return req, &Error{http.StatusForbidden, CodeForbidden,
				"you are not a current member of team " + team, map[string]any{"field": "team"}}
		}
		return req, authzError(err, false, "")
	}
	teamRoot := teamsRoot + team
	workdir := in.Workdir
	if workdir == "" {
		workdir = teamRoot + "/" + p.Username
	}
	if workdir != teamRoot && !strings.HasPrefix(workdir, teamRoot+"/") {
		return req, invalid("workdir", "workdir must be inside "+teamRoot+", the folder of the charged team")
	}
	output := in.Output
	if output == "" {
		output = defaultOutput
	}
	if !path.IsAbs(output) {
		output = workdir + "/" + output
	}
	if err := checkPath("output", output); err != nil {
		return req, err
	}
	if !strings.HasPrefix(output, teamRoot+"/") {
		return req, invalid("output", "output must be inside "+teamRoot)
	}

	// Partition and QoS.
	pname := in.Partition
	if pname == "" {
		for _, c := range cat.Partitions {
			if c.IsDefault {
				pname = c.Name
			}
		}
	}
	part := cat.Partition(pname)
	if part == nil {
		return req, invalid("partition", fmt.Sprintf("no partition %q", pname))
	}
	qname := in.QOS
	if qname == "" {
		qname = "normal"
	}
	qos := cat.QoS(qname)
	if qos == nil {
		return req, invalid("qos", fmt.Sprintf("no QoS %q", qname))
	}
	if qname == "interactive" {
		return req, invalid("qos", "the interactive QoS is only for interactive jobs")
	}
	if in.Runtime != "" && in.Runtime != part.Runtime {
		return req, invalid("runtime", "partition "+part.Name+" offers only its current runtime "+part.Runtime)
	}

	res, err := resolveResources(in, part)
	if err != nil {
		return req, err
	}
	timeLimit := part.DefaultTimeLimit
	if in.TimeLimitSeconds != nil {
		if *in.TimeLimitSeconds < 1 {
			return req, invalid("time_limit_seconds", "time limit must be at least 1 second")
		}
		timeLimit = time.Duration(min(*in.TimeLimitSeconds, 1<<32)) * time.Second
	}
	maxTime := part.MaxTimeLimit
	if qos.MaxTimeLimit != nil && *qos.MaxTimeLimit < maxTime {
		maxTime = *qos.MaxTimeLimit
	}
	if timeLimit > maxTime {
		return req, limitExceeded("time_limit_seconds",
			fmt.Sprintf("time limit is more than the %s maximum of %s", part.Name, maxTime))
	}
	grace := part.DefaultSignalGrace
	if in.SignalGraceSeconds != nil {
		g := *in.SignalGraceSeconds
		if g < int64(executor.MinSignalGrace/time.Second) || g > int64(executor.MaxSignalGrace/time.Second) {
			return req, invalid("signal_grace_seconds", fmt.Sprintf("signal grace must be %d–%d seconds",
				executor.MinSignalGrace/time.Second, executor.MaxSignalGrace/time.Second))
		}
		grace = time.Duration(g) * time.Second
	}

	return scheduler.Request{
		Username: p.Username, Team: team, Partition: part.Name, QOS: qos.Name, Name: name,
		Kind: executor.Kind(in.Kind), Resources: res, TimeLimit: timeLimit, SignalGrace: grace,
		Workdir: workdir, OutputPath: output,
		Script: in.Script, Args: in.Args, Argv: in.Argv, Env: in.Env,
	}, nil
}

// checkPath accepts only clean absolute paths: no "..", ".", empty or
// trailing components. The executor still opens paths beneath the team
// root without following symlinks out of it; this check only keeps
// obviously hostile or ambiguous strings out of the database.
func checkPath(field, p string) error {
	if len(p) > maxPath || !utf8.ValidString(p) || strings.IndexFunc(p, unicode.IsControl) >= 0 {
		return invalid(field, field+" must be a printable path of at most 4096 bytes")
	}
	if !path.IsAbs(p) || path.Clean(p) != p {
		return invalid(field, field+" must be a clean absolute path, without .. or . components")
	}
	return nil
}

func resolveResources(in SubmitRequest, p *scheduler.Partition) (executor.Resources, error) {
	var res executor.Resources
	get := func(v *int64, def int64) int64 {
		if v == nil {
			return def
		}
		return *v
	}
	res.GPUs, res.TTBoards = get(in.Resources.GPUs, 0), get(in.Resources.TTBoards, 0)
	if res.GPUs < 0 || res.TTBoards < 0 {
		return res, invalid("resources", "accelerator counts cannot be negative")
	}
	n := int64(1) // CPU and memory defaults scale with accelerators
	switch p.Accelerator {
	case "gpu":
		res.GPUs = get(in.Resources.GPUs, p.MinAccelerators)
		if res.TTBoards != 0 {
			return res, invalid("resources.tt_boards", "partition "+p.Name+" has no Tenstorrent boards")
		}
		n = res.GPUs
	case "tt":
		res.TTBoards = get(in.Resources.TTBoards, p.MinAccelerators)
		if res.GPUs != 0 {
			return res, invalid("resources.gpus", "partition "+p.Name+" has no GPUs")
		}
		n = res.TTBoards
	default:
		if res.GPUs != 0 || res.TTBoards != 0 {
			return res, invalid("resources", "partition "+p.Name+" has no accelerators; use the gpu partition for GPUs")
		}
	}
	if p.Accelerator != "none" {
		if n < p.MinAccelerators {
			return res, invalid("resources", fmt.Sprintf("partition %s needs at least %d %s", p.Name, p.MinAccelerators, accelName(p)))
		}
		if n > p.MaxAccelerators {
			return res, limitExceeded("resources", fmt.Sprintf("partition %s allows at most %d %s per job", p.Name, p.MaxAccelerators, accelName(p)))
		}
	}
	per := ""
	if p.Accelerator != "none" {
		per = " per " + strings.TrimSuffix(accelName(p), "s")
	}

	res.MilliCPU = p.DefaultMilliCPU * n
	if in.Resources.CPUs != nil {
		c := *in.Resources.CPUs
		if c < 1 || c > maxCPUs {
			return res, invalid("resources.cpus", "cpus must be a whole number of cores, at least 1")
		}
		res.MilliCPU = c * 1000
	}
	if res.MilliCPU > p.MaxMilliCPU*n {
		return res, limitExceeded("resources.cpus", fmt.Sprintf("partition %s allows at most %d CPUs%s", p.Name, p.MaxMilliCPU/1000, per))
	}
	res.MemoryBytes = get(in.Resources.MemoryBytes, p.DefaultMemory*n)
	if res.MemoryBytes < 1 {
		return res, invalid("resources.memory_bytes", "memory must be positive")
	}
	if res.MemoryBytes > p.MaxMemory*n {
		return res, limitExceeded("resources.memory_bytes", fmt.Sprintf("partition %s allows at most %s of memory%s", p.Name, gib(p.MaxMemory), per))
	}
	res.ScratchBytes = get(in.Resources.ScratchBytes, p.DefaultScratch)
	if res.ScratchBytes < 1 {
		return res, invalid("resources.scratch_bytes", "scratch must be positive")
	}
	if res.ScratchBytes > p.MaxScratch {
		return res, limitExceeded("resources.scratch_bytes", fmt.Sprintf("partition %s allows at most %s of scratch", p.Name, gib(p.MaxScratch)))
	}
	if (p.CapMilliCPU != nil && res.MilliCPU > *p.CapMilliCPU) || (p.CapMemory != nil && res.MemoryBytes > *p.CapMemory) {
		return res, limitExceeded("resources", "the job is larger than partition "+p.Name+"'s total capacity")
	}

	// It must fit at least one node, or it would wait forever.
	reported := false
	for _, node := range p.Nodes {
		if node.Reported {
			reported = true
			if node.Total.Fits(res) {
				return res, nil
			}
		}
	}
	if !reported {
		return res, unavailable("partition " + p.Name + " has no nodes reported yet")
	}
	return res, limitExceeded("resources", "no node in partition "+p.Name+" is large enough for this job")
}

func accelName(p *scheduler.Partition) string {
	if p.Accelerator == "tt" {
		return "boards"
	}
	return "GPUs"
}

func gib(b int64) string {
	if b%(1<<30) == 0 {
		return fmt.Sprintf("%d GiB", b>>30)
	}
	return fmt.Sprintf("%d bytes", b)
}
