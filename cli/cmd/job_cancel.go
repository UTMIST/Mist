package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

type JobCancelCmd struct {
	ID string `arg:"" help:"ID of job you want to cancel"`
}

func (c *JobCancelCmd) Run(ctx *AppContext) error {
	// Same Mock data from job list.
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

	// Check if job exists
	if !jobExists(jobs, c.ID) {
		fmt.Printf("%s does not exist in your jobs.\n", c.ID)
		fmt.Printf("Use the command \"job list\" for your list of jobs.")
		return nil
	}

	fmt.Printf("Are you sure you want to cancel %s? (y/n): \n", c.ID)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "y" || input == "yes" {
		fmt.Println("Confirmed, proceeding job cancellation....")

		// Confirmed job cancellation logic

		fmt.Println("Cancelling job with ID:", c.ID)
		fmt.Printf("Job cancelled successfully with ID: %s\n", c.ID)
		return nil
	} else if input == "n" || input == "no" {
		fmt.Println("Cancelled.")
		return nil
	} else {
		fmt.Println("Invalid response.")
		return nil
	}

	return nil

}
