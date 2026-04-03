package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	TruncateMaxLines  = 2000
	TruncateMaxBytes  = 50 * 1024
	TruncateDir       = ".morpheus/truncations"
	TruncateRetention = 7 * 24 * time.Hour
)

type TruncateResult struct {
	Content    string
	Truncated  bool
	OutputPath string
}

func truncateToFile(content string, direction string, maxLines, maxBytes int, keepTail bool) (string, bool, int, string) {
	lines := strings.Split(content, "\n")
	totalBytes := len(content)

	if len(lines) <= maxLines && totalBytes <= maxBytes {
		return content, false, 0, ""
	}

	var kept []string
	var removed int
	var bytes int

	if keepTail {
		for idx := len(lines) - 1; idx >= 0 && len(kept) < maxLines; idx-- {
			line := lines[idx]
			size := len(line)
			if bytes+size > maxBytes {
				break
			}
			kept = append([]string{line}, kept...)
			bytes += size
		}
		removed = len(lines) - len(kept)
	} else {
		for idx := 0; idx < len(lines) && len(kept) < maxLines; idx++ {
			line := lines[idx]
			size := len(line)
			if bytes+size > maxBytes {
				break
			}
			kept = append(kept, line)
			bytes += size
		}
		removed = len(lines) - len(kept)
	}

	file := filepath.Join(TruncateDir, generateTruncationID())
	if err := os.MkdirAll(TruncateDir, 0755); err != nil {
		return strings.Join(kept, "\n"), true, removed, ""
	}
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		return strings.Join(kept, "\n"), true, removed, ""
	}

	return strings.Join(kept, "\n"), true, removed, file
}

func generateTruncationID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "tool_" + hex.EncodeToString(b)
}

func cleanupOldTruncations() error {
	entries, err := os.ReadDir(TruncateDir)
	if err != nil {
		return nil
	}
	cutoff := time.Now().Add(-TruncateRetention)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "tool_") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(TruncateDir, entry.Name()))
		}
	}
	return nil
}

func formatTruncatedOutput(preview string, removed int, unit string, outputPath string, hasTaskTool bool) string {
	var hint string
	if outputPath != "" {
		if hasTaskTool {
			hint = fmt.Sprintf("Full output saved to: %s\nUse the Task tool to have explore agent process this file with Grep and Read (with offset/limit). Do NOT read the full file yourself - delegate to save context.", outputPath)
		} else {
			hint = fmt.Sprintf("Full output saved to: %s\nUse Grep to search the full content or Read with offset/limit to view specific sections.", outputPath)
		}
	} else {
		hint = "Output was truncated."
	}

	if unit == "bytes" {
		return preview + "\n\n...output truncated...\n\n" + hint
	}
	return preview + fmt.Sprintf("\n\n...%d %s truncated...\n\n%s", removed, unit, hint)
}

func shouldCompactToolOutput(content string, maxTokens int) bool {
	return estimateTokens(content) > maxTokens
}

func compactForTokenLimit(content string, maxTokens int) (string, bool) {
	targetChars := maxTokens * 4
	if len(content) <= targetChars {
		return content, false
	}

	preview := content
	if len(preview) > targetChars {
		preview = content[:targetChars]
		lastNewline := strings.LastIndex(preview, "\n")
		if lastNewline > targetChars/2 {
			preview = preview[:lastNewline]
		}
	}

	return preview + "\n\n[Output truncated to fit token limit]", true
}
