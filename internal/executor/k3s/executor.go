package k3s

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	listersv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"github.com/UTMIST/Mist/internal/clock"
	"github.com/UTMIST/Mist/internal/executor"
)

// Options are the runtime settings beyond the pod builder's Config.
type Options struct {
	Config
	Client kubernetes.Interface
	Pool   *pgxpool.Pool
	Clock  clock.Clock
	Log    *slog.Logger
	// Resync re-examines every execution this often (default 60 s).
	Resync time.Duration
	// LostAfter is how long a node must be NotReady before its unfinished
	// executions are reported Lost (default 2 min).
	LostAfter time.Duration
	// TailBytes of output kept in the database (default 64 KiB).
	TailBytes int
}

// Executor implements executor.Executor on k3s.
type Executor struct {
	opt    Options
	client kubernetes.Interface
	store  store
	clock  clock.Clock
	log    *slog.Logger
	events *executor.Queue

	pods    listersv1.PodLister
	nodes   listersv1.NodeLister
	synced  []cache.InformerSynced
	queue   workqueue.TypedRateLimitingInterface[int64]
	started chan struct{}

	jobLocks sync.Map // int64 -> *sync.Mutex; serializes launch/stop/sync per job
}

// New wires informers but starts nothing; call Start.
func New(opt Options) *Executor {
	if opt.Clock == nil {
		opt.Clock = clock.Real{}
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.Resync == 0 {
		opt.Resync = 60 * time.Second
	}
	if opt.LostAfter == 0 {
		opt.LostAfter = 2 * time.Minute
	}
	if opt.TailBytes == 0 {
		opt.TailBytes = 64 << 10
	}
	e := &Executor{
		opt:     opt,
		client:  opt.Client,
		store:   store{opt.Pool},
		clock:   opt.Clock,
		log:     opt.Log,
		events:  executor.NewQueue(),
		queue:   workqueue.NewTypedRateLimitingQueue(workqueue.DefaultTypedControllerRateLimiter[int64]()),
		started: make(chan struct{}),
	}
	return e
}

func (e *Executor) Name() string { return "k3s" }

func (e *Executor) Events() <-chan executor.Event { return e.events.C() }

// Start runs informers and the sync worker until ctx ends. It returns once
// caches have synced; before that List reports an incomplete observation.
func (e *Executor) Start(ctx context.Context) error {
	podFactory := informers.NewSharedInformerFactoryWithOptions(e.client, e.opt.Resync,
		informers.WithNamespace(e.opt.Namespace),
		informers.WithTweakListOptions(func(o *metav1.ListOptions) { o.LabelSelector = LabelJobID }))
	nodeFactory := informers.NewSharedInformerFactory(e.client, e.opt.Resync)
	podInf := podFactory.Core().V1().Pods()
	nodeInf := nodeFactory.Core().V1().Nodes()
	e.pods, e.nodes = podInf.Lister(), nodeInf.Lister()
	e.synced = []cache.InformerSynced{podInf.Informer().HasSynced, nodeInf.Informer().HasSynced}

	enqueuePod := func(obj any) {
		if tomb, ok := obj.(cache.DeletedFinalStateUnknown); ok {
			obj = tomb.Obj
		}
		if p, ok := obj.(*corev1.Pod); ok {
			if id, err := strconv.ParseInt(p.Labels[LabelJobID], 10, 64); err == nil {
				e.queue.Add(id)
			}
		}
	}
	podInf.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    enqueuePod,
		UpdateFunc: func(_, obj any) { enqueuePod(obj) },
		DeleteFunc: enqueuePod,
	})
	// Node changes can make executions Lost: re-examine everything.
	nodeInf.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		UpdateFunc: func(_, _ any) { e.enqueueAll(ctx) },
	})

	podFactory.Start(ctx.Done())
	nodeFactory.Start(ctx.Done())
	if !cache.WaitForCacheSync(ctx.Done(), e.synced...) {
		return fmt.Errorf("k3s: informer caches did not sync")
	}
	close(e.started)

	go func() {
		<-ctx.Done()
		e.queue.ShutDown()
		e.events.Close()
	}()
	go e.worker(ctx)
	go func() {
		for {
			e.enqueueAll(ctx)
			t := e.clock.NewTimer(e.opt.Resync)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C():
			}
		}
	}()
	return nil
}

func (e *Executor) enqueueAll(ctx context.Context) {
	recs, err := e.store.all(ctx)
	if err != nil {
		e.log.Warn("k3s: list records", "err", err)
		return
	}
	for _, r := range recs {
		if r.CleanedAt == nil {
			e.queue.Add(r.JobID)
		}
	}
}

func (e *Executor) worker(ctx context.Context) {
	for {
		id, quit := e.queue.Get()
		if quit {
			return
		}
		if err := e.sync(ctx, id); err != nil {
			e.log.Warn("k3s: sync", "job", id, "err", err)
			e.queue.AddRateLimited(id)
		} else {
			e.queue.Forget(id)
		}
		e.queue.Done(id)
	}
}

func (e *Executor) lock(jobID int64) func() {
	m, _ := e.jobLocks.LoadOrStore(jobID, &sync.Mutex{})
	m.(*sync.Mutex).Lock()
	return m.(*sync.Mutex).Unlock
}

func (e *Executor) cachesSynced() bool {
	select {
	case <-e.started:
	default:
		return false
	}
	for _, s := range e.synced {
		if !s() {
			return false
		}
	}
	return true
}

// Launch ensures the one Pod for spec exists. See executor.Executor.
func (e *Executor) Launch(ctx context.Context, spec executor.LaunchSpec) error {
	cm, pod, err := Build(spec, e.opt.Config)
	if err != nil {
		return err
	}
	defer e.lock(spec.JobID)()

	rec, err := e.store.byJob(ctx, spec.JobID)
	if err != nil {
		return err
	}
	if rec != nil {
		if rec.ExecutionID != spec.ExecutionID || rec.SpecHash != spec.SpecHash {
			return executor.ErrSpecConflict
		}
		if rec.StartedAt != nil || rec.FinishedAt != nil || rec.LostAt != nil {
			return executor.ErrExecutionStarted
		}
		if rec.ObjectUID != "" {
			// Read back from the API, not the cache: the Pod must be the very
			// object we recorded. If it is gone it may have run; never recreate.
			live, err := e.client.CoreV1().Pods(e.opt.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
			switch {
			case apierrors.IsNotFound(err):
				return executor.ErrExecutionStarted
			case err != nil:
				return err
			case string(live.UID) != rec.ObjectUID:
				return executor.ErrSpecConflict
			}
			return nil
		}
	} else if err := e.store.createIntent(ctx, spec, e.clock.Now()); err != nil {
		return err
	}

	// ConfigMap first, then Pod, then the owner reference. A crash between
	// steps is repaired by the next Launch or by orphan collection.
	if _, err := e.client.CoreV1().ConfigMaps(e.opt.Namespace).Create(ctx, cm, metav1.CreateOptions{}); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return err
		}
		live, err := e.client.CoreV1().ConfigMaps(e.opt.Namespace).Get(ctx, cm.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if !sameExecution(live.ObjectMeta, spec) {
			return executor.ErrSpecConflict // never overwrite another spec
		}
	}
	created, err := e.client.CoreV1().Pods(e.opt.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		created, err = e.client.CoreV1().Pods(e.opt.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err == nil && !sameExecution(created.ObjectMeta, spec) {
			return executor.ErrSpecConflict
		}
	}
	if err != nil {
		return err // ambiguous: the next Launch reads back by name
	}
	if err := e.recordUID(ctx, spec.ExecutionID, string(created.UID)); err != nil {
		return err
	}
	owner := fmt.Sprintf(`{"metadata":{"ownerReferences":[{"apiVersion":"v1","kind":"Pod","name":%q,"uid":%q}]}}`,
		created.Name, created.UID)
	if _, err := e.client.CoreV1().ConfigMaps(e.opt.Namespace).Patch(ctx, cm.Name, types.MergePatchType,
		[]byte(owner), metav1.PatchOptions{}); err != nil {
		e.log.Warn("k3s: set ConfigMap owner", "job", spec.JobID, "err", err)
	}
	return nil
}

func sameExecution(m metav1.ObjectMeta, spec executor.LaunchSpec) bool {
	return m.Labels[LabelExecutionID] == spec.ExecutionID && m.Annotations[AnnotSpecHash] == spec.SpecHash
}

func (e *Executor) recordUID(ctx context.Context, execID, uid string) error {
	_, err := e.store.update(ctx, execID, executor.ReceiptCreated, e.clock.Now(), uid, "",
		"object_uid = $2", "object_uid IS NULL", uid)
	return err
}

// Terminate shortens the Pod's activeDeadlineSeconds rather than deleting
// it: kubelet then sends SIGTERM and SIGKILL after the Pod's grace period,
// and keeps the final status so the result can be saved before deletion.
// The grace is fixed in the Pod at creation (the job's signal grace).
func (e *Executor) Terminate(ctx context.Context, jobID int64, _ time.Duration, reason string) error {
	defer e.lock(jobID)()
	rec, err := e.store.byJob(ctx, jobID)
	if err != nil || rec == nil {
		return err
	}
	if _, err := e.store.update(ctx, rec.ExecutionID, "", e.clock.Now(), "", reason,
		"terminate_requested_at = $2", "terminate_requested_at IS NULL", e.clock.Now()); err != nil {
		return err
	}
	return e.stopPod(ctx, jobID)
}

func (e *Executor) stopPod(ctx context.Context, jobID int64) error {
	pods := e.client.CoreV1().Pods(e.opt.Namespace)
	p, err := pods.Get(ctx, Name(jobID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil // not created yet; sync stops it as soon as it appears
	}
	if err != nil {
		return err
	}
	if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
		return nil
	}
	secs := int64(1)
	if p.Status.StartTime != nil {
		secs = max(int64(e.clock.Now().Sub(p.Status.StartTime.Time)/time.Second)+1, 1)
	}
	if p.Spec.ActiveDeadlineSeconds != nil && *p.Spec.ActiveDeadlineSeconds <= secs {
		return nil // already shortened (it may only decrease)
	}
	patch := fmt.Sprintf(`{"spec":{"activeDeadlineSeconds":%d}}`, secs)
	_, err = pods.Patch(ctx, p.Name, types.StrategicMergePatchType, []byte(patch),
		metav1.PatchOptions{})
	return err
}

// sync drives one job's execution from its durable record and the
// observed Pod. It is safe to run any number of times: every fact is
// recorded once, before it is acted on.
func (e *Executor) sync(ctx context.Context, jobID int64) error {
	defer e.lock(jobID)()
	rec, err := e.store.byJob(ctx, jobID)
	if err != nil {
		return err
	}
	pod, err := e.pods.Pods(e.opt.Namespace).Get(Name(jobID))
	if apierrors.IsNotFound(err) {
		pod, err = nil, nil
	}
	if err != nil {
		return err
	}
	if rec == nil || rec.CleanedAt != nil {
		return nil // unknown Pods are reported by List for the scheduler to stop
	}
	if pod != nil && (pod.Labels[LabelExecutionID] != rec.ExecutionID ||
		(rec.ObjectUID != "" && string(pod.UID) != rec.ObjectUID)) {
		e.log.Warn("k3s: Pod does not match the recorded execution", "job", jobID)
		return nil
	}

	if pod == nil {
		switch {
		case rec.FinishedAt != nil || rec.LostAt != nil:
			if rec.PodDeletedAt == nil {
				_, err = e.store.update(ctx, rec.ExecutionID, "", e.clock.Now(), "", "",
					"pod_deleted_at = $2", "pod_deleted_at IS NULL", e.clock.Now())
			}
			return err // cleanup now waits for the node's receipt
		case rec.ObjectUID != "":
			// The recorded Pod vanished before we saw it finish.
			return e.markLost(ctx, rec, "Pod deleted outside Mist before its result was recorded")
		}
		return nil // create not yet observed
	}

	if rec.ObjectUID == "" {
		if err := e.recordUID(ctx, rec.ExecutionID, string(pod.UID)); err != nil {
			return err
		}
		rec.ObjectUID = string(pod.UID)
	}
	if rec.TerminateRequestedAt != nil || rec.LostAt != nil {
		if err := e.stopPod(ctx, jobID); err != nil {
			return err
		}
	}

	cs := jobContainer(pod)
	if at := startedAt(cs); at != nil && rec.StartedAt == nil {
		applied, err := e.store.update(ctx, rec.ExecutionID, executor.ReceiptStarted, e.clock.Now(), rec.ObjectUID, "",
			"started_at = $2", "started_at IS NULL", *at)
		if err != nil {
			return err
		}
		if applied {
			rec.StartedAt = at
			e.emit(rec, executor.EventStarted, nil, *at)
		}
	}
	// Independent backstop in case the Pod deadline and the scheduler both fail.
	if rec.StartedAt != nil && rec.FinishedAt == nil && rec.TerminateRequestedAt == nil {
		var limit, grace int64
		if pod.Spec.TerminationGracePeriodSeconds != nil {
			grace = *pod.Spec.TerminationGracePeriodSeconds
		}
		if pod.Spec.ActiveDeadlineSeconds != nil {
			limit = *pod.Spec.ActiveDeadlineSeconds
		}
		if limit > 0 && e.clock.Now().After(rec.StartedAt.Add(time.Duration(limit+grace)*time.Second)) {
			if err := e.stopPod(ctx, jobID); err != nil {
				return err
			}
		}
	}

	if lost, detail := e.nodeLost(pod.Spec.NodeName); lost && rec.FinishedAt == nil && rec.LostAt == nil {
		return e.markLost(ctx, rec, detail)
	}
	if !podDone(pod) {
		return nil
	}
	if rec.FinishedAt == nil {
		res, kind := e.result(pod, cs, rec)
		at := e.clock.Now()
		if cs != nil && cs.State.Terminated != nil && !cs.State.Terminated.FinishedAt.IsZero() {
			at = cs.State.Terminated.FinishedAt.Time
		}
		resJSON, _ := json.Marshal(res)
		applied, err := e.store.update(ctx, rec.ExecutionID, executor.ReceiptFinished, e.clock.Now(), rec.ObjectUID, res.Detail,
			"finished_at = $2, result = $3, log_tail = $4", "finished_at IS NULL", at, resJSON, e.logTail(ctx, pod))
		if err != nil {
			return err
		}
		if applied {
			rec.FinishedAt, rec.Result = &at, &res
			e.emit(rec, kind, &res, at)
		}
	}
	// Result and tail are saved: delete the Pod (its ConfigMap follows).
	err = e.client.CoreV1().Pods(e.opt.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
		Preconditions: &metav1.Preconditions{UID: &pod.UID},
	})
	if apierrors.IsNotFound(err) || apierrors.IsConflict(err) {
		err = nil
	}
	return err
}

func (e *Executor) markLost(ctx context.Context, rec *record, detail string) error {
	applied, err := e.store.update(ctx, rec.ExecutionID, "", e.clock.Now(), "", detail,
		"lost_at = $2, lost_detail = $3", "lost_at IS NULL AND finished_at IS NULL", e.clock.Now(), detail)
	if err == nil && applied {
		e.emit(rec, executor.EventLost, &executor.Result{Reason: executor.ExitNodeLost, Detail: detail}, e.clock.Now())
	}
	return err
}

// nodeLost reports whether a node has been NotReady for LostAfter.
func (e *Executor) nodeLost(name string) (bool, string) {
	n, err := e.nodes.Get(name)
	if err != nil {
		return false, "" // unknown node: not proof of loss
	}
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status != corev1.ConditionTrue &&
			e.clock.Now().Sub(c.LastTransitionTime.Time) >= e.opt.LostAfter {
			return true, fmt.Sprintf("node %s unreachable since %s", name, c.LastTransitionTime.UTC().Format(time.RFC3339))
		}
	}
	return false, ""
}

func jobContainer(p *corev1.Pod) *corev1.ContainerStatus {
	for i := range p.Status.ContainerStatuses {
		if p.Status.ContainerStatuses[i].Name == "job" {
			return &p.Status.ContainerStatuses[i]
		}
	}
	return nil
}

func startedAt(cs *corev1.ContainerStatus) *time.Time {
	switch {
	case cs == nil:
	case cs.State.Running != nil && !cs.State.Running.StartedAt.IsZero():
		return &cs.State.Running.StartedAt.Time
	case cs.State.Terminated != nil && !cs.State.Terminated.StartedAt.IsZero():
		return &cs.State.Terminated.StartedAt.Time
	}
	return nil
}

func podDone(p *corev1.Pod) bool {
	if p.Status.Phase == corev1.PodSucceeded || p.Status.Phase == corev1.PodFailed {
		return true
	}
	cs := jobContainer(p)
	return cs != nil && cs.State.Terminated != nil
}

// result works out the outcome. Kubernetes' own observations (OOMKilled,
// eviction, deadline) outrank the wrapper's message, which a dying or
// hostile process could have written or failed to write.
func (e *Executor) result(p *corev1.Pod, cs *corev1.ContainerStatus, rec *record) (executor.Result, executor.EventKind) {
	var res executor.Result
	stopped := rec.TerminateRequestedAt != nil
	term := (*corev1.ContainerStateTerminated)(nil)
	if cs != nil {
		term = cs.State.Terminated
	}
	if term == nil {
		// The work never started.
		switch {
		case stopped:
			return executor.Result{Reason: executor.ExitSignaled, Detail: "stopped before start"}, executor.EventFinished
		case p.Status.Reason == "DeadlineExceeded":
			return executor.Result{Reason: executor.ExitLaunchTimeout, Detail: "did not start before the Pod deadline"}, executor.EventLaunchFailed
		}
		detail := strings.TrimSpace(p.Status.Reason + " " + p.Status.Message)
		for _, ic := range p.Status.InitContainerStatuses {
			if t := ic.State.Terminated; t != nil && t.ExitCode != 0 {
				detail = fmt.Sprintf("%s failed: %s", ic.Name, strings.TrimSpace(t.Message))
			}
		}
		return executor.Result{Reason: executor.ExitLaunchError, Detail: detail}, executor.EventLaunchFailed
	}

	json.Unmarshal([]byte(term.Message), &res) // wrapper report, advisory
	code := int(term.ExitCode)
	res.ExitCode = &code
	if term.Message == "" {
		res.OutputIncomplete = true
	}
	switch {
	case term.Reason == "OOMKilled":
		res.Reason = executor.ExitOutOfMemory
		if res.Detail == "" {
			res.Detail = "killed by the kernel for exceeding the memory limit"
		}
	case p.Status.Reason == "Evicted":
		res.Reason, res.Detail = executor.ExitEvicted, p.Status.Message
		if strings.Contains(p.Status.Message, "ephemeral") {
			res.Reason = executor.ExitScratchExceeded
		}
	case p.Status.Reason == "DeadlineExceeded" && !stopped:
		res.Reason, res.Detail = executor.ExitDeadline, "killed by the Pod deadline backstop"
	case stopped && res.Reason != executor.ExitDeadline:
		res.Reason = executor.ExitSignaled
	case res.Reason == "":
		res.Reason = executor.ExitNormal
		if code == 137 {
			res.Detail = "killed by SIGKILL; output may be incomplete"
		}
	}
	if res.Reason == executor.ExitLaunchError {
		return res, executor.EventLaunchFailed
	}
	return res, executor.EventFinished
}

func (e *Executor) logTail(ctx context.Context, p *corev1.Pod) []byte {
	b, err := e.client.CoreV1().Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{
		Container: "job", TailLines: ptr[int64](2000),
	}).DoRaw(ctx)
	if err != nil {
		e.log.Warn("k3s: log tail", "job", p.Labels[LabelJobID], "err", err)
		return nil
	}
	if len(b) > e.opt.TailBytes {
		b = b[len(b)-e.opt.TailBytes:]
	}
	return b
}

func (e *Executor) emit(rec *record, kind executor.EventKind, r *executor.Result, at time.Time) {
	ev := executor.Event{
		EventID: rec.ExecutionID + "/" + string(kind), JobID: rec.JobID, ExecutionID: rec.ExecutionID,
		Kind: kind, Node: rec.Node, At: at,
	}
	if r != nil {
		ev.ExitCode, ev.Reason, ev.Detail = r.ExitCode, r.Reason, r.Detail
	}
	e.events.Push(ev)
}

// CleanupReceipt is a node's proof that an execution left nothing behind.
type CleanupReceipt struct {
	ExecutionID string `json:"execution_id"`
	PodUID      string `json:"pod_uid"`
	Node        string `json:"node"`
	Evidence    string `json:"evidence"`
}

// RecordCleanup accepts a node cleanup receipt. Only a receipt matching
// the recorded execution, Pod UID and node, for a Pod the API no longer
// has, confirms cleanup and lets the scheduler release resources.
func (e *Executor) RecordCleanup(ctx context.Context, r CleanupReceipt) error {
	recs, err := e.store.all(ctx)
	if err != nil {
		return err
	}
	var rec *record
	for _, x := range recs {
		if x.ExecutionID == r.ExecutionID {
			rec = x
		}
	}
	if rec == nil || rec.ObjectUID == "" || rec.ObjectUID != r.PodUID || rec.Node != r.Node {
		return fmt.Errorf("k3s: cleanup receipt does not match a recorded execution")
	}
	defer e.lock(rec.JobID)()
	if rec.FinishedAt == nil && rec.LostAt == nil {
		return fmt.Errorf("k3s: execution %s has no recorded outcome yet", rec.ExecutionID)
	}
	live, err := e.client.CoreV1().Pods(e.opt.Namespace).Get(ctx, Name(rec.JobID), metav1.GetOptions{})
	if err == nil && string(live.UID) == rec.ObjectUID {
		return fmt.Errorf("k3s: Pod %s still exists", live.Name)
	} else if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	applied, err := e.store.update(ctx, rec.ExecutionID, executor.ReceiptCleaned, e.clock.Now(), r.PodUID, r.Evidence,
		"cleaned_at = $2, cleanup_evidence = $3", "cleaned_at IS NULL", e.clock.Now(), r.Evidence)
	if err == nil && applied {
		res := rec.Result
		if res == nil {
			res = &executor.Result{Reason: executor.ExitNodeLost}
		}
		x := *res
		x.Detail = r.Evidence
		e.emit(rec, executor.EventExited, &x, e.clock.Now())
	}
	return err
}

// Nodes reports node allocatable capacity. A node is healthy only if it
// is Ready, schedulable, under no pressure and supports strict
// supplemental groups; otherwise jobs could keep image-defined groups.
func (e *Executor) Nodes(context.Context) ([]executor.Node, error) {
	if !e.cachesSynced() {
		return nil, executor.ErrIncompleteObservation
	}
	list, err := e.nodes.List(labels.Everything())
	if err != nil {
		return nil, err
	}
	out := make([]executor.Node, 0, len(list))
	for _, n := range list {
		a, c := n.Status.Allocatable, n.Status.Capacity
		node := executor.Node{
			Name: n.Name,
			Capacity: executor.Resources{
				MilliCPU:     a.Cpu().MilliValue(),
				MemoryBytes:  a.Memory().Value(),
				GPUs:         quantity(a, "nvidia.com/gpu"),
				ScratchBytes: a.StorageEphemeral().Value(),
			},
			Reserved: executor.Resources{
				MilliCPU:    c.Cpu().MilliValue() - a.Cpu().MilliValue(),
				MemoryBytes: c.Memory().Value() - a.Memory().Value(),
			},
			Healthy: true,
		}
		var why []string
		for _, cond := range n.Status.Conditions {
			bad := cond.Status == corev1.ConditionTrue
			if cond.Type == corev1.NodeReady {
				bad = cond.Status != corev1.ConditionTrue
			}
			if bad {
				why = append(why, string(cond.Type))
			}
		}
		if n.Spec.Unschedulable {
			why = append(why, "cordoned")
		}
		if f := n.Status.Features; f == nil || f.SupplementalGroupsPolicy == nil || !*f.SupplementalGroupsPolicy {
			why = append(why, "no strict supplemental groups support")
		}
		if len(why) > 0 {
			node.Healthy, node.Detail = false, strings.Join(why, ", ")
		}
		out = append(out, node)
	}
	return out, nil
}

func quantity(l corev1.ResourceList, name corev1.ResourceName) int64 {
	if q, ok := l[name]; ok {
		return q.Value()
	}
	return 0
}

// List merges durable records with live Pods. Pods without a record are
// reported as unknown executions for the scheduler to stop.
func (e *Executor) List(ctx context.Context) ([]executor.Execution, error) {
	if !e.cachesSynced() {
		return nil, fmt.Errorf("k3s: caches not synced: %w", executor.ErrIncompleteObservation)
	}
	recs, err := e.store.all(ctx)
	if err != nil {
		return nil, fmt.Errorf("k3s: %w: %v", executor.ErrIncompleteObservation, err)
	}
	seen := map[int64]bool{}
	var out []executor.Execution
	for _, r := range recs {
		seen[r.JobID] = true
		x := executor.Execution{
			JobID: r.JobID, ExecutionID: r.ExecutionID, SpecHash: r.SpecHash, Node: r.Node,
			ObjectUID: r.ObjectUID, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, Result: r.Result,
			CleanupEvidence: r.CleanupEvidence,
		}
		switch {
		case r.CleanedAt != nil:
			x.Phase, x.Cleanup = executor.PhaseCleaned, executor.CleanupConfirmed
		case r.LostAt != nil:
			x.Phase, x.Cleanup = executor.PhaseLost, executor.CleanupPending
		case r.FinishedAt != nil:
			x.Phase, x.Cleanup = executor.PhaseFinished, executor.CleanupPending
		case r.StartedAt != nil:
			x.Phase = executor.PhaseRunning
		case r.ObjectUID != "":
			x.Phase = executor.PhasePending
		default:
			x.Phase = executor.PhaseCreating
		}
		out = append(out, x)
	}
	pods, err := e.pods.Pods(e.opt.Namespace).List(labels.Everything())
	if err != nil {
		return nil, err
	}
	for _, p := range pods {
		id, err := strconv.ParseInt(p.Labels[LabelJobID], 10, 64)
		if err != nil || seen[id] {
			continue
		}
		out = append(out, executor.Execution{
			JobID: id, ExecutionID: p.Labels[LabelExecutionID], Node: p.Spec.NodeName,
			ObjectUID: string(p.UID), Phase: executor.PhaseRunning,
		})
	}
	return out, nil
}

// Logs streams live output while the Pod exists, then the stored tail.
func (e *Executor) Logs(ctx context.Context, jobID int64, follow bool) (io.ReadCloser, error) {
	rc, err := e.client.CoreV1().Pods(e.opt.Namespace).GetLogs(Name(jobID), &corev1.PodLogOptions{
		Container: "job", Follow: follow,
	}).Stream(ctx)
	if err == nil {
		return rc, nil
	}
	rec, rerr := e.store.byJob(ctx, jobID)
	if rerr != nil || rec == nil {
		return nil, executor.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(string(rec.LogTail))), nil
}

func (e *Executor) Attach(context.Context, int64, executor.Terminal) error {
	return errors.New("k3s: attach arrives in M2")
}

// Receipts returns an execution's durable receipts, for diagnostics.
func (e *Executor) Receipts(ctx context.Context, executionID string) ([]executor.Receipt, error) {
	return e.store.receipts(ctx, executionID)
}

var _ executor.Executor = (*Executor)(nil)
