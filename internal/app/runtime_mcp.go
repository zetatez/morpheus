package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

func formatMCPResourceUpdate(server, uri string, payload map[string]any) string {
	delete(payload, "session_id")
	structured := summarizeMCPResourcePayload(server, uri, payload)
	body, _ := json.MarshalIndent(structured, "", "  ")
	return fmt.Sprintf("MCP resource updated\n%s", string(body))
}

func mcpResourceUpdatePart(server, uri string, payload map[string]any) sdk.MessagePart {
	delete(payload, "session_id")
	return sdk.MessagePart{
		Type:   "tool",
		Tool:   "mcp.resource_update",
		Status: "updated",
		Input: map[string]any{
			"server": server,
			"uri":    uri,
		},
		Output: summarizeMCPResourcePayload(server, uri, payload),
	}
}

func summarizeMCPResourcePayload(server, uri string, payload map[string]any) map[string]any {
	result := map[string]any{
		"server":      server,
		"uri":         uri,
		"change_type": detectMCPChangeType(payload),
	}

	contents, _ := payload["contents"].([]any)
	if len(contents) > 0 {
		result["summary"] = summarizeMCPContents(contents)
		result["highlights"] = extractMCPHighlights(contents)
		result["truncated"] = isMCPContentTruncated(contents)
		return result
	}

	result["summary"] = summarizeGenericMCPPayload(payload)
	result["highlights"] = topLevelMCPKeys(payload)
	result["truncated"] = len(fmt.Sprint(payload)) > 800
	return result
}

func detectMCPChangeType(payload map[string]any) string {
	if _, ok := payload["contents"]; ok {
		return "resource_contents"
	}
	if _, ok := payload["resources"]; ok {
		return "resource_list"
	}
	return "resource_update"
}

func summarizeMCPContents(contents []any) string {
	if len(contents) == 0 {
		return "empty resource payload"
	}
	first, _ := contents[0].(map[string]any)
	if text, _ := first["text"].(string); strings.TrimSpace(text) != "" {
		if mime, _ := first["mimeType"].(string); mime != "" {
			return summarizeMIMEText(mime, text)
		}
		lines := strings.Split(strings.TrimSpace(text), "\n")
		if len(lines) > 3 {
			lines = lines[:3]
		}
		return truncate(strings.Join(lines, " | "), 240)
	}
	if blob, _ := first["blob"].(string); blob != "" {
		return fmt.Sprintf("binary content (%d bytes base64)", len(blob))
	}
	if mime, _ := first["mimeType"].(string); mime != "" {
		return "resource content with mime type " + mime
	}
	body, _ := json.Marshal(first)
	return truncate(string(body), 240)
}

func extractMCPHighlights(contents []any) []string {
	if len(contents) == 0 {
		return nil
	}
	var highlights []string
	for i, item := range contents {
		if i >= 3 {
			break
		}
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if text, _ := m["text"].(string); text != "" {
			lines := strings.Split(strings.TrimSpace(text), "\n")
			for j, line := range lines {
				if j >= 2 {
					break
				}
				highlights = append(highlights, strings.TrimSpace(line))
			}
		}
	}
	return highlights
}

func summarizeMIMEText(mime, text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 3 {
		return truncate(strings.Join(lines[:3], " | "), 240)
	}
	return truncate(text, 240)
}

func isMCPContentTruncated(contents []any) bool {
	if len(contents) > 10 {
		return true
	}
	for _, c := range contents {
		m, ok := c.(map[string]any)
		if !ok {
			continue
		}
		if text, _ := m["text"].(string); len(text) > 4000 {
			return true
		}
	}
	return false
}

func summarizeGenericMCPPayload(payload map[string]any) string {
	keys := topLevelMCPKeys(payload)
	if len(keys) == 0 {
		return "empty MCP payload"
	}
	return fmt.Sprintf("MCP payload with keys: %s", strings.Join(keys[:min(3, len(keys))], ", "))
}

func topLevelMCPKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 8 {
		keys = keys[:8]
	}
	return keys
}
