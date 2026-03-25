package config

import (
	"fmt"
	"regexp"
)

type ValidationRule struct {
	Field    string
	Min      *int
	Max      *int
	Pattern  *regexp.Regexp
	Required bool
}

func ValidateAgentConfig(cfg AgentConfig) error {
	rules := []ValidationRule{
		{Field: "DefaultMode", Required: false},
	}

	for _, rule := range rules {
		if rule.Required {
			switch rule.Field {
			case "DefaultMode":
				if cfg.DefaultMode != "" && cfg.DefaultMode != "build" && cfg.DefaultMode != "plan" {
					return fmt.Errorf("invalid agent.default_mode: must be 'build' or 'plan'")
				}
			}
		}
	}

	return nil
}

func ValidatePlannerConfig(cfg PlannerConfig) error {
	if cfg.Temperature < 0 || cfg.Temperature > 2 {
		return fmt.Errorf("invalid planner.temperature: must be between 0 and 2")
	}

	validProviders := map[string]struct{}{
		"openai": {}, "deepseek": {}, "minimax": {}, "glm": {}, "gemini": {},
		"anthropic": {}, "openrouter": {}, "azure": {}, "groq": {}, "mistral": {},
		"cohere": {}, "togetherai": {}, "perplexity": {}, "ollama": {},
		"openai-compatible": {}, "builtin": {}, "keyword": {},
	}
	if _, ok := validProviders[cfg.Provider]; !ok && cfg.Provider != "" {
		return fmt.Errorf("invalid planner.provider: %q is not a supported provider", cfg.Provider)
	}

	return nil
}

func ValidatePermissions(cfg Permissions) error {
	validConfirmAbove := map[string]struct{}{
		"low": {}, "medium": {}, "high": {}, "critical": {},
	}
	if _, ok := validConfirmAbove[cfg.ConfirmAbove]; !ok && cfg.ConfirmAbove != "" {
		return fmt.Errorf("invalid permissions.confirm_above: must be 'low', 'medium', 'high', or 'critical'")
	}

	if cfg.FileSystem.MaxWriteSizeKB < 0 {
		return fmt.Errorf("invalid permissions.file_system.max_write_size_kb: must be non-negative")
	}

	return nil
}

func ValidateSessionConfig(cfg SessionConfig) error {
	if cfg.Retention < 0 {
		return fmt.Errorf("invalid session.retention: must be non-negative")
	}

	return nil
}

func (c Config) ValidateAll() error {
	if err := ValidatePlannerConfig(c.Planner); err != nil {
		return err
	}
	if err := ValidateSessionConfig(c.Session); err != nil {
		return err
	}
	if err := ValidatePermissions(c.Permissions); err != nil {
		return err
	}
	if err := ValidateAgentConfig(c.Agent); err != nil {
		return err
	}

	return nil
}
