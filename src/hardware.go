package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type MachineHardware struct {
	Name              string `json:"name"`
	Address           string `json:"address"`
	Ready             bool   `json:"ready"`
	Schedulable       bool   `json:"schedulable"`
	CPUAllocatable    string `json:"cpu_allocatable"`
	MemoryAllocatable string `json:"memory_allocatable"`
}

type AcceleratorPool struct {
	Accelerator    string `json:"accelerator"`
	Node           string `json:"node"`
	Unit           string `json:"unit"`
	Total          int64  `json:"total"`
	Allocated      int64  `json:"allocated"`
	Available      *int64 `json:"available"`
	Ready          bool   `json:"ready"`
	ChipsPerDevice int    `json:"chips_per_device,omitempty"`
	Message        string `json:"message,omitempty"`
}

type HardwareSnapshot struct {
	ObservedAt time.Time         `json:"observed_at"`
	Machines   []MachineHardware `json:"machines"`
	Pools      []AcceleratorPool `json:"pools"`
}

func (e *KubernetesExecutor) hardware(ctx context.Context) (*HardwareSnapshot, error) {
	var nodes *corev1.NodeList
	var pods *corev1.PodList
	var claims *resourcev1.ResourceClaimList
	var slices *resourcev1.ResourceSliceList
	// These reads are independent. Fail rather than invent free-device counts
	// when permissions/connectivity are incomplete.
	reads := []func() error{
		func() (err error) { nodes, err = e.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); return },
		func() (err error) { pods, err = e.client.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); return },
		func() (err error) {
			claims, err = e.client.ResourceV1().ResourceClaims("").List(ctx, metav1.ListOptions{})
			return
		},
		func() (err error) {
			slices, err = e.client.ResourceV1().ResourceSlices().List(ctx, metav1.ListOptions{})
			return
		},
	}
	errors := make([]error, len(reads))
	var wg sync.WaitGroup
	for i, read := range reads {
		wg.Add(1)
		go func(i int, read func() error) { defer wg.Done(); errors[i] = read() }(i, read)
	}
	wg.Wait()
	for _, err := range errors {
		if err != nil {
			return nil, fmt.Errorf("hardware discovery: %w", err)
		}
	}
	return e.hardwareFrom(nodes.Items, pods.Items, claims.Items, slices.Items), nil
}

func conditionReady(conditions []corev1.PodCondition) bool {
	for _, condition := range conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func driverReady(pods []corev1.Pod, node, driver string) bool {
	for _, pod := range pods {
		if pod.Spec.NodeName != node || pod.DeletionTimestamp != nil {
			continue
		}
		matches := pod.Labels["app.kubernetes.io/name"] == driver || pod.Labels["app"] == driver
		if matches && conditionReady(pod.Status.Conditions) {
			return true
		}
	}
	return false
}

// podGPURequest follows effective scheduling requests: app containers plus
// restartable init containers, or the peak of sequential init requirements.
// Scheduled Pending and Terminating pods still reserve resources; terminal
// pods and unplaced pods do not. This counts every namespace, not just Mist.
func podGPURequest(pod corev1.Pod) int64 {
	if pod.Spec.NodeName == "" || pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return 0
	}
	request := func(c corev1.Container) int64 {
		q, ok := c.Resources.Requests["nvidia.com/gpu"]
		if !ok {
			q = c.Resources.Limits["nvidia.com/gpu"]
		}
		return q.Value()
	}
	var app, sidecars, peak int64
	for _, c := range pod.Spec.Containers {
		app += request(c)
	}
	for _, c := range pod.Spec.InitContainers {
		value := request(c)
		if c.RestartPolicy != nil && *c.RestartPolicy == corev1.ContainerRestartPolicyAlways {
			sidecars += value
			value = 0
		}
		peak = max(peak, sidecars+value)
	}
	return max(app+sidecars, peak)
}

func (e *KubernetesExecutor) hardwareFrom(nodes []corev1.Node, pods []corev1.Pod, claims []resourcev1.ResourceClaim, slices []resourcev1.ResourceSlice) *HardwareSnapshot {
	snapshot := &HardwareSnapshot{ObservedAt: time.Now().UTC(), Machines: []MachineHardware{}, Pools: []AcceleratorPool{}}
	machines := map[string]MachineHardware{}
	nvidiaCapacity, nvidiaAllocatable := int64(0), int64(0)
	for _, node := range nodes {
		if node.Name != e.cpuNode && node.Name != e.ttNode {
			continue
		}
		machine := MachineHardware{Name: node.Name}
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				machine.Ready = condition.Status == corev1.ConditionTrue
			}
		}
		machine.Schedulable = machine.Ready && !node.Spec.Unschedulable
		for _, taint := range node.Spec.Taints {
			// Pilot jobs have no tolerations; a blocking taint prevents placement.
			if taint.Effect == corev1.TaintEffectNoSchedule || taint.Effect == corev1.TaintEffectNoExecute {
				machine.Schedulable = false
			}
		}
		for _, address := range node.Status.Addresses {
			if address.Type == corev1.NodeInternalIP {
				if machine.Address == "" {
					machine.Address = address.Address
				}
				if ip := net.ParseIP(address.Address); ip != nil && ip.To4() != nil {
					machine.Address = address.Address
					break
				}
			}
		}
		cpu, memory := node.Status.Allocatable[corev1.ResourceCPU], node.Status.Allocatable[corev1.ResourceMemory]
		machine.CPUAllocatable, machine.MemoryAllocatable = cpu.String(), memory.String()
		machines[node.Name] = machine
		if node.Name == e.cpuNode {
			capacity, allocatable := node.Status.Capacity["nvidia.com/gpu"], node.Status.Allocatable["nvidia.com/gpu"]
			nvidiaCapacity, nvidiaAllocatable = capacity.Value(), allocatable.Value()
		}
	}
	for _, name := range []string{e.cpuNode, e.ttNode} {
		machine, ok := machines[name]
		if !ok {
			machine = MachineHardware{Name: name}
		}
		snapshot.Machines = append(snapshot.Machines, machine)
	}
	nvidia := AcceleratorPool{Accelerator: "nvidia", Node: e.cpuNode, Unit: "GPU", Total: nvidiaCapacity}
	for _, pod := range pods {
		if pod.Spec.NodeName == e.cpuNode {
			nvidia.Allocated += podGPURequest(pod)
		}
	}
	nvidia.Ready = machines[e.cpuNode].Schedulable && nvidiaAllocatable > 0 && driverReady(pods, e.cpuNode, "nvidia-device-plugin-daemonset")
	free := max(int64(0), nvidiaAllocatable-nvidia.Allocated)
	if !nvidia.Ready {
		free = 0
		nvidia.Message = "Node or NVIDIA device plugin is unavailable for placement"
	}
	nvidia.Available = &free

	// Use only the newest complete generation of each advertised device pool.
	// Ignore the vendor's incorrect memory capacity and deduplicate devices.
	type poolGeneration struct {
		generation int64
		expected   int64
		slices     []resourcev1.ResourceSlice
	}
	pools := map[string]*poolGeneration{}
	for _, slice := range slices {
		if slice.Spec.Driver != "tenstorrent.com" || slice.Spec.NodeName == nil || *slice.Spec.NodeName != e.ttNode {
			continue
		}
		key := slice.Spec.Driver + "/" + slice.Spec.Pool.Name
		pool := pools[key]
		if pool == nil || slice.Spec.Pool.Generation > pool.generation {
			pool = &poolGeneration{generation: slice.Spec.Pool.Generation, expected: slice.Spec.Pool.ResourceSliceCount}
			pools[key] = pool
		}
		if slice.Spec.Pool.Generation == pool.generation {
			pool.slices = append(pool.slices, slice)
		}
	}
	devices := map[string]bool{}
	complete := len(pools) > 0
	for _, pool := range pools {
		if int64(len(pool.slices)) != pool.expected {
			complete = false
		}
		for _, slice := range pool.slices {
			for _, device := range slice.Spec.Devices {
				board, chips := device.Attributes["boardName"], device.Attributes["chipCount"]
				if board.StringValue == nil || *board.StringValue != "n300" || chips.IntValue == nil || *chips.IntValue != 2 {
					continue
				}
				devices[slice.Spec.Driver+"/"+slice.Spec.Pool.Name+"/"+device.Name] = true
			}
		}
	}
	allocated := map[string]bool{}
	for _, claim := range claims {
		if claim.Status.Allocation == nil {
			continue
		}
		for _, allocation := range claim.Status.Allocation.Devices.Results {
			if allocation.AdminAccess != nil && *allocation.AdminAccess {
				continue
			}
			key := allocation.Driver + "/" + allocation.Pool + "/" + allocation.Device
			if devices[key] {
				allocated[key] = true
			}
		}
	}
	tt := AcceleratorPool{Accelerator: "tenstorrent", Node: e.ttNode, Unit: "board", ChipsPerDevice: 2, Total: int64(len(devices)), Allocated: int64(len(allocated))}
	tt.Ready = machines[e.ttNode].Schedulable && complete && driverReady(pods, e.ttNode, "tt-dra-driver")
	ttFree := max(int64(0), tt.Total-tt.Allocated)
	if !tt.Ready {
		ttFree = 0
		tt.Message = "Node, TT device driver, or complete device inventory is unavailable"
	}
	tt.Available = &ttFree
	if !complete {
		tt.Available = nil
	}
	snapshot.Pools = append(snapshot.Pools, nvidia, tt)
	return snapshot
}

func (a *App) hardware(w http.ResponseWriter, r *http.Request) {
	snapshot, err := a.executor.hardware(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, snapshot)
}
