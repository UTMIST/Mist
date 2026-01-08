// cmd/testutil.go

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	// "strings"
)

// To simulate multiple consecutive user inputs, just put \n between your text ("Enter Key")
func MockInput(input string, f func()) {
	old := os.Stdin
	r, w, _ := os.Pipe()
	w.Write([]byte(input))
	w.Close()
	os.Stdin = r
	f()
	os.Stdin = old
}

func CaptureOutput(f func()) string {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	f()

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

func contains(s, substr string) bool {
	return bytes.Contains([]byte(s), []byte(substr))
}

func jobExists(jobs []Job, id string) bool {
	for _, job := range jobs {
		if job.ID == id {
			return true
		}
	}
	return false
}

func findJobByID(jobs []Job, id string) (Job, error) {
	for _, job := range jobs {
		if job.ID == id {
			return job, nil
		}
	}
	return Job{}, fmt.Errorf("job with ID %s not found", id)
}

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "mist", "config.json")
	}
	return filepath.Join(dir, "mist", "config.json")
}

// Creating a helper function for capturing
// Stdout outputs
