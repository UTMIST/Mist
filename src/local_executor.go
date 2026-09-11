package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// The local demo runs only repository-defined commands inside its own GPU
// container. It does not need access to the host Docker control socket.
func (s *Supervisor) runGPUCommand(ctx context.Context, command []string) (string, error) {
	if os.Getenv("MIST_EXECUTOR") != "local" {
		if s.dockerMgr == nil {
			return "", fmt.Errorf("Docker is unavailable")
		}
		return s.dockerMgr.RunCommand(ctx, cudaImage(), command, true)
	}
	var output boundedOutput
	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	cmd.Stdout, cmd.Stderr = &output, &output
	err := cmd.Run()
	if ctx.Err() != nil {
		err = ctx.Err()
	}
	if err != nil {
		return output.String(), fmt.Errorf("GPU workload failed: %w", err)
	}
	return output.String(), nil
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := (64 << 10) - b.Len()
	if len(data) > remaining {
		data = data[:remaining]
	}
	_, _ = b.Buffer.Write(data)
	return n, nil
}
