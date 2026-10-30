// Package k3s is the K3s executor (M1): one plain Pod and ConfigMap per
// job in the mist-jobs namespace, created after Mist has admitted the job
// and chosen its node. Contract: docs/agents/architecture/executors.md.
package k3s

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/UTMIST/Mist/internal/executor"
	"github.com/UTMIST/Mist/internal/jobentry"
)

// Labels and annotations on every object this executor creates.
const (
	LabelJobID       = "mist/job-id"
	LabelExecutionID = "mist/execution-id"
	LabelPartition   = "mist/partition"
	LabelTeam        = "mist/team"
	AnnotSpecHash    = "mist/spec-hash"

	MistUsersGID = 19000

	jobDir         = "/etc/mist"
	terminationLog = "/dev/termination-log"
)

// Volume says how to reach one storage volume from a node.
type Volume struct {
	Server   string // NFS server address on the direct link
	Export   string // NFS export root
	HostNode string // node that hosts the volume locally, if any
	HostPath string // the volume root on that node
}

// Config holds the cluster-specific settings the pod builder needs.
type Config struct {
	Namespace     string
	EntryPath     string        // mist-job-entry inside every runtime image
	LaunchTimeout time.Duration // admission to work start
	BackstopSlack time.Duration // beyond launch timeout + time limit + grace
	Nameservers   []string      // approved resolvers; jobs never use cluster DNS
	Volumes       map[string]Volume
}

// Name is the Pod and ConfigMap name for a job.
func Name(jobID int64) string { return "job-" + strconv.FormatInt(jobID, 10) }

// Build turns a LaunchSpec into the ConfigMap and Pod for it. It is a pure
// function: the same spec and config always give the same objects, which
// golden tests pin.
func Build(spec executor.LaunchSpec, cfg Config) (*corev1.ConfigMap, *corev1.Pod, error) {
	if err := spec.Validate(); err != nil {
		return nil, nil, err
	}
	if spec.Kind == executor.KindInteractive {
		return nil, nil, fmt.Errorf("k3s: interactive jobs arrive in M2")
	}
	if spec.Resources.TTBoards > 0 {
		return nil, nil, fmt.Errorf("k3s: TT boards need DRA claims (M4)")
	}
	name := Name(spec.JobID)
	teamRoot := "/mist/teams/" + spec.TeamSlug
	meta := metav1.ObjectMeta{
		Name:      name,
		Namespace: cfg.Namespace,
		Labels: map[string]string{
			LabelJobID:       strconv.FormatInt(spec.JobID, 10),
			LabelExecutionID: spec.ExecutionID,
			LabelPartition:   spec.Partition,
			LabelTeam:        spec.TeamSlug,
		},
		Annotations: map[string]string{AnnotSpecHash: spec.SpecHash},
	}

	entry := jobentry.Spec{
		JobID: spec.JobID, Name: spec.JobName, User: spec.User.Name, Team: spec.TeamSlug,
		Partition: spec.Partition, QOS: spec.QOS, Node: spec.Node, Kind: spec.Kind,
		Resources:      spec.Resources,
		TimeLimit:      int64(spec.TimeLimit / time.Second),
		SignalGrace:    int64(spec.SignalGrace / time.Second),
		TeamRoot:       teamRoot,
		Home:           teamRoot + "/" + spec.User.Name,
		Workdir:        spec.Workdir,
		OutputPath:     spec.OutputPath,
		Scratch:        "/scratch",
		Args:           spec.Args,
		Argv:           spec.Argv,
		Env:            spec.Env,
		TerminationLog: terminationLog,
	}
	data := map[string]string{"passwd": passwd(spec), "group": group(spec)}
	if spec.Kind == executor.KindBatch {
		entry.ScriptPath = jobDir + "/script"
		data["script"] = spec.Script
	}
	if err := entry.Validate(); err != nil {
		return nil, nil, err
	}
	jobJSON, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	data["job.json"] = string(jobJSON) + "\n"
	cm := &corev1.ConfigMap{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: meta,
		Data:       data,
		Immutable:  ptr(true),
	}

	mounts, volumes, err := durableMounts(spec, cfg)
	if err != nil {
		return nil, nil, err
	}
	r := spec.Resources
	quantities := corev1.ResourceList{
		corev1.ResourceCPU:              *resource.NewMilliQuantity(r.MilliCPU, resource.DecimalSI),
		corev1.ResourceMemory:           *resource.NewQuantity(r.MemoryBytes, resource.BinarySI),
		corev1.ResourceEphemeralStorage: *resource.NewQuantity(r.ScratchBytes, resource.BinarySI),
	}
	if r.GPUs > 0 {
		quantities["nvidia.com/gpu"] = *resource.NewQuantity(r.GPUs, resource.DecimalSI)
	}
	// The gate needs almost nothing; equal requests and limits keep the Pod
	// in the Guaranteed QoS class.
	gateQuantities := corev1.ResourceList{
		corev1.ResourceCPU:    *resource.NewMilliQuantity(100, resource.DecimalSI),
		corev1.ResourceMemory: *resource.NewQuantity(32<<20, resource.BinarySI),
	}
	scratchSize := resource.NewQuantity(r.ScratchBytes, resource.BinarySI)
	shmSize := resource.NewQuantity(r.MemoryBytes/2, resource.BinarySI) // counts against memory

	volumes = append(volumes,
		corev1.Volume{Name: "scratch", VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: scratchSize}}},
		corev1.Volume{Name: "shm", VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory, SizeLimit: shmSize}}},
		corev1.Volume{Name: "job", VolumeSource: corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
			LocalObjectReference: corev1.LocalObjectReference{Name: name},
			DefaultMode:          ptr[int32](0o444),
		}}},
	)
	mounts = append(mounts,
		corev1.VolumeMount{Name: "scratch", MountPath: "/scratch"},
		// The same directory, not a subPath: kubelet creates subPath
		// directories root-owned, which a non-root job could not write.
		corev1.VolumeMount{Name: "scratch", MountPath: "/tmp"},
		corev1.VolumeMount{Name: "shm", MountPath: "/dev/shm"},
		corev1.VolumeMount{Name: "job", MountPath: jobDir + "/job.json", SubPath: "job.json", ReadOnly: true},
		corev1.VolumeMount{Name: "job", MountPath: "/etc/passwd", SubPath: "passwd", ReadOnly: true},
		corev1.VolumeMount{Name: "job", MountPath: "/etc/group", SubPath: "group", ReadOnly: true},
	)
	if spec.Kind == executor.KindBatch {
		mounts = append(mounts, corev1.VolumeMount{Name: "job", MountPath: jobDir + "/script", SubPath: "script", ReadOnly: true})
	}

	groups := append(slices.Clone(spec.ExtraGIDs), spec.User.PersonalGID)
	slices.Sort(groups)
	groups = slices.Compact(groups)
	sup := make([]int64, len(groups))
	for i, g := range groups {
		sup[i] = int64(g)
	}
	strict := corev1.SupplementalGroupsPolicyStrict
	// A coarse backstop that holds even with the scheduler down. Pod time
	// includes startup, so it allows the launch timeout on top of walltime.
	backstop := int64((cfg.LaunchTimeout + spec.TimeLimit + spec.SignalGrace + cfg.BackstopSlack) / time.Second)

	pod := &corev1.Pod{
		TypeMeta:   metav1.TypeMeta{APIVersion: "v1", Kind: "Pod"},
		ObjectMeta: meta,
		Spec: corev1.PodSpec{
			NodeName:                      spec.Node, // Mist placed it; the Kubernetes scheduler never does
			RestartPolicy:                 corev1.RestartPolicyNever,
			ActiveDeadlineSeconds:         &backstop,
			TerminationGracePeriodSeconds: ptr(int64(spec.SignalGrace / time.Second)),
			AutomountServiceAccountToken:  ptr(false),
			EnableServiceLinks:            ptr(false),
			DNSPolicy:                     corev1.DNSNone,
			DNSConfig:                     &corev1.PodDNSConfig{Nameservers: cfg.Nameservers},
			SecurityContext: &corev1.PodSecurityContext{
				RunAsUser:                ptr(int64(spec.User.UID)),
				RunAsGroup:               ptr(int64(spec.TeamGID)),
				SupplementalGroups:       sup,
				SupplementalGroupsPolicy: &strict,
				RunAsNonRoot:             ptr(true),
				SeccompProfile:           &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
				// No fsGroup: it would rewrite ownership of team files.
			},
			// Network policy is programmed only after the Pod starts. This
			// gate holds user code back until the node's kubelet, which is
			// certainly listening, has become unreachable from the Pod.
			InitContainers: []corev1.Container{{
				Name:            "network-gate",
				Image:           spec.Runtime,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{cfg.EntryPath, "-await-isolation", "$(MIST_HOST_IP):10250"},
				Env: []corev1.EnvVar{{Name: "MIST_HOST_IP", ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"}}}},
				Resources:                corev1.ResourceRequirements{Requests: gateQuantities, Limits: gateQuantities},
				TerminationMessagePath:   terminationLog,
				TerminationMessagePolicy: corev1.TerminationMessageReadFile,
				SecurityContext:          containerSecurity(),
			}},
			Containers: []corev1.Container{{
				Name:                     "job",
				Image:                    spec.Runtime,
				ImagePullPolicy:          corev1.PullIfNotPresent,
				Command:                  []string{cfg.EntryPath, "-spec", jobDir + "/job.json"},
				Resources:                corev1.ResourceRequirements{Requests: quantities, Limits: quantities},
				VolumeMounts:             mounts,
				TerminationMessagePath:   terminationLog,
				TerminationMessagePolicy: corev1.TerminationMessageReadFile,
				SecurityContext:          containerSecurity(),
			}},
			Volumes: volumes,
		},
	}
	return cm, pod, nil
}

// durableMounts resolves each LaunchSpec mount to NFS, or to hostPath when
// the job runs on the node that hosts the volume.
func durableMounts(spec executor.LaunchSpec, cfg Config) ([]corev1.VolumeMount, []corev1.Volume, error) {
	var mounts []corev1.VolumeMount
	var volumes []corev1.Volume
	for i, m := range spec.Mounts {
		v, ok := cfg.Volumes[m.VolumeID]
		if !ok {
			return nil, nil, fmt.Errorf("k3s: unknown volume %q", m.VolumeID)
		}
		name := fmt.Sprintf("durable-%d", i)
		var src corev1.VolumeSource
		if v.HostNode == spec.Node {
			src.HostPath = &corev1.HostPathVolumeSource{
				Path: path.Join(v.HostPath, m.Source),
				Type: ptr(corev1.HostPathDirectory), // never create missing folders
			}
		} else {
			src.NFS = &corev1.NFSVolumeSource{Server: v.Server, Path: path.Join(v.Export, m.Source), ReadOnly: m.ReadOnly}
		}
		volumes = append(volumes, corev1.Volume{Name: name, VolumeSource: src})
		mounts = append(mounts, corev1.VolumeMount{Name: name, MountPath: m.Target, ReadOnly: m.ReadOnly})
	}
	return mounts, volumes, nil
}

// passwd and group project only what the job needs: system entries, the
// submitter and the charged team. No other team's membership is visible.
// Names were validated against ^[a-z][a-z0-9-]{1,30}$, so they cannot
// inject fields or lines.
func passwd(s executor.LaunchSpec) string {
	home := "/mist/teams/" + s.TeamSlug + "/" + s.User.Name
	return strings.Join([]string{
		"root:x:0:0:root:/root:/usr/sbin/nologin",
		"nobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin",
		fmt.Sprintf("%s:x:%d:%d::%s:/bin/bash", s.User.Name, s.User.UID, s.TeamGID, home),
	}, "\n") + "\n"
}

func group(s executor.LaunchSpec) string {
	lines := []string{"root:x:0:", "nogroup:x:65534:",
		fmt.Sprintf("team-%s:x:%d:%s", s.TeamSlug, s.TeamGID, s.User.Name),
		fmt.Sprintf("%s:x:%d:%s", s.User.Name, s.User.PersonalGID, s.User.Name),
	}
	for _, g := range s.ExtraGIDs {
		name := fmt.Sprintf("mist-gid-%d", g)
		if g == MistUsersGID {
			name = "mist-users"
		}
		lines = append(lines, fmt.Sprintf("%s:x:%d:%s", name, g, s.User.Name))
	}
	return strings.Join(lines, "\n") + "\n"
}

func containerSecurity() *corev1.SecurityContext {
	return &corev1.SecurityContext{
		AllowPrivilegeEscalation: ptr(false),
		Privileged:               ptr(false),
		ReadOnlyRootFilesystem:   ptr(true),
		Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}},
	}
}

func ptr[T any](v T) *T { return &v }
