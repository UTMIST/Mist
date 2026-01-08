package cmd 

import (
	"testing"
	// "os"
	// "fmt"
)


// Just printing out the confirmation 
func TestJobSubmitConfirmation(t *testing.T){
	// This job should not exist in the dummy 
	cmd := &JobSubmitCmd{Script: "test", Compute:"TT"}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})
	if want := "Are you sure? (y/n): "; !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

// Valid proceeding with TT work 
func TestJobSubmitProceed(t *testing.T){
	cmd := &JobSubmitCmd{Script: "test", Compute: "TT"}
	output := CaptureOutput(func(){
		MockInput("y\n", func() {
			_ = cmd.Run()
		})

	})
	
	if !contains(output, "Confirmed, proceeding...\nSubmitting job with script: test\nRequested GPU type: TT") {
		t.Errorf("expected 'Confirmed, proceeding...' but got:\n%s", output)
	}
}

// Valid Cancellation: Putting in N 
func TestJobSubmitCancel(t *testing.T){
	cmd := &JobSubmitCmd{Script: "test", Compute: "TT"}
	output := CaptureOutput(func(){
		MockInput("n\n", func() {
			_ = cmd.Run()
		})
	})

	if !contains(output, "Cancelled.") {
		t.Errorf("expected 'Cancelled.' but got:\n%s", output)
	}
	// fmt.Printf("Got the output %s", output)
}

// Valid Cancellation: Putting in bogus response 
func TestJobSubmitBogusResponse(t *testing.T){
	cmd := &JobSubmitCmd{Script: "test", Compute: "TT"}
	output := CaptureOutput(func(){
		MockInput("bogus\n", func() {
			_ = cmd.Run()
		})
	})

	if !contains(output, "Cancelled.") {
		t.Errorf("expected 'Cancelled.' but got:\n%s", output)
	}
	// fmt.Printf("Got the output %s", output)
}