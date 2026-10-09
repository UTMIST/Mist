package cmd

import "fmt"

type TeamCmd struct {
	List TeamListCmd `cmd:"" help:"List your available team workspaces"`
	Use  TeamUseCmd  `cmd:"" help:"Save a team workspace selection; use legacy for historical account jobs"`
}
type TeamListCmd struct{}
type TeamUseCmd struct {
	ID string `arg:"" help:"Team ID, or legacy"`
}
type teamSummary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Disabled bool   `json:"disabled"`
	Members  []struct {
		ID string `json:"id"`
	} `json:"members"`
}

func (j *TeamListCmd) Run(ctx *AppContext) error {
	var response struct {
		Teams []teamSummary `json:"teams"`
	}
	if err := ctx.api("GET", "/teams", nil, &response); err != nil {
		return err
	}
	fmt.Printf("%-24s  %-28s  %s\n", "TEAM ID", "NAME", "STATE")
	for _, t := range response.Teams {
		state := "Active"
		if t.Disabled {
			state = "Disabled"
		}
		if ctx.teamID() == t.ID {
			state += " · Selected"
		}
		fmt.Printf("%-24s  %-28s  %s\n", t.ID, t.Name, state)
	}
	return nil
}
func (j *TeamUseCmd) Run(ctx *AppContext) error {
	if j.ID != "legacy" {
		var response struct {
			Teams []teamSummary `json:"teams"`
		}
		if err := ctx.api("GET", "/teams", nil, &response); err != nil {
			return err
		}
		var session struct {
			User *struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		if err := ctx.api("GET", "/session", nil, &session); err != nil {
			return err
		}
		found := false
		for _, t := range response.Teams {
			if t.ID == j.ID && !t.Disabled && session.User != nil {
				for _, m := range t.Members {
					if m.ID == session.User.ID {
						found = true
					}
				}
			}
		}
		if !found {
			return fmt.Errorf("select an active team you belong to; run team list")
		}
	}
	if ctx.Config == nil {
		ctx.Config = &Config{}
	}
	ctx.Config.TeamID = j.ID
	if j.ID == "legacy" {
		ctx.Config.TeamID = ""
	}
	if err := ctx.saveConfig(); err != nil {
		return err
	}
	fmt.Println("Workspace selected:", j.ID)
	return nil
}
