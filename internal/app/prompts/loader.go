package prompts

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed *.md
var FS embed.FS

var (
	Agents   string
	System   string
	Coding   string
	Debug    string
	Testing  string
	Refactor string
)

func Load() error {
	var err error
	Agents, err = loadPrompt("AGENTS.md")
	if err != nil {
		return fmt.Errorf("load AGENTS: %w", err)
	}
	System, err = loadPrompt("system.md")
	if err != nil {
		return fmt.Errorf("load system: %w", err)
	}
	Coding, err = loadPrompt("coding.md")
	if err != nil {
		return fmt.Errorf("load coding: %w", err)
	}
	Debug, err = loadPrompt("debug.md")
	if err != nil {
		return fmt.Errorf("load debug: %w", err)
	}
	Testing, err = loadPrompt("testing.md")
	if err != nil {
		return fmt.Errorf("load testing: %w", err)
	}
	Refactor, err = loadPrompt("refactor.md")
	if err != nil {
		return fmt.Errorf("load refactor: %w", err)
	}
	return nil
}

func loadPrompt(name string) (string, error) {
	data, err := FS.ReadFile(name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func Skill(name string) string {
	switch name {
	case "coding":
		return Coding
	case "debug":
		return Debug
	case "testing":
		return Testing
	case "refactor":
		return Refactor
	default:
		return ""
	}
}
