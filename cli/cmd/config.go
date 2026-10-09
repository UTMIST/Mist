package cmd

import (
	"os"
	"path/filepath"
)

func defaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "mist", "config.json")
	}
	return filepath.Join(dir, "mist", "config.json")
}
