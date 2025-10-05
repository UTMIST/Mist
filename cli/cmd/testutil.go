// cmd/testutil.go 

package cmd 

import (
	"bytes"
	"io"
	"os"
)

// Useful help functions. 
// I actually don't know if we have something like this already. 

func CaptureOutput(f func()) string{
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
