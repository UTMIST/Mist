package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func (s *TeamService) submitTeam(ctx context.Context, e *KubernetesExecutor, request CreateJobRequest) (*KubernetesJobStatus, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.team(ctx, e.team.Team.ID)
	if err != nil {
		return nil, err
	}
	e.team.Team = t
	req, err := e.normalize(request)
	if err != nil {
		return nil, &submissionError{err}
	}
	if err = s.provisionNamespace(ctx, t); err != nil {
		return nil, err
	}
	jobs, err := e.client.BatchV1().Jobs(e.namespace).List(ctx, metav1.ListOptions{LabelSelector: managedLabel + "=mist"})
	if err != nil {
		return nil, err
	}
	queued := 0
	for _, job := range jobs.Items {
		if !terminal(&job) && job.Annotations[cancelAnnotation] == "" && job.Annotations["mist.io/admitted-at"] == "" {
			queued++
		}
	}
	if queued >= t.Policy.Queued {
		return nil, &submissionError{errors.New("team queue is full; wait for jobs to start or cancel a queued job")}
	}
	id, err := randomJobID()
	if err != nil {
		return nil, err
	}
	var source *Team
	if req.DatasetID != "" {
		var ds *Dataset
		_, source, ds, err = s.resolveDataset(ctx, t, req.DatasetID)
		if err != nil {
			return nil, &submissionError{errors.New("dataset is unavailable to this team")}
		}
		e.inputSubPath = ds.ContentPath
		if source.ID != t.ID {
			claim := "input-" + req.DatasetID
			if err = s.ensureVolume(ctx, source.ID, e.namespace, claim, "teams/"+source.ID+"/"+ds.ContentPath, source.Policy.StorageGiB, true); err != nil {
				return nil, err
			}
			e.inputPVC, e.inputSubPath = claim, ""
		}
	}
	e.outputSubPath = scopePath(req.StorageScope) + "/jobs/" + id + "/outputs"
	output := filepath.Join(e.storage.root, e.outputSubPath)
	if err = os.MkdirAll(output, 0755); err != nil {
		return nil, err
	}
	if err = os.Chmod(output, 0777); err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(filepath.Dir(output))
		}
	}()
	if req.Script != "" {
		_, err = e.client.CoreV1().ConfigMaps(e.namespace).Create(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: id + "-script", Labels: map[string]string{managedLabel: "mist"}}, Data: map[string]string{req.ScriptName: req.Script}}, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}
	job := e.buildJob(id, req)
	job.Spec.Suspend = ptr(true)
	if source != nil {
		job.Annotations["mist.io/input-team"] = source.ID
	}
	job, err = e.client.BatchV1().Jobs(e.namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		if req.Script != "" {
			_ = e.client.CoreV1().ConfigMaps(e.namespace).Delete(ctx, id+"-script", metav1.DeleteOptions{})
		}
		return nil, err
	}
	if req.Script != "" {
		defer func() {
			if !committed {
				propagation := metav1.DeletePropagationForeground
				_ = e.client.BatchV1().Jobs(e.namespace).Delete(ctx, id, metav1.DeleteOptions{PropagationPolicy: &propagation})
				_ = e.client.CoreV1().ConfigMaps(e.namespace).Delete(ctx, id+"-script", metav1.DeleteOptions{})
			}
		}()

		cm, readErr := e.client.CoreV1().ConfigMaps(e.namespace).Get(ctx, id+"-script", metav1.GetOptions{})
		if readErr != nil {
			return nil, readErr
		}
		cm.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}
		if _, err = e.client.CoreV1().ConfigMaps(e.namespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
			return nil, err
		}
	}
	committed = true
	return e.baseStatus(job)
}
func randomJobID() (string, error) {
	id, err := randomID("mist-")
	if err != nil {
		return "", err
	}
	suffix, err := randomID("")
	if err != nil {
		return "", err
	}
	return id + suffix[:8], nil
}
func (s *TeamService) start() {
	go func() {
		defer close(s.done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			s.mu.Lock()
			err := s.reconcile(ctx)
			s.mu.Unlock()
			cancel()
			if err != nil {
				s.app.log.Error("team reconciliation failed", "error", err)
			}
			select {
			case <-s.stop:
				return
			case <-ticker.C:
			}
		}
	}()
}
func (s *TeamService) shutdown() { close(s.stop); <-s.done }
func (s *TeamService) reconcile(ctx context.Context) error {
	_, teams, err := s.registry(ctx)
	if err != nil {
		return err
	}
	for i := range teams {
		t := &teams[i]
		if err = s.provisionRequest(t); err != nil {
			return err
		}
		if _, err = s.store(t); err != nil {
			continue
		}
		if err = s.provisionNamespace(ctx, t); err != nil {
			return err
		}
	}
	if err = s.revokeJobs(ctx); err != nil {
		return err
	}
	return s.admit(ctx, teams)
}
func (s *TeamService) revoked(ctx context.Context, t *Team, job *batchv1.Job, active map[string]AccountStatus) (string, error) {
	if t == nil || t.Disabled {
		return "Team disabled or unavailable", nil
	}
	id := job.Annotations["mist.io/creator-id"]
	if t.member(id) == nil {
		return "Team membership revoked", nil
	}
	if !active[id].Active {
		return "Account deactivated", nil
	}
	var err error
	var req CreateJobRequest
	if err = json.Unmarshal([]byte(job.Annotations[requestAnnotation]), &req); err != nil {
		return "Invalid stored submission", nil
	}
	m := &Member{ID: id, Role: active[id].Role}
	if err = validateTeamRequest(t, m, &req); err != nil {
		return "Team policy changed: " + err.Error(), nil
	}
	if err = validateImageReference(req.Image, t.Policy.Registries, s.app.executor.allowedImages); err != nil {
		return "Image policy changed", nil
	}
	if req.DatasetID != "" {
		if _, _, _, err = s.resolveDataset(ctx, t, req.DatasetID); err != nil {
			return "Dataset access revoked", nil
		}
	}
	return "", nil
}
func (s *TeamService) revokeJobs(ctx context.Context) error {
	_, teams, err := s.registry(ctx)
	if err != nil {
		return err
	}
	// A fresh authoritative account snapshot is used for each reconciliation, so
	// disabling an account also releases jobs even if its team membership remains.
	active, err := s.app.auth.activeMembers(ctx)
	if err != nil {
		return err
	}
	jobs, err := s.app.executor.client.BatchV1().Jobs("").List(ctx, metav1.ListOptions{LabelSelector: "mist.io/team"})
	if err != nil {
		return err
	}
	for _, job := range jobs.Items {
		if terminal(&job) || job.Annotations[cancelAnnotation] != "" {
			continue
		}
		reason, err := s.revoked(ctx, findTeam(teams, job.Labels["mist.io/team"]), &job, active)
		if err != nil {
			return err
		}
		if reason != "" {
			if err = s.stopJob(ctx, &job, reason); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *TeamService) stopJob(ctx context.Context, job *batchv1.Job, reason string) error {
	e := *s.app.executor
	e.namespace, e.owner = job.Namespace, job.Labels["mist.io/owner"]
	logs, err := e.logs(ctx, job.Name)
	if err != nil {
		logs = "Logs unavailable at cancellation: " + err.Error()
	}
	if len(logs) > 32*1024 {
		logs = logs[len(logs)-32*1024:]
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := e.managedJob(ctx, job.Name)
		if err != nil {
			return err
		}
		if terminal(current) || current.Annotations[cancelAnnotation] != "" {
			return nil
		}
		current.Spec.Suspend = ptr(true)
		current.Annotations[cancelAnnotation] = time.Now().UTC().Format(time.RFC3339)
		current.Annotations[cancelLogsAnnotation] = logs
		current.Annotations["mist.io/stop-reason"] = reason
		_, err = e.client.BatchV1().Jobs(job.Namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	})
}

type admissionUse struct {
	CPU, Memory            int64
	NVIDIA, TT, Concurrent int
}

func (u *admissionUse) add(req CreateJobRequest) {
	u.CPU += quantity(req.CPU).MilliValue()
	u.Memory += quantity(req.Memory).Value()
	if req.Accelerator == "nvidia" {
		u.NVIDIA += req.DeviceCount
	}
	if req.Accelerator == "tenstorrent" {
		u.TT += req.DeviceCount
	}
	u.Concurrent++
}
func (u admissionUse) fits(p TeamPolicy, req CreateJobRequest) bool {
	v := u
	v.add(req)
	return v.CPU <= quantity(p.CPU).MilliValue() && v.Memory <= quantity(p.Memory).Value() && v.NVIDIA <= p.NVIDIA && v.TT <= p.Tenstorrent && v.Concurrent <= p.Concurrent
}
func (s *TeamService) admit(ctx context.Context, teams []Team) error {
	c := s.app.executor.client
	jobs, err := c.BatchV1().Jobs("").List(ctx, metav1.ListOptions{LabelSelector: "mist.io/team"})
	if err != nil {
		return err
	}
	pods, err := c.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	snapshot, err := s.app.executor.hardware(ctx)
	if err != nil {
		return err
	}
	machineFree := map[string]*admissionUse{}
	ready := map[string]bool{}
	for _, m := range snapshot.Machines {
		machineFree[m.Name] = &admissionUse{CPU: quantity(m.CPUAllocatable).MilliValue(), Memory: quantity(m.MemoryAllocatable).Value()}
		ready[m.Name] = m.Schedulable
	}
	nvidiaFree, ttFree := 0, 0
	for _, pool := range snapshot.Pools {
		if pool.Ready && pool.Available != nil {
			if pool.Accelerator == "nvidia" {
				nvidiaFree = int(*pool.Available)
			} else {
				ttFree = int(*pool.Available)
			}
		}
	}
	placed := map[string]bool{}
	ttAllocated := map[string]int{}
	podJobs := map[string]string{}
	claimJobs := map[string]string{}
	for _, pod := range pods.Items {
		jobName := pod.Labels["batch.kubernetes.io/job-name"]
		if jobName == "" {
			continue
		}
		jobKey := pod.Namespace + "/" + jobName
		podJobs[pod.Namespace+"/"+pod.Name] = jobKey
		for _, status := range pod.Status.ResourceClaimStatuses {
			if status.ResourceClaimName != nil {
				claimJobs[pod.Namespace+"/"+*status.ResourceClaimName] = jobKey
			}
		}
	}
	claims, err := c.ResourceV1().ResourceClaims("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, claim := range claims.Items {
		if claim.Status.Allocation == nil {
			continue
		}
		for _, owner := range claim.OwnerReferences {
			if owner.Kind == "Job" {
				claimJobs[claim.Namespace+"/"+claim.Name] = claim.Namespace + "/" + owner.Name
			} else if owner.Kind == "Pod" {
				if jobKey := podJobs[claim.Namespace+"/"+owner.Name]; jobKey != "" {
					claimJobs[claim.Namespace+"/"+claim.Name] = jobKey
				}
			}
		}
		if jobKey := claimJobs[claim.Namespace+"/"+claim.Name]; jobKey != "" {
			for _, result := range claim.Status.Allocation.Devices.Results {
				if result.Driver == "tenstorrent.com" {
					ttAllocated[jobKey]++
				}
			}
		}
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		if pod.Spec.NodeName != "" {
			placed[pod.Namespace+"/"+pod.Labels["batch.kubernetes.io/job-name"]] = true
			if free := machineFree[pod.Spec.NodeName]; free != nil {
				var cpu, mem int64
				for _, ct := range pod.Spec.Containers {
					a, b := ct.Resources.Requests[corev1.ResourceCPU], ct.Resources.Requests[corev1.ResourceMemory]
					cpu += a.MilliValue()
					mem += b.Value()
				}
				free.CPU -= cpu
				free.Memory -= mem
			}
		}
	}
	use := map[string]*admissionUse{}
	queue := map[string][]batchv1.Job{}
	for _, t := range teams {
		use[t.ID] = &admissionUse{}
	}
	for _, job := range jobs.Items {
		if terminal(&job) || job.Annotations[cancelAnnotation] != "" {
			continue
		}
		var req CreateJobRequest
		if json.Unmarshal([]byte(job.Annotations[requestAnnotation]), &req) != nil {
			continue
		}
		id := job.Labels["mist.io/team"]
		if use[id] == nil {
			continue
		}
		if job.Annotations["mist.io/admitted-at"] == "" {
			if time.Since(queuedAt(&job)) > 24*time.Hour {
				if err = s.stopJob(ctx, &job, "Queue wait exceeded 24 hours"); err != nil {
					return err
				}
				continue
			}
			queue[id] = append(queue[id], job)
		} else {
			use[id].add(req)
			if !placed[job.Namespace+"/"+job.Name] {
				node := s.app.executor.cpuNode
				if req.Accelerator == "tenstorrent" {
					node = s.app.executor.ttNode
					ttFree -= max(0, req.DeviceCount-ttAllocated[job.Namespace+"/"+job.Name])
				}
				if req.Accelerator == "nvidia" {
					nvidiaFree -= req.DeviceCount
				}
				if free := machineFree[node]; free != nil {
					free.CPU -= quantity(req.CPU).MilliValue()
					free.Memory -= quantity(req.Memory).Value()
				}
			}
		}
	}
	cursor, err := c.CoreV1().ConfigMaps("mist-system").Get(ctx, "mist-queue-cursor", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		cursor = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "mist-queue-cursor"}, Data: map[string]string{}}
	} else if err != nil {
		return err
	}
	sortedTeams(teams)
	start := 0
	for i, t := range teams {
		if t.ID == cursor.Data["last_team"] {
			start = (i + 1) % len(teams)
		}
	}
	aged := map[string]batchv1.Job{}
	for _, t := range teams {
		q := queue[t.ID]
		sort.Slice(q, func(i, j int) bool { return queuedAt(&q[i]).Before(queuedAt(&q[j])) })
		queue[t.ID] = q
		if t.Disabled || len(q) == 0 || time.Since(queuedAt(&q[0])) < 30*time.Second {
			continue
		}
		var req CreateJobRequest
		_ = json.Unmarshal([]byte(q[0].Annotations[requestAnnotation]), &req)
		if !use[t.ID].fits(t.Policy, req) {
			continue
		}
		node := s.app.executor.cpuNode
		if req.Accelerator == "tenstorrent" {
			node = s.app.executor.ttNode
		}
		if !ready[node] {
			continue
		}
		if old, ok := aged[node]; !ok || queuedAt(&q[0]).Before(queuedAt(&old)) {
			aged[node] = q[0]
		}
	}
	admitted := false
	for i := 0; i < len(teams); i++ {
		t := teams[(start+i)%len(teams)]
		if t.Disabled {
			continue
		}
		q := queue[t.ID]
		sort.Slice(q, func(i, j int) bool { return queuedAt(&q[i]).Before(queuedAt(&q[j])) })
		// FIFO within a team; one admission per team per pass. Large requests cannot
		// be perpetually overtaken by smaller later submissions from the same team.
		if len(q) == 0 {
			continue
		}
		job := q[0]
		var req CreateJobRequest
		if json.Unmarshal([]byte(job.Annotations[requestAnnotation]), &req) != nil {
			continue
		}
		if !use[t.ID].fits(t.Policy, req) {
			continue
		}
		node := s.app.executor.cpuNode
		if req.Accelerator == "tenstorrent" {
			node = s.app.executor.ttNode
		}
		if reserved, ok := aged[node]; ok && reserved.Name != job.Name {
			continue
		}
		free := machineFree[node]
		if free == nil || !ready[node] || quantity(req.CPU).MilliValue() > free.CPU || quantity(req.Memory).Value() > free.Memory {
			continue
		}
		if req.Accelerator == "nvidia" && req.DeviceCount > nvidiaFree || req.Accelerator == "tenstorrent" && req.DeviceCount > ttFree {
			continue
		}
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			fresh, err := c.BatchV1().Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if fresh.Annotations[cancelAnnotation] != "" || fresh.Annotations["mist.io/admitted-at"] != "" {
				return nil
			}
			fresh.Spec.Suspend = ptr(false)
			fresh.Annotations["mist.io/admitted-at"] = time.Now().UTC().Format(time.RFC3339Nano)
			_, err = c.BatchV1().Jobs(job.Namespace).Update(ctx, fresh, metav1.UpdateOptions{})
			return err
		})
		if err != nil {
			return err
		}
		admitted = true
		cursor.Data["last_team"] = t.ID
		use[t.ID].add(req)
		free.CPU -= quantity(req.CPU).MilliValue()
		free.Memory -= quantity(req.Memory).Value()
		if req.Accelerator == "nvidia" {
			nvidiaFree -= req.DeviceCount
		}
		if req.Accelerator == "tenstorrent" {
			ttFree -= req.DeviceCount
		}
	}
	if admitted {
		if cursor.ResourceVersion == "" {
			_, err = c.CoreV1().ConfigMaps("mist-system").Create(ctx, cursor, metav1.CreateOptions{})
		} else {
			_, err = c.CoreV1().ConfigMaps("mist-system").Update(ctx, cursor, metav1.UpdateOptions{})
		}
		return err
	}
	return nil
}

// Silence is not successful admission: a queued job remains suspended and its
// status explains that resource availability and team limits govern placement.

func queuedAt(job *batchv1.Job) time.Time {
	t, err := time.Parse(time.RFC3339Nano, job.Annotations["mist.io/queued-at"])
	if err == nil {
		return t
	}
	return job.CreationTimestamp.Time
}
