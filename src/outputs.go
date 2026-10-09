package main

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// prepareOutputs creates the job's PVC subdirectory before kubelet mounts it
// into the workload. Both supported root and nonroot images can write results.
// Retained node-local jobs use this initializer. Team jobs prepare their scoped
// NFS output directory through the API and do not mount the parent checkpoint PVC.
func prepareOutputs(id string) corev1.Container {
	return corev1.Container{
		Name: "prepare-outputs", Image: "busybox:1.36", ImagePullPolicy: corev1.PullIfNotPresent,
		Command:         []string{"sh", "-c", `mkdir -p "/checkpoints/$MIST_JOB_ID" && chmod 0777 "/checkpoints/$MIST_JOB_ID"`},
		Env:             []corev1.EnvVar{{Name: "MIST_JOB_ID", Value: id}},
		VolumeMounts:    []corev1.VolumeMount{{Name: "checkpoints", MountPath: "/checkpoints"}},
		SecurityContext: &corev1.SecurityContext{RunAsUser: ptr(int64(0)), Privileged: ptr(false), AllowPrivilegeEscalation: ptr(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("10m"), corev1.ResourceMemory: resource.MustParse("16Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
		},
	}
}
