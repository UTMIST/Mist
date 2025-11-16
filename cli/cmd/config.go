package cmd

import "fmt"

// Config flags
type ConfigCmd struct {
	DefaultCluster string `help:"Set the default compute cluster." optional: ""`
	Show           bool   `help:"Show current configuration."`
}

func (h *ConfigCmd) Run(ctx *AppContext) error {
	// Some dummy config; Call API or something
	defaultConfig := map[string]string{
		"defaultCluster": "AMD-cluster-1",
		"region":         "us-east",
	}

	if h.Show && h.DefaultCluster != "" {
		fmt.Printf("Cannot use --show and --default-cluster together")
		return nil
	}

	if h.DefaultCluster != "" {
		// This is not actually set.
		fmt.Printf("Setting default cluster to: %s\n", h.DefaultCluster)
		return nil
	}

	if h.Show {
		fmt.Println("Current configuration: ")
		for key, value := range defaultConfig {
			fmt.Printf(" %s: %s \n", key, value)
		}
		return nil
	}

	fmt.Println("No config action specified. Use --help for options.")

	return nil
}
