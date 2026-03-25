package cli

import (
	"os"
	"path/filepath"
)

// Options holds global CLI flags.
type Options struct {
	ConfigPath string
}

func defaultOptions() *Options {
	configPath := "config.yaml"

	// Check for config in default location
	home, err := os.UserHomeDir()
	if err == nil {
		defaultPath := filepath.Join(home, ".config", "morph", "config.yaml")
		if _, err := os.Stat(defaultPath); err == nil {
			configPath = defaultPath
		}
	}

	return &Options{ConfigPath: configPath}
}
