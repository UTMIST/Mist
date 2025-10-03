// cmd/testutil.go

package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

// Useful help functions.
// I actually don't know if we have something like this already.

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

// Creating a helper function for capturing
// Stdout outputs
