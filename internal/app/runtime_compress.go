package app

import (
	"regexp"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

var (
	whitespaceRE = regexp.MustCompile(`\s+`)
	codeBlockRE  = regexp.MustCompile("(?s)```[\\s\\S]*?```|`[^`]+`")
)

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	chars := len(text)
	if chars <= 4 {
		return 1
	}
	whitespace := float64(len(whitespaceRE.ReplaceAllString(text, " ")))
	code := float64(len(codeBlockRE.ReplaceAllString(text, "")))
	plain := float64(chars) - code
	codeTokens := int(code / 4)
	plainTokens := int((whitespace + plain*2) / 6)
	return codeTokens + plainTokens + 1
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
	return preview + "\n\n[Compacted tool output: original length " + itoa(len(trimmed)) + " chars]"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
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
