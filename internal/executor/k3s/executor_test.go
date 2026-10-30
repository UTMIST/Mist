package k3s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/db"
	"github.com/UTMIST/Mist/internal/db/dbtest"
	"github.com/UTMIST/Mist/internal/executor"
)

func TestMain(m *testing.M) { dbtest.Main(m) }

// kube plays kubelet: tests move Pods through their lifecycle by writing
// status, as kubelet would, and the executor reacts through its informers.
type kube struct {
	t      *testing.T
	ctx    context.Context
	client *fake.Clientset
	clock  *clock.Fake
	ex     *Executor
}

var now0 = time.Date(2026, 11, 2, 15, 0, 0, 0, time.UTC)

func newKube(t *testing.T, nodes ...*corev1.Node) *kube {
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	pool, err := db.Open(ctx, dbtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if len(nodes) == 0 {
		nodes = []*corev1.Node{readyNode("utmist-z1opa08", true)}
	}
	objs := make([]runtime.Object, 0, len(nodes))
	for _, n := range nodes {
		objs = append(objs, n)
	}
	client := fake.NewSimpleClientset(objs...)
	// The real API server assigns UIDs; the fake does not.
	uids := 0
	client.PrependReactor("create", "*", func(a k8stesting.Action) (bool, runtime.Object, error) {
		if o, ok := a.(k8stesting.CreateAction).GetObject().(metav1.Object); ok && o.GetUID() == "" {
			uids++
			o.SetUID(types.UID(fmt.Sprintf("uid-%d", uids)))
		}
		return false, nil, nil
	})
	k := &kube{t: t, ctx: ctx, client: client, clock: clock.NewFake(now0)}
	return k.start(pool)
}

func (k *kube) start(pool *pgxpool.Pool) *kube {
	k.ex = New(Options{Config: testConfig(), Client: k.client, Pool: pool, Clock: k.clock})
	if err := k.ex.Start(k.ctx); err != nil {
		k.t.Fatal(err)
	}
	return k
}

func readyNode(name string, strict bool) *corev1.Node {
	n := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("32"), corev1.ResourceMemory: resource.MustParse("128Gi")},
			Allocatable: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("30"), corev1.ResourceMemory: resource.MustParse("120Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("400Gi"), "nvidia.com/gpu": resource.MustParse("2")},
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(now0.Add(-time.Hour))}},
			Features: &corev1.NodeFeatures{SupplementalGroupsPolicy: ptr(strict)},
		},
	}
	return n
}

func (k *kube) next(kind executor.EventKind) executor.Event {
	k.t.Helper()
	select {
	case ev := <-k.ex.Events():
		if ev.Kind != kind {
			k.t.Fatalf("got %s (%s %s), want %s", ev.Kind, ev.Reason, ev.Detail, kind)
		}
		return ev
	case <-time.After(5 * time.Second):
		k.t.Fatalf("no %s event", kind)
		return executor.Event{}
	}
}

func (k *kube) quiet() {
	k.t.Helper()
	select {
	case ev := <-k.ex.Events():
		k.t.Fatalf("unexpected %s (%s)", ev.Kind, ev.Detail)
	case <-time.After(100 * time.Millisecond):
	}
}

func (k *kube) pod(id int64) *corev1.Pod {
	k.t.Helper()
	p, err := k.client.CoreV1().Pods("mist-jobs").Get(k.ctx, Name(id), metav1.GetOptions{})
	if err != nil {
		k.t.Fatal(err)
	}
	return p
}

func (k *kube) setStatus(id int64, mutate func(*corev1.PodStatus)) {
	k.t.Helper()
	p := k.pod(id)
	mutate(&p.Status)
	if _, err := k.client.CoreV1().Pods("mist-jobs").UpdateStatus(k.ctx, p, metav1.UpdateOptions{}); err != nil {
		k.t.Fatal(err)
	}
}

func running(at time.Time) func(*corev1.PodStatus) {
	return func(s *corev1.PodStatus) {
		s.Phase, s.StartTime = corev1.PodRunning, &metav1.Time{Time: at}
		s.ContainerStatuses = []corev1.ContainerStatus{{Name: "job", State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{StartedAt: metav1.NewTime(at)}}}}
	}
}

func terminated(code int32, reason string, msg *executor.Result, podReason string) func(*corev1.PodStatus) {
	return func(s *corev1.PodStatus) {
		var m []byte
		if msg != nil {
			m, _ = json.Marshal(msg)
		}
		s.Phase, s.Reason = corev1.PodFailed, podReason
		if code == 0 {
			s.Phase = corev1.PodSucceeded
		}
		s.ContainerStatuses = []corev1.ContainerStatus{{Name: "job", State: corev1.ContainerState{
			Terminated: &corev1.ContainerStateTerminated{ExitCode: code, Reason: reason, Message: string(m),
				StartedAt: metav1.NewTime(now0.Add(time.Second)), FinishedAt: metav1.NewTime(now0.Add(time.Minute))}}}}
	}
}

func (k *kube) waitGone(id int64) {
	k.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, err := k.client.CoreV1().Pods("mist-jobs").Get(k.ctx, Name(id), metav1.GetOptions{})
		if err != nil {
			return
		}
		if time.Now().After(deadline) {
			k.t.Fatal("Pod was not deleted after its result was saved")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestLaunchIsRepeatSafe(t *testing.T) {
	k := newKube(t)
	spec := gpuBatch()
	if err := k.ex.Launch(k.ctx, spec); err != nil {
		t.Fatal(err)
	}
	if err := k.ex.Launch(k.ctx, spec); err != nil {
		t.Fatalf("repeat: %v", err)
	}
	if _, err := k.client.CoreV1().ConfigMaps("mist-jobs").Get(k.ctx, Name(spec.JobID), metav1.GetOptions{}); err != nil {
		t.Fatal(err)
	}
	cm, _ := k.client.CoreV1().ConfigMaps("mist-jobs").Get(k.ctx, Name(spec.JobID), metav1.GetOptions{})
	if len(cm.OwnerReferences) != 1 || cm.OwnerReferences[0].UID != k.pod(spec.JobID).UID {
		t.Fatal("ConfigMap not owned by its Pod")
	}
	other := spec
	other.Resources.GPUs = 2
	other.Seal()
	if err := k.ex.Launch(k.ctx, other); !errors.Is(err, executor.ErrSpecConflict) {
		t.Fatalf("conflicting spec: %v", err)
	}
}

// A create whose response was lost: the Pod exists but no UID was recorded.
// The retry adopts it instead of failing or creating a second one.
func TestLaunchAdoptsAmbiguousCreate(t *testing.T) {
	k := newKube(t)
	spec := gpuBatch()
	if err := k.ex.store.createIntent(k.ctx, spec, now0); err != nil {
		t.Fatal(err)
	}
	cm, pod, _ := Build(spec, testConfig())
	k.client.CoreV1().ConfigMaps("mist-jobs").Create(k.ctx, cm, metav1.CreateOptions{})
	k.client.CoreV1().Pods("mist-jobs").Create(k.ctx, pod, metav1.CreateOptions{})
	if err := k.ex.Launch(k.ctx, spec); err != nil {
		t.Fatal(err)
	}
	rec, _ := k.ex.store.byJob(k.ctx, spec.JobID)
	if rec.ObjectUID != string(k.pod(spec.JobID).UID) {
		t.Fatal("did not adopt the existing Pod")
	}
}

func TestLifecycleAndCleanup(t *testing.T) {
	k := newKube(t)
	spec := gpuBatch()
	k.ex.Launch(k.ctx, spec)
	k.setStatus(spec.JobID, running(now0.Add(time.Second)))
	if ev := k.next(executor.EventStarted); !ev.At.Equal(now0.Add(time.Second)) {
		t.Fatalf("start time %v is not the container's", ev.At)
	}
	uid := string(k.pod(spec.JobID).UID)
	k.setStatus(spec.JobID, terminated(0, "Completed", &executor.Result{Reason: executor.ExitNormal}, ""))
	ev := k.next(executor.EventFinished)
	if *ev.ExitCode != 0 || ev.Reason != executor.ExitNormal {
		t.Fatalf("finished %+v", ev)
	}
	k.waitGone(spec.JobID)
	rec, _ := k.ex.store.byJob(k.ctx, spec.JobID)
	if len(rec.LogTail) == 0 {
		t.Fatal("log tail not saved before deletion")
	}
	k.quiet() // deletion alone is not cleanup

	for _, bad := range []CleanupReceipt{
		{ExecutionID: spec.ExecutionID, PodUID: "someone-else", Node: spec.Node},
		{ExecutionID: spec.ExecutionID, PodUID: uid, Node: "utmist-tt"},
	} {
		if err := k.ex.RecordCleanup(k.ctx, bad); err == nil {
			t.Fatalf("mismatched receipt accepted: %+v", bad)
		}
	}
	good := CleanupReceipt{ExecutionID: spec.ExecutionID, PodUID: uid, Node: spec.Node, Evidence: "pod dir and cgroup gone"}
	if err := k.ex.RecordCleanup(k.ctx, good); err != nil {
		t.Fatal(err)
	}
	k.next(executor.EventExited)
	k.ex.RecordCleanup(k.ctx, good) // repeated receipt: no second event
	k.quiet()

	if err := k.ex.Launch(k.ctx, spec); !errors.Is(err, executor.ErrExecutionStarted) {
		t.Fatalf("relaunch after finish: %v", err)
	}
}

func TestResults(t *testing.T) {
	cases := []struct {
		name      string
		terminate bool
		status    func(*corev1.PodStatus)
		kind      executor.EventKind
		reason    executor.ExitReason
	}{
		{"kernel OOM outranks wrapper", false,
			terminated(137, "OOMKilled", &executor.Result{Reason: executor.ExitNormal}, ""),
			executor.EventFinished, executor.ExitOutOfMemory},
		{"wrapper killed, no message", false, terminated(137, "Error", nil, ""),
			executor.EventFinished, executor.ExitNormal},
		{"scratch eviction", false, terminated(137, "Error", nil, "Evicted"),
			executor.EventFinished, executor.ExitEvicted},
		{"Pod deadline backstop", false, terminated(143, "Error", nil, "DeadlineExceeded"),
			executor.EventFinished, executor.ExitDeadline},
		{"terminated by Mist", true, terminated(143, "Error", &executor.Result{Reason: executor.ExitSignaled}, "DeadlineExceeded"),
			executor.EventFinished, executor.ExitSignaled},
		{"wrapper setup failure", false, terminated(125, "Error", &executor.Result{Reason: executor.ExitLaunchError, Detail: "bad output path"}, ""),
			executor.EventLaunchFailed, executor.ExitLaunchError},
		{"never started", false, func(s *corev1.PodStatus) { s.Phase, s.Reason = corev1.PodFailed, "DeadlineExceeded" },
			executor.EventLaunchFailed, executor.ExitLaunchTimeout},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			k := newKube(t)
			spec := gpuBatch()
			k.ex.Launch(k.ctx, spec)
			if c.terminate {
				if err := k.ex.Terminate(k.ctx, spec.JobID, time.Minute, "cancelled"); err != nil {
					t.Fatal(err)
				}
				if d := k.pod(spec.JobID).Spec.ActiveDeadlineSeconds; d == nil || *d > 2 {
					t.Fatalf("Terminate did not shorten the deadline: %v", d)
				}
			}
			k.setStatus(spec.JobID, c.status)
			if c.name != "never started" { // the container itself started
				k.next(executor.EventStarted)
			}
			ev := k.next(c.kind)
			if ev.Reason != c.reason {
				t.Fatalf("reason %s (%s), want %s", ev.Reason, ev.Detail, c.reason)
			}
		})
	}
}

func TestPodDeletedOutsideMistIsLost(t *testing.T) {
	k := newKube(t)
	spec := gpuBatch()
	k.ex.Launch(k.ctx, spec)
	k.setStatus(spec.JobID, running(now0))
	k.next(executor.EventStarted)
	k.client.CoreV1().Pods("mist-jobs").Delete(k.ctx, Name(spec.JobID), metav1.DeleteOptions{})
	k.next(executor.EventLost)
	if err := k.ex.Launch(k.ctx, spec); !errors.Is(err, executor.ErrExecutionStarted) {
		t.Fatalf("relaunch of lost execution: %v", err)
	}
}

func TestNodeNotReadyIsLostAfterGrace(t *testing.T) {
	k := newKube(t)
	spec := gpuBatch()
	k.ex.Launch(k.ctx, spec)
	k.setStatus(spec.JobID, running(now0))
	k.next(executor.EventStarted)

	n, _ := k.client.CoreV1().Nodes().Get(k.ctx, spec.Node, metav1.GetOptions{})
	n.Status.Conditions[0].Status = corev1.ConditionUnknown
	n.Status.Conditions[0].LastTransitionTime = metav1.NewTime(now0)
	k.client.CoreV1().Nodes().UpdateStatus(k.ctx, n, metav1.UpdateOptions{})
	k.quiet() // not yet 2 minutes
	nodes, _ := k.ex.Nodes(k.ctx)
	if nodes[0].Healthy {
		t.Fatal("NotReady node reported healthy")
	}

	k.clock.Set(now0.Add(2 * time.Minute))
	n.Status.Conditions[0].Message = "still unreachable" // any update triggers a resync
	k.client.CoreV1().Nodes().UpdateStatus(k.ctx, n, metav1.UpdateOptions{})
	k.next(executor.EventLost)
}

func TestNodesRequireStrictGroups(t *testing.T) {
	k := newKube(t, readyNode("utmist-z1opa08", true), readyNode("old-node", false))
	nodes, err := k.ex.Nodes(k.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range nodes {
		if n.Name == "old-node" && n.Healthy {
			t.Fatal("node without strict supplemental groups reported healthy")
		}
		if n.Name == "utmist-z1opa08" && (!n.Healthy || n.Capacity.GPUs != 2 || n.Capacity.MilliCPU != 30000 || n.Reserved.MilliCPU != 2000) {
			t.Fatalf("node %+v", n)
		}
	}
}

func TestListReportsUnknownPods(t *testing.T) {
	k := newKube(t)
	_, pod, _ := Build(cpuCommand(), testConfig())
	k.client.CoreV1().Pods("mist-jobs").Create(k.ctx, pod, metav1.CreateOptions{}) // nobody recorded it
	deadline := time.Now().Add(5 * time.Second)
	for {
		xs, err := k.ex.List(k.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(xs) == 1 && xs[0].JobID == 413 && xs[0].Phase == executor.PhaseRunning {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("unknown Pod not listed: %+v", xs)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestListBeforeSyncIsIncomplete(t *testing.T) {
	ex := New(Options{Config: testConfig(), Client: fake.NewSimpleClientset()})
	if _, err := ex.List(context.Background()); !errors.Is(err, executor.ErrIncompleteObservation) {
		t.Fatalf("want incomplete observation, got %v", err)
	}
}
