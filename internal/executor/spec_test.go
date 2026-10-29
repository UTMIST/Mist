package executor

import (
	"strings"
	"testing"
	"time"
)

func validSpec() LaunchSpec {
	s := LaunchSpec{
		JobID:          412,
		ExecutionID:    "3f1c2a9e-8b7d-4c6e-9a5b-1d2e3f4a5b6c",
		AuthGeneration: 7,
		LaunchDeadline: time.Date(2026, 11, 2, 15, 14, 5, 0, time.UTC),
		Kind:           KindBatch,
		Node:           "utmist-z1opa08",
		User:           User{Name: "alice", UID: 20001, PersonalGID: 20001},
		TeamSlug:       "vision",
		TeamGID:        40001,
		ExtraGIDs:      []int{19000},
		Resources:      Resources{MilliCPU: 8000, MemoryBytes: 32 << 30, GPUs: 1, ScratchBytes: 100 << 30},
		Mounts: []Mount{
			{VolumeID: "vol1", Source: "/teams/vision", Target: "/mist/teams/vision"},
			{VolumeID: "vol1", Source: "/software", Target: "/mist/software", ReadOnly: true},
		},
		Workdir:     "/mist/teams/vision/alice/resnet",
		OutputPath:  "/mist/teams/vision/alice/resnet/mist-412.out",
		Script:      "#!/bin/bash\npython train.py\n",
		Args:        []string{"--lr", "3e-4"},
		Env:         map[string]string{"WANDB_PROJECT": "resnet", "A": "1"},
		Runtime:     "registry.local/mist/gpu@sha256:0000",
		TimeLimit:   4 * time.Hour,
		SignalGrace: 60 * time.Second,
	}
	s.Seal()
	return s
}

func TestSpecHashStable(t *testing.T) {
	a := validSpec()
	b := validSpec()
	b.LaunchDeadline = b.LaunchDeadline.In(time.FixedZone("EST", -5*3600))
	b.Env = map[string]string{"A": "1", "WANDB_PROJECT": "resnet"}
	if a.SpecHash != b.ComputeSpecHash() {
		t.Fatal("equal specs hash differently")
	}

	c := validSpec()
	c.Args = []string{}
	c.Seal()
	d := validSpec()
	d.Args = nil
	d.Seal()
	if c.SpecHash != d.SpecHash {
		t.Fatal("empty and nil args hash differently")
	}

	e := validSpec()
	e.Args = []string{"--lr", "1e-3"}
	if e.ComputeSpecHash() == a.SpecHash {
		t.Fatal("different args, same hash")
	}
}

func TestValidate(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	cases := map[string]func(*LaunchSpec){
		"unsealed":           func(s *LaunchSpec) { s.SpecHash = "" },
		"bad execution id":   func(s *LaunchSpec) { s.ExecutionID = "job-412" },
		"no deadline":        func(s *LaunchSpec) { s.LaunchDeadline = time.Time{} },
		"no auth generation": func(s *LaunchSpec) { s.AuthGeneration = 0 },
		"no node":            func(s *LaunchSpec) { s.Node = "" },
		"batch with argv":    func(s *LaunchSpec) { s.Argv = []string{"ls"} },
		"command with args":  func(s *LaunchSpec) { s.Kind = KindCommand; s.Script = ""; s.Argv = []string{"ls"} },
		"interactive script": func(s *LaunchSpec) { s.Kind = KindInteractive; s.Args = nil },
		"unknown kind":       func(s *LaunchSpec) { s.Kind = "service" },
		"relative mount":     func(s *LaunchSpec) { s.Mounts[0].Target = "mist/teams/vision" },
		"unclean output":     func(s *LaunchSpec) { s.OutputPath = "/mist/teams/vision/../other/x.out" },
		"grace too long":     func(s *LaunchSpec) { s.SignalGrace = 10 * time.Minute },
		"zero time limit":    func(s *LaunchSpec) { s.TimeLimit = 0 },
		"no memory":          func(s *LaunchSpec) { s.Resources.MemoryBytes = 0 },
		"partial device ids": func(s *LaunchSpec) { s.Resources.GPUs = 2; s.DeviceIDs = []string{"GPU-1"} },
		"no runtime":         func(s *LaunchSpec) { s.Runtime = "" },
	}
	for name, mutate := range cases {
		s := validSpec()
		mutate(&s)
		if name != "unsealed" {
			s.Seal()
		}
		if err := s.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	tampered := validSpec()
	tampered.Resources.GPUs = 2
	if err := tampered.Validate(); err == nil || !strings.Contains(err.Error(), "spec_hash") {
		t.Errorf("tampered spec: got %v", err)
	}
}

func TestNewExecutionID(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		id := NewExecutionID()
		if !uuidPattern.MatchString(id) || seen[id] {
			t.Fatalf("bad or repeated id %q", id)
		}
		seen[id] = true
	}
}

func TestResources(t *testing.T) {
	node := Resources{MilliCPU: 48000, MemoryBytes: 240 << 30, GPUs: 2, ScratchBytes: 400 << 30}
	job := Resources{MilliCPU: 8000, MemoryBytes: 32 << 30, GPUs: 1, ScratchBytes: 100 << 30}
	free := node.Sub(job).Sub(job)
	if !node.Fits(job) || free.Fits(job) {
		t.Fatalf("two 1-GPU jobs should fill the node: free=%+v", free)
	}
	if free.Add(job) != node.Sub(job) {
		t.Fatal("Add does not undo Sub")
	}
}
