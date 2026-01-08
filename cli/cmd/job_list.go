package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

type ListCmd struct {
	All bool `help:"List all jobs, including completed and failed ones." short:"a"`
}

type Job struct {
	ID        string
	Name      string
	Status    string
	GPUType   string
	CreatedAt time.Time
}

func (l *ListCmd) Run(ctx *AppContext) error {
	// Mock data - pull from API in real implementation
	jobs := []Job{
		{
			ID:        "ID:1",
			Name:      "docker_container_name_1",
			Status:    "Running",
			GPUType:   "AMD",
			CreatedAt: time.Now(),
		},
		{
			ID:        "ID:2",
			Name:      "docker_container_name_2",
			Status:    "Enqueued",
			GPUType:   "TT",
			CreatedAt: time.Now().Add(-time.Hour * 24),
		},
		{
			ID:        "ID:3",
			Name:      "docker_container_name_3",
			Status:    "Running",
			GPUType:   "TT",
			CreatedAt: time.Now().Add(-time.Hour * 24),
		},
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Job ID\tName\tStatus\tGPU Type\tCreated At")
	fmt.Fprintln(w, "--------------------------------------------------------------")

	for _, job := range jobs {
		// Maybe filter based on running?
		fmt.Fprintf(
			w,
			"%s\t%s\t%s\t%s\t%s\n",
			job.ID,
			job.Name,
			job.Status,
			job.GPUType,
			job.CreatedAt.Format(time.RFC1123),
		)
	}

	w.Flush()

	return nil
}
