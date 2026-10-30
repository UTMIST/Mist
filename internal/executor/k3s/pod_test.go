package k3s

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/UTMIST/Mist/internal/executor"
)

var update = flag.Bool("update", false, "rewrite golden files")

func testConfig() Config {
	return Config{
		Namespace:     "mist-jobs",
		EntryPath:     "/usr/local/bin/mist-job-entry",
		LaunchTimeout: 10 * time.Minute,
		BackstopSlack: time.Minute,
		Nameservers:   []string{"1.1.1.1", "9.9.9.9"},
		Volumes: map[string]Volume{
			"vol1": {Server: "192.168.240.2", Export: "/mist", HostNode: "utmist-tt", HostPath: "/mist"},
		},
	}
}

func gpuBatch() executor.LaunchSpec {
	s := executor.LaunchSpec{
		JobID:          412,
		ExecutionID:    "3f1c2a9e-8b7d-4c6e-9a5b-1d2e3f4a5b6c",
		AuthGeneration: 7,
		LaunchDeadline: time.Date(2026, 11, 2, 15, 14, 5, 0, time.UTC),
		Kind:           executor.KindBatch,
		JobName:        "resnet-sweep-3",
		Partition:      "gpu",
		QOS:            "normal",
		Node:           "utmist-z1opa08",
		User:           executor.User{Name: "alice", UID: 20001, PersonalGID: 20001},
		TeamSlug:       "vision",
		TeamGID:        40001,
		ExtraGIDs:      []int{19000},
		Resources:      executor.Resources{MilliCPU: 8000, MemoryBytes: 32 << 30, GPUs: 1, ScratchBytes: 100 << 30},
		Mounts: []executor.Mount{
			{VolumeID: "vol1", Source: "/teams/vision", Target: "/mist/teams/vision"},
			{VolumeID: "vol1", Source: "/software", Target: "/mist/software", ReadOnly: true},
		},
		Workdir:     "/mist/teams/vision/alice/resnet",
		OutputPath:  "/mist/teams/vision/alice/resnet/mist-412.out",
		Script:      "#!/bin/bash\npython train.py --lr \"$1\"\n",
		Args:        []string{"3e-4"},
		Env:         map[string]string{"WANDB_PROJECT": "resnet"},
		Runtime:     "registry.mist.local/runtime/gpu@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		TimeLimit:   4 * time.Hour,
		SignalGrace: 60 * time.Second,
	}
	s.Seal()
	return s
}

func cpuCommand() executor.LaunchSpec {
	s := gpuBatch()
	s.JobID, s.Kind, s.Partition, s.Node = 413, executor.KindCommand, "cpu", "utmist-tt"
	s.Script, s.Args = "", nil
	s.Argv = []string{"huggingface-cli", "download", "meta-llama/Llama-3-8B"}
	s.Resources = executor.Resources{MilliCPU: 2000, MemoryBytes: 8 << 30, ScratchBytes: 50 << 30}
	s.Runtime = "registry.mist.local/runtime/cpu@sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	s.Seal()
	return s
}

func TestBuildGolden(t *testing.T) {
	for name, spec := range map[string]executor.LaunchSpec{"gpu-batch-nfs": gpuBatch(), "cpu-command-hostpath": cpuCommand()} {
		t.Run(name, func(t *testing.T) {
			cm, pod, err := Build(spec, testConfig())
			if err != nil {
				t.Fatal(err)
			}
			var got []byte
			for _, obj := range []any{cm, pod} {
				b, err := yaml.Marshal(obj)
				if err != nil {
					t.Fatal(err)
				}
				got = append(append(got, "---\n"...), b...)
			}
			file := filepath.Join("testdata", name+".golden.yaml")
			if *update {
				os.MkdirAll("testdata", 0o755)
				if err := os.WriteFile(file, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v (run with -update to create)", err)
			}
			if string(got) != string(want) {
				t.Fatalf("%s differs from golden; run go test ./internal/executor/k3s -update and review the diff", file)
			}
		})
	}
}

// Security properties checked directly, so a golden regeneration cannot
// silently drop one.
func TestBuildInvariants(t *testing.T) {
	for _, spec := range []executor.LaunchSpec{gpuBatch(), cpuCommand()} {
		cm, pod, err := Build(spec, testConfig())
		if err != nil {
			t.Fatal(err)
		}
		ps, c := pod.Spec, pod.Spec.Containers[0]
		sc := ps.SecurityContext
		check := func(ok bool, what string) {
			if !ok {
				t.Errorf("job %d: %s", spec.JobID, what)
			}
		}
		check(ps.NodeName == spec.Node, "nodeName is Mist's chosen node")
		check(ps.SchedulerName == "", "default scheduler fields untouched")
		check(ps.RestartPolicy == corev1.RestartPolicyNever, "restartPolicy Never")
		check(!*ps.AutomountServiceAccountToken && !*ps.EnableServiceLinks, "no service account token or service links")
		check(ps.DNSPolicy == corev1.DNSNone, "no cluster DNS")
		check(sc.FSGroup == nil, "no fsGroup")
		check(*sc.RunAsUser == int64(spec.User.UID) && *sc.RunAsGroup == int64(spec.TeamGID), "uid and team gid")
		check(*sc.SupplementalGroupsPolicy == corev1.SupplementalGroupsPolicyStrict, "strict supplemental groups")
		check(len(sc.SupplementalGroups) == 2, "only mist-users and the personal group")
		check(*sc.RunAsNonRoot, "runAsNonRoot")
		check(!*c.SecurityContext.AllowPrivilegeEscalation && *c.SecurityContext.ReadOnlyRootFilesystem, "no escalation, read-only root")
		check(len(c.SecurityContext.Capabilities.Drop) == 1 && c.SecurityContext.Capabilities.Drop[0] == "ALL", "drop ALL")
		check(c.Resources.Requests.Cpu().Equal(*c.Resources.Limits.Cpu()) &&
			c.Resources.Requests.Memory().Equal(*c.Resources.Limits.Memory()), "requests = limits")
		check(len(ps.InitContainers) == 1 && ps.InitContainers[0].Name == "network-gate", "network isolation gate before user code")
		check(*ps.InitContainers[0].SecurityContext.ReadOnlyRootFilesystem, "gate is unprivileged too")
		check(*ps.TerminationGracePeriodSeconds == int64(spec.SignalGrace/time.Second), "grace = signal grace")
		check(pod.Annotations[AnnotSpecHash] == spec.SpecHash, "spec hash annotation")
		for _, v := range ps.Volumes {
			check(v.HostPath == nil || spec.Node == "utmist-tt", "hostPath only on the volume's host")
		}
		for _, f := range []string{"passwd", "group"} {
			check(!strings.Contains(cm.Data[f], "carol") && strings.Count(cm.Data[f], "team-") <= 1, f+" has only the charged team")
		}
	}

	if _, _, err := Build(gpuBatch(), Config{Namespace: "mist-jobs"}); err == nil {
		t.Error("unknown volume accepted")
	}
	tt := gpuBatch()
	tt.Resources.GPUs, tt.Resources.TTBoards = 0, 1
	tt.Seal()
	if _, _, err := Build(tt, testConfig()); err == nil {
		t.Error("TT job built without DRA")
	}
}
