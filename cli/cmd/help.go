package cmd

import "fmt"

type HelpCmd struct{}

func (h *HelpCmd) Run() error {
	fmt.Println("MIST CLI Help")
	fmt.Println("Usage: mist [command] [options]")
	fmt.Println("Commands:")
	fmt.Println("  auth     Authentication commands")
	fmt.Println("  job      Job management commands")
	fmt.Println("  config   Configuration commands")
	fmt.Println("  help     Show help information")
	return nil
}
