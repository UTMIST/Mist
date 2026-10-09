package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
)

const (
	managedLabel               = "app.kubernetes.io/managed-by"
	requestAnnotation          = "mist.io/submission"
	cancelAnnotation           = "mist.io/cancelled-at"
	cancelLogsAnnotation       = "mist.io/cancelled-logs"
	ttImage                    = "ghcr.io/tenstorrent/tt-metal/tt-metalium/ubuntu-22.04-dev-amd64:6b765c0bc2b40ea02566d169d7b0ee902377ba04"
	nvidiaImage                = "pytorch/pytorch:2.5.1-cuda12.4-cudnn9-runtime"
	cpuImage                   = "python:3.10-slim"
	ttMetal                    = "/home/utmist-tt/TT/tt-metal"
	ttPython                   = ttMetal + "/python_env/bin/python"
	maxLogBytes          int64 = 64 * 1024
)

type KubernetesExecutor struct {
	client          kubernetes.Interface
	namespace       string
	owner           string
	allowedImages   map[string]bool
	cpuNode, ttNode string
}

func NewKubernetesApp(executor *KubernetesExecutor, log *slog.Logger) *App {
	mux := http.NewServeMux()
	a := &App{executor: executor, log: log, httpServer: &http.Server{
		Addr: envOr("MIST_HTTP_ADDR", "127.0.0.1:3000"), Handler: mux,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second, IdleTimeout: 60 * time.Second}}
	mux.HandleFunc("/jobs", a.kubernetesJobs)
	mux.HandleFunc("/jobs/status", a.getJobStatus)
	mux.HandleFunc("/jobs/", a.kubernetesJob)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, 405, map[string]string{"error": "method not allowed"})
			return
		}
		_, err := executor.client.BatchV1().Jobs(executor.namespace).List(r.Context(), metav1.ListOptions{Limit: 1, LabelSelector: managedLabel + "=mist"})
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"status": "ok", "executor": "kubernetes"})
	})
	return a
}

func NewKubernetesExecutor() (*KubernetesExecutor, error) {
	var config *rest.Config
	var err error
	if path := os.Getenv("KUBECONFIG"); path != "" {
		config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
	} else {
		config, err = rest.InClusterConfig()
		if err != nil {
			config, err = clientcmd.NewNonInteractiveDeferredLoadingClientConfig(clientcmd.NewDefaultClientConfigLoadingRules(), &clientcmd.ConfigOverrides{}).ClientConfig()
		}
	}
	if err != nil {
		return nil, fmt.Errorf("load Kubernetes credentials: %w", err)
	}
	config.Timeout = 20 * time.Second
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	e := &KubernetesExecutor{client: client, namespace: envOr("MIST_NAMESPACE", "mist"),
		owner: envOr("MIST_PILOT_OWNER", "pilot"), cpuNode: envOr("MIST_CPU_NODE", "utmist-z1opa08"),
		ttNode: envOr("MIST_TT_NODE", "utmist-tt"), allowedImages: map[string]bool{cpuImage: true, ttImage: true,
			nvidiaImage: true, "busybox:1.36": true, "alpine:3.16.3": true,
			"nvcr.io/nvidia/k8s/cuda-sample:vectoradd-cuda12.5.0": true}}
	if images := os.Getenv("MIST_ALLOWED_IMAGES"); images != "" {
		e.allowedImages = map[string]bool{}
		for _, image := range strings.Split(images, ",") {
			e.allowedImages[strings.TrimSpace(image)] = true
		}
	}
	if len(validation.IsValidLabelValue(e.owner)) != 0 {
		return nil, errors.New("MIST_PILOT_OWNER must be a valid label value")
	}
	return e, nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func ptr[T any](value T) *T { return &value }

type KubernetesJobStatus struct {
	Job
	Name                string            `json:"name"`
	Image               string            `json:"image"`
	Accelerator         string            `json:"accelerator"`
	DeviceCount         int               `json:"device_count"`
	CPU                 string            `json:"cpu"`
	Memory              string            `json:"memory"`
	Owner               string            `json:"owner"`
	Node                string            `json:"node,omitempty"`
	Pod                 string            `json:"pod,omitempty"`
	ExitCode            *int32            `json:"exit_code,omitempty"`
	Message             string            `json:"message,omitempty"`
	Devices             []AllocatedDevice `json:"devices,omitempty"`
	CheckpointDirectory string            `json:"checkpoint_directory"`
}

type AllocatedDevice struct {
	Driver string `json:"driver"`
	Pool   string `json:"pool"`
	Device string `json:"device"`
}

func (e *KubernetesExecutor) normalize(req CreateJobRequest) (CreateJobRequest, error) {
	if len(req.Payload) != 0 {
		return req, errors.New("use image/command/args or script instead of the legacy payload")
	}
	if req.Accelerator == "" {
		req.Accelerator = req.RequiredGPU
	}
	switch strings.ToLower(req.Accelerator) {
	case "", "cpu":
		req.Accelerator = "cpu"
	case "nvidia", "cuda":
		req.Accelerator = "nvidia"
	case "tt", "tenstorrent":
		req.Accelerator = "tenstorrent"
	default:
		return req, errors.New("accelerator must be cpu, nvidia, or tenstorrent")
	}
	if req.RequiredGPU != "" {
		alias := strings.ToLower(req.RequiredGPU)
		if alias == "tt" {
			alias = "tenstorrent"
		}
		if alias == "cuda" {
			alias = "nvidia"
		}
		if alias != req.Accelerator {
			return req, errors.New("gpu and accelerator disagree")
		}
	}
	req.RequiredGPU = ""
	if req.Type == "" {
		req.Type = "command"
	}
	if req.Type != "command" && req.Type != "training-smoke" {
		return req, errors.New("type must be command or training-smoke")
	}
	if req.Accelerator == "cpu" {
		if req.DeviceCount != 0 {
			return req, errors.New("CPU jobs cannot request accelerator devices")
		}
	} else {
		if req.DeviceCount == 0 {
			req.DeviceCount = 1
		}
		maximum := 2
		if req.Accelerator == "tenstorrent" {
			maximum = 4
		}
		if req.DeviceCount < 1 || req.DeviceCount > maximum {
			return req, fmt.Errorf("device_count must be between 1 and %d", maximum)
		}
	}
	if req.Image == "" {
		switch req.Accelerator {
		case "tenstorrent":
			req.Image = ttImage
		case "nvidia":
			req.Image = nvidiaImage
		default:
			req.Image = cpuImage
		}
	}
	if !e.allowedImages[req.Image] {
		return req, errors.New("image is not in the server's approved image list")
	}
	if req.Accelerator == "tenstorrent" && req.Image != ttImage {
		return req, errors.New("this QuietBox pilot requires the configured TT runtime image")
	}
	if req.Type == "training-smoke" && (req.Accelerator == "cpu" || req.Script != "" || len(req.Command) != 0) {
		return req, errors.New("training-smoke requires NVIDIA or Tenstorrent and supplies its own command")
	}
	if req.Script != "" {
		if len(req.Command) != 0 {
			return req, errors.New("provide script or command, not both")
		}
		if len(req.Script) > 32*1024 {
			return req, errors.New("script exceeds 32 KiB")
		}
		if req.ScriptName == "" {
			req.ScriptName = "script.py"
		}
		if filepath.Base(req.ScriptName) != req.ScriptName || strings.ContainsAny(req.ScriptName, "\\\x00") || len(req.ScriptName) > 63 ||
			(filepath.Ext(req.ScriptName) != ".py" && filepath.Ext(req.ScriptName) != ".sh") {
			return req, errors.New("script_name must be a .py or .sh filename without directories")
		}
	} else if req.Type == "command" && (len(req.Command) == 0 || req.Command[0] == "") {
		return req, errors.New("command or script is required")
	}
	if len(req.Command)+len(req.Args) > 64 {
		return req, errors.New("too many command arguments")
	}
	for _, value := range append(append([]string{}, req.Command...), req.Args...) {
		if len(value) > 8192 || strings.ContainsRune(value, 0) {
			return req, errors.New("invalid command argument")
		}
	}
	if len(req.Env) > 32 {
		return req, errors.New("too many environment variables")
	}
	for key, value := range req.Env {
		if len(validation.IsEnvVarName(key)) != 0 || len(value) > 4096 || strings.ContainsRune(value, 0) {
			return req, errors.New("invalid environment variable")
		}
		if key == "MIST_JOB_ID" || key == "MIST_CHECKPOINT_DIR" {
			return req, fmt.Errorf("%s is managed by Mist", key)
		}
		if req.Accelerator == "tenstorrent" && (strings.HasPrefix(key, "TT_METAL_") || key == "PYTHONPATH" || key == "LD_LIBRARY_PATH") {
			return req, fmt.Errorf("%s is managed by the TT runtime profile", key)
		}
	}
	if len(req.Name) > 128 {
		return req, errors.New("name exceeds 128 characters")
	}
	if req.TimeoutSeconds == 0 {
		req.TimeoutSeconds = 600
	}
	if req.TimeoutSeconds < 10 || req.TimeoutSeconds > 86400 {
		return req, errors.New("timeout_seconds must be between 10 and 86400")
	}
	if req.CPU == "" {
		req.CPU = "250m"
		if req.Accelerator != "cpu" {
			req.CPU = "2"
		}
		if req.Accelerator == "tenstorrent" {
			req.CPU = strconv.Itoa(req.DeviceCount * 2)
		}
	}
	if req.Memory == "" {
		req.Memory = "256Mi"
		if req.Accelerator == "nvidia" {
			req.Memory = "2Gi"
		}
		if req.Accelerator == "tenstorrent" {
			req.Memory = strconv.Itoa(req.DeviceCount*4) + "Gi"
		}
	}
	cpu, err := resource.ParseQuantity(req.CPU)
	if err != nil || cpu.Sign() <= 0 || cpu.Cmp(resource.MustParse("8")) > 0 {
		return req, errors.New("cpu must be a Kubernetes quantity greater than zero and at most 8")
	}
	memory, err := resource.ParseQuantity(req.Memory)
	if err != nil || memory.Cmp(resource.MustParse("64Mi")) < 0 || memory.Cmp(resource.MustParse("32Gi")) > 0 {
		return req, errors.New("memory must be between 64Mi and 32Gi")
	}
	if req.Accelerator == "tenstorrent" && memory.Cmp(resource.MustParse("2Gi")) < 0 {
		return req, errors.New("Tenstorrent runtime requires at least 2Gi memory")
	}
	return req, nil
}

func (e *KubernetesExecutor) buildJob(id string, req CreateJobRequest) *batchv1.Job {
	requestJSON, _ := json.Marshal(req)
	labels := map[string]string{managedLabel: "mist", "mist.io/owner": e.owner}
	resources := corev1.ResourceList{corev1.ResourceCPU: resource.MustParse(req.CPU), corev1.ResourceMemory: resource.MustParse(req.Memory)}
	container := corev1.Container{Name: "workload", Image: req.Image, ImagePullPolicy: corev1.PullIfNotPresent,
		Command: req.Command, Args: req.Args, WorkingDir: "/tmp",
		SecurityContext: &corev1.SecurityContext{Privileged: ptr(false), AllowPrivilegeEscalation: ptr(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
		Resources:       corev1.ResourceRequirements{Requests: resources.DeepCopy(), Limits: resources.DeepCopy()},
		VolumeMounts:    []corev1.VolumeMount{{Name: "checkpoints", MountPath: "/checkpoints"}},
		Env:             []corev1.EnvVar{{Name: "MIST_JOB_ID", Value: id}, {Name: "MIST_CHECKPOINT_DIR", Value: "/checkpoints/" + id}}}
	keys := []string{}
	for key := range req.Env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		container.Env = append(container.Env, corev1.EnvVar{Name: key, Value: req.Env[key]})
	}
	pvc := "mist-cpu-checkpoints"
	pod := corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, AutomountServiceAccountToken: ptr(false),
		TerminationGracePeriodSeconds: ptr(int64(5)), NodeSelector: map[string]string{"kubernetes.io/hostname": e.cpuNode}}
	if req.Accelerator == "nvidia" {
		pvc = "training-nvidia-checkpoints"
		count := resource.MustParse(strconv.Itoa(req.DeviceCount))
		container.Resources.Requests["nvidia.com/gpu"] = count
		container.Resources.Limits["nvidia.com/gpu"] = count
	}
	if req.Accelerator == "tenstorrent" {
		pvc = "training-tenstorrent-checkpoints"
		pod.NodeSelector = map[string]string{"kubernetes.io/hostname": e.ttNode}
		pod.ResourceClaims = []corev1.PodResourceClaim{{Name: "boards", ResourceClaimTemplateName: ptr(fmt.Sprintf("mist-tenstorrent-%d-boards", req.DeviceCount))}}
		container.Resources.Claims = []corev1.ResourceClaim{{Name: "boards"}}
		hugepages := resource.MustParse(fmt.Sprintf("%dGi", req.DeviceCount*2))
		container.Resources.Requests["hugepages-1Gi"] = hugepages
		container.Resources.Limits["hugepages-1Gi"] = hugepages
		container.Env = append(container.Env, corev1.EnvVar{Name: "TT_METAL_HOME", Value: ttMetal}, corev1.EnvVar{Name: "PYTHONPATH", Value: ttMetal},
			corev1.EnvVar{Name: "LD_LIBRARY_PATH", Value: ttMetal + "/build/lib"}, corev1.EnvVar{Name: "TT_METAL_CACHE", Value: "/tmp/tt-metal-cache"})
		for _, host := range []struct{ name, path string }{{"metal", ttMetal}, {"python", "/home/utmist-tt/.local/share/uv/python"}} {
			pod.Volumes = append(pod.Volumes, corev1.Volume{Name: host.name, VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: host.path, Type: ptr(corev1.HostPathDirectory)}}})
			container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: host.name, MountPath: host.path, ReadOnly: true})
		}
	}
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "checkpoints", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc}}})
	if req.Script != "" || req.Type == "training-smoke" {
		configmap, mount := id+"-script", "/mist-script"
		python := "python"
		if req.Accelerator == "tenstorrent" {
			python = ttPython
		}
		if req.Type == "training-smoke" {
			configmap, mount = "training-smoke-script", "/scripts"
			expected := req.DeviceCount
			if req.Accelerator == "tenstorrent" {
				expected *= 2
			}
			container.Command = []string{python, "-u", "/scripts/training_smoke.py", req.Accelerator,
				"--expected-devices", strconv.Itoa(expected), "--output", "/checkpoints/" + id}
		} else {
			container.Command = []string{python, "-u", mount + "/" + req.ScriptName}
			if filepath.Ext(req.ScriptName) == ".sh" {
				container.Command = []string{"sh", mount + "/" + req.ScriptName}
			}
		}
		pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "script", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: configmap}}}})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "script", MountPath: mount, ReadOnly: true})
	}
	pod.Containers = []corev1.Container{container}
	return &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: id, Namespace: e.namespace, Labels: labels, Annotations: map[string]string{requestAnnotation: string(requestJSON)}},
		Spec: batchv1.JobSpec{BackoffLimit: ptr(int32(0)), ActiveDeadlineSeconds: ptr(req.TimeoutSeconds),
			Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: pod}}}
}

func (e *KubernetesExecutor) submit(ctx context.Context, request CreateJobRequest) (*KubernetesJobStatus, error) {
	req, err := e.normalize(request)
	if err != nil {
		return nil, &submissionError{err}
	}
	if req.Accelerator == "tenstorrent" {
		if _, err := e.client.ResourceV1().ResourceClaimTemplates(e.namespace).Get(ctx, fmt.Sprintf("mist-tenstorrent-%d-boards", req.DeviceCount), metav1.GetOptions{}); err != nil {
			return nil, fmt.Errorf("Tenstorrent claim template unavailable: %w", err)
		}
	}
	if req.Type == "training-smoke" {
		if _, err := e.client.CoreV1().ConfigMaps(e.namespace).Get(ctx, "training-smoke-script", metav1.GetOptions{}); err != nil {
			return nil, fmt.Errorf("training smoke script unavailable: %w", err)
		}
	}
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return nil, err
	}
	id := "mist-" + hex.EncodeToString(random)
	var script *corev1.ConfigMap
	if req.Script != "" {
		script, err = e.client.CoreV1().ConfigMaps(e.namespace).Create(ctx, &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: id + "-script", Labels: map[string]string{managedLabel: "mist"}}, Data: map[string]string{req.ScriptName: req.Script}}, metav1.CreateOptions{})
		if err != nil {
			return nil, err
		}
	}
	job, err := e.client.BatchV1().Jobs(e.namespace).Create(ctx, e.buildJob(id, req), metav1.CreateOptions{})
	if err != nil {
		if script != nil {
			_ = e.client.CoreV1().ConfigMaps(e.namespace).Delete(ctx, script.Name, metav1.DeleteOptions{})
		}
		return nil, err
	}
	if script != nil {
		script.OwnerReferences = []metav1.OwnerReference{*metav1.NewControllerRef(job, batchv1.SchemeGroupVersion.WithKind("Job"))}
		if _, err := e.client.CoreV1().ConfigMaps(e.namespace).Update(ctx, script, metav1.UpdateOptions{}); err != nil {
			// Roll back submission rather than returning an error for a running orphan.
			_ = e.client.BatchV1().Jobs(e.namespace).Delete(ctx, id, metav1.DeleteOptions{PropagationPolicy: ptr(metav1.DeletePropagationForeground)})
			_ = e.client.CoreV1().ConfigMaps(e.namespace).Delete(ctx, script.Name, metav1.DeleteOptions{})
			return nil, fmt.Errorf("attach script ownership: %w", err)
		}
	}
	return e.baseStatus(job)
}

type submissionError struct{ error }

func (e *KubernetesExecutor) managedJob(ctx context.Context, id string) (*batchv1.Job, error) {
	if len(validation.IsDNS1123Subdomain(id)) != 0 {
		return nil, &submissionError{errors.New("invalid job ID")}
	}
	job, err := e.client.BatchV1().Jobs(e.namespace).Get(ctx, id, metav1.GetOptions{})
	if err == nil && (job.Labels[managedLabel] != "mist" || job.Labels["mist.io/owner"] != e.owner) {
		return nil, apierrors.NewNotFound(batchv1.Resource("jobs"), id)
	}
	return job, err
}

func (e *KubernetesExecutor) get(ctx context.Context, id string) (*KubernetesJobStatus, error) {
	job, err := e.managedJob(ctx, id)
	if err != nil {
		return nil, err
	}
	return e.statusFor(ctx, job)
}

func terminal(job *batchv1.Job) bool {
	for _, condition := range job.Status.Conditions {
		if condition.Status == corev1.ConditionTrue && (condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed) {
			return true
		}
	}
	return false
}

func (e *KubernetesExecutor) baseStatus(job *batchv1.Job) (*KubernetesJobStatus, error) {
	var req CreateJobRequest
	if err := json.Unmarshal([]byte(job.Annotations[requestAnnotation]), &req); err != nil {
		return nil, fmt.Errorf("invalid stored submission: %w", err)
	}
	status := &KubernetesJobStatus{Job: Job{ID: job.Name, Type: req.Type, Created: job.CreationTimestamp.Time,
		JobState: JobStateScheduled, Payload: map[string]interface{}{"command": req.Command, "args": req.Args}}, Name: req.Name,
		Image: req.Image, Accelerator: req.Accelerator, DeviceCount: req.DeviceCount, CPU: req.CPU, Memory: req.Memory, Owner: e.owner, CheckpointDirectory: "/checkpoints/" + job.Name}
	if status.Name == "" {
		status.Name = job.Name
	}
	if req.Accelerator != "cpu" {
		status.RequiredGPU = req.Accelerator
	}
	return status, nil
}

func (e *KubernetesExecutor) statusFor(ctx context.Context, job *batchv1.Job) (*KubernetesJobStatus, error) {
	status, err := e.baseStatus(job)
	if err != nil {
		return nil, err
	}
	if job.Status.StartTime != nil {
		status.TimeAssigned = &job.Status.StartTime.Time
	}
	if job.Status.CompletionTime != nil {
		status.TimeCompleted = &job.Status.CompletionTime.Time
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		if condition.Type == batchv1.JobComplete {
			status.JobState = JobStateSuccess
		}
		if condition.Type == batchv1.JobFailed {
			status.JobState = JobStateFailure
			status.Message = condition.Reason + ": " + condition.Message
			status.Error = ptr(status.Message)
			status.TimeCompleted = &condition.LastTransitionTime.Time
		}
	}
	pods, err := e.client.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=" + job.Name})
	if err != nil {
		return nil, err
	}
	sort.Slice(pods.Items, func(i, j int) bool { return pods.Items[i].CreationTimestamp.Before(&pods.Items[j].CreationTimestamp) })
	if len(pods.Items) != 0 {
		pod := pods.Items[len(pods.Items)-1]
		status.Node, status.Pod = pod.Spec.NodeName, pod.Name
		if pod.Status.Phase == corev1.PodRunning && !terminal(job) {
			status.JobState = JobStateInProgress
		}
		for _, container := range pod.Status.ContainerStatuses {
			if container.Name != "workload" {
				continue
			}
			if container.State.Running != nil {
				status.TimeStarted = &container.State.Running.StartedAt.Time
			}
			if container.State.Terminated != nil {
				exit := container.State.Terminated
				status.ExitCode = &exit.ExitCode
				status.TimeStarted = &exit.StartedAt.Time
				status.TimeCompleted = &exit.FinishedAt.Time
				if exit.ExitCode != 0 {
					status.JobState = JobStateFailure
					if status.Message == "" {
						status.Message = exit.Reason
					}
					status.Error = ptr(status.Message)
				}
			}
			if container.State.Waiting != nil {
				status.Message = container.State.Waiting.Reason + ": " + container.State.Waiting.Message
			}
		}
		if pod.Status.Phase == corev1.PodPending {
			events, err := e.client.CoreV1().Events(e.namespace).List(ctx, metav1.ListOptions{FieldSelector: "involvedObject.uid=" + string(pod.UID)})
			if err != nil {
				return nil, err
			}
			for _, event := range events.Items {
				if event.Reason == "FailedScheduling" {
					status.Message = event.Message
				}
			}
		}
		for _, claimStatus := range pod.Status.ResourceClaimStatuses {
			if claimStatus.ResourceClaimName == nil {
				continue
			}
			claim, err := e.client.ResourceV1().ResourceClaims(e.namespace).Get(ctx, *claimStatus.ResourceClaimName, metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				continue
			}
			if err != nil {
				return nil, err
			}
			if claim.Status.Allocation != nil {
				for _, result := range claim.Status.Allocation.Devices.Results {
					status.Devices = append(status.Devices, AllocatedDevice{result.Driver, result.Pool, result.Device})
				}
			}
		}
	}
	if cancelled := job.Annotations[cancelAnnotation]; cancelled != "" {
		status.JobState = JobStateCancelled
		status.Message = "Cancelled by user"
		if when, err := time.Parse(time.RFC3339, cancelled); err == nil {
			status.TimeCompleted = &when
		}
	}
	return status, nil
}

func (e *KubernetesExecutor) list(ctx context.Context) ([]KubernetesJobStatus, error) {
	jobs, err := e.client.BatchV1().Jobs(e.namespace).List(ctx, metav1.ListOptions{LabelSelector: managedLabel + "=mist,mist.io/owner=" + e.owner})
	if err != nil {
		return nil, err
	}
	result := make([]KubernetesJobStatus, 0, len(jobs.Items))
	for i := range jobs.Items {
		status, err := e.statusFor(ctx, &jobs.Items[i])
		if err != nil {
			return nil, err
		}
		result = append(result, *status)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Created.After(result[j].Created) })
	return result, nil
}

func (e *KubernetesExecutor) logs(ctx context.Context, id string) (string, error) {
	job, err := e.managedJob(ctx, id)
	if err != nil {
		return "", err
	}
	if job.Annotations[cancelAnnotation] != "" {
		return job.Annotations[cancelLogsAnnotation], nil
	}
	pods, err := e.client.CoreV1().Pods(e.namespace).List(ctx, metav1.ListOptions{LabelSelector: "batch.kubernetes.io/job-name=" + id})
	if err != nil {
		return "", err
	}
	var logs strings.Builder
	for _, pod := range pods.Items {
		if len(pod.Status.ContainerStatuses) == 0 {
			continue
		}
		state := pod.Status.ContainerStatuses[0].State
		if state.Running == nil && state.Terminated == nil {
			continue
		}
		stream, err := e.client.CoreV1().Pods(e.namespace).GetLogs(pod.Name, &corev1.PodLogOptions{Container: "workload", TailLines: ptr(int64(1000)), LimitBytes: ptr(maxLogBytes)}).Stream(ctx)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(stream, maxLogBytes))
		stream.Close()
		if err != nil {
			return "", err
		}
		logs.Write(data)
	}
	value := logs.String()
	if len(value) > int(maxLogBytes) {
		value = value[len(value)-int(maxLogBytes):]
	}
	return value, nil
}

func (e *KubernetesExecutor) cancel(ctx context.Context, id string) (*KubernetesJobStatus, error) {
	job, err := e.managedJob(ctx, id)
	if err != nil {
		return nil, err
	}
	if job.Annotations[cancelAnnotation] != "" {
		return e.statusFor(ctx, job)
	}
	if terminal(job) {
		return nil, &submissionError{errors.New("completed jobs cannot be cancelled")}
	}
	initial, err := e.statusFor(ctx, job)
	if err != nil {
		return nil, err
	}
	if initial.ExitCode != nil {
		return nil, &submissionError{errors.New("workload has already exited")}
	}
	logs, logErr := e.logs(ctx, id)
	if logErr != nil {
		return nil, fmt.Errorf("preserve logs before cancellation: %w", logErr)
	}
	// Keep cancellation annotations below Kubernetes' metadata size limit,
	// including submissions and replacement encoding of non-UTF-8 output.
	if len(logs) > 32*1024 {
		logs = logs[len(logs)-32*1024:]
	}
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := e.managedJob(ctx, id)
		if err != nil {
			return err
		}
		if terminal(current) {
			return &submissionError{errors.New("job completed before cancellation")}
		}
		status, err := e.statusFor(ctx, current)
		if err != nil {
			return err
		}
		if status.ExitCode != nil {
			return &submissionError{errors.New("workload has already exited")}
		}
		if current.Annotations[cancelAnnotation] != "" {
			job = current
			return nil
		}
		current.Spec.Suspend = ptr(true)
		current.Annotations[cancelAnnotation] = time.Now().UTC().Format(time.RFC3339)
		current.Annotations[cancelLogsAnnotation] = logs
		job, err = e.client.BatchV1().Jobs(e.namespace).Update(ctx, current, metav1.UpdateOptions{})
		return err
	})
	if err != nil {
		return nil, err
	}
	return e.statusFor(ctx, job)
}

func writeJSON(w http.ResponseWriter, code int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(value)
}

func executorError(w http.ResponseWriter, err error) {
	code := http.StatusBadGateway
	var invalid *submissionError
	if errors.As(err, &invalid) {
		code = http.StatusBadRequest
	}
	if apierrors.IsNotFound(err) {
		code = http.StatusNotFound
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (a *App) kubernetesJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var req CreateJobRequest
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 128*1024))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid submission: " + err.Error()})
			return
		}
		if err := decoder.Decode(new(interface{})); err != io.EOF {
			writeJSON(w, 400, map[string]string{"error": "request must contain one JSON object"})
			return
		}
		status, err := a.executor.submit(r.Context(), req)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{"job_id": status.ID, "job": status})
	case http.MethodGet:
		jobs, err := a.executor.list(r.Context())
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]interface{}{"jobs": jobs, "count": len(jobs)})
	default:
		w.Header().Set("Allow", "GET, POST")
		writeJSON(w, 405, map[string]string{"error": "method not allowed"})
	}
}

func (a *App) kubernetesJob(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/jobs/"), "/")
	if len(parts) == 0 || parts[0] == "" || len(parts) > 2 {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	if len(parts) == 2 && parts[1] == "logs" && r.Method == http.MethodGet {
		logs, err := a.executor.logs(r.Context(), id)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, map[string]string{"job_id": id, "logs": logs})
		return
	}
	if (len(parts) == 2 && parts[1] == "cancel" && r.Method == http.MethodPost) || (len(parts) == 1 && r.Method == http.MethodDelete) {
		status, err := a.executor.cancel(r.Context(), id)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	if len(parts) == 1 && r.Method == http.MethodGet {
		status, err := a.executor.get(r.Context(), id)
		if err != nil {
			executorError(w, err)
			return
		}
		writeJSON(w, 200, status)
		return
	}
	writeJSON(w, 405, map[string]string{"error": "method not allowed"})
}
