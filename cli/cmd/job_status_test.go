package cmd 

import (
	"testing"
)


// This is just a dummy! 
func TestJobStatus(t *testing.T){
	// This job should not exist in the dummy 
	cmd := &JobStatusCmd{JobID: "ID:1"}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})
	if want := "Job ID  Name  Status  GPU Type  Created At"; !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

// func TestJobSubmitProceed(t *testing.T){
// 	cmd := &JobSubmitCmd{Script: "test", Compute: "TT"}
// 	output := CaptureOutput(func(){
// 		_ = cmd.Run()
// 	})




// }

