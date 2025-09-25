package cmd

import (
	"github.com/alecthomas/kong"
)

type CLI struct {
	// Define your CLI structure here
	Auth AuthCmd `cmd:"" help:"Authentication commands"`
	Job  JobCmd  `cmd:"" help:"Job management commands"`
	// Config ConfigCmd `cmd:"" help:"Configuration commands"`
	Help HelpCmd `cmd:"" help:"Show help information"`
}

func Main() {
	var cli CLI
	ctx := kong.Parse(&cli,
		kong.Name("mist"),
		kong.Description("MIST CLI - Manage your MIST jobs and configurations"),
		kong.UsageOnError(),
	)

	err := ctx.Run()
	ctx.FatalIfErrorf(err)
	// fmt.Println("Command executed successfully")
}
