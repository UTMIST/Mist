//go:build k3d

// Integration tests on a real k3s (k3d) cluster: run scripts/k3d-test.sh.
// They prove Pod construction, informers, status mapping, strict groups,
// read-only root, NetworkPolicy, OOM and cleanup receipts on real kubelet
// and containerd. They do not prove GPU, NFS, host firewall or production
// cleanup-reporter behaviour; those are checked on the real nodes (step 8).
package k3s

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/db"
	"github.com/UTMIST/Mist/internal/db/dbtest"
	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/identity/identitytest"
	"github.com/UTMIST/Mist/internal/scheduler"
)

type cluster struct {
	t      *testing.T
	ctx    context.Context
	cancel context.CancelFunc
	node   string
	client kubernetes.Interface
	pool   *pgxpool.Pool
	ex     *Executor

	mu     sync.Mutex
	events map[int64][]executor.Event
}

var nextJob = time.Now().Unix() % 1_000_000 * 100 // unique Pod names across runs

func newCluster(t *testing.T) *cluster {
	kctx, node := os.Getenv("MIST_K3D_CONTEXT"), os.Getenv("MIST_K3D_NODE")
	if kctx == "" || node == "" {
		t.Skip("run through scripts/k3d-test.sh")
	}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{CurrentContext: kctx}).ClientConfig()
	if err != nil {
		t.Fatal(err)
	}
	// Act as mistd's service account, so deploy/k3s/rbac.yaml is what is tested.
	cfg.Impersonate.UserName = "system:serviceaccount:mist-system:mistd"
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.Open(context.Background(), dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	c := &cluster{t: t, node: node, client: client, pool: pool, events: map[int64][]executor.Event{}}
	c.startExecutor()
	return c
}

func (c *cluster) startExecutor() {
	if c.cancel != nil {
		c.cancel()
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.t.Cleanup(c.cancel)
	c.ex = New(Options{
		Config: Config{
			Namespace:     "mist-jobs",
			EntryPath:     "/usr/local/bin/mist-job-entry",
			LaunchTimeout: 10 * time.Minute,
			BackstopSlack: time.Minute,
			Nameservers:   []string{"1.1.1.1"},
			Volumes:       map[string]Volume{"vol1": {HostNode: c.node, HostPath: "/mist"}},
		},
		Client: c.client,
		Pool:   c.pool,
		Resync: 5 * time.Second,
	})
	if err := c.ex.Start(c.ctx); err != nil {
		c.t.Fatal(err)
	}
	ex := c.ex
	go func() {
		for ev := range ex.Events() {
			c.mu.Lock()
			c.events[ev.JobID] = append(c.events[ev.JobID], ev)
			c.mu.Unlock()
		}
	}()
}

func (c *cluster) spec(script string, mutate ...func(*executor.LaunchSpec)) executor.LaunchSpec {
	nextJob++
	id := nextJob
	s := executor.LaunchSpec{
		JobID: id, ExecutionID: executor.NewExecutionID(), AuthGeneration: 1,
		LaunchDeadline: time.Now().Add(10 * time.Minute),
		Kind:           executor.KindBatch, JobName: c.t.Name(), Partition: "cpu", QOS: "normal",
		Node:     c.node,
		User:     executor.User{Name: "alice", UID: 20001, PersonalGID: 20001},
		TeamSlug: "team-a", TeamGID: 40001, ExtraGIDs: []int{19000},
		Resources: executor.Resources{MilliCPU: 500, MemoryBytes: 256 << 20, ScratchBytes: 1 << 30},
		Mounts: []executor.Mount{
			{VolumeID: "vol1", Source: "/teams/team-a", Target: "/mist/teams/team-a"},
			{VolumeID: "vol1", Source: "/software", Target: "/mist/software", ReadOnly: true},
		},
		Workdir:     "/mist/teams/team-a/alice",
		OutputPath:  fmt.Sprintf("/mist/teams/team-a/alice/mist-%d.out", id),
		Script:      script,
		Runtime:     "mist-test-runtime:dev",
		TimeLimit:   5 * time.Minute,
		SignalGrace: 5 * time.Second,
	}
	for _, m := range mutate {
		m(&s)
	}
	s.Seal()
	return s
}

func (c *cluster) launch(s executor.LaunchSpec) {
	c.t.Helper()
	if err := c.ex.Launch(c.ctx, s); err != nil {
		c.t.Fatal(err)
	}
}

func (c *cluster) wait(job int64, kind executor.EventKind) executor.Event {
	c.t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, ev := range c.events[job] {
			if ev.Kind == kind {
				c.mu.Unlock()
				return ev
			}
		}
		c.mu.Unlock()
		time.Sleep(200 * time.Millisecond)
	}
	p, _ := c.client.CoreV1().Pods("mist-jobs").Get(c.ctx, Name(job), metav1.GetOptions{})
	c.t.Fatalf("job %d: no %s event; events %+v; pod %+v", job, kind, c.events[job], p.Status)
	return executor.Event{}
}

// cleanup runs the node-side check inside the k3d node and delivers its
// receipt, retrying while kubelet is still tearing the Pod down.
func (c *cluster) cleanup(s executor.LaunchSpec) {
	c.t.Helper()
	rec, err := c.ex.store.byJob(c.ctx, s.JobID)
	if err != nil || rec == nil {
		c.t.Fatalf("no record: %v", err)
	}
	var last string
	for range 60 {
		out, err := exec.Command("docker", "exec", c.node, "/mistd", "cleanup-report",
			"-pod-uid", rec.ObjectUID, "-execution-id", s.ExecutionID, "-node", c.node).CombinedOutput()
		if err == nil {
			var r CleanupReceipt
			if err := json.Unmarshal(out, &r); err != nil {
				c.t.Fatal(err)
			}
			if err := c.ex.RecordCleanup(c.ctx, r); err != nil {
				c.t.Fatal(err)
			}
			c.wait(s.JobID, executor.EventExited)
			return
		}
		last = string(out)
		time.Sleep(time.Second)
	}
	c.t.Fatalf("node never confirmed cleanup: %s", last)
}

func (c *cluster) output(s executor.LaunchSpec) string {
	out, err := exec.Command("docker", "exec", c.node, "cat", s.OutputPath).CombinedOutput()
	if err != nil {
		c.t.Fatalf("read output: %v %s", err, out)
	}
	return string(out)
}

func TestK3dBatchJobIsolation(t *testing.T) {
	c := newCluster(t)
	nodeIP, _ := exec.Command("docker", "inspect", "-f", "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}", c.node).Output()
	s := c.spec(`
echo "id: $(id)"
echo "teams: $(ls /mist/teams)"
[ -e /var/run/secrets/kubernetes.io/serviceaccount ] || echo "no-sa-token"
touch /etc/probe 2>/dev/null || echo "rootfs-readonly"
echo x > /tmp/t && echo x > /scratch/s && echo "scratch-writable"
echo "umask $(umask)"
probe() { timeout 3 bash -c "</dev/tcp/$1/$2" 2>/dev/null && echo "open $1:$2" || echo "blocked $1:$2"; }
probe 10.43.0.1 443
probe "$NODE_IP" 6443
probe "$NODE_IP" 10250
probe 1.1.1.1 53
`, func(s *executor.LaunchSpec) { s.Env = map[string]string{"NODE_IP": strings.TrimSpace(string(nodeIP))} })
	c.launch(s)
	c.wait(s.JobID, executor.EventStarted)
	fin := c.wait(s.JobID, executor.EventFinished)
	if *fin.ExitCode != 0 {
		t.Fatalf("exit %d: %s\n%s", *fin.ExitCode, fin.Detail, c.output(s))
	}
	c.cleanup(s)

	out := c.output(s)
	t.Logf("job output:\n%s", out)
	ip := strings.TrimSpace(string(nodeIP))
	for _, want := range []string{
		// Strict supplemental groups: exactly team, mist-users and personal.
		"id: uid=20001(alice) gid=40001(team-team-a) groups=40001(team-team-a),19000(mist-users),20001(alice)\n",
		"teams: team-a\n", // other teams are absent, not merely denied
		"no-sa-token", "rootfs-readonly", "scratch-writable", "umask 0027",
		"blocked 10.43.0.1:443", "blocked " + ip + ":6443", "blocked " + ip + ":10250",
		"open 1.1.1.1:53",
		"[mist] job", "[mist] exit code 0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	if err := c.ex.Launch(c.ctx, s); err == nil {
		t.Error("finished execution relaunched")
	}
}

func TestK3dTerminateForwardsSIGTERM(t *testing.T) {
	c := newCluster(t)
	s := c.spec(`trap 'echo caught-term; exit 0' TERM; echo ready; sleep 300 & wait`)
	c.launch(s)
	c.wait(s.JobID, executor.EventStarted)
	time.Sleep(time.Second) // let the trap install
	if err := c.ex.Terminate(c.ctx, s.JobID, s.SignalGrace, "cancelled by test"); err != nil {
		t.Fatal(err)
	}
	fin := c.wait(s.JobID, executor.EventFinished)
	if fin.Reason != executor.ExitSignaled {
		t.Fatalf("reason %s (%s)", fin.Reason, fin.Detail)
	}
	c.cleanup(s)
	if !strings.Contains(c.output(s), "caught-term") {
		t.Fatalf("SIGTERM did not reach the script:\n%s", c.output(s))
	}
}

func TestK3dOutOfMemory(t *testing.T) {
	c := newCluster(t)
	s := c.spec(`echo allocating; head -c 1G /dev/zero | tail > /dev/null`,
		func(s *executor.LaunchSpec) { s.Resources.MemoryBytes = 64 << 20 })
	c.launch(s)
	fin := c.wait(s.JobID, executor.EventFinished)
	if fin.Reason != executor.ExitOutOfMemory {
		t.Fatalf("reason %s (%s), exit %v", fin.Reason, fin.Detail, *fin.ExitCode)
	}
	c.cleanup(s)
}

func TestK3dLocalTimeLimit(t *testing.T) {
	c := newCluster(t)
	s := c.spec(`echo ready; sleep 120`, func(s *executor.LaunchSpec) { s.TimeLimit = 3 * time.Second })
	c.launch(s)
	fin := c.wait(s.JobID, executor.EventFinished)
	if fin.Reason != executor.ExitDeadline || *fin.ExitCode != 143 {
		t.Fatalf("reason %s exit %d", fin.Reason, *fin.ExitCode)
	}
	c.cleanup(s)
}

func TestK3dSetupFailureIsLaunchFailed(t *testing.T) {
	c := newCluster(t)
	s := c.spec(`echo should-not-run`, func(s *executor.LaunchSpec) {
		s.OutputPath = "/mist/teams/team-a/alice/no-such-dir/mist.out"
	})
	c.launch(s)
	fin := c.wait(s.JobID, executor.EventLaunchFailed)
	if fin.Reason != executor.ExitLaunchError || !strings.Contains(fin.Detail, "no-such-dir") {
		t.Fatalf("reason %s (%s)", fin.Reason, fin.Detail)
	}
	c.cleanup(s)
}

// A restarted executor rebuilds its view from receipts and the API, and
// can still stop the job it did not launch.
func TestK3dExecutorRestart(t *testing.T) {
	c := newCluster(t)
	s := c.spec(`echo ready; sleep 300`)
	c.launch(s)
	c.wait(s.JobID, executor.EventStarted)
	c.startExecutor()
	xs, err := c.ex.List(c.ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range xs {
		found = found || (x.JobID == s.JobID && x.Phase == executor.PhaseRunning && x.StartedAt != nil)
	}
	if !found {
		t.Fatalf("restarted executor lost the running job: %+v", xs)
	}
	if err := c.ex.Launch(c.ctx, s); err == nil || !strings.Contains(err.Error(), "started") {
		t.Fatalf("relaunch after restart: %v", err)
	}
	c.ex.Terminate(c.ctx, s.JobID, s.SignalGrace, "stop")
	c.wait(s.JobID, executor.EventFinished)
	c.cleanup(s)
}

func TestK3dRBACIsNarrow(t *testing.T) {
	c := newCluster(t)
	if _, err := c.client.CoreV1().Secrets("mist-jobs").List(c.ctx, metav1.ListOptions{}); !apierrors.IsForbidden(err) {
		t.Errorf("mistd can list secrets: %v", err)
	}
	if _, err := c.client.CoreV1().Pods("kube-system").List(c.ctx, metav1.ListOptions{}); !apierrors.IsForbidden(err) {
		t.Errorf("mistd can list pods outside mist-jobs: %v", err)
	}
}

// The whole M1 path on a real cluster: submit, admit, launch on k3s, run,
// node cleanup receipt, COMPLETED, allocation released.
func TestK3dSchedulerEndToEnd(t *testing.T) {
	c := newCluster(t)
	ctx := c.ctx
	for _, q := range []string{
		"INSERT INTO nodes (name, executor) VALUES ($1, 'k3s')",
		"INSERT INTO partition_nodes VALUES ('cpu', $1)",
	} {
		if _, err := c.pool.Exec(ctx, q, c.node); err != nil {
			t.Fatal(err)
		}
	}
	c.pool.Exec(ctx, "UPDATE partitions SET runtime = 'mist-test-runtime:dev'")
	// Keep job IDs clear of other tests' Pods in the shared namespace.
	c.pool.Exec(ctx, "SELECT setval('jobs_id_seq', $1)", nextJob+50)

	s, err := scheduler.New(ctx, scheduler.Config{
		Pool: c.pool, Identity: identitytest.New(),
		Executors: map[string]executor.Executor{"k3s": c.ex},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	jobs := scheduler.Jobs{Pool: c.pool, Clock: clock.Real{}}
	id, err := jobs.Submit(ctx, scheduler.Request{
		Username: "alice", Team: "team-a", Partition: "cpu", QOS: "normal", Name: "e2e",
		Kind:      executor.KindBatch,
		Resources: executor.Resources{MilliCPU: 500, MemoryBytes: 256 << 20, ScratchBytes: 1 << 30},
		TimeLimit: 5 * time.Minute, SignalGrace: 5 * time.Second,
		Workdir:    "/mist/teams/team-a/alice",
		OutputPath: "/mist/teams/team-a/alice/mist-e2e.out",
		Script:     "echo hello from $MIST_JOB_ID on $MIST_NODE\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	state := func() (st string, released bool) {
		c.pool.QueryRow(ctx, "SELECT state FROM jobs WHERE id = $1", id).Scan(&st)
		var rel *time.Time
		c.pool.QueryRow(ctx, "SELECT released_at FROM allocations WHERE job_id = $1", id).Scan(&rel)
		return st, rel != nil
	}
	handled, cleaned := 0, false
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if err := s.Cycle(ctx); err != nil {
			t.Fatal(err)
		}
		c.mu.Lock()
		evs := append([]executor.Event(nil), c.events[id]...)
		c.mu.Unlock()
		for ; handled < len(evs); handled++ {
			if err := s.HandleEvent(ctx, evs[handled]); err != nil {
				t.Fatal(err)
			}
		}
		st, released := state()
		if st == "CLEANING" && !cleaned {
			if released {
				t.Fatal("allocation released before the node confirmed cleanup")
			}
			var execID string
			c.pool.QueryRow(ctx, "SELECT execution_id::text FROM jobs WHERE id = $1", id).Scan(&execID)
			c.cleanup(executor.LaunchSpec{JobID: id, ExecutionID: execID})
			cleaned = true
		}
		if st == "COMPLETED" && released {
			out := c.output(executor.LaunchSpec{OutputPath: "/mist/teams/team-a/alice/mist-e2e.out"})
			if !strings.Contains(out, fmt.Sprintf("hello from %d on %s", id, c.node)) {
				t.Fatalf("output:\n%s", out)
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	st, released := state()
	t.Fatalf("job %d ended in %s (released=%v)", id, st, released)
}
