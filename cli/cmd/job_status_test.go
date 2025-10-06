package cmd

import (
	"testing"
)

// Added job, with no compute type added
func TestJobStatusJobDoesNotExist(t *testing.T) {
	// This job should not exist in the dummy
	cmd := &JobStatusCmd{ID: "job_12345"}
	output := CaptureOutput(func() {
		_ = cmd.Run()
	})
	if want := "job_12345 does not exist in your jobs.\nUse the command \"job list\" for your list of jobs."; !contains(output, want) {
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

// Added job, with compute type
func TestJobStatusValid(t *testing.T) {
	// This job should not exist in the dummy
	cmd := &JobStatusCmd{ID: "ID:1"}
	output := CaptureOutput(func() {
		_ = cmd.Run()
	})
	if want := "docker_container_name_1"; !contains(output, want) {
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}
