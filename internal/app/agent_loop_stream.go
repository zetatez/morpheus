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

	"go.uber.org/zap"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type replEmitter func(event string, data interface{}) error

// AgentLoopStream runs the shared agent runner and emits SSE-friendly events.
func (rt *Runtime) AgentLoopStream(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode, emit replEmitter) (Response, error) {
	var currentRunID string
	return rt.runAgentLoop(ctx, sessionID, input, format, mode, runnerCallbacks{
		emit:      emit,
		streaming: true,
		callChat:  rt.callChatWithToolsStream,
		onToolPending: func(toolName string, call toolCall) {
			_ = emit("tool_pending", map[string]any{"tool": toolName, "input": call.Arguments, "call_id": call.ID})
			if run, ok := rt.runs.get(currentRunID); ok {
				_ = rt.emitRunEvent(run, emit, "tool_pending", map[string]any{"tool": toolName, "call_id": call.ID, "input": call.Arguments})
			}
		},
		onToolResult: func(toolName string, callID string, step sdk.PlanStep, result sdk.ToolResult) {
			_ = emit("tool_result", map[string]any{"call_id": callID, "step": step, "result": result})
			if run, ok := rt.runs.get(currentRunID); ok {
				_ = rt.emitRunEvent(run, emit, "tool_result", map[string]any{"tool": toolName, "call_id": callID, "step": step, "result": result})
			}
		},
		onRunEvent: func(eventType string, data map[string]any) {
			if currentRunID == "" {
				if runID, ok := data["run_id"].(string); ok {
					currentRunID = runID
				}
			}
			if run, ok := rt.runs.get(currentRunID); ok {
				_ = rt.emitRunEvent(run, emit, eventType, data)
			}
		},
	})
}

func (rt *Runtime) callChatWithToolsStream(ctx context.Context, messages []map[string]any, tools []map[string]any, toolChoice any, emit replEmitter) (chatResponse, error) {
	rt.logger.Info("callChatWithToolsStream called", zap.Int("msgCount", len(messages)), zap.Int("toolsCount", len(tools)))
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
	if plannerCfg.Provider == "minimax" || plannerCfg.Provider == "minmax" {
		payload["max_tokens"] = 2048
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

	rt.logger.Info("MiniMax request",
		zap.String("endpoint", endpoint),
		zap.Any("payload", payload),
		zap.Any("provider", plannerCfg.Provider))

	switch plannerCfg.Provider {
	case "openai", "glm", "deepseek", "anthropic", "openrouter", "groq", "mistral", "togetherai", "perplexity":
		httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
	case "minimax", "minmax":
		httpReq.Header.Set("x-api-key", plannerCfg.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return chatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		rt.logger.Error("API error", zap.Int("status", resp.StatusCode), zap.String("body", string(respBody)))
		return chatResponse{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	rt.logger.Info("API response content-type", zap.String("contentType", ct))
	if strings.Contains(ct, "application/json") {
		return rt.callChatWithTools(ctx, messages, tools, toolChoice, emit)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var content strings.Builder
	toolNames := map[int]string{}
	toolIDs := map[int]string{}
	toolArgs := map[int]*strings.Builder{}
	finishReason := ""
	const maxToolArgBytes = 64 * 1024

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			rt.logger.Info("SSE stream [DONE] received")
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
			Usage struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		c := chunk.Choices[0]
		if c.FinishReason != "" && finishReason == "" {
			finishReason = c.FinishReason
			rt.logger.Info("Finish reason received", zap.String("reason", finishReason), zap.Int("toolNamesLen", len(toolNames)), zap.Int("toolArgsLen", len(toolArgs)))
			if rt.metrics != nil && chunk.Usage.TotalTokens > 0 {
				rt.metrics.addTokens(int64(chunk.Usage.PromptTokens), int64(chunk.Usage.CompletionTokens))
				rt.metrics.addCost(calculateCost(chunk.Usage.PromptTokens, chunk.Usage.CompletionTokens, model, plannerCfg.Provider))
			}
		}
		if c.Delta.Content != "" {
			content.WriteString(c.Delta.Content)
			if emit != nil {
				_ = emit("assistant_delta", map[string]string{"text": c.Delta.Content})
			}
		}
		for _, tc := range c.Delta.ToolCalls {
			if tc.Function.Name != "" {
				toolNames[tc.Index] = tc.Function.Name
				rt.logger.Info("Tool call name received", zap.String("name", tc.Function.Name), zap.Int("index", tc.Index))
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
				argLen := len(tc.Function.Arguments)
				b.WriteString(tc.Function.Arguments)
				if b.Len() > maxToolArgBytes {
					return chatResponse{}, fmt.Errorf("tool call arguments exceeded %d bytes for %s", maxToolArgBytes, toolNames[tc.Index])
				}
				rt.logger.Info("Tool call args received", zap.Int("index", tc.Index), zap.Int("argLen", argLen), zap.Int("totalLen", b.Len()))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return chatResponse{}, err
	}

	respOut := chatResponse{Content: content.String(), FinishReason: finishReason}
	rt.logger.Info("callChatWithToolsStream returning", zap.String("content", respOut.Content), zap.String("finishReason", respOut.FinishReason), zap.Int("toolNamesLen", len(toolNames)), zap.Int("toolIDsLen", len(toolIDs)), zap.Int("toolArgsLen", len(toolArgs)))
	if len(toolNames) > 0 || len(toolArgs) > 0 {
		rt.logger.Info("Building tool calls", zap.Any("toolNames", toolNames), zap.Any("toolIDs", toolIDs), zap.Any("toolArgsKeys", func() []int {
			r := []int{}
			for k := range toolArgs {
				r = append(r, k)
			}
			return r
		}()))
		for i := 0; i < 32; i++ {
			name, hasName := toolNames[i]
			argsBuf, hasArgs := toolArgs[i]
			if !hasName && !hasArgs {
				continue
			}
			rt.logger.Info("Processing tool call index", zap.Int("i", i), zap.String("name", name), zap.Bool("hasArgs", hasArgs))
			var args map[string]any
			if hasArgs {
				_ = json.Unmarshal([]byte(argsBuf.String()), &args)
			}
			respOut.ToolCalls = append(respOut.ToolCalls, toolCall{ID: toolIDs[i], Name: name, Arguments: args})
		}
	}
	rt.logger.Info("Final tool calls count", zap.Int("count", len(respOut.ToolCalls)))
	return respOut, nil
}
