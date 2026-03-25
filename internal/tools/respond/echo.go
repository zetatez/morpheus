package respond

import (
	"context"

	"github.com/zetatez/morpheus/pkg/sdk"
)

// EchoTool simply echoes text back to the caller.
type EchoTool struct{}

// NewEcho returns a conversation echo tool.
func NewEcho() *EchoTool { return &EchoTool{} }

func (t *EchoTool) Name() string { return "conversation.echo" }

func (t *EchoTool) Describe() string { return "Return a text response directly to the user." }

func (t *EchoTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}
}

func (t *EchoTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	text, _ := input["text"].(string)
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"text": text,
		},
	}, nil
}
