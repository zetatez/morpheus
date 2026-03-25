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
	Logging       LoggingConfig       `mapstructure:"logging"`
	Planner       PlannerConfig       `mapstructure:"planner"`
	Server        ServerConfig        `mapstructure:"server"`
	Session       SessionConfig       `mapstructure:"session"`
	MCP           MCPConfig           `mapstructure:"mcp"`
	KnowledgeBase KnowledgeBaseConfig `mapstructure:"knowledge_base"`
	Permissions   Permissions         `mapstructure:"permissions"`
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
	Listen string `mapstructure:"listen"`
}

type SessionConfig struct {
	Path      string        `mapstructure:"path"`
	Retention time.Duration `mapstructure:"retention"`
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
	c.Logging.File = expandPath(c.Logging.File)
	c.Session.Path = expandPath(c.Session.Path)
	c.KnowledgeBase.Path = expandPath(c.KnowledgeBase.Path)
	c.Planner.Endpoint = expandPath(c.Planner.Endpoint)
	c.Planner.APIKey = expandEnvValue(c.Planner.APIKey)

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
		"openai":   "OPENAI_API_KEY",
		"deepseek": "DEEPSEEK_API_KEY",
		"minmax":   "MINMAX_API_KEY",
		"glm":      "GLM_API_KEY",
	}
	if envName, ok := providerEnvMap[c.Planner.Provider]; ok {
		if key := os.Getenv(envName); key != "" {
			c.Planner.APIKey = key
		}
	}
}

func expandPath(path string) string {
	if path == "" {
		return path
	}
	if !filepath.IsAbs(path) {
		if strings.HasPrefix(path, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				return filepath.Join(home, path[2:])
			}
		}
		if absPath, err := filepath.Abs(path); err == nil {
			return absPath
		}
		return path
	}
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
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
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.file", filepath.Join(dataHome, "logs", "morph.log"))
	v.SetDefault("server.listen", ":8080")
	v.SetDefault("planner.provider", "builtin")
	v.SetDefault("planner.model", "keyword")
	v.SetDefault("planner.temperature", 0.2)
	v.SetDefault("permissions.confirm_above", "high")
	v.SetDefault("session.path", filepath.Join(dataHome, "sessions"))
	v.SetDefault("session.retention", "720h")
	v.SetDefault("knowledge_base.path", filepath.Join(configHome, "knowledge_base"))
}

func defaultConfigHome() string {
	if configHome, err := os.UserConfigDir(); err == nil {
		return filepath.Join(configHome, "morph")
	}
	return filepath.Join(".", "config")
}

func defaultDataHome() string {
	if dataHome, err := os.UserHomeDir(); err == nil {
		return filepath.Join(dataHome, ".local", "share", "morph")
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
	return nil
}
