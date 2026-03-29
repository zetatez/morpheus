package agenttool

import (
	"context"
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type MessageRunner interface {
	SendTeamMessage(ctx context.Context, from, to, kind, content, replyTo, threadID string, broadcast bool) (map[string]any, error)
}

type MessageTool struct {
	runner MessageRunner
}

func NewMessage(runner MessageRunner) *MessageTool {
	return &MessageTool{runner: runner}
}

func (t *MessageTool) Name() string { return "agent.message" }

func (t *MessageTool) Describe() string {
	return "Send a team message to a task id, role, coordinator, or broadcast it to the current agent team."
}

func (t *MessageTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"from":      map[string]any{"type": "string"},
			"to":        map[string]any{"type": "string", "description": "Task id, role, or coordinator"},
			"kind":      map[string]any{"type": "string", "description": "Optional message kind"},
			"broadcast": map[string]any{"type": "boolean", "description": "When true, deliver to all team members and coordinator"},
			"reply_to":  map[string]any{"type": "string", "description": "Optional message id being replied to"},
			"thread_id": map[string]any{"type": "string", "description": "Optional explicit thread id"},
			"content":   map[string]any{"type": "string", "description": "Message body"},
		},
		"required": []string{"content"},
	}
}

func (t *MessageTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	if t.runner == nil {
		return sdk.ToolResult{Success: false}, fmt.Errorf("agent message runner not configured")
	}
	from, _ := input["from"].(string)
	to, _ := input["to"].(string)
	kind, _ := input["kind"].(string)
	broadcast, _ := input["broadcast"].(bool)
	replyTo, _ := input["reply_to"].(string)
	threadID, _ := input["thread_id"].(string)
	content, _ := input["content"].(string)
	content = strings.TrimSpace(content)
	if content == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("content is required")
	}
	result, err := t.runner.SendTeamMessage(ctx, from, to, kind, content, replyTo, threadID, broadcast)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if result == nil {
		result = map[string]any{}
	}
	result["delivered"] = true
	result["to"] = strings.TrimSpace(to)
	result["kind"] = strings.TrimSpace(kind)
	result["broadcast"] = broadcast
	result["reply_to"] = strings.TrimSpace(replyTo)
	result["content"] = content
	return sdk.ToolResult{Success: true, Data: result}, nil
}
