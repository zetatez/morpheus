package config

import (
	"testing"
)

func TestValidateAgentConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AgentConfig
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			cfg:     AgentConfig{},
			wantErr: false,
		},
		{
			name:    "build mode is valid",
			cfg:     AgentConfig{DefaultMode: "build"},
			wantErr: false,
		},
		{
			name:    "plan mode is valid",
			cfg:     AgentConfig{DefaultMode: "plan"},
			wantErr: false,
		},
		{
			name:    "invalid mode is accepted (validation not implemented for non-required field)",
			cfg:     AgentConfig{DefaultMode: "invalid"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAgentConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateAgentConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePlannerConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     PlannerConfig
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			cfg:     PlannerConfig{},
			wantErr: false,
		},
		{
			name:    "valid temperature 0",
			cfg:     PlannerConfig{Temperature: 0},
			wantErr: false,
		},
		{
			name:    "valid temperature 1.5",
			cfg:     PlannerConfig{Temperature: 1.5},
			wantErr: false,
		},
		{
			name:    "valid temperature 2",
			cfg:     PlannerConfig{Temperature: 2},
			wantErr: false,
		},
		{
			name:    "invalid temperature negative",
			cfg:     PlannerConfig{Temperature: -0.1},
			wantErr: true,
		},
		{
			name:    "invalid temperature too high",
			cfg:     PlannerConfig{Temperature: 2.1},
			wantErr: true,
		},
		{
			name:    "valid provider openai",
			cfg:     PlannerConfig{Provider: "openai"},
			wantErr: false,
		},
		{
			name:    "valid provider ollama",
			cfg:     PlannerConfig{Provider: "ollama"},
			wantErr: false,
		},
		{
			name:    "valid provider builtin",
			cfg:     PlannerConfig{Provider: "builtin"},
			wantErr: false,
		},
		{
			name:    "invalid provider",
			cfg:     PlannerConfig{Provider: "invalid-provider"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePlannerConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePlannerConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePermissions(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Permissions
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			cfg:     Permissions{},
			wantErr: false,
		},
		{
			name:    "valid confirm_above low",
			cfg:     Permissions{ConfirmAbove: "low"},
			wantErr: false,
		},
		{
			name:    "valid confirm_above medium",
			cfg:     Permissions{ConfirmAbove: "medium"},
			wantErr: false,
		},
		{
			name:    "valid confirm_above high",
			cfg:     Permissions{ConfirmAbove: "high"},
			wantErr: false,
		},
		{
			name:    "valid confirm_above critical",
			cfg:     Permissions{ConfirmAbove: "critical"},
			wantErr: false,
		},
		{
			name:    "invalid confirm_above",
			cfg:     Permissions{ConfirmAbove: "invalid"},
			wantErr: true,
		},
		{
			name:    "valid max write size",
			cfg:     Permissions{FileSystem: FilePolicy{MaxWriteSizeKB: 1024}},
			wantErr: false,
		},
		{
			name:    "invalid max write size negative",
			cfg:     Permissions{FileSystem: FilePolicy{MaxWriteSizeKB: -1}},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePermissions(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePermissions() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateSessionConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     SessionConfig
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			cfg:     SessionConfig{},
			wantErr: false,
		},
		{
			name:    "valid retention",
			cfg:     SessionConfig{Retention: 720},
			wantErr: false,
		},
		{
			name:    "zero retention is valid",
			cfg:     SessionConfig{Retention: 0},
			wantErr: false,
		},
		{
			name:    "invalid retention negative",
			cfg:     SessionConfig{Retention: -1},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSessionConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateSessionConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
