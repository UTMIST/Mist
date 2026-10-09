package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

type ListCmd struct {
	All bool `help:"Include completed, failed, and cancelled jobs" short:"a"`
}
type Job struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Status      string    `json:"job_state"`
	GPUType     string    `json:"accelerator"`
	CreatedAt   time.Time `json:"created"`
	Node        string    `json:"node"`
	DeviceCount int       `json:"device_count"`
	ExitCode    *int      `json:"exit_code"`
	Message     string    `json:"message"`
}

func printJobs(jobs []Job) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Job ID\tName\tStatus\tCompute\tDevices\tNode\tCreated")
	for _, job := range jobs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\t%s\t%s\n", job.ID, job.Name, job.Status, job.GPUType, job.DeviceCount, job.Node, job.CreatedAt.Format(time.RFC3339))
	}
	_ = w.Flush()
}

func (l *ListCmd) Run(ctx *AppContext) error {
	var response struct {
		Jobs []Job `json:"jobs"`
	}
	if err := ctx.api("GET", "/jobs", nil, &response); err != nil {
		return err
	}
	jobs := []Job{}
	for _, job := range response.Jobs {
		if l.All || job.Status == "Scheduled" || job.Status == "InProgress" {
			jobs = append(jobs, job)
		}
	}
	printJobs(jobs)
	return nil
}
