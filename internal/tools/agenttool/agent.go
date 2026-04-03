package agenttool

import (
	"context"
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type Runner interface {
	RunSubAgent(ctx context.Context, prompt string, allowedTools []string) (string, error)
	RunSubAgentWithBackground(ctx context.Context, prompt string, allowedTools []string, background bool) (string, error)
	RunSubAgentFork(ctx context.Context, prompt string, allowedTools []string) (string, error)
}

type Tool struct {
	runner Runner
}

func New(runner Runner) *Tool {
	return &Tool{runner: runner}
}

func (t *Tool) Name() string { return "agent.run" }

func (t *Tool) Describe() string {
	return "Run a focused sub-agent in an isolated context and return only its summary."
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{"type": "string", "description": "The task or question for the sub-agent to handle"},
			"tools": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Optional list of allowed tools (e.g., [\"fs.read\", \"cmd.exec\"])",
			},
			"background": map[string]any{
				"type":        "boolean",
				"description": "If true, run the sub-agent in background (non-blocking)",
			},
			"fork": map[string]any{
				"type":        "boolean",
				"description": "If true, run the sub-agent with strict fork isolation (no access to parent context, memory, or team state)",
			},
		},
		"required": []string{"prompt"},
	}
}

func (t *Tool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	if t.runner == nil {
		return sdk.ToolResult{Success: false}, fmt.Errorf("agent runner not configured")
	}
	prompt, _ := input["prompt"].(string)
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("prompt is required")
	}

	var allowedTools []string
	if tools, ok := input["tools"].([]any); ok {
		for _, tool := range tools {
			if t, ok := tool.(string); ok {
				allowedTools = append(allowedTools, t)
			}
		}
	}

	background, _ := input["background"].(bool)
	fork, _ := input["fork"].(bool)

	var summary string
	var err error

	if fork {
		if runner, ok := t.runner.(interface {
			RunSubAgentFork(ctx context.Context, prompt string, allowedTools []string) (string, error)
		}); ok {
			summary, err = runner.RunSubAgentFork(ctx, prompt, allowedTools)
		} else {
			summary, err = t.runner.RunSubAgentWithBackground(ctx, prompt, allowedTools, background)
		}
	} else {
		summary, err = t.runner.RunSubAgentWithBackground(ctx, prompt, allowedTools, background)
	}

	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{Success: true, Data: map[string]any{"summary": summary}}, nil
}
