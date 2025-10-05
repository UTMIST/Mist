package cmd 

import (
	"testing"
)


// Added job, with no compute type added 
func TestJobSubmitConfirmation(t *testing.T){
	// This job should not exist in the dummy 
	cmd := &JobSubmitCmd{Script: "test", Compute:"TT"}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})
	if want := "Are you sure? (y/N): "; !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

// func TestJobSubmitProceed(t *testing.T){
// 	cmd := &JobSubmitCmd{Script: "test", Compute: "TT"}
// 	output := CaptureOutput(func(){
// 		_ = cmd.Run()
// 	})




// }

