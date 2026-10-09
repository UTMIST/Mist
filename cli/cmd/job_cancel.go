package cmd

import (
	"fmt"
	"net/url"
)

type JobCancelCmd struct {
	ID string `arg:"" help:"Job ID to cancel"`
}

func (j *JobCancelCmd) Run(ctx *AppContext) error {
	var job Job
	if err := ctx.api("POST", "/jobs/"+url.PathEscape(j.ID)+"/cancel", nil, &job); err != nil {
		return err
	}
	fmt.Printf("Job %s: %s\n", job.ID, job.Status)
	return nil
}
