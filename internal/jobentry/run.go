package jobentry

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/UTMIST/Mist/internal/executor"
)

// ExitSetupFailed is returned when the work never started (bad output
// path, unsafe dot-folder, missing program). The executor reports it as a
// launch failure, not as the job's own exit.
const ExitSetupFailed = 125

// footerMargin is how long before the executor's SIGKILL the wrapper kills
// the work itself, so it still has time to write the footer.
const footerMargin = 2 * time.Second

// Run executes the job described by s and returns the wrapper's exit code.
// stdout receives a copy of everything written to the output file, so
// executor log streaming works without reading team files. Signals sent to
// the wrapper arrive on sigs.
func Run(s Spec, stdout io.Writer, sigs <-chan os.Signal) int {
	unix.Umask(0o027)
	started := time.Now()
	out := &output{stdout: stdout}
	cg := ownCgroup()
	oomBefore := oomKills(cg)

	setupFailed := func(err error) int {
		out.mist("job %d could not start: %v", s.JobID, err)
		writeTermination(s.TerminationLog, executor.Result{
			ExitCode: ptr(ExitSetupFailed), Reason: executor.ExitLaunchError, Detail: err.Error(),
			OutputIncomplete: out.file == nil || out.failed,
		})
		return ExitSetupFailed
	}

	if err := ensurePrivateDirs(s.Home); err != nil {
		return setupFailed(err)
	}
	f, err := openOutput(s.TeamRoot, s.OutputPath)
	if err != nil {
		return setupFailed(err)
	}
	out.file = f
	out.header(s, started)

	env := childEnv(s, os.Environ(), started)
	var cmd *exec.Cmd
	if s.Kind == executor.KindBatch {
		cmd = exec.Command("/bin/bash", append([]string{s.ScriptPath}, s.Args...)...)
	} else {
		prog, err := lookPath(s.Argv[0], env)
		if err != nil {
			return setupFailed(err)
		}
		cmd = exec.Command(prog, s.Argv[1:]...)
		cmd.Args[0] = s.Argv[0]
	}
	cmd.Dir, cmd.Env = s.Workdir, env
	// Own process group so signals reach the whole job; die with the wrapper.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	r, w, err := os.Pipe()
	if err != nil {
		return setupFailed(err)
	}
	cmd.Stdout, cmd.Stderr = w, w // one pipe keeps stdout/stderr interleaving
	if err := cmd.Start(); err != nil {
		w.Close()
		r.Close()
		return setupFailed(err)
	}
	w.Close()
	copied := make(chan struct{})
	go func() { io.Copy(out, r); close(copied) }()
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()

	group := -cmd.Process.Pid
	deadline := time.NewTimer(s.timeLimit())
	defer deadline.Stop()
	var kill <-chan time.Time
	stopReason := executor.ExitNormal
	stopWith := func(why string, after time.Duration) {
		syscall.Kill(group, syscall.SIGTERM)
		if kill == nil {
			out.mist("%s; sent SIGTERM, SIGKILL in %s", why, after)
			kill = time.After(after)
		}
	}
wait:
	for {
		select {
		case sig := <-sigs:
			if stopReason == executor.ExitNormal {
				stopReason = executor.ExitSignaled
			}
			stopWith(fmt.Sprintf("received %s", sig), max(s.grace()-footerMargin, time.Second))
		case <-deadline.C:
			if stopReason == executor.ExitNormal { // the first stop cause wins
				stopReason = executor.ExitDeadline
			}
			stopWith(fmt.Sprintf("time limit %s reached", s.timeLimit()), s.grace())
		case <-kill:
			out.mist("grace period over; sent SIGKILL")
			syscall.Kill(group, syscall.SIGKILL)
		case err = <-waited:
			break wait
		}
	}
	// Background processes left in the group would keep the pipe open.
	syscall.Kill(group, syscall.SIGKILL)
	select {
	case <-copied:
	case <-time.After(5 * time.Second):
		out.mist("output still open after job exit; some output may be missing")
		out.failed = true
	}
	r.Close()

	res := executor.Result{Reason: stopReason, PeakMemoryBytes: peakMemory(cg)}
	code := 0
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		ws := exitErr.Sys().(syscall.WaitStatus)
		if ws.Signaled() {
			code = 128 + int(ws.Signal())
		} else {
			code = ws.ExitStatus()
		}
	default:
		code = 1
	}
	res.ExitCode = &code
	if after := oomKills(cg); after > oomBefore {
		res.Reason = executor.ExitOutOfMemory
		res.Detail = fmt.Sprintf("exceeded memory limit %s (peak %s)",
			bytes(s.Resources.MemoryBytes), bytes(res.PeakMemoryBytes))
	} else if after < 0 && code == 128+int(syscall.SIGKILL) && stopReason == executor.ExitNormal {
		res.Detail = "killed by SIGKILL (memory events unavailable; possibly out of memory)"
	}

	out.footer(s, started, res)
	if err := f.Close(); err != nil {
		out.failed = true
	}
	res.OutputIncomplete = out.failed
	writeTermination(s.TerminationLog, res)
	return code
}

// output copies job output to the file and the wrapper's stdout. A failed
// file write is remembered rather than retried, and reported as
// output_incomplete; stdout keeps receiving everything.
type output struct {
	mu     sync.Mutex
	stdout io.Writer
	file   *os.File
	failed bool
}

func (o *output) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stdout.Write(p)
	if o.file != nil && !o.failed {
		if _, err := o.file.Write(p); err != nil {
			o.failed = true
		}
	}
	return len(p), nil
}

func (o *output) mist(format string, a ...any) {
	o.Write([]byte("[mist] " + fmt.Sprintf(format, a...) + "\n"))
}

func (o *output) header(s Spec, at time.Time) {
	r := s.Resources
	o.mist("job %d (%s) started %s on %s", s.JobID, s.Name, at.UTC().Format(time.RFC3339), s.Node)
	o.mist("%s in team %s, partition %s, qos %s", s.User, s.Team, s.Partition, s.QOS)
	acc := ""
	if r.GPUs > 0 {
		acc = fmt.Sprintf(", %d GPU", r.GPUs)
	}
	if r.TTBoards > 0 {
		acc += fmt.Sprintf(", %d TT board", r.TTBoards)
	}
	o.mist("%g CPUs, %s memory%s, %s scratch, time limit %s",
		float64(r.MilliCPU)/1000, bytes(r.MemoryBytes), acc, bytes(r.ScratchBytes), s.timeLimit())
}

func (o *output) footer(s Spec, started time.Time, r executor.Result) {
	end := time.Now()
	o.mist("job %d ended %s on %s", s.JobID, end.UTC().Format(time.RFC3339), s.Node)
	switch r.Reason {
	case executor.ExitOutOfMemory:
		o.mist("out of memory: %s; request more with --mem", r.Detail)
	case executor.ExitDeadline:
		o.mist("stopped at the time limit (%s)", s.timeLimit())
	case executor.ExitSignaled:
		o.mist("stopped by request")
	default:
		if r.Detail != "" {
			o.mist("%s", r.Detail)
		}
	}
	peak := "unknown"
	if r.PeakMemoryBytes > 0 {
		peak = bytes(r.PeakMemoryBytes)
	}
	o.mist("exit code %d, elapsed %s, peak memory %s", *r.ExitCode, end.Sub(started).Round(time.Second), peak)
}

func writeTermination(file string, r executor.Result) {
	if file == "" {
		return
	}
	if len(r.Detail) > 1024 { // Kubernetes keeps at most 4 KiB
		r.Detail = r.Detail[:1024]
	}
	b, _ := json.Marshal(r)
	os.WriteFile(file, b, 0o644)
}

// ownCgroup returns this process's cgroup v2 directory, or "".
func ownCgroup() string {
	b, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if p, ok := strings.CutPrefix(line, "0::"); ok {
			return filepath.Join("/sys/fs/cgroup", p)
		}
	}
	return ""
}

// oomKills returns the cgroup's oom_kill count, or -1 if unreadable.
func oomKills(cg string) int64 {
	b, err := os.ReadFile(filepath.Join(cg, "memory.events"))
	if cg == "" || err != nil {
		return -1
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "oom_kill "); ok {
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
	}
	return -1
}

func peakMemory(cg string) int64 {
	b, err := os.ReadFile(filepath.Join(cg, "memory.peak"))
	if cg == "" || err != nil {
		return 0
	}
	n, _ := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64)
	return n
}

func lookPath(name string, env []string) (string, error) {
	if strings.Contains(name, "/") {
		return name, nil
	}
	for _, kv := range env {
		if p, ok := strings.CutPrefix(kv, "PATH="); ok {
			for _, dir := range filepath.SplitList(p) {
				cand := filepath.Join(dir, name)
				if st, err := os.Stat(cand); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
					return cand, nil
				}
			}
		}
	}
	return "", fmt.Errorf("%s: command not found", name)
}

func bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	}
	return fmt.Sprintf("%d B", n)
}

func ptr[T any](v T) *T { return &v }
