package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
)

func hardwareFixture() ([]corev1.Node, []corev1.Pod, []resourcev1.ResourceSlice) {
	nodes := []corev1.Node{}
	for _, name := range []string{"cpu-node", "tt-node"} {
		nodes = append(nodes, corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: name}, Status: corev1.NodeStatus{
			Conditions:  []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
			Capacity:    corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")},
			Allocatable: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse("2")},
		}})
	}
	pods := []corev1.Pod{}
	for _, driver := range []struct{ node, name string }{{"cpu-node", "nvidia-device-plugin-daemonset"}, {"tt-node", "tt-dra-driver"}} {
		pods = append(pods, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app.kubernetes.io/name": driver.name}},
			Spec: corev1.PodSpec{NodeName: driver.node}, Status: corev1.PodStatus{Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}})
	}
	slice := resourcev1.ResourceSlice{Spec: resourcev1.ResourceSliceSpec{Driver: "tenstorrent.com", NodeName: ptr("tt-node"), Pool: resourcev1.ResourcePool{Name: "tt-node", Generation: 1, ResourceSliceCount: 1}}}
	for _, name := range []string{"tt-0", "tt-1", "tt-2", "tt-3"} {
		slice.Spec.Devices = append(slice.Spec.Devices, resourcev1.Device{Name: name, Attributes: map[resourcev1.QualifiedName]resourcev1.DeviceAttribute{
			"boardName": {StringValue: ptr("n300")}, "chipCount": {IntValue: ptr(int64(2))},
		}})
	}
	return nodes, pods, []resourcev1.ResourceSlice{slice}
}

func gpuContainer(count string) corev1.Container {
	return corev1.Container{Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{"nvidia.com/gpu": resource.MustParse(count)}}}
}

func TestHardwareCountsExternalScheduledPodsAndDRAReservations(t *testing.T) {
	e := testExecutor()
	nodes, pods, slices := hardwareFixture()
	for _, state := range []struct {
		node  string
		phase corev1.PodPhase
	}{{"cpu-node", corev1.PodPending}, {"", corev1.PodPending}, {"cpu-node", corev1.PodSucceeded}} {
		pods = append(pods, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "another-team"}, Spec: corev1.PodSpec{NodeName: state.node, Containers: []corev1.Container{gpuContainer("1")}}, Status: corev1.PodStatus{Phase: state.phase}})
	}
	claim := resourcev1.ResourceClaim{ObjectMeta: metav1.ObjectMeta{Namespace: "another-team"}, Status: resourcev1.ResourceClaimStatus{Allocation: &resourcev1.AllocationResult{
		Devices: resourcev1.DeviceAllocationResult{Results: []resourcev1.DeviceRequestAllocationResult{{Driver: "tenstorrent.com", Pool: "tt-node", Device: "tt-0"}}},
	}}}
	// An allocation reserves the board even before its pod starts; duplicate
	// references must not count the same physical device twice.
	snapshot := e.hardwareFrom(nodes, pods, []resourcev1.ResourceClaim{claim, claim}, slices)
	if p := snapshot.Pools[0]; p.Total != 2 || p.Allocated != 1 || p.Available == nil || *p.Available != 1 {
		t.Fatalf("bad GPU accounting: %+v", p)
	}
	if p := snapshot.Pools[1]; p.Total != 4 || p.Allocated != 1 || p.Available == nil || *p.Available != 3 || p.ChipsPerDevice != 2 {
		t.Fatalf("bad TT accounting: %+v", p)
	}
}

func TestHardwareDoesNotAdvertiseOfflineOrIncompleteInventory(t *testing.T) {
	e := testExecutor()
	nodes, pods, slices := hardwareFixture()
	nodes[0].Status.Conditions[0].Status = corev1.ConditionFalse
	slices[0].Spec.Pool.Generation = 2
	slices[0].Spec.Pool.ResourceSliceCount = 2
	snapshot := e.hardwareFrom(nodes, pods, nil, slices)
	if p := snapshot.Pools[0]; p.Ready || *p.Available != 0 {
		t.Fatalf("offline node appeared available: %+v", p)
	}
	if p := snapshot.Pools[1]; p.Ready || p.Available != nil {
		t.Fatalf("partial inventory appeared available: %+v", p)
	}
	nodes[0].Status.Conditions[0].Status = corev1.ConditionTrue
	nodes[0].Spec.Unschedulable = true
	snapshot = e.hardwareFrom(nodes, pods, nil, nil)
	if snapshot.Pools[0].Ready {
		t.Fatal("cordoned node appeared schedulable")
	}
}

func TestGPURequestIncludesInitPeakAndSidecars(t *testing.T) {
	app, sidecar, init := gpuContainer("1"), gpuContainer("1"), gpuContainer("3")
	sidecar.RestartPolicy = ptr(corev1.ContainerRestartPolicyAlways)
	pod := corev1.Pod{Spec: corev1.PodSpec{NodeName: "cpu-node", Containers: []corev1.Container{app}, InitContainers: []corev1.Container{sidecar, init}}}
	if n := podGPURequest(pod); n != 4 {
		t.Fatalf("init peak reservation = %d, want 4", n)
	}
}

func TestHardwarePermissionFailureIsAnError(t *testing.T) {
	e := testExecutor()
	e.client.(*fake.Clientset).PrependReactor("list", "nodes", func(ktesting.Action) (bool, runtime.Object, error) { return true, nil, errors.New("forbidden") })
	app := NewKubernetesApp(e, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	app.httpServer.Handler.ServeHTTP(response, httptest.NewRequest("GET", "/hardware", nil))
	if response.Code != 503 || !strings.Contains(response.Body.String(), "forbidden") {
		t.Fatalf("invented availability after failed discovery: %d %s", response.Code, response.Body.String())
	}
}
