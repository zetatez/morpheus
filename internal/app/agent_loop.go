package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	maxAgentSteps   = 12
	maxHistoryTurns = 20
)

type toolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type chatResponse struct {
	Content      string
	ToolCalls    []toolCall
	FinishReason string
}

type OutputFormat struct {
	Type       string         `json:"type"`
	Schema     map[string]any `json:"schema,omitempty"`
	RetryCount int            `json:"retry_count,omitempty"`
}

func (rt *Runtime) AgentLoop(ctx context.Context, sessionID, input string, format *OutputFormat) (Response, error) {
	if sessionID == "" {
		sessionID = "default"
	}
	if _, err := rt.appendMessage(ctx, sessionID, "user", input, nil); err != nil {
		return Response{}, err
	}
	rt.allowMentionedSkills(sessionID, input)
	rt.setIsCodeTask(sessionID, rt.isCodeTask(sessionID) || looksLikeCodeTask([]sdk.Message{{Role: "user", Content: input}}))

	rt.checkAndCompress(ctx, sessionID)

	tools, toolChoice, nameMap, structuredName := rt.collectToolSpecs(format)
	baseMessages := rt.buildAgentMessages(sessionID)
	plan := sdk.Plan{
		ID:      uuid.NewString(),
		Summary: input,
		Status:  sdk.PlanStatusInProgress,
	}
	results := []sdk.ToolResult{}
	planReq := sdk.PlanRequest{ConversationID: sessionID, Prompt: input, Intent: "agent"}
	retries := 0
	if format != nil && format.Type == "json_schema" {
		if format.RetryCount > 0 {
			retries = format.RetryCount
		} else {
			retries = 2
		}
	}

	for step := 0; step < maxAgentSteps; step++ {
		resp, err := rt.callChatWithTools(ctx, baseMessages, tools, toolChoice)
		if err != nil {
			plan.Status = sdk.PlanStatusBlocked
			return Response{Plan: plan, Results: results, Reply: ""}, err
		}

		if format != nil && format.Type == "json_schema" {
			if output, ok := extractStructuredOutput(resp.ToolCalls, structuredName); ok {
				serialized, _ := json.Marshal(output)
				_, _ = rt.appendMessage(ctx, sessionID, "assistant", string(serialized), []sdk.MessagePart{outputPart(output)})
				plan.Status = sdk.PlanStatusDone
				_ = rt.audit.Record(planReq, plan, results)
				_ = rt.session.Write(ctx, sessionID, rt.conversation.History(ctx, sessionID))
				return Response{Plan: plan, Results: results, Reply: string(serialized)}, nil
			}
			if resp.FinishReason != "tool_calls" && resp.FinishReason != "tool-calls" {
				if retries > 0 {
					retries--
					baseMessages = append(baseMessages, map[string]any{
						"role":    "system",
						"content": "You must call the StructuredOutput tool with a JSON object matching the schema.",
					})
					continue
				}
				plan.Status = sdk.PlanStatusBlocked
				return Response{Plan: plan, Results: results, Reply: ""}, fmt.Errorf("structured output not produced")
			}
		}

		if len(resp.ToolCalls) == 0 {
			if resp.Content != "" {
				_, _ = rt.appendMessage(ctx, sessionID, "assistant", resp.Content, nil)
			}
			plan.Status = sdk.PlanStatusDone
			_ = rt.audit.Record(planReq, plan, results)
			_ = rt.session.Write(ctx, sessionID, rt.conversation.History(ctx, sessionID))
			return Response{Plan: plan, Results: results, Reply: resp.Content}, nil
		}

		assistantMessage := map[string]any{
			"role":       "assistant",
			"content":    resp.Content,
			"tool_calls": buildToolCallPayload(resp.ToolCalls),
		}
		baseMessages = append(baseMessages, assistantMessage)

		for _, call := range resp.ToolCalls {
			toolName := nameMap[call.Name]
			if toolName == "" {
				toolName = call.Name
			}
			pending := sdk.MessagePart{
				Type:   "tool",
				Tool:   toolName,
				CallID: call.ID,
				Input:  call.Arguments,
				Status: "pending",
			}
			_, _ = rt.appendMessage(ctx, sessionID, "assistant", fmt.Sprintf("Tool call: %s", toolName), []sdk.MessagePart{pending})

			stepID := uuid.NewString()
			planStep := sdk.PlanStep{
				ID:          stepID,
				Description: fmt.Sprintf("Tool call: %s", toolName),
				Tool:        toolName,
				Inputs:      call.Arguments,
				Status:      sdk.StepStatusRunning,
			}
			result, err := rt.orchestrator.ExecuteStep(ctx, sessionID, planStep)
			result.StepID = stepID
			if err != nil {
				result.Success = false
				result.Error = err.Error()
			}
			result.Data = rt.truncateToolResult(ctx, sessionID, planStep.Tool, result.Data)
			results = append(results, result)
			if result.Success {
				planStep.Status = sdk.StepStatusSucceeded
			} else {
				planStep.Status = sdk.StepStatusFailed
			}
			plan.Steps = append(plan.Steps, planStep)

			if (toolName == "conversation.echo" || toolName == "conversation.ask") && result.Success {
				if text, ok := result.Data["text"].(string); ok && strings.TrimSpace(text) != "" {
					_, _ = rt.appendMessage(ctx, sessionID, "assistant", text, nil)
					plan.Status = sdk.PlanStatusDone
					rt.updateLastTaskNote(sessionID, &plan, results)
					_ = rt.audit.Record(planReq, plan, results)
					_ = rt.session.Write(ctx, sessionID, rt.conversation.History(ctx, sessionID))
					return Response{Plan: plan, Results: results, Reply: text}, nil
				}
				if toolName == "conversation.ask" {
					questionText := formatAskQuestion(result.Data)
					if strings.TrimSpace(questionText) != "" {
						_, _ = rt.appendMessage(ctx, sessionID, "assistant", questionText, nil)
						plan.Status = sdk.PlanStatusDone
						rt.updateLastTaskNote(sessionID, &plan, results)
						_ = rt.audit.Record(planReq, plan, results)
						_ = rt.session.Write(ctx, sessionID, rt.conversation.History(ctx, sessionID))
						return Response{Plan: plan, Results: results, Reply: questionText}, nil
					}
				}
			}

			partStatus := "completed"
			if !result.Success || result.Error != "" {
				partStatus = "error"
			}
			toolPart := sdk.MessagePart{
				Type:   "tool",
				Tool:   toolName,
				CallID: call.ID,
				Input:  call.Arguments,
				Output: result.Data,
				Error:  result.Error,
				Status: partStatus,
			}
			_, _ = rt.appendMessage(ctx, sessionID, "assistant", fmt.Sprintf("Tool call: %s", toolName), []sdk.MessagePart{toolPart})

			baseMessages = append(baseMessages, map[string]any{
				"role":         "tool",
				"tool_call_id": call.ID,
				"content":      formatToolResultContent(result),
			})

			if !result.Success && result.Error != "" {
				recoveryPrompt := fmt.Sprintf(`The previous tool call '%s' failed with error: %s.

You have access to these internal tools with examples:
- fs.read({"path": "file.go"})
- fs.write({"path": "file.go", "content": "..."})
- fs.edit({"path": "file.go", "old_string": "old", "new_string": "new", "replace_all": false})
- fs.glob({"pattern": "*.go"})
- fs.grep({"query": "search term", "include": "*.go"})
- cmd.exec({"command": "ls -la", "timeout": 30})
- git.diff({})
- git.status({})
- web.fetch({"url": "https://..."})
- conversation.echo({"text": "your response to user"})

Please analyze the error and try an alternative approach. You can:
1. Try a different tool to achieve the same result
2. Fix the parameters and retry
3. Use cmd.exec for shell commands
4. Use fs tools to inspect/modify files directly`, toolName, result.Error)
				baseMessages = append(baseMessages, map[string]any{"role": "system", "content": recoveryPrompt})
			}

			rt.checkAndCompress(ctx, sessionID)
		}
	}

	plan.Status = sdk.PlanStatusBlocked
	return Response{Plan: plan, Results: results, Reply: ""}, fmt.Errorf("agent loop exceeded max steps")
}

func (rt *Runtime) buildAgentMessages(sessionID string) []map[string]any {
	var messages []map[string]any
	if systemPrompt := rt.systemPrompt(sessionID); systemPrompt != "" {
		messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	}
	messages = append(messages, map[string]any{"role": "system", "content": toolSystemPrompt})
	if summary := rt.conversation.Summary(sessionID); summary != "" {
		messages = append(messages, map[string]any{"role": "system", "content": summary})
	}

	history := rt.conversation.Messages(sessionID)
	start := 0
	if len(history) > maxHistoryTurns {
		start = len(history) - maxHistoryTurns
	}
	for _, msg := range history[start:] {
		if msg.Role == "system" {
			continue
		}
		if msg.Content == "" {
			continue
		}
		messages = append(messages, map[string]any{
			"role":    msg.Role,
			"content": msg.Content,
		})
	}
	return messages
}

const toolSystemPrompt = "You are a code assistant with tool access. Use tools to read files, search, run commands, and make changes. When tool use is required, emit tool calls instead of plain text. Only provide final answers after necessary tool calls complete."

func (rt *Runtime) systemPrompt(sessionID string) string {
	systemPrompt := rt.conversation.SystemPrompt()
	if rt.plugins != nil {
		systemPrompt = rt.plugins.ApplySystem(plugin.SystemContext{SessionID: sessionID}, systemPrompt)
	}
	return systemPrompt
}

func (rt *Runtime) collectToolSpecs(format *OutputFormat) ([]map[string]any, any, map[string]string, string) {
	var specs []map[string]any
	nameMap := map[string]string{}
	for _, tool := range rt.registry.All() {
		meta, ok := tool.(sdk.ToolSpec)
		if !ok {
			continue
		}
		llmName := normalizeToolName(meta.Name())
		nameMap[llmName] = meta.Name()
		specs = append(specs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        llmName,
				"description": meta.Describe(),
				"parameters":  meta.Schema(),
			},
		})
	}

	var toolChoice any
	structuredName := ""
	if format != nil && format.Type == "json_schema" && len(format.Schema) > 0 {
		structuredName = normalizeToolName("StructuredOutput")
		specs = append(specs, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        structuredName,
				"description": "Return the final response in the requested structured format.",
				"parameters":  format.Schema,
			},
		})
		toolChoice = map[string]any{
			"type":     "function",
			"function": map[string]any{"name": structuredName},
		}
	}
	return specs, toolChoice, nameMap, structuredName
}

func buildToolCallPayload(calls []toolCall) []map[string]any {
	var payload []map[string]any
	for _, call := range calls {
		args, _ := json.Marshal(call.Arguments)
		payload = append(payload, map[string]any{
			"id":   call.ID,
			"type": "function",
			"function": map[string]any{
				"name":      call.Name,
				"arguments": string(args),
			},
		})
	}
	return payload
}

func formatToolResultContent(result sdk.ToolResult) string {
	payload := map[string]any{
		"success": result.Success,
		"data":    result.Data,
		"error":   result.Error,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%v", payload)
	}
	return string(data)
}

func (rt *Runtime) callChatWithTools(ctx context.Context, messages []map[string]any, tools []map[string]any, toolChoice any) (chatResponse, error) {
	plannerCfg := rt.cfg.Planner
	if plannerCfg.Provider == "builtin" || plannerCfg.APIKey == "" {
		return chatResponse{}, fmt.Errorf("LLM provider not configured")
	}

	model := plannerCfg.Model
	if model == "" {
		model = defaultModel(plannerCfg.Provider)
	}

	payload := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": plannerCfg.Temperature,
	}
	if len(tools) > 0 {
		payload["tools"] = tools
		if toolChoice != nil {
			payload["tool_choice"] = toolChoice
		} else {
			payload["tool_choice"] = "auto"
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return chatResponse{}, err
	}

	endpoint := plannerCfg.Endpoint
	if endpoint == "" {
		endpoint = chatEndpoint(plannerCfg.Provider)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	switch plannerCfg.Provider {
	case "openai", "glm", "minmax", "deepseek":
		httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return chatResponse{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return chatResponse{}, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return chatResponse{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return chatResponse{}, err
	}
	if len(result.Choices) == 0 {
		return chatResponse{}, fmt.Errorf("no response from API")
	}

	choice := result.Choices[0]
	respOut := chatResponse{Content: choice.Message.Content, FinishReason: choice.FinishReason}
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		respOut.ToolCalls = append(respOut.ToolCalls, toolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}
	return respOut, nil
}

func extractStructuredOutput(calls []toolCall, structuredName string) (map[string]any, bool) {
	for _, call := range calls {
		if call.Name == structuredName {
			return call.Arguments, true
		}
	}
	return nil, false
}

func outputPart(output map[string]any) sdk.MessagePart {
	return sdk.MessagePart{
		Type:   "text",
		Text:   formatStructuredOutput(output),
		Status: "completed",
	}
}

func formatStructuredOutput(output map[string]any) string {
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", output)
	}
	return string(data)
}

var toolNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func normalizeToolName(name string) string {
	if name == "" {
		return name
	}
	clean := toolNamePattern.ReplaceAllString(name, "_")
	clean = strings.Trim(clean, "_")
	if clean == "" {
		return "tool"
	}
	return clean
}
