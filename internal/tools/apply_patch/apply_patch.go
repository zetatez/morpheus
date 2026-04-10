package apply_patch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type Tool struct {
	root         string
	maxWriteSize int
}

func NewTool(root string, maxWriteSizeKB int) *Tool {
	return &Tool{root: root, maxWriteSize: maxWriteSizeKB}
}

func (t *Tool) Name() string { return "apply_patch" }

func (t *Tool) Describe() string {
	return "Apply a unified diff patch to a file. Uses standard diff/patch format. " +
		"Preferred over edit/write when model is in GPT family."
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Path to the file to patch (relative to workspace root)",
			},
			"diff": map[string]any{
				"type":        "string",
				"description": "Unified diff to apply to the file. Use standard diff format with ---/+++ headers and @@ hunk headers.",
			},
			"strip": map[string]any{
				"type":        "integer",
				"description": "Number of leading path components to strip from diff paths (default: 0)",
			},
		},
		"required": []string{"file_path", "diff"},
	}
}

func (t *Tool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	filePath, _ := input["file_path"].(string)
	diff, _ := input["diff"].(string)
	strip, _ := input["strip"].(int)

	if filePath == "" {
		return sdk.ToolResult{Success: false, Error: "file_path is required"}, nil
	}
	if diff == "" {
		return sdk.ToolResult{Success: false, Error: "diff is required"}, nil
	}

	absPath := filepath.Join(t.root, filePath)

	// Security: ensure file is within root
	cleanPath, err := filepath.Abs(absPath)
	if err != nil {
		return sdk.ToolResult{Success: false, Error: fmt.Sprintf("invalid path: %v", err)}, nil
	}
	if !strings.HasPrefix(cleanPath, t.root) {
		return sdk.ToolResult{Success: false, Error: "file path is outside workspace root"}, nil
	}

	// Check file exists
	info, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return sdk.ToolResult{Success: false, Error: fmt.Sprintf("file not found: %s", filePath)}, nil
		}
		return sdk.ToolResult{Success: false, Error: fmt.Sprintf("cannot access file: %v", err)}, nil
	}

	// Check write size limits
	if t.maxWriteSize > 0 {
		estimatedSize := len(diff) * 2
		if estimatedSize > t.maxWriteSize*1024 {
			return sdk.ToolResult{
				Success: false,
				Error:   fmt.Sprintf("patch result exceeds max write size (%d KB)", t.maxWriteSize),
			}, nil
		}
		_ = info
	}

	// Read current content
	currentContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return sdk.ToolResult{Success: false, Error: fmt.Sprintf("cannot read file: %v", err)}, nil
	}

	// Normalize line endings
	content := string(currentContent)

	// Apply the diff
	patched, err := applyDiff(content, diff, strip)
	if err != nil {
		return sdk.ToolResult{Success: false, Error: fmt.Sprintf("failed to apply patch: %v", err)}, nil
	}

	// Write the patched content
	if err := os.WriteFile(cleanPath, []byte(patched), 0o644); err != nil {
		return sdk.ToolResult{Success: false, Error: fmt.Sprintf("cannot write file: %v", err)}, nil
	}

	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"content":   fmt.Sprintf("Patch applied to %s", filePath),
			"file":      filePath,
			"old_size":  len(content),
			"new_size":  len(patched),
			"diff_size": len(diff),
		},
	}, nil
}

// applyDiff applies a unified diff to the given content.
// This is a simplified diff application that handles common unified diff patterns.
func applyDiff(content, diff string, strip int) (string, error) {
	lines := strings.Split(content, "\n")
	diffLines := strings.Split(diff, "\n")

	// Track if we're in a hunk
	var hunks []hunk
	var currentHunk hunk
	inHunk := false

	for _, line := range diffLines {
		switch {
		case strings.HasPrefix(line, "@@ "):
			// New hunk
			if inHunk {
				hunks = append(hunks, currentHunk)
			}
			currentHunk = hunk{header: line, lines: nil}
			inHunk = true

		case inHunk:
			currentHunk.lines = append(currentHunk.lines, line)
		}
	}
	if inHunk && len(currentHunk.lines) > 0 {
		hunks = append(hunks, currentHunk)
	}

	// Apply hunks in reverse order (bottom-up to avoid offset issues)
	var result []string
	lastEnd := len(lines)

	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		oldStart, _, err := parseHunkHeader(h.header)
		if err != nil {
			return "", fmt.Errorf("cannot parse hunk header %q: %w", h.header, err)
		}
		oldStart-- // Convert to 0-indexed
		if oldStart < 0 {
			oldStart = 0
		}
		if oldStart > len(lines) {
			oldStart = len(lines)
		}

		// Build patch result for this hunk
		patchedLines := applyHunk(lines[oldStart:lastEnd], h.lines)
		result = append(result, patchedLines...)
		lastEnd = oldStart
	}

	// Add lines before the first hunk
	if lastEnd > 0 {
		result = append(lines[:lastEnd], result...)
	}

	return strings.Join(result, "\n"), nil
}

type hunk struct {
	header string
	lines  []string
}

func parseHunkHeader(header string) (oldStart, oldCount int, err error) {
	// Format: @@ -oldStart,oldCount +newStart,newCount @@
	header = strings.TrimPrefix(header, "@@ ")
	header = strings.TrimSuffix(header, " @@")
	parts := strings.Split(header, " ")
	if len(parts) < 2 {
		return 0, 0, fmt.Errorf("invalid hunk header format")
	}
	oldPart := strings.TrimPrefix(parts[0], "-")
	if strings.Contains(oldPart, ",") {
		_, err := fmt.Sscanf(oldPart, "%d,%d", &oldStart, &oldCount)
		if err != nil {
			return 0, 0, fmt.Errorf("cannot parse old range: %w", err)
		}
	} else {
		fmt.Sscanf(oldPart, "%d", &oldStart)
		oldCount = 1
	}
	return oldStart, oldCount, nil
}

func applyHunk(targetLines []string, hunkLines []string) []string {
	var result []string
	var removed int

	for _, line := range hunkLines {
		if len(line) == 0 {
			result = append(result, "")
			continue
		}

		switch line[0] {
		case ' ':
			// Context line - keep as-is
			if removed < len(targetLines) {
				result = append(result, targetLines[removed])
				removed++
			} else {
				// Context beyond target means we're adding new lines
				result = append(result, "")
			}

		case '-':
			// Removal - skip the corresponding target line
			removed++

		case '+':
			// Addition - add the new content
			result = append(result, line[1:])
		}
	}

	// Include remaining target lines
	if removed < len(targetLines) {
		result = append(result, targetLines[removed:]...)
	}

	return result
}
