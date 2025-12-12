package cmd

import (
	"fmt"
	// "os"
	// "text/tabwriter"
	// "time"
	"mist/cli/api"
)

type JobStatusCmd struct {
	ID string `arg:"" help:"The ID of the job to check the status for"`
}

func (j *JobStatusCmd) Run() error {
	// Mock data implementatino 
	// jobs := []Job{{
	// 	ID:        "ID:1",
	// 	Name:      "docker_container_name_1",
	// 	Status:    "Running",
	// 	GPUType:   "AMD",
	// 	CreatedAt: time.Now(),
	// }}

	// job, err := findJobByID(jobs, j.ID)
	// if err != nil {
	// 	fmt.Printf("%s does not exist in your jobs.\n", j.ID)
	// 	fmt.Printf("Use the command \"job list\" for your list of jobs.")
	// 	return nil
	// }

	// println("Checking status for job ID:", j.ID)
	// w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	// fmt.Fprintln(w, "Job ID\tName\tStatus\tGPU Type\tCreated At")
	// fmt.Fprintln(w, "--------------------------------------------------------------")

	// fmt.Fprintf(
	// 	w,
	// 	"%s\t%s\t%s\t%s\t%s\n",
	// 	job.ID,
	// 	job.Name,
	// 	job.Status,
	// 	job.GPUType,
	// 	job.CreatedAt.Format(time.RFC1123),
	// )
	// w.Flush()
	// return nil	

	client := cli.NewAPIClient("http://localhost:3000")

	fmt.Println("Checking status for job ID: ", j.ID)
	status, err := client.GetJobStatus(j.ID)

	if err != nil {
		fmt.Println("Error fetching job status:", err)
		return nil 
	}

    fmt.Println("Server response:")
    fmt.Println(status)

    return nil	
}
