package app

import (
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

func isToolLikeContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "```") {
		return true
	}
	if strings.Contains(trimmed, "Output:") || strings.Contains(trimmed, "stdout:") {
		return true
	}
	prefixes := []string{"Written ", "Created ", "Removed ", "Done ", "$ ", "Step: "}
	for _, p := range prefixes {
		if strings.Contains(trimmed, p) {
			return true
		}
	}
	return false
}

func compactToolOutput(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	preview := trimmed
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return fmt.Sprintf("%s\n\n[Compacted tool output: original length %d chars]", preview, len(trimmed))
}

func hasToolParts(parts []sdk.MessagePart) bool {
	for _, part := range parts {
		if part.Type == "tool" {
			return true
		}
	}
	return false
}

func compactToolMessage(msg sdk.Message) sdk.Message {
	msg.Content = compactToolOutput(msg.Content)
	if len(msg.Parts) == 0 {
		return msg
	}
	for i, part := range msg.Parts {
		if part.Type != "tool" || part.Output == nil {
			continue
		}
		part.Output = truncateOutputMap(part.Output)
		msg.Parts[i] = part
	}
	return msg
}

func truncateOutputMap(output map[string]any) map[string]any {
	const maxLen = 1000
	const previewLen = 200
	truncated := false
	for k, v := range output {
		str, ok := v.(string)
		if !ok || len(str) <= maxLen {
			continue
		}
		output[k] = truncate(str, previewLen)
		truncated = true
	}
	if truncated {
		if _, ok := output["truncated"]; !ok {
			output["truncated"] = true
		}
	}
	return output
}
