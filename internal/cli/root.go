package cli

import (
	"github.com/spf13/cobra"
)

// NewRootCommand creates the top-level morph CLI command tree.
func NewRootCommand() *cobra.Command {
	opts := defaultOptions()
	cmd := &cobra.Command{
		Use:   "morpheus",
		Short: "Morpheus agent CLI",
		Long:  "Morpheus is a self-steering CLI agent for developer workflows.",
	}
	cmd.PersistentFlags().StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "Path to morph config file")
	cmd.AddCommand(newReplCommand(opts))
	cmd.AddCommand(newServeCommand(opts))
	cmd.AddCommand(newAuthCommand(opts))
	return cmd
}

// Execute runs the CLI.
func Execute() error {
	return NewRootCommand().Execute()
}
