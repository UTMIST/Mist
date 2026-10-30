// Package jobentry is mist-job-entry: the first process of every job under
// every executor. It is the one place that implements the job contract in
// docs/agents/architecture/runtime-environment.md: umask, private
// dot-folders, the output file with header and footer, signal forwarding,
// the local time limit, the exit reason and the termination message.
//
// It runs unprivileged as the job's user. It cannot guarantee a footer
// after SIGKILL, OOM or node loss, so executors take authoritative reasons
// from kubelet/cgroup observations and treat its report as advisory.
package jobentry

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/UTMIST/Mist/internal/executor"
)

// Spec is /etc/mist/job.json, written by the executor from the LaunchSpec.
type Spec struct {
	JobID       int64              `json:"job_id"`
	Name        string             `json:"name"`
	User        string             `json:"user"`
	Team        string             `json:"team"`
	Partition   string             `json:"partition"`
	QOS         string             `json:"qos"`
	Node        string             `json:"node"`
	Kind        executor.Kind      `json:"kind"`
	Resources   executor.Resources `json:"resources"`
	TimeLimit   int64              `json:"time_limit_seconds"`
	SignalGrace int64              `json:"signal_grace_seconds"`

	TeamRoot   string `json:"team_root"`   // /mist/teams/<team>; outputs must stay beneath it
	Home       string `json:"home"`        // /mist/teams/<team>/<user>
	Workdir    string `json:"workdir"`     // start directory
	OutputPath string `json:"output_path"` // absolute, inside TeamRoot
	Scratch    string `json:"scratch"`     // /scratch

	ScriptPath string            `json:"script_path"` // batch: the script file from the ConfigMap
	Args       []string          `json:"args"`        // batch arguments
	Argv       []string          `json:"argv"`        // command
	Env        map[string]string `json:"env"`         // user --env values; protected names are dropped

	// TerminationLog receives the structured Result (K3s: the Pod's
	// terminationMessagePath). Empty disables it.
	TerminationLog string `json:"termination_log"`
}

// LoadSpec reads and checks a job.json file.
func LoadSpec(file string) (Spec, error) {
	var s Spec
	b, err := os.ReadFile(file)
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("jobentry: parse %s: %w", file, err)
	}
	return s, s.Validate()
}

func (s Spec) timeLimit() time.Duration { return time.Duration(s.TimeLimit) * time.Second }
func (s Spec) grace() time.Duration     { return time.Duration(s.SignalGrace) * time.Second }

// Validate rejects a spec the wrapper cannot run safely.
func (s Spec) Validate() error {
	clean := func(p string) bool { return path.IsAbs(p) && path.Clean(p) == p }
	under := func(p, root string) bool { return p == root || strings.HasPrefix(p, root+"/") }
	switch {
	case s.JobID <= 0:
		return fmt.Errorf("jobentry: job_id must be positive")
	case !clean(s.TeamRoot) || !clean(s.Home) || !clean(s.Workdir) || !clean(s.OutputPath) || !clean(s.Scratch):
		return fmt.Errorf("jobentry: paths must be clean and absolute")
	case !under(s.Home, s.TeamRoot) || !under(s.OutputPath, s.TeamRoot):
		return fmt.Errorf("jobentry: home and output must be inside %s", s.TeamRoot)
	case s.TimeLimit <= 0 || s.SignalGrace <= 0:
		return fmt.Errorf("jobentry: time limit and signal grace must be positive")
	}
	switch s.Kind {
	case executor.KindBatch:
		if s.ScriptPath == "" || len(s.Argv) != 0 {
			return fmt.Errorf("jobentry: batch needs script_path and no argv")
		}
	case executor.KindCommand:
		if len(s.Argv) == 0 || s.ScriptPath != "" || len(s.Args) != 0 {
			return fmt.Errorf("jobentry: command needs argv and no script or args")
		}
	case executor.KindInteractive:
		return fmt.Errorf("jobentry: interactive jobs arrive in M2")
	default:
		return fmt.Errorf("jobentry: unknown kind %q", s.Kind)
	}
	return nil
}
