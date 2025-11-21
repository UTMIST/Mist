package cmd

import (
	"github.com/alecthomas/kong"
)

type CLI struct {
	// Define your CLI structure here: Top Level Commands 
	Auth AuthCmd `cmd:"" help:"Authentication commands"`
	Job  JobCmd  `cmd:"" help:"Job management commands"`
	// Config ConfigCmd `cmd:"" help:"Configuration commands"`
	Help HelpCmd `cmd:"" help:"Show help information"`
	Config ConfigCmd `cmd:"" help: "Display Cluster Configuration"`
	Supervisor SupervisorCmd `cmd:"" help:"Supervisor commands"`
}

func Main() {
	var cli CLI
	// Read command-line arguments 
	ctx := kong.Parse(&cli,
		kong.Name("mist"),
		kong.Description("MIST CLI - Manage your MIST jobs and configurations"),
		kong.UsageOnError(),
	)

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
	// fmt.Println("Command executed successfully")
}
