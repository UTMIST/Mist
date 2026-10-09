package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func testExecutor() *KubernetesExecutor {
	return &KubernetesExecutor{client: fake.NewSimpleClientset(), namespace: "mist", owner: "test", cpuNode: "cpu-node", ttNode: "tt-node",
		allowedImages: map[string]bool{cpuImage: true, nvidiaImage: true, ttImage: true}}
}

func TestKubernetesRejectInvalidSubmission(t *testing.T) {
	e := testExecutor()
	for _, request := range []CreateJobRequest{
		{Command: []string{"true"}, Accelerator: "amd"},
		{Command: []string{"true"}, Accelerator: "cpu", DeviceCount: 1},
		{Command: []string{"true"}, Accelerator: "nvidia", DeviceCount: 3},
		{Command: []string{"true"}, Accelerator: "tenstorrent", DeviceCount: 5},
		{Command: []string{"true"}, CPU: "-1"},
		{Command: []string{"true"}, CPU: "9"},
		{Command: []string{"true"}, Memory: "1Ti"},
		{Command: []string{"true"}, Image: "unapproved/image:latest"},
		{Script: "print(1)", ScriptName: "../escape.py"},
		{Script: "print(1)", Command: []string{"sh"}},
		{Command: []string{"true"}, Env: map[string]string{"MIST_JOB_ID": "spoofed"}},
		{Command: []string{"true"}, Accelerator: "tenstorrent", Env: map[string]string{"TT_METAL_HOME": "/host"}},
	} {
		if _, err := e.submit(context.Background(), request); err == nil {
			t.Errorf("accepted invalid submission: %+v", request)
		}
	}
	jobs, _ := e.client.BatchV1().Jobs("mist").List(context.Background(), metav1.ListOptions{})
	if len(jobs.Items) != 0 {
		t.Fatal("invalid submissions created jobs")
	}
}

func TestKubernetesReportsActualExitBeforeControllerCompletion(t *testing.T) {
	e := testExecutor()
	ctx := context.Background()
	status, err := e.submit(ctx, CreateJobRequest{Command: []string{"sh", "-c", "exit 17"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.client.CoreV1().Pods("mist").Create(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "failed-pod", Labels: map[string]string{"batch.kubernetes.io/job-name": status.ID}},
		Status: corev1.PodStatus{Phase: corev1.PodFailed, ContainerStatuses: []corev1.ContainerStatus{{Name: "workload", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{ExitCode: 17, Reason: "Error"}}}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := e.get(ctx, status.ID)
	if err != nil {
		t.Fatal(err)
	}
	if actual.JobState != JobStateFailure || actual.ExitCode == nil || *actual.ExitCode != 17 {
		t.Fatalf("wrong status: %+v", actual)
	}
	if _, err := e.cancel(ctx, status.ID); err == nil {
		t.Fatal("cancellation overwrote an already failed workload")
	}
}

func TestKubernetesReportsCurrentWaitingReasonAfterPlacement(t *testing.T) {
	e := testExecutor()
	ctx := context.Background()
	job, err := e.submit(ctx, CreateJobRequest{Accelerator: "nvidia", Command: []string{"true"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.client.CoreV1().Pods("mist").Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "waiting-pod", UID: "waiting-uid", Labels: map[string]string{
			"batch.kubernetes.io/job-name": job.ID, managedLabel: "mist", "mist.io/owner": e.owner}},
		Spec: corev1.PodSpec{NodeName: e.cpuNode},
		Status: corev1.PodStatus{Phase: corev1.PodPending, ContainerStatuses: []corev1.ContainerStatus{{Name: "workload",
			State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff", Message: "image unavailable"}}}}},
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.client.CoreV1().Events("mist").Create(ctx, &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Name: "old-scheduling-failure"},
		InvolvedObject: corev1.ObjectReference{UID: "waiting-uid"}, Reason: "FailedScheduling", Message: "Insufficient nvidia.com/gpu",
	}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := e.get(ctx, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	listed, err := e.list(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(actual.Message, "ImagePullBackOff") || len(listed) != 1 || !strings.Contains(listed[0].Message, "ImagePullBackOff") {
		t.Fatalf("old queue event replaced current pull failure: single=%+v list=%+v", actual, listed)
	}
}

func TestKubernetesCancellationPersistsAcrossExecutorRestart(t *testing.T) {
	e := testExecutor()
	ctx := context.Background()
	status, err := e.submit(ctx, CreateJobRequest{Command: []string{"sleep", "120"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.cancel(ctx, status.ID); err != nil {
		t.Fatal(err)
	}
	restarted := *e
	actual, err := restarted.get(ctx, status.ID)
	if err != nil {
		t.Fatal(err)
	}
	job, _ := e.client.BatchV1().Jobs("mist").Get(ctx, status.ID, metav1.GetOptions{})
	if actual.JobState != JobStateCancelled || job.Spec.Suspend == nil || !*job.Spec.Suspend {
		t.Fatal("cancelled job did not retain state and stop scheduling")
	}
	if _, err := restarted.cancel(ctx, status.ID); err != nil {
		t.Fatal("repeat cancellation must be idempotent", err)
	}
}

func TestKubernetesDoesNotExposeUnmanagedJobs(t *testing.T) {
	e := testExecutor()
	ctx := context.Background()
	_, _ = e.client.BatchV1().Jobs("mist").Create(ctx, &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "operator-job"}}, metav1.CreateOptions{})
	if _, err := e.get(ctx, "operator-job"); err == nil {
		t.Fatal("exposed unmanaged job")
	}
	if _, err := e.cancel(ctx, "operator-job"); err == nil {
		t.Fatal("cancelled unmanaged job")
	}
}

func TestKubernetesHTTPRejectsUnknownAndMultipleObjects(t *testing.T) {
	app := NewKubernetesApp(testExecutor(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, body := range []string{`{"command":["true"],"privileged":true}`, `{"command":["true"]} {"command":["false"]}`} {
		recorder := httptest.NewRecorder()
		app.httpServer.Handler.ServeHTTP(recorder, httptest.NewRequest("POST", "/jobs", strings.NewReader(body)))
		if recorder.Code != 400 {
			t.Fatalf("unexpected response %d: %s", recorder.Code, recorder.Body.String())
		}
		var result map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil || result["error"] == "" {
			t.Fatal("missing structured error")
		}
	}
}
