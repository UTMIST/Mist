package cmd 

import (
	"testing"
	// "fmt"
	// "bytes"
)

// No Flag config 
func TestConfigNoFlags(t *testing.T){
	cmd := &ConfigCmd{}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})
	if want := "No config action specified. Use --help for options."; !contains(output, want){
	t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

// Set Default Cluster to tt-gpu-cluster-1
func TestConfigDefaultCluster(t *testing.T){
	cmd := &ConfigCmd{DefaultCluster: "tt-gpu-cluster-1"} // Create config object 

	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})

	// fmt.Printf("Captured the output:  %s\n", output)

	if want := "Setting default cluster to: tt-gpu-cluster-1"; !contains(output, want) {
	t.Errorf("expected output to contain %q, got %q", want, output)
	} 
}

// Show Config 
func TestConfigCmd_Show(t *testing.T){
	cmd := &ConfigCmd{Show: true}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})

	if want := "Current configuration:"; !contains(output, want){
		t.Errorf("expected output to contain %q, got %q", want, output)
	}
}

// Show Error message if both flags are sent 
func TestConfigBothFlagError(t *testing.T){
	cmd := &ConfigCmd{DefaultCluster: "tt-gpu-cluster-1", Show: true}
	output := CaptureOutput(func(){
		_ = cmd.Run() 
	})

	if want := "Cannot use --show and --default-cluster together"; !contains(output, want){
		t.Errorf("Expected the error message of \"%s\", got %q", want, output)
	}

}
