package app

import (
	"context"

	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/pkg/sdk"
)

func (rt *Runtime) composeReply(plan *sdk.Plan, results []sdk.ToolResult) string {
	if len(results) == 0 {
		return ""
	}
	last := results[len(results)-1]
	switch planStepTool(plan, last.StepID) {
	case "question":
		return formatAskQuestion(last.Data)
	case "read":
		if content, ok := last.Data["content"].(string); ok {
			return truncate(content, 400)
		}
	case "bash":
		if out, ok := last.Data["stdout"].(string); ok {
			return truncate(out, 400)
		}
	}
	return ""
}

func (rt *Runtime) appendMessage(ctx context.Context, sessionID, role, content string, parts []sdk.MessagePart) (sdk.Message, error) {
	payload := plugin.MessagePayload{
		Role:    role,
		Content: content,
		Parts:   parts,
	}
	if rt.plugins != nil {
		payload = rt.plugins.ApplyMessage(plugin.MessageContext{SessionID: sessionID}, payload)
	}
	return rt.conversation.AppendWithParts(ctx, sessionID, payload.Role, payload.Content, payload.Parts)
}

func (rt *Runtime) truncateToolResult(ctx context.Context, sessionID, tool string, data map[string]any) map[string]any {
	if data == nil {
		return data
	}
	const maxLen = 4000
	const previewLen = 2000
	fields := []string{"stdout", "content", "body", "tree", "patch"}
	outputFiles := map[string]string{}
	truncated := false
	for _, field := range fields {
		value, ok := data[field].(string)
		if !ok || len(value) <= maxLen {
			continue
		}
		path, err := rt.session.SaveToolOutput(ctx, sessionID, tool, []byte(value))
		if err == nil && path != "" {
			outputFiles[field] = path
		}
		data[field] = truncate(value, previewLen)
		truncated = true
	}
	if truncated {
		data["truncated"] = true
		if len(outputFiles) > 0 {
			data["output_files"] = outputFiles
		}
	}
	return data
}
