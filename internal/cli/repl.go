package cli

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zetatez/morpheus/internal/app"
	"github.com/zetatez/morpheus/internal/config"
)

type replSettings struct {
	apiURL       string
	frontendPath string
	session      string
	prompt       string
}

func newReplCommand(opts *Options) *cobra.Command {
	settings := replSettings{
		frontendPath: "cli",
	}
	cmd := &cobra.Command{
		Use:   "repl",
		Short: "Start the BruteCode REPL (REST backend + TS frontend)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRepl(cmd.Context(), opts, settings)
		},
	}
	cmd.Flags().StringVar(&settings.apiURL, "url", "", "API base URL (skip local server)")
	cmd.Flags().StringVar(&settings.frontendPath, "frontend", settings.frontendPath, "Path to TypeScript CLI frontend")
	cmd.Flags().StringVar(&settings.session, "session", "", "Session ID")
	cmd.Flags().StringVar(&settings.prompt, "prompt", "", "Initial prompt to submit")
	return cmd
}

func runRepl(ctx context.Context, opts *Options, settings replSettings) error {
	cfg, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	applyStoredModelConfig(&cfg)

	apiURL := normalizeBaseURL(settings.apiURL)
	if apiURL == "" {
		apiURL = apiURLFromListen(cfg.Server.Listen)
	}

	if settings.apiURL != "" {
		return runFrontend(ctx, settings, apiURL)
	}
	if ok, existingURL := detectExistingServer(ctx, apiURL); ok {
		return runFrontend(ctx, settings, existingURL)
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

	if err := waitForServer(ctx, apiURL); err != nil {
		select {
		case serveErr := <-errCh:
			if serveErr != nil {
				if reused, existingURL := recoverFromBindConflict(ctx, apiURL, serveErr); reused {
					cancel()
					return runFrontend(ctx, settings, existingURL)
				}
				cancel()
				return serveErr
			}
		default:
		}
		cancel()
		return err
	}

	frontendErr := runFrontend(ctx, settings, apiURL)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-time.After(2 * time.Second):
	}

	return frontendErr
}

func detectExistingServer(ctx context.Context, apiURL string) (bool, string) {
	healthURL := strings.TrimRight(apiURL, "/") + "/health"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
	if err != nil {
		return false, ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, ""
	}
	return true, apiURL
}

func recoverFromBindConflict(ctx context.Context, apiURL string, serveErr error) (bool, string) {
	if serveErr == nil {
		return false, ""
	}
	if !strings.Contains(strings.ToLower(serveErr.Error()), "address already in use") {
		return false, ""
	}
	if ok, existingURL := detectExistingServer(ctx, apiURL); ok {
		return true, existingURL
	}
	return false, ""
}

func runFrontend(ctx context.Context, settings replSettings, apiURL string) error {
	frontendPath := filepath.Clean(settings.frontendPath)
	info, err := os.Stat(frontendPath)
	if err != nil {
		return fmt.Errorf("frontend path not found: %s", frontendPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("frontend path is not a directory: %s", frontendPath)
	}

	bunPath, err := exec.LookPath("bun")
	if err != nil {
		return fmt.Errorf("bun not found; install bun to run the TS CLI frontend")
	}

	args := []string{"--preload", "@opentui/solid/preload", "src/index.tsx", "--url", apiURL}
	if settings.session != "" {
		args = append(args, "--session", settings.session)
	}
	if settings.prompt != "" {
		args = append(args, "--prompt", settings.prompt)
	}

	cmd := exec.CommandContext(ctx, bunPath, args...)
	cmd.Dir = frontendPath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd.Run()
}

func waitForServer(ctx context.Context, apiURL string) error {
	healthURL := strings.TrimRight(apiURL, "/") + "/health"
	deadline := time.After(10 * time.Second)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if resp != nil {
			_ = resp.Body.Close()
		}

		select {
		case <-deadline:
			return fmt.Errorf("server not ready at %s", healthURL)
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func apiURLFromListen(listen string) string {
	if strings.TrimSpace(listen) == "" {
		return "http://localhost:8080"
	}
	return normalizeBaseURL(listen)
}

func normalizeBaseURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return strings.TrimRight(raw, "/")
	}
	if strings.HasPrefix(raw, ":") {
		return "http://localhost" + raw
	}
	host, port, err := net.SplitHostPort(raw)
	if err == nil {
		if host == "" || host == "0.0.0.0" {
			host = "localhost"
		}
		return "http://" + host + ":" + port
	}
	return "http://" + strings.TrimRight(raw, "/")
}
