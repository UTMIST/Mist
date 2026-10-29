package executor

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"time"
)

// LaunchSpec is the complete, frozen description of one execution. The
// scheduler resolves it at admission and stores it with the allocation;
// later configuration changes never alter it.
type LaunchSpec struct {
	JobID          int64             `json:"job_id"`
	ExecutionID    string            `json:"execution_id"` // immutable UUID; one execution per job
	SpecHash       string            `json:"-"`            // digest of every other field
	AuthGeneration int64             `json:"auth_generation"`
	LaunchDeadline time.Time         `json:"launch_deadline"` // admitted_at + launch timeout
	Kind           Kind              `json:"kind"`
	Node           string            `json:"node"`
	User           User              `json:"user"`
	TeamSlug       string            `json:"team_slug"`
	TeamGID        int               `json:"team_gid"`
	ExtraGIDs      []int             `json:"extra_gids"` // mist-users
	Resources      Resources         `json:"resources"`
	DeviceIDs      []string          `json:"device_ids"` // set when the scheduler assigns devices
	Mounts         []Mount           `json:"mounts"`
	Workdir        string            `json:"workdir"`
	OutputPath     string            `json:"output_path"`
	Script         string            `json:"script"`  // batch
	Args           []string          `json:"args"`    // batch arguments, never shell-concatenated
	Argv           []string          `json:"argv"`    // command
	Env            map[string]string `json:"env"`     // frozen base environment
	Runtime        string            `json:"runtime"` // pinned image digest or host profile
	TimeLimit      time.Duration     `json:"time_limit"`
	SignalGrace    time.Duration     `json:"signal_grace"`
}

// ComputeSpecHash returns the SHA-256 of the spec's canonical JSON, without
// the SpecHash field itself. encoding/json writes struct fields in
// declaration order and map keys sorted, so equal specs hash equally.
func (s LaunchSpec) ComputeSpecHash() string {
	s.LaunchDeadline = s.LaunchDeadline.UTC()
	// Empty and nil mean the same thing here; make them encode the same.
	if len(s.ExtraGIDs) == 0 {
		s.ExtraGIDs = nil
	}
	if len(s.DeviceIDs) == 0 {
		s.DeviceIDs = nil
	}
	if len(s.Mounts) == 0 {
		s.Mounts = nil
	}
	if len(s.Args) == 0 {
		s.Args = nil
	}
	if len(s.Argv) == 0 {
		s.Argv = nil
	}
	if len(s.Env) == 0 {
		s.Env = nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("executor: marshal LaunchSpec: %v", err)) // only plain types inside
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Seal sets SpecHash. Call it once, after every other field is final.
func (s *LaunchSpec) Seal() { s.SpecHash = s.ComputeSpecHash() }

var (
	uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,30}$`)
)

// Signal grace bounds, from the API contract.
const (
	MinSignalGrace = time.Second
	MaxSignalGrace = 120 * time.Second
)

// Validate checks that the spec is complete and internally consistent. It
// does not check policy (limits, partitions); the scheduler did that.
// Executors call it before acting, so a malformed spec fails closed.
func (s LaunchSpec) Validate() error {
	var errs []error
	bad := func(format string, a ...any) { errs = append(errs, fmt.Errorf(format, a...)) }

	if s.JobID <= 0 {
		bad("job_id must be positive")
	}
	if !uuidPattern.MatchString(s.ExecutionID) {
		bad("execution_id must be a lowercase UUIDv4")
	}
	if s.SpecHash == "" {
		bad("spec is not sealed")
	} else if s.SpecHash != s.ComputeSpecHash() {
		bad("spec_hash does not match the spec")
	}
	if s.AuthGeneration <= 0 {
		bad("auth_generation must be positive")
	}
	if s.LaunchDeadline.IsZero() {
		bad("launch_deadline is required")
	}
	if s.Node == "" {
		bad("node is required")
	}
	if !namePattern.MatchString(s.User.Name) || s.User.UID <= 0 || s.User.PersonalGID <= 0 {
		bad("user needs a valid name, uid and personal gid")
	}
	if !namePattern.MatchString(s.TeamSlug) || s.TeamGID <= 0 {
		bad("team needs a valid slug and gid")
	}
	r := s.Resources
	if r.MilliCPU <= 0 || r.MemoryBytes <= 0 || r.ScratchBytes <= 0 {
		bad("cpu, memory and scratch must be positive")
	}
	if r.GPUs < 0 || r.TTBoards < 0 || r.HugepagesBytes < 0 {
		bad("accelerators and hugepages cannot be negative")
	}
	if n := len(s.DeviceIDs); n != 0 && int64(n) != r.GPUs+r.TTBoards {
		bad("device_ids must list every allocated accelerator or none")
	}
	for _, m := range s.Mounts {
		if !path.IsAbs(m.Target) || path.Clean(m.Target) != m.Target || m.VolumeID == "" || m.Source == "" {
			bad("mount %q needs a volume, a source and a clean absolute target", m.Target)
		}
	}
	for _, p := range []struct{ name, v string }{{"workdir", s.Workdir}, {"output_path", s.OutputPath}} {
		if !path.IsAbs(p.v) || path.Clean(p.v) != p.v {
			bad("%s must be a clean absolute path", p.name)
		}
	}
	switch s.Kind {
	case KindBatch:
		if s.Script == "" || len(s.Argv) != 0 {
			bad("batch needs script and no argv")
		}
	case KindCommand:
		if len(s.Argv) == 0 || s.Script != "" || len(s.Args) != 0 {
			bad("command needs argv and no script or args")
		}
	case KindInteractive:
		if s.Script != "" || len(s.Args) != 0 || len(s.Argv) != 0 {
			bad("interactive takes no script, args or argv")
		}
	default:
		bad("unknown kind %q", s.Kind)
	}
	if s.Runtime == "" {
		bad("runtime is required")
	}
	if s.TimeLimit <= 0 {
		bad("time_limit must be positive")
	}
	if s.SignalGrace < MinSignalGrace || s.SignalGrace > MaxSignalGrace {
		bad("signal_grace must be between %s and %s", MinSignalGrace, MaxSignalGrace)
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("executor: invalid LaunchSpec for job %d: %w", s.JobID, err)
	}
	return nil
}

// NewExecutionID returns a random UUIDv4.
func NewExecutionID() string {
	var b [16]byte
	rand.Read(b[:])         // never fails, per crypto/rand
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}
