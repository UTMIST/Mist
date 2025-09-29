package cmd

import "fmt"


// Config flgas 
type ConfigCmd struct{
	DefaultCluster string `help:"Set the default compute cluster." optional: ""`
	Show bool `help:"Show current configuration."`

}

func (h *ConfigCmd) Run() error {
	// Some dummy config; Call API or something 
	defaultConfig := map[string]string{
		"defaultCluster": "AMD-cluster-1",
		"region": "us-east",
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
