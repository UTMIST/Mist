package jobentry

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/UTMIST/Mist/internal/executor"
)

func TestPrivateDirs(t *testing.T) {
	home := t.TempDir()
	os.Mkdir(filepath.Join(home, ".cache"), 0o755) // too open: reset
	if err := ensurePrivateDirs(home); err != nil {
		t.Fatal(err)
	}
	for _, d := range privateDirs {
		st, err := os.Lstat(filepath.Join(home, d))
		if err != nil || !st.IsDir() || st.Mode().Perm() != 0o700 {
			t.Fatalf("%s: %v %v", d, st.Mode(), err)
		}
	}

	// A symlink in place of .ssh fails setup and its target is untouched.
	elsewhere := t.TempDir()
	os.Chmod(elsewhere, 0o755)
	os.RemoveAll(filepath.Join(home, ".ssh"))
	os.Symlink(elsewhere, filepath.Join(home, ".ssh"))
	if err := ensurePrivateDirs(home); err == nil {
		t.Fatal("symlinked .ssh accepted")
	}
	if st, _ := os.Stat(elsewhere); st.Mode().Perm() != 0o755 {
		t.Fatalf("symlink target chmod-ed to %v", st.Mode().Perm())
	}

	// A file in place of a directory also fails.
	os.Remove(filepath.Join(home, ".ssh"))
	os.WriteFile(filepath.Join(home, ".ssh"), nil, 0o600)
	if err := ensurePrivateDirs(home); err == nil {
		t.Fatal("file in place of .ssh accepted")
	}
}

func TestOpenOutput(t *testing.T) {
	old := unix.Umask(0o027)
	defer unix.Umask(old)
	team := t.TempDir()
	os.MkdirAll(filepath.Join(team, "alice", "proj"), 0o750)

	f, err := openOutput(team, filepath.Join(team, "alice", "proj", "mist-1.out"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	if st, _ := os.Stat(filepath.Join(team, "alice", "proj", "mist-1.out")); st.Mode().Perm() != 0o640 {
		t.Fatalf("output mode %v, want 0640", st.Mode().Perm())
	}

	// A symlink inside the team tree is fine.
	os.Symlink("proj", filepath.Join(team, "alice", "latest"))
	if f, err := openOutput(team, filepath.Join(team, "alice", "latest", "mist-2.out")); err != nil {
		t.Fatalf("in-tree symlink rejected: %v", err)
	} else {
		f.Close()
	}

	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(team, "alice", "escape"))
	os.Symlink(filepath.Join(outside, "victim"), filepath.Join(team, "alice", "link.out"))
	syscall.Mkfifo(filepath.Join(team, "alice", "fifo.out"), 0o600)
	for _, bad := range []string{
		filepath.Join(team, "alice", "escape", "mist.out"), // directory symlink out of the tree
		filepath.Join(team, "alice", "link.out"),           // final component is a symlink
		filepath.Join(team, "alice", "fifo.out"),           // not a regular file; must not block
		filepath.Join(team, "alice", "..", "..", "x.out"),  // not beneath the team root
		"/etc/passwd",
	} {
		done := make(chan error, 1)
		go func() {
			f, err := openOutput(team, bad)
			if f != nil {
				f.Close()
			}
			done <- err
		}()
		select {
		case err := <-done:
			if err == nil {
				t.Errorf("%s: accepted", bad)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("%s: open blocked", bad)
		}
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Fatal("wrote outside the team tree")
	}
}

func testSpec(root string) Spec {
	team := filepath.Join(root, "mist", "teams", "team-a")
	return Spec{
		JobID: 7, Name: "t", User: "alice", Team: "team-a", Partition: "cpu", QOS: "normal", Node: "n1",
		Kind:        executor.KindBatch,
		Resources:   executor.Resources{MilliCPU: 4000, MemoryBytes: 8 << 30, ScratchBytes: 1 << 30},
		TimeLimit:   3600,
		SignalGrace: 60,
		TeamRoot:    team,
		Home:        filepath.Join(team, "alice"),
		Workdir:     filepath.Join(team, "alice"),
		OutputPath:  filepath.Join(team, "alice", "mist-7.out"),
		Scratch:     filepath.Join(root, "scratch"),
		ScriptPath:  filepath.Join(root, "script.sh"),
	}
}

func TestChildEnv(t *testing.T) {
	s := testSpec("/r")
	s.Env = map[string]string{
		"HOME": "/evil", "MIST_JOB_ID": "1", "CUDA_VISIBLE_DEVICES": "0,1", "PATH": "/evil",
		"OMP_NUM_THREADS": "2", "WANDB_PROJECT": "x",
	}
	start := time.Unix(1_800_000_000, 0)
	env := childEnv(s, []string{"PATH=/usr/bin", "AWS_SECRET_ACCESS_KEY=leak", "CUDA_VISIBLE_DEVICES=GPU-abc"}, start)
	get := func(k string) string {
		for _, kv := range env {
			if v, ok := strings.CutPrefix(kv, k+"="); ok {
				return v
			}
		}
		return "<unset>"
	}
	want := map[string]string{
		"HOME":                  s.Home,
		"MIST_JOB_ID":           "7",
		"CUDA_VISIBLE_DEVICES":  "GPU-abc", // from the device runtime, not the user
		"PATH":                  "/usr/bin",
		"OMP_NUM_THREADS":       "2", // tuning values may be overridden
		"WANDB_PROJECT":         "x",
		"MIST_CPUS":             "4",
		"MIST_DEADLINE":         "1800003600",
		"TMPDIR":                s.Scratch,
		"AWS_SECRET_ACCESS_KEY": "<unset>",
		"HF_HOME":               s.TeamRoot + "/.cache/huggingface",
	}
	for k, v := range want {
		if got := get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if !slices.IsSorted(env) {
		t.Error("environment is not deterministic")
	}
}

func TestSpecValidate(t *testing.T) {
	if err := testSpec("/r").Validate(); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Spec){
		"output outside team": func(s *Spec) { s.OutputPath = "/r/other/x.out" },
		"unclean path":        func(s *Spec) { s.Workdir = s.TeamRoot + "/alice/../bob" },
		"interactive":         func(s *Spec) { s.Kind = executor.KindInteractive; s.ScriptPath = "" },
		"command with script": func(s *Spec) { s.Kind = executor.KindCommand; s.Argv = []string{"ls"} },
		"no time limit":       func(s *Spec) { s.TimeLimit = 0 },
	} {
		s := testSpec("/r")
		mutate(&s)
		if s.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
