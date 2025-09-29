package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)


type JobCancelCmd struct {
	Script  string `arg:"" help:"Path to the job you want to cancel"`
	Compute string `help:"Type of compute required for the job: AMD|TT|CPU" default:"AMD"`

}

func (c* JobCancelCmd) Run() error {
	
	// Maybe no need to validate the compute type? 
	fmt.Printf("Are you sure you want to cancel %s? (Y/N): \n", c.Script)

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "y" || input == "yes"{
		fmt.Println("Confirmed, proceeding job cancellation....")

		// Confirmed job cancellation logic 

		fmt.Println("Cancelling job with script:", c.Script)
		fmt.Println("Requested GPU type:", c.Compute)
		println("Job cancelled successfully with ID: job_12345")
		return nil
	} else if input == "n" || input == "no"{
		fmt.Println("Cancelled.")
		return nil
	} else{
		fmt.Println("Invalid response.")
		return nil
	}

	return nil 

}