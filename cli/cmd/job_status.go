package cmd

import (
	"fmt"
	"net/url"
)

type JobStatusCmd struct {
	ID string `arg:"" help:"Job ID"`
}

func (j *JobStatusCmd) Run(ctx *AppContext) error {
	var job Job
	if err := ctx.api("GET", "/jobs/"+url.PathEscape(j.ID), nil, &job); err != nil {
		return err
	}
	printJobs([]Job{job})
	if job.ExitCode != nil {
		fmt.Println("Exit code:", *job.ExitCode)
	}
	if job.Message != "" {
		fmt.Println(job.Message)
	}
	return nil
}
