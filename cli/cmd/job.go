package cmd

type JobCmd struct {
	Submit JobSubmitCmd `cmd:"" help:"Submit a new job"`
	Cancel JobCancelCmd `cmd:"" help:"Cancel an existing job"`
	// Delete JobDeleteCmd `cmd: "" help: "Delete an existing job"`
	Status JobStatusCmd `cmd:"" help:"Check the status of a job"`
	// Cancel   CancelCmd   `cmd:"" help:"Cancel a running job"`
	List ListCmd `cmd:"" help:"List all jobs" default:1`
}

func (j *JobCmd) Run() error {
	// Possible fallback if no subcommand is provided
	// fmt.Println("(job root) – try 'mist job submit|status|list|cancel' or mist help")
	return nil
}
