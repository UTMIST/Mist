package cmd

type JobCmd struct {
	Submit JobSubmitCmd `cmd:"" help:"Submit a new job"`
	Cancel JobCancelCmd `cmd:"" help:"Cancel an existing job"`
	Status JobStatusCmd `cmd:"" help:"Check the status of a job"`
	Logs   JobLogsCmd   `cmd:"" help:"Read workload logs"`
	List   ListCmd      `cmd:"" help:"List jobs"`
}
