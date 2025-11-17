package cmd 

import (
	"testing"
	// "fmt"
)

// Right now supervisors should be empty! Add more in the future. 
func TestSupervisorDoesNotExist(t *testing.T){
	cmd := &SupervisorListCmd{Active: true}
	output := CaptureOutput( func() {
		_ = cmd.Run() 
	})

	want := "Supervisor List Response:  {\"active_only\":true,\"count\":0,\"supervisors\":null}\n\n"
	if !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}
