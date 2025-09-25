package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

type JobStatusCmd struct {
	JobID string `arg:"" help:"The ID of the job to check the status for"`
}

func (j *JobStatusCmd) Run() error {

	// Get job by id
	// job, err := api.GetJob(j.JobID)
	// if err != nil {
	// 	fmt.Println("Error fetching job/Job id not found", err)

	job := Job{
		ID:        j.JobID,
		Name:      "docker_container_name_1",
		Status:    "Running",
		GPUType:   "AMD",
		CreatedAt: time.Now(),
	}

	println("Checking status for job ID:", j.JobID)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Job ID\tName\tStatus\tGPU Type\tCreated At")
	fmt.Fprintln(w, "--------------------------------------------------------------")

	fmt.Fprintf(
		w,
		"%s\t%s\t%s\t%s\t%s\n",
		job.ID,
		job.Name,
		job.Status,
		job.GPUType,
		job.CreatedAt.Format(time.RFC1123),
	)
	w.Flush()
	return nil
}
