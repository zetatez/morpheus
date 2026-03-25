package app

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type replEmitter func(event string, data interface{}) error

// AgentLoopStream runs the agent loop and emits SSE-friendly events.
// It always returns the final Response (even though the caller typically streams it).
func (rt *Runtime) AgentLoopStream(ctx context.Context, sessionID, input string, format *OutputFormat, emit replEmitter) (Response, error) {
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
	plan := sdk.Plan{Summary: input, Status: sdk.PlanStatusInProgress}
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
		resp, err := rt.callChatWithToolsStream(ctx, baseMessages, tools, toolChoice, emit)
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

			_ = emit("tool_pending", map[string]any{"tool": toolName, "input": call.Arguments, "call_id": call.ID})

			stepID := uuid.NewString()
			planStep := sdk.PlanStep{
				ID:          stepID,
				Description: fmt.Sprintf("Tool call: %s", toolName),
				Tool:        toolName,
				Inputs:      call.Arguments,
				Status:      sdk.StepStatusRunning,
			}
			result, execErr := rt.orchestrator.ExecuteStep(ctx, sessionID, planStep)
			result.StepID = stepID
			if execErr != nil {
				result.Success = false
				result.Error = execErr.Error()
			}
			result.Data = rt.truncateToolResult(ctx, sessionID, planStep.Tool, result.Data)
			results = append(results, result)
			if result.Success {
				planStep.Status = sdk.StepStatusSucceeded
			} else {
				planStep.Status = sdk.StepStatusFailed
			}
			plan.Steps = append(plan.Steps, planStep)

			_ = emit("tool_result", map[string]any{"call_id": call.ID, "step": planStep, "result": result})

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

Please analyze the error and try an alternative approach.`, toolName, result.Error)
				baseMessages = append(baseMessages, map[string]any{"role": "system", "content": recoveryPrompt})
			}

			rt.checkAndCompress(ctx, sessionID)
		}
	}

	plan.Status = sdk.PlanStatusBlocked
	return Response{Plan: plan, Results: results, Reply: ""}, fmt.Errorf("agent loop exceeded max steps")
}

func (rt *Runtime) callChatWithToolsStream(ctx context.Context, messages []map[string]any, tools []map[string]any, toolChoice any, emit replEmitter) (chatResponse, error) {
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
		"stream":      true,
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
	httpReq.Header.Set("Accept", "text/event-stream")

	switch plannerCfg.Provider {
	case "openai", "glm", "minmax", "deepseek":
		httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return chatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return chatResponse{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	// Some providers ignore stream=true and return a normal JSON response.
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(ct, "application/json") {
		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return chatResponse{}, err
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
		if choice.Message.Content != "" {
			_ = emit("assistant_delta", map[string]string{"text": choice.Message.Content})
		}
		out := chatResponse{Content: choice.Message.Content, FinishReason: choice.FinishReason}
		for _, tc := range choice.Message.ToolCalls {
			var args map[string]any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			out.ToolCalls = append(out.ToolCalls, toolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: args})
		}
		return out, nil
	}

	// OpenAI-compatible SSE: lines like "data: {json}" and "data: [DONE]".
	scanner := bufio.NewScanner(resp.Body)
	// Allow long lines for tool call argument chunks.
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var content strings.Builder
	toolNames := map[int]string{}
	toolIDs := map[int]string{}
	toolArgs := map[int]*strings.Builder{}
	finishReason := ""

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Type     string `json:"type"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		c := chunk.Choices[0]
		if c.FinishReason != "" {
			finishReason = c.FinishReason
		}
		if c.Delta.Content != "" {
			content.WriteString(c.Delta.Content)
			_ = emit("assistant_delta", map[string]string{"text": c.Delta.Content})
		}
		for _, tc := range c.Delta.ToolCalls {
			if tc.Function.Name != "" {
				toolNames[tc.Index] = tc.Function.Name
			}
			if tc.ID != "" {
				toolIDs[tc.Index] = tc.ID
			}
			if tc.Function.Arguments != "" {
				b, ok := toolArgs[tc.Index]
				if !ok {
					b = &strings.Builder{}
					toolArgs[tc.Index] = b
				}
				b.WriteString(tc.Function.Arguments)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return chatResponse{}, err
	}

	respOut := chatResponse{Content: content.String(), FinishReason: finishReason}
	if len(toolNames) > 0 || len(toolArgs) > 0 {
		for i := 0; i < 32; i++ {
			name, hasName := toolNames[i]
			argsBuf, hasArgs := toolArgs[i]
			if !hasName && !hasArgs {
				continue
			}
			var args map[string]any
			if hasArgs {
				_ = json.Unmarshal([]byte(argsBuf.String()), &args)
			}
			respOut.ToolCalls = append(respOut.ToolCalls, toolCall{
				ID:        toolIDs[i],
				Name:      name,
				Arguments: args,
			})
		}
	}
	return respOut, nil
}
