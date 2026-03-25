package cli

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/zetatez/morpheus/internal/app/effect_runtime"
	"github.com/zetatez/morpheus/internal/config"
)

func newServeCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the BruteCode HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runServer(ctx, opts)
		},
	}
	return cmd
}

func runServer(ctx context.Context, opts *Options) error {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	applyStoredModelConfig(&cfg)
	runtime, err := effect_runtime.NewEffectRuntime(cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()
	if err := runtime.Initialize(ctx); err != nil {
		return err
	}
	if err := runtime.StartServer(ctx); err != nil {
		return err
	}
	return nil
}
