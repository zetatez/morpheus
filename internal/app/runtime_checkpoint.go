package app

import (
	"fmt"
	"strings"
)

func checkpointMessage(id, summary string) string {
	return fmt.Sprintf("morpheus checkpoint %s [%s]", id, summary)
}

func checkpointSummary(toolName string, inputs map[string]any) string {
	summary := strings.TrimSpace(toolName)
	switch toolName {
	case "cmd.exec":
		if command, _ := inputs["command"].(string); strings.TrimSpace(command) != "" {
			summary = truncate(strings.TrimSpace(command), 80)
		}
	case "write", "edit", "read", "bash":
		if path, _ := inputs["path"].(string); strings.TrimSpace(path) != "" {
			summary = fmt.Sprintf("%s %s", toolName, strings.TrimSpace(path))
		}
	}
	if summary == "" {
		summary = "tool execution"
	}
	return summary
}
