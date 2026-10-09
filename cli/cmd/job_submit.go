package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type JobSubmitCmd struct {
	Script  string `arg:"" help:"Path to a Python or shell script"`
	Compute string `help:"CPU, NVIDIA, or TT" default:"CPU"`
	Devices int    `help:"NVIDIA GPU count or Tenstorrent board count" default:"1"`
	Dataset string `help:"Owned ready dataset ID to mount at /inputs (optional)"`
	Image   string `help:"Approved container image (optional; defaults by compute type)"`
	CPU     string `help:"Requested CPU quantity, e.g. 2 or 500m"`
	Memory  string `help:"Requested memory, e.g. 4Gi"`
	Timeout int64  `help:"Maximum execution time in seconds" default:"600"`
	Name    string `help:"Display name for the job"`
}

func (j *JobSubmitCmd) Run(ctx *AppContext) error {
	compute := strings.ToUpper(j.Compute)
	if compute == "" {
		compute = "CPU"
	}
	accelerator := "cpu"
	devices := j.Devices
	if devices == 0 {
		devices = 1
	}
	switch compute {
	case "CPU":
		devices = 0
	case "NVIDIA", "CUDA":
		accelerator = "nvidia"
	case "TT", "TENSTORRENT":
		accelerator = "tenstorrent"
	default:
		return fmt.Errorf("compute must be CPU, NVIDIA, or TT")
	}
	data, err := os.ReadFile(j.Script)
	if err != nil {
		return err
	}
	name := j.Name
	if name == "" {
		name = filepath.Base(j.Script)
	}
	request := map[string]interface{}{"type": "command", "name": name, "accelerator": accelerator,
		"device_count": devices, "script": string(data), "script_name": filepath.Base(j.Script),
		"dataset_id": j.Dataset, "image": j.Image, "cpu": j.CPU, "memory": j.Memory, "timeout_seconds": j.Timeout}
	var response struct {
		JobID string `json:"job_id"`
	}
	if err := ctx.api("POST", "/jobs", request, &response); err != nil {
		return err
	}
	fmt.Println("Job submitted:", response.JobID)
	return nil
}
