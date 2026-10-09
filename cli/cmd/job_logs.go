package cmd

import (
	"fmt"
	"net/url"
)

type JobLogsCmd struct {
	ID string `arg:"" help:"Job ID"`
}

func (j *JobLogsCmd) Run(ctx *AppContext) error {
	var response struct {
		Logs string `json:"logs"`
	}
	if err := ctx.api("GET", "/jobs/"+url.PathEscape(j.ID)+"/logs", nil, &response); err != nil {
		return err
	}
	fmt.Print(response.Logs)
	return nil
}
