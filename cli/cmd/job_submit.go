package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type JobSubmitCmd struct {
	Script  string `arg:"" help:"Path to the job script file to submit"`
	Compute string `help:"Type of compute required for the job: AMD|TT|CPU" default:"AMD"`
}

func (j *JobSubmitCmd) Run() error {
	// mist job submit <script> <compute_type>

	// TODO: ADD AUTH CHECK

	// TODO: MAKE THIS GLOBAL OR LOADED FROM ENV?
	// Validate compute type
	validComputeTypes := map[string]bool{
		"AMD": true,
		"TT":  true,
		"CPU": true,
	}

	// Validate script file exists
	// if _, err := os.Stat(j.Script); os.IsNotExist(err) {
	// 	fmt.Println("Error: Script file does not exist at path:", j.Script)
	// 	return nil
	// }

	if !validComputeTypes[strings.ToUpper(j.Compute)] {
		fmt.Println("Error: Invalid compute type. Valid options are: AMD, TT, CPU")
		return nil
	}

	// Maybe turn this into some type of wrapper function later?
	fmt.Print("Are you sure? (y/N): ")

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "y" || input == "yes" {
		fmt.Println("Confirmed, proceeding...")

		// CONFIRMED LOGIC

		fmt.Println("Submitting job with script:", j.Script)
		fmt.Println("Requested GPU type:", j.Compute)
		println("Job submitted successfully with ID: job_12345")

		return nil

	} else {
		fmt.Println("Cancelled.")
		return nil
	}

}
