package app

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	contextFileName      = ".morpheus.md"
	contextLocalFileName = ".morpheus.local.md"
)

type ContextLevel int

const (
	ContextLevelSystem ContextLevel = iota
	ContextLevelUser
	ContextLevelProject
	ContextLevelLocal
)

func (c ContextLevel) String() string {
	switch c {
	case ContextLevelSystem:
		return "system"
	case ContextLevelUser:
		return "user"
	case ContextLevelProject:
		return "project"
	case ContextLevelLocal:
		return "local"
	default:
		return "unknown"
	}
}

type ContextFile struct {
	Level   ContextLevel
	Content string
	Path    string
}

func loadContextFiles(workspaceRoot string) (string, error) {
	levels := loadContextFilesWithLevels(workspaceRoot)

	var parts []string
	for _, ctx := range levels {
		if ctx.Content != "" {
			parts = append(parts, ctx.Content)
		}
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, "\n\n"), nil
}

func loadContextFilesWithLevels(workspaceRoot string) []ContextFile {
	var results []ContextFile

	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	if systemCtx, ok := loadSystemContext(); ok && systemCtx != "" {
		results = append(results, ContextFile{
			Level:   ContextLevelSystem,
			Content: systemCtx,
			Path:    "/etc/morpheus/.morpheus.md",
		})
	}

	if userCtx, ok := loadUserContext(); ok && userCtx != "" {
		results = append(results, ContextFile{
			Level:   ContextLevelUser,
			Content: userCtx,
			Path:    filepath.Join(os.Getenv("HOME"), ".config", "morpheus", ".morpheus.md"),
		})
	}

	if projectCtx, ok := loadProjectContext(workspaceRoot); ok && projectCtx != "" {
		results = append(results, ContextFile{
			Level:   ContextLevelProject,
			Content: projectCtx,
			Path:    filepath.Join(workspaceRoot, contextFileName),
		})
	}

	if localCtx, ok := loadLocalContext(workspaceRoot); ok && localCtx != "" {
		results = append(results, ContextFile{
			Level:   ContextLevelLocal,
			Content: localCtx,
			Path:    filepath.Join(workspaceRoot, contextLocalFileName),
		})
	}

	return results
}

func loadProjectContext(workspaceRoot string) (string, bool) {
	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	contextPath := filepath.Join(workspaceRoot, contextFileName)
	data, err := os.ReadFile(contextPath)
	if err != nil {
		return "", false
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", false
	}

	return content, true
}

func loadLocalContext(workspaceRoot string) (string, bool) {
	if workspaceRoot == "" {
		workspaceRoot = "."
	}

	contextPath := filepath.Join(workspaceRoot, contextLocalFileName)
	data, err := os.ReadFile(contextPath)
	if err != nil {
		return "", false
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", false
	}

	return content, true
}

func loadUserContext() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	userPaths := []string{
		filepath.Join(home, ".config", "morpheus", contextFileName),
		filepath.Join(home, ".config", "morph", contextFileName),
		filepath.Join(home, ".morpheus.md"),
		filepath.Join(home, ".morph.md"),
	}

	for _, p := range userPaths {
		if data, err := os.ReadFile(p); err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				return content, true
			}
		}
	}

	return "", false
}

func loadSystemContext() (string, bool) {
	systemPaths := []string{
		"/etc/morpheus/.morpheus.md",
		"/etc/morpheus/.morpheus.md",
	}

	for _, p := range systemPaths {
		if data, err := os.ReadFile(p); err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				return content, true
			}
		}
	}

	return "", false
}

func GetContextFilePath(workspaceRoot string) string {
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	return filepath.Join(workspaceRoot, contextFileName)
}

func GetContextLocalFilePath(workspaceRoot string) string {
	if workspaceRoot == "" {
		workspaceRoot = "."
	}
	return filepath.Join(workspaceRoot, contextLocalFileName)
}

func EnsureContextFileExists(workspaceRoot string) error {
	path := GetContextFilePath(workspaceRoot)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		content := `# Morpheus Project Context
# This file contains project-specific rules and context that always applies to this project.
# Unlike conversation history, this file is NEVER compressed and is always included in every request.
#
# Use this file to:
# - Define project-specific coding conventions
# - Set persistent rules and constraints
# - Document project architecture decisions
# - Specify which files should be ignored
#
# Examples:
# - "Always use tabs for indentation in this project"
# - "This project uses Go, version 1.21+"
# - "API endpoints are prefixed with /api/v1/"
`
		return os.WriteFile(path, []byte(content), 0o644)
	}
	return nil
}
