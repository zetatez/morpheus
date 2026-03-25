package ask

import (
	"context"
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type QuestionTool struct{}

func NewQuestionTool() *QuestionTool { return &QuestionTool{} }

func (t *QuestionTool) Name() string { return "conversation.ask" }

func (t *QuestionTool) Describe() string {
	return "Ask the user a targeted multiple-choice question when clarification is required."
}

func (t *QuestionTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{"type": "string"},
			"options": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "string",
				},
				"minItems": 1,
			},
			"multiple": map[string]any{"type": "boolean"},
		},
		"required": []string{"question", "options"},
	}
}

func (t *QuestionTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	question, _ := input["question"].(string)
	if strings.TrimSpace(question) == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("question is required")
	}
	optionsRaw, _ := input["options"].([]any)
	if len(optionsRaw) == 0 {
		return sdk.ToolResult{Success: false}, fmt.Errorf("options are required")
	}
	options := make([]string, 0, len(optionsRaw))
	for _, item := range optionsRaw {
		text, _ := item.(string)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		options = append(options, text)
	}
	if len(options) == 0 {
		return sdk.ToolResult{Success: false}, fmt.Errorf("options are required")
	}
	multiple, _ := input["multiple"].(bool)
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"question": question,
			"options":  options,
			"multiple": multiple,
		},
	}, nil
}
