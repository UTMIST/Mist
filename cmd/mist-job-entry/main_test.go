package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/jobentry"
)

// The test binary re-executes itself as mist-job-entry, so these tests run
// the real process: real signals, real exit statuses, real cgroups.
func TestMain(m *testing.M) {
	if os.Getenv("MIST_JOB_ENTRY_TEST_CHILD") == "1" {
		os.Args = append(os.Args[:1], os.Args[2:]...) // drop -test.run
		main()
		return
	}
	os.Exit(m.Run())
}

type job struct {
	t      *testing.T
	root   string
	spec   jobentry.Spec
	stdout *lockedBuffer
	cmd    *exec.Cmd
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newJob(t *testing.T, script string) *job {
	root := t.TempDir()
	team := filepath.Join(root, "mist", "teams", "team-a")
	home := filepath.Join(team, "alice")
	for _, d := range []string{home, filepath.Join(root, "scratch")} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	os.WriteFile(filepath.Join(root, "script.sh"), []byte(script), 0o644)
	return &job{t: t, root: root, stdout: &lockedBuffer{}, spec: jobentry.Spec{
		JobID: 412, Name: "test", User: "alice", Team: "team-a", Partition: "cpu", QOS: "normal", Node: "local",
		Kind:           executor.KindBatch,
		Resources:      executor.Resources{MilliCPU: 2000, MemoryBytes: 64 << 20, ScratchBytes: 1 << 30},
		TimeLimit:      60,
		SignalGrace:    5,
		TeamRoot:       team,
		Home:           home,
		Workdir:        home,
		OutputPath:     filepath.Join(home, "mist-412.out"),
		Scratch:        filepath.Join(root, "scratch"),
		ScriptPath:     filepath.Join(root, "script.sh"),
		TerminationLog: filepath.Join(root, "termination-log"),
	}}
}

// start runs mist-job-entry, optionally under a wrapper such as systemd-run.
func (j *job) start(wrapper ...string) {
	j.t.Helper()
	b, _ := json.Marshal(j.spec)
	specFile := filepath.Join(j.root, "job.json")
	os.WriteFile(specFile, b, 0o644)
	self, _ := os.Executable()
	argv := append(wrapper, self, "-test.run=none", "-spec", specFile)
	j.cmd = exec.Command(argv[0], argv[1:]...)
	j.cmd.Env = append(os.Environ(), "MIST_JOB_ENTRY_TEST_CHILD=1")
	j.cmd.Stdout, j.cmd.Stderr = j.stdout, j.stdout
	if err := j.cmd.Start(); err != nil {
		j.t.Fatal(err)
	}
}

func (j *job) wait() int {
	j.t.Helper()
	done := make(chan error, 1)
	go func() { done <- j.cmd.Wait() }()
	select {
	case err := <-done:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		return 0
	case <-time.After(30 * time.Second):
		j.cmd.Process.Kill()
		j.t.Fatalf("job did not finish; output:\n%s", j.stdout.String())
		return -1
	}
}

func (j *job) output() string {
	b, _ := os.ReadFile(j.spec.OutputPath)
	return string(b)
}

func (j *job) result() *executor.Result {
	b, err := os.ReadFile(j.spec.TerminationLog)
	if err != nil {
		return nil
	}
	var r executor.Result
	if err := json.Unmarshal(b, &r); err != nil {
		j.t.Fatal(err)
	}
	return &r
}

func (j *job) waitForOutput(s string) {
	j.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(j.output(), s) {
		if time.Now().After(deadline) {
			j.t.Fatalf("output never contained %q:\n%s", s, j.output())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestBatchArgsAndOutput(t *testing.T) {
	j := newJob(t, `echo "argc=$#"; for a in "$@"; do echo "arg=[$a]"; done; echo err >&2; pwd; umask; echo "home=$HOME"`)
	j.spec.Args = []string{"two words", "$(id)", "; rm -rf /"}
	j.start()
	if code := j.wait(); code != 0 {
		t.Fatalf("exit %d:\n%s", code, j.stdout.String())
	}
	out := j.output()
	for _, want := range []string{
		"[mist] job 412 (test) started", "argc=3", "arg=[two words]", "arg=[$(id)]", "arg=[; rm -rf /]",
		"err\n", j.spec.Workdir + "\n", "0027\n", "home=" + j.spec.Home,
		"[mist] job 412 ended", "[mist] exit code 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
	if !strings.Contains(j.stdout.String(), "argc=3") {
		t.Error("output not copied to stdout")
	}
	if st, _ := os.Stat(j.spec.OutputPath); st.Mode().Perm() != 0o640 {
		t.Errorf("output mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(filepath.Join(j.spec.Home, ".ssh")); st == nil || st.Mode().Perm() != 0o700 {
		t.Error(".ssh not created private")
	}
	if r := j.result(); r == nil || *r.ExitCode != 0 || r.Reason != executor.ExitNormal || r.OutputIncomplete {
		t.Errorf("result %+v", r)
	}
}

func TestCommandAndExitCode(t *testing.T) {
	j := newJob(t, "")
	j.spec.Kind, j.spec.ScriptPath = executor.KindCommand, ""
	j.spec.Argv = []string{"sh", "-c", "echo running; exit 3"}
	j.start()
	if code := j.wait(); code != 3 {
		t.Fatalf("exit %d", code)
	}
	if r := j.result(); *r.ExitCode != 3 || r.Reason != executor.ExitNormal {
		t.Fatalf("result %+v", r)
	}
	if !strings.Contains(j.output(), "running") {
		t.Fatal("command output missing")
	}
}

func TestSIGTERMIsForwarded(t *testing.T) {
	j := newJob(t, `trap 'echo caught; exit 7' TERM; echo ready; sleep 30 & wait`)
	j.start()
	j.waitForOutput("ready")
	j.cmd.Process.Signal(syscall.SIGTERM)
	if code := j.wait(); code != 7 {
		t.Fatalf("exit %d:\n%s", code, j.output())
	}
	if !strings.Contains(j.output(), "caught") || !strings.Contains(j.output(), "stopped by request") {
		t.Fatalf("output:\n%s", j.output())
	}
	if r := j.result(); r.Reason != executor.ExitSignaled {
		t.Fatalf("reason %s", r.Reason)
	}
}

func TestLocalTimeLimit(t *testing.T) {
	t.Run("SIGTERM", func(t *testing.T) {
		j := newJob(t, "sleep 30")
		j.spec.TimeLimit, j.spec.SignalGrace = 1, 5
		j.start()
		start := time.Now()
		if code := j.wait(); code != 143 {
			t.Fatalf("exit %d", code)
		}
		if el := time.Since(start); el > 4*time.Second {
			t.Fatalf("took %s", el)
		}
		if r := j.result(); r.Reason != executor.ExitDeadline {
			t.Fatalf("reason %s", r.Reason)
		}
	})
	t.Run("SIGKILL after grace", func(t *testing.T) {
		j := newJob(t, `trap '' TERM; echo ready; while :; do sleep 0.1; done`)
		j.spec.TimeLimit, j.spec.SignalGrace = 1, 1
		j.start()
		if code := j.wait(); code != 137 {
			t.Fatalf("exit %d:\n%s", code, j.output())
		}
		if !strings.Contains(j.output(), "sent SIGKILL") || j.result().Reason != executor.ExitDeadline {
			t.Fatalf("output:\n%s", j.output())
		}
	})
}

// If the wrapper itself is killed there is no footer and no result: the
// executor must use its own observations. Nothing fabricates success.
func TestWrapperKilledLeavesNoFooter(t *testing.T) {
	j := newJob(t, `echo "pid=$$"; echo working; exec sleep 30`)
	j.start()
	j.waitForOutput("working")
	j.cmd.Process.Kill()
	j.wait()
	out := j.output()
	if strings.Contains(out, "ended") || strings.Contains(out, "exit code") {
		t.Fatalf("footer written after wrapper SIGKILL:\n%s", out)
	}
	if j.result() != nil {
		t.Fatal("termination message written after wrapper SIGKILL")
	}
	// The work dies with the wrapper (parent-death signal).
	pid, _ := strconv.Atoi(strings.TrimSpace(strings.SplitN(strings.SplitN(out, "pid=", 2)[1], "\n", 2)[0]))
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			syscall.Kill(pid, syscall.SIGKILL)
			t.Fatal("work survived the wrapper")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestUnsafeOutputNeverRunsWork(t *testing.T) {
	j := newJob(t, "touch "+"ran-marker")
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(j.spec.Home, "escape"))
	j.spec.OutputPath = filepath.Join(j.spec.Home, "escape", "mist.out")
	j.start()
	if code := j.wait(); code != jobentry.ExitSetupFailed {
		t.Fatalf("exit %d", code)
	}
	if r := j.result(); r == nil || r.Reason != executor.ExitLaunchError || !r.OutputIncomplete {
		t.Fatalf("result %+v", r)
	}
	if _, err := os.Stat(filepath.Join(j.spec.Home, "ran-marker")); err == nil {
		t.Fatal("work ran despite unsafe output path")
	}
	if !strings.Contains(j.stdout.String(), "could not start") {
		t.Fatalf("stdout:\n%s", j.stdout.String())
	}
}

// Runs the job in a transient systemd scope with a 64 MiB memory limit and
// no swap, as the kernel would enforce it inside a Pod. OOMPolicy=continue
// isolates the kernel OOM kill: systemd's default (stop) would also SIGTERM
// the whole scope, which the M5 node executor must account for.
func TestOutOfMemoryInCgroup(t *testing.T) {
	if err := exec.Command("systemd-run", "--user", "--scope", "-q", "true").Run(); err != nil {
		t.Skipf("UNVERIFIED: no user systemd scopes here (%v)", err)
	}
	j := newJob(t, "echo allocating; head -c 512M /dev/zero | tail > /dev/null")
	j.start("systemd-run", "--user", "--scope", "-q", "-p", "MemoryMax=64M", "-p", "MemorySwapMax=0", "-p", "OOMPolicy=continue")
	code := j.wait()
	r := j.result()
	if r == nil || r.Reason != executor.ExitOutOfMemory {
		t.Fatalf("exit %d, result %+v, output:\n%s", code, r, j.output())
	}
	if code != 137 || !strings.Contains(j.output(), "out of memory: exceeded memory limit 64.0 MiB") {
		t.Fatalf("exit %d, output:\n%s", code, j.output())
	}
	if r.PeakMemoryBytes < 32<<20 {
		t.Fatalf("peak %d", r.PeakMemoryBytes)
	}
}
