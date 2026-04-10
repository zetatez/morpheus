package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/zetatez/morpheus/internal/app"
	"github.com/zetatez/morpheus/internal/config"
)

func newServeCommand(opts *Options) *cobra.Command {
	var apiURL string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Morpheus HTTP server",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			return runServe(ctx, opts, apiURL)
		},
	}
	cmd.Flags().StringVar(&apiURL, "url", "", "API base URL (skip local server)")
	return cmd
}

func runServe(ctx context.Context, opts *Options, apiURL string) error {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	applyStoredModelConfig(&cfg)

	if apiURL != "" {
		return waitForServer(ctx, apiURL)
	}

	apiURL = apiURLFromListen(cfg.Server.Listen)
	if ok, existingURL := detectExistingServer(ctx, apiURL); ok {
		fmt.Fprintf(os.Stderr, "server already running at %s\n", existingURL)
		return nil
	}

	runtime, err := app.NewRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()

	serverCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- runtime.StartServer(serverCtx)
	}()

	addr := cfg.Server.Listen
	if addr == "" {
		addr = ":8080"
	}
	fmt.Fprintf(os.Stderr, "listening on %s\n", normalizeBaseURL(addr))

	select {
	case <-ctx.Done():
		cancel()
		return nil
	case err := <-errCh:
		return err
	}
}
