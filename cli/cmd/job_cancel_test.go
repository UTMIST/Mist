package cmd 

import (
	"testing"
	// "fmt"
)


// Added job, with no compute type added 
func TestJobCancelJobDoesNotExist(t *testing.T){
	// This job should not exist in the dummy 
	cmd := &JobCancelCmd{ID: "job_12345"}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})
	if want := "job_12345 does not exist in your jobs.\nUse the command \"job list\" for your list of jobs."; !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

// Added job, with compute type 
func TestJobCancelValid(t *testing.T){
	// This job should not exist in the dummy 
	cmd := &JobCancelCmd{ID: "ID:1"}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})
	if want := "Are you sure you want to cancel ID:1? (y/n):"; !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}


func TestJobCancelProceed(t *testing.T){
	cmd := &JobCancelCmd{ID: "ID:1"}
	// Lowkey, we should refactor this into a 
	output := CaptureOutput(func(){
		MockInput("y\n", func() {
			_ = cmd.Run()
		})
	})
	if !contains(output, "Confirmed, proceeding job cancellation...."){
		t.Errorf("expected 'Confirmed, proceeding job cancellation....' but got:\n%s", output)
	}
	// fmt.Printf("Got the output %s\n", output)
}