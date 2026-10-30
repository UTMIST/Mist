package jobentry

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

// passthrough are variables taken from the wrapper's own environment: the
// runtime profile's baseline and what the executor/device runtime sets.
var passthrough = []string{"PATH", "LANG", "LC_ALL", "TERM", "CUDA_VISIBLE_DEVICES", "NVIDIA_VISIBLE_DEVICES"}

// protected reports whether a user --env value may not set name.
func protected(name string) bool {
	switch name {
	case "HOME", "USER", "LOGNAME", "PWD", "SHELL", "TMPDIR",
		"CUDA_VISIBLE_DEVICES", "NVIDIA_VISIBLE_DEVICES", "PATH":
		return true
	}
	return strings.HasPrefix(name, "MIST_") || strings.HasPrefix(name, "NVIDIA_")
}

// childEnv builds the job's complete environment. Nothing else from the
// wrapper's environment leaks in, so jobs behave the same however they
// were submitted. started is the actual work start, for MIST_DEADLINE.
func childEnv(s Spec, own []string, started time.Time) []string {
	env := map[string]string{}
	for _, kv := range own {
		k, v, _ := strings.Cut(kv, "=")
		if slices.Contains(passthrough, k) {
			env[k] = v
		}
	}
	if env["PATH"] == "" {
		env["PATH"] = "/usr/local/bin:/usr/bin:/bin"
	}
	cpus := fmt.Sprint(max(s.Resources.MilliCPU/1000, 1))
	teamCache := s.TeamRoot + "/.cache"
	tool := map[string]string{ // user --env may override these
		"OMP_NUM_THREADS":  cpus,
		"MKL_NUM_THREADS":  cpus,
		"PYTHONUNBUFFERED": "1",
		"HF_HOME":          teamCache + "/huggingface",
		"HF_TOKEN_PATH":    s.Home + "/.cache/huggingface/token",
		"TORCH_HOME":       teamCache + "/torch",
	}
	for k, v := range tool {
		env[k] = v
	}
	for k, v := range s.Env {
		if !protected(k) && k != "" && !strings.ContainsAny(k, "=\x00") && !strings.Contains(v, "\x00") {
			env[k] = v
		}
	}
	fixed := map[string]string{ // set last: nothing overrides these
		"HOME":                    s.Home,
		"USER":                    s.User,
		"LOGNAME":                 s.User,
		"SHELL":                   "/bin/bash",
		"PWD":                     s.Workdir,
		"TMPDIR":                  s.Scratch,
		"MIST_JOB_ID":             fmt.Sprint(s.JobID),
		"MIST_JOB_NAME":           s.Name,
		"MIST_USER":               s.User,
		"MIST_TEAM":               s.Team,
		"MIST_PARTITION":          s.Partition,
		"MIST_QOS":                s.QOS,
		"MIST_NODE":               s.Node,
		"MIST_CPUS":               cpus,
		"MIST_MEMORY_BYTES":       fmt.Sprint(s.Resources.MemoryBytes),
		"MIST_GPUS":               fmt.Sprint(s.Resources.GPUs),
		"MIST_TT_BOARDS":          fmt.Sprint(s.Resources.TTBoards),
		"MIST_TIME_LIMIT_SECONDS": fmt.Sprint(s.TimeLimit),
		"MIST_DEADLINE":           fmt.Sprint(started.Add(s.timeLimit()).Unix()),
		"MIST_SUBMIT_DIR":         s.Workdir,
		"MIST_SCRATCH":            s.Scratch,
	}
	for k, v := range fixed {
		env[k] = v
	}
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	slices.Sort(out)
	return out
}
