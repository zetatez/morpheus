package cli

import (
	"fmt"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"
)

func newAuthCommand(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth <provider>",
		Short: "Show how to get API key for a provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return showAuthHelp(cmd, args)
		},
	}
	return cmd
}

func showAuthHelp(cmd *cobra.Command, args []string) error {
	if len(args) < 1 {
		return cmd.Help()
	}

	provider := args[0]

	urls := map[string]string{
		"openai":    "https://platform.openai.com/settings/organization/api-keys",
		"anthropic": "https://console.anthropic.com/settings/keys",
		"google":    "https://aistudio.google.com/app/apikey",
		"deepseek":  "https://platform.deepseek.com/api-keys",
		"minimax":   "https://platform.minimax.io/account/settings",
	}

	envVars := map[string]string{
		"openai":    "OPENAI_API_KEY",
		"anthropic": "ANTHROPIC_API_KEY",
		"google":    "GOOGLE_API_KEY",
		"deepseek":  "DEEPSEEK_API_KEY",
		"minimax":   "MINIMAX_API_KEY",
	}

	url, hasURL := urls[provider]
	envVar := envVars[provider]

	if !hasURL {
		return fmt.Errorf("unknown provider: %s", provider)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "🔐 Getting API key for %s\n\n", provider)

	if err := openURL(url); err == nil {
		fmt.Fprintf(cmd.OutOrStdout(), "Browser opened. If not, visit: %s\n\n", url)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Please visit: %s\n\n", url)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "📝 After creating the API key, configure Morpheus:\n\n")
	fmt.Fprintf(cmd.OutOrStdout(), "Option 1 - Environment variable:\n")
	fmt.Fprintf(cmd.OutOrStdout(), "   export %s=your-api-key\n\n", envVar)

	fmt.Fprintf(cmd.OutOrStdout(), "Option 2 - Config file (morph.yaml):\n")
	fmt.Fprintf(cmd.OutOrStdout(), "   planner:\n")
	fmt.Fprintf(cmd.OutOrStdout(), "     provider: %s\n", provider)
	fmt.Fprintf(cmd.OutOrStdout(), "     api_key: your-api-key\n\n")

	fmt.Fprintf(cmd.OutOrStdout(), "Supported: openai, anthropic, google, deepseek, minimax\n")

	return nil
}

func openURL(url string) error {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	case "windows":
		err = exec.Command("cmd", "/c", "start", url).Start()
	default:
		return fmt.Errorf("unsupported platform")
	}
	return err
}
