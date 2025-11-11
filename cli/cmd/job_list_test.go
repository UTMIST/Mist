package cmd 

import (
	"testing"
)

// Will be more specific in the future!
func TestJobList(t *testing.T){
	cmd := &ListCmd{All: true}
	output := CaptureOutput(func(){
		_ = cmd.Run()
	})
	// Note the time is dynamic. 
	want := "Job ID  Name  Status  GPU Type  Created At\n--------------------------------------------------------------\nID:1  docker_container_name_1  Running   AMD"	
	if !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
	}