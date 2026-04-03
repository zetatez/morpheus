package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	WorkspaceRoot string              `mapstructure:"workspace_root"`
	Morpheus      MorpheusConfig      `mapstructure:"morpheus"`
	Logging       LoggingConfig       `mapstructure:"logging"`
	Planner       PlannerConfig       `mapstructure:"planner"`
	Server        ServerConfig        `mapstructure:"server"`
	Session       SessionConfig       `mapstructure:"session"`
	MCP           MCPConfig           `mapstructure:"mcp"`
	KnowledgeBase KnowledgeBaseConfig `mapstructure:"knowledge_base"`
	Permissions   Permissions         `mapstructure:"permissions"`
	Agent         AgentConfig         `mapstructure:"subagents"`
}

type MorpheusConfig struct {
	ContextFile string `mapstructure:"context_file"`
}

type LoggingConfig struct {
	Level string `mapstructure:"level"`
	File  string `mapstructure:"file"`
}

type PlannerConfig struct {
	Provider    string  `mapstructure:"provider"`
	Model       string  `mapstructure:"model"`
	Temperature float64 `mapstructure:"temperature"`
	APIKey      string  `mapstructure:"api_key"`
	Endpoint    string  `mapstructure:"endpoint"`
}

type ServerConfig struct {
	Listen string             `mapstructure:"listen"`
	Limits ServerLimitConfig  `mapstructure:"limits"`
	Remote ServerRemoteConfig `mapstructure:"remote"`
}

type ServerRemoteConfig struct {
	Enabled     bool   `mapstructure:"enabled"`
	BearerToken string `mapstructure:"bearer_token"`
}

type ServerLimitConfig struct {
	Enabled          bool    `mapstructure:"enabled"`
	MaxCPUPercent    float64 `mapstructure:"max_cpu_percent"`
	MaxMemoryPercent float64 `mapstructure:"max_memory_percent"`
	SampleIntervalMs int     `mapstructure:"sample_interval_ms"`
}

type SessionConfig struct {
	Path       string        `mapstructure:"path"`
	SQLitePath string        `mapstructure:"sqlite_path"`
	Retention  time.Duration `mapstructure:"retention"`
}

type KnowledgeBaseConfig struct {
	Path string `mapstructure:"path"`
}

type MCPConfig struct {
	Servers []MCPServerConfig `mapstructure:"servers"`
}

type MCPServerConfig struct {
	Name       string `mapstructure:"name"`
	Command    string `mapstructure:"command"`
	Transport  string `mapstructure:"transport"`
	URL        string `mapstructure:"url"`
	SSEURL     string `mapstructure:"sse_url"`
	AuthToken  string `mapstructure:"auth_token"`
	AuthHeader string `mapstructure:"auth_header"`
}

type Permissions struct {
	ConfirmAbove          string              `mapstructure:"confirm_above"`
	ConfirmProtectedPaths []string            `mapstructure:"confirm_protected_paths"`
	RiskFactors           map[string][]string `mapstructure:"risk_factors"`
	FileSystem            FilePolicy          `mapstructure:"file_system"`
	AutoApprove           bool                `mapstructure:"auto_approve"`
	AllowAsk              bool                `mapstructure:"allow_ask"`
}

type AgentConfig struct {
	DefaultMode        string            `mapstructure:"default_mode"`
	Agents             []AgentDefinition `mapstructure:"agents"`
	MaxConcurrentTasks int               `mapstructure:"max_concurrent_tasks"`
	MaxAgentSteps      int               `mapstructure:"max_agent_steps"`
}

type AgentDefinition struct {
	Name         string   `mapstructure:"name"`
	Description  string   `mapstructure:"description"`
	Instructions string   `mapstructure:"instructions"`
	Tools        []string `mapstructure:"tools"`
	Enabled      bool     `mapstructure:"enabled"`
}

type FilePolicy struct {
	MaxWriteSizeKB int `mapstructure:"max_write_size_kb"`
}

func Load(path string) (Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshal config: %w", err)
	}

	cfg.expandPaths()

	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func (c *Config) expandPaths() {
	c.WorkspaceRoot = expandPath(c.WorkspaceRoot)
	c.Morpheus.ContextFile = expandPath(c.Morpheus.ContextFile)
	c.Logging.File = expandPath(c.Logging.File)
	c.Session.Path = expandPath(c.Session.Path)
	c.Session.SQLitePath = expandPath(c.Session.SQLitePath)
	c.KnowledgeBase.Path = expandPath(c.KnowledgeBase.Path)
	c.Planner.Endpoint = expandEnvValue(c.Planner.Endpoint)
	c.Planner.APIKey = expandEnvValue(c.Planner.APIKey)
	c.Planner.Provider = strings.ToLower(c.Planner.Provider)
	c.Server.Remote.BearerToken = expandEnvValue(c.Server.Remote.BearerToken)

	c.loadAPIKeyFromEnv()
}

func (c *Config) loadAPIKeyFromEnv() {
	if c.Planner.APIKey != "" {
		return
	}

	envKey := "BRUTECODE_API_KEY"
	if c.Planner.Provider != "" && c.Planner.Provider != "builtin" {
		envKey = strings.ToUpper(c.Planner.Provider) + "_API_KEY"
	}
	if key := os.Getenv(envKey); key != "" {
		c.Planner.APIKey = key
		return
	}

	providerEnvMap := map[string]string{
		"openai":            "OPENAI_API_KEY",
		"deepseek":          "DEEPSEEK_API_KEY",
		"minimax":           "MINIMAX_API_KEY",
		"minmax":            "MINIMAX_API_KEY",
		"glm":               "GLM_API_KEY",
		"gemini":            "GEMINI_API_KEY",
		"anthropic":         "ANTHROPIC_API_KEY",
		"openrouter":        "OPENROUTER_API_KEY",
		"azure":             "AZURE_API_KEY",
		"groq":              "GROQ_API_KEY",
		"mistral":           "MISTRAL_API_KEY",
		"cohere":            "COHERE_API_KEY",
		"togetherai":        "TOGETHERAI_API_KEY",
		"perplexity":        "PERPLEXITY_API_KEY",
		"ollama":            "OLLAMA_API_KEY",
		"lmstudio":          "LMSTUDIO_API_KEY",
		"openai-compatible": "OPENAI_COMPATIBLE_API_KEY",
	}
	if envName, ok := providerEnvMap[c.Planner.Provider]; ok {
		if key := os.Getenv(envName); key != "" {
			c.Planner.APIKey = key
		}
	}
}

func expandPath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return trimmed
	}
	if trimmed == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return trimmed
	}
	if strings.HasPrefix(trimmed, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, trimmed[2:])
		}
		return trimmed
	}
	if !filepath.IsAbs(trimmed) {
		if absPath, err := filepath.Abs(trimmed); err == nil {
			return absPath
		}
		return trimmed
	}
	return trimmed
}

func expandEnvValue(value string) string {
	if value == "" {
		return value
	}
	return os.ExpandEnv(value)
}

func setDefaults(v *viper.Viper) {
	configHome := defaultConfigHome()
	dataHome := defaultDataHome()

	v.SetDefault("workspace_root", "./")
	v.SetDefault("morpheus.context_file", ".morpheus.md")
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", filepath.Join(dataHome, "logs", "morpheus.log"))
	v.SetDefault("server.listen", ":8080")
	v.SetDefault("server.remote.enabled", true)
	v.SetDefault("server.remote.bearer_token", "")
	v.SetDefault("server.limits.enabled", true)
	v.SetDefault("server.limits.max_cpu_percent", 85)
	v.SetDefault("server.limits.max_memory_percent", 85)
	v.SetDefault("server.limits.sample_interval_ms", 1000)
	v.SetDefault("planner.provider", "builtin")
	v.SetDefault("planner.model", "keyword")
	v.SetDefault("planner.temperature", 0.2)
	v.SetDefault("permissions.confirm_above", "high")
	v.SetDefault("session.path", filepath.Join(dataHome, "sessions"))
	v.SetDefault("session.sqlite_path", filepath.Join(dataHome, "sessions.db"))
	v.SetDefault("session.retention", "720h")
	v.SetDefault("knowledge_base.path", filepath.Join(configHome, "knowledge_base"))
	v.SetDefault("subagents.default_mode", "build")
	v.SetDefault("subagents.max_concurrent_tasks", 3)
}

func defaultConfigHome() string {
	if configHome, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configHome, "morpheus")
	}
	return filepath.Join(".", "config")
}

func defaultDataHome() string {
	if dataHome, err := os.UserHomeDir(); err == nil {
		return filepath.Join(dataHome, ".local", "share", "morpheus")
	}
	return filepath.Join(".", "data")
}

func (c Config) Validate() error {
	if c.WorkspaceRoot == "" {
		return fmt.Errorf("workspace_root is required")
	}
	if c.Planner.Provider == "" {
		return fmt.Errorf("planner.provider is required")
	}
	if c.Agent.DefaultMode != "" {
		mode := strings.ToLower(strings.TrimSpace(c.Agent.DefaultMode))
		if mode != "build" && mode != "plan" {
			return fmt.Errorf("subagents.default_mode must be build or plan")
		}
	}
	return nil
}
