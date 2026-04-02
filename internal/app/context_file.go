package app

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	contextFileName = ".morpheus.md"
)

func loadContextFiles(workspaceRoot string) (string, error) {
	var parts []string

	if morpheusContext, ok := loadMorpheusContext(workspaceRoot); ok && morpheusContext != "" {
		parts = append(parts, morpheusContext)
	}

	if globalContext, ok := loadGlobalMorpheusContext(); ok && globalContext != "" {
		parts = append(parts, globalContext)
	}

	if len(parts) == 0 {
		return "", nil
	}

	return strings.Join(parts, "\n\n"), nil
}

func loadMorpheusContext(workspaceRoot string) (string, bool) {
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

func loadGlobalMorpheusContext() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}

	globalPath := filepath.Join(home, ".config", "morph", contextFileName)
	data, err := os.ReadFile(globalPath)
	if err != nil {
		return "", false
	}

	content := strings.TrimSpace(string(data))
	if content == "" {
		return "", false
	}

	return content, true
}
