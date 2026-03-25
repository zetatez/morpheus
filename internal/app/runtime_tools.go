package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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

func formatAskQuestion(data map[string]any) string {
	question, _ := data["question"].(string)
	question = strings.TrimSpace(question)
	if question == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(question)
	options, _ := data["options"].([]string)
	if len(options) == 0 {
		if raw, ok := data["options"].([]any); ok {
			for _, item := range raw {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					options = append(options, strings.TrimSpace(text))
				}
			}
		}
	}
	for i, option := range options {
		b.WriteString(fmt.Sprintf("\n%d. %s", i+1, option))
	}
	if multiple, _ := data["multiple"].(bool); multiple {
		b.WriteString("\nYou may choose more than one option.")
	}
	return b.String()
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

func planStepTool(plan *sdk.Plan, stepID string) string {
	for _, step := range plan.Steps {
		if step.ID == stepID {
			return step.Tool
		}
	}
	return ""
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func estimateMessageTokens(msg sdk.Message) int {
	total := estimateTokens(msg.Content)
	for _, part := range msg.Parts {
		total += estimateTokens(part.Text)
		total += estimateTokens(part.Error)
		total += estimateTokens(renderMapForTokens(part.Input))
		total += estimateTokens(renderMapForTokens(part.Output))
	}
	return total
}

func renderMapForTokens(value map[string]any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
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

func (rt *Runtime) buildReflectionPrompt(results []sdk.ToolResult, goal string) string {
	if len(results) == 0 {
		return ""
	}

	var successfulTools []string
	var failedTools []string
	for _, r := range results {
		if r.Success {
			if tool, ok := r.Data["tool"].(string); ok {
				successfulTools = append(successfulTools, tool)
			}
		} else {
			if tool, ok := r.Data["tool"].(string); ok {
				failedTools = append(failedTools, tool)
			}
			if r.Error != "" {
				failedTools = append(failedTools, r.Error)
			}
		}
	}

	var b strings.Builder
	b.WriteString("\n\n## Reflection\n")
	b.WriteString("After reviewing your recent actions, consider the following:\n\n")

	if len(failedTools) > 0 && len(failedTools) >= len(successfulTools) {
		b.WriteString("1. **Failure Analysis**: You've had more failures than successes. Consider:\n")
		b.WriteString("   - Are you taking the right approach?\n")
		b.WriteString("   - Could there be a simpler way to achieve the same goal?\n")
		b.WriteString("   - Are there missing prerequisites or permissions?\n")
	}

	if len(results) >= 3 {
		b.WriteString("2. **Progress Check**: Based on recent tool results:\n")
		b.WriteString("   - What has been accomplished so far?\n")
		b.WriteString("   - Is the remaining work necessary?\n")
		b.WriteString("   - Should you verify intermediate results before proceeding?\n")
	}

	toolTypes := make(map[string]int)
	for _, r := range results {
		if tool, ok := r.Data["tool"].(string); ok {
			toolTypes[tool]++
			if toolTypes[tool] >= 3 {
				b.WriteString("3. **Loop Detection**: You've used the same tool multiple times.\n")
				b.WriteString("   - If it's not working, try a different approach.\n")
				b.WriteString("   - Consider combining multiple steps into one.\n")
				break
			}
		}
	}

	b.WriteString("\nProceed with the most promising approach. If stuck, consider asking the user for clarification.")

	return b.String()
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
