package cmd 

import (
	"testing"
	// "fmt"
)

// Should return nothing for now...? No supervisors are being made 

func TestGetAllSupervisorsStatusesEmpty(t * testing.T){
	cmd := &SupervisorStatusCmd{}
	output := CaptureOutput( func() {
		_ = cmd.Run() 
	})

	want := "All Supervisor Status Response:  {\"count\":0,\"supervisors\":null}\n\n"
	if !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

func TestGetSupervisorByIDEmpty(t * testing.T){
	cmd := &SupervisorStatusCmd{ID: "123"}
	output := CaptureOutput( func() {
		_ = cmd.Run() 
	})

	want := "Supervisor Status by ID Response:  supervisor not found\n\n"
	if !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}