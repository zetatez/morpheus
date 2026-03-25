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
	"time"

	"go.uber.org/zap"

	"github.com/zetatez/morpheus/internal/planner/llm"
	"github.com/zetatez/morpheus/pkg/sdk"
)

var streamingClient = &http.Client{
	Transport: &streamingTransport{},
	Timeout:   llmAPITimeout,
}

type streamingTransport struct{}

func (t *streamingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	resp.Header.Set("X-Content-Type-Options", "nosniff")
	return resp, nil
}

type replEmitter func(event string, data interface{}) error

var defaultEvaluator = sdk.DefaultToolEvaluator()

// AgentLoopStream runs the shared agent runner and emits SSE-friendly events.
// For thinking/summary output, SSE streaming is used for better UX.
// For tool calls, non-streaming is used to ensure JSON validity.
func (rt *Runtime) AgentLoopStream(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode, emit replEmitter) (Response, error) {
	return rt.AgentLoopStreamV2(ctx, sessionID, input, format, mode, emit)
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

	profile := llm.DetectProviderProfile(plannerCfg.Provider, model)

	temp := plannerCfg.Temperature
	if temp <= 0 {
		temp = profile.DefaultTemperature
		if temp <= 0 {
			temp = 0.7
		}
	}

	cleanedMessages, _ := profile.BuildMessages(messages)
	payload, _ := profile.BuildPayload(model, cleanedMessages, tools, int(temp), profile.DefaultMaxTokens)
	payload["stream"] = true

	body, err := json.Marshal(payload)
	if err != nil {
		return chatResponse{}, err
	}

	endpoint := profile.GetEndpoint(plannerCfg.Endpoint)

	rt.logger.Info("callChatWithToolsStream request", zap.String("provider", plannerCfg.Provider), zap.String("model", model), zap.String("endpoint", endpoint), zap.Int("toolsCount", len(tools)), zap.Int("msgCount", len(messages)), zap.String("body", string(body)), zap.Any("temp", temp))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	if profile.UseAnthropicFormat && !profile.UseOpenAIFormat {
		httpReq.Header.Set("x-api-key", plannerCfg.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	} else {
		switch plannerCfg.Provider {
		case "openai", "glm", "deepseek", "anthropic", "openrouter", "groq", "mistral", "togetherai", "perplexity", "minimax":
			httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
		}
	}

	resp, err := streamingClient.Do(httpReq)
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
	rt.logger.Info("API response headers received", zap.String("contentType", ct), zap.String("provider", plannerCfg.Provider))
	streamStart := time.Now()
	var eventCount int
	if strings.Contains(ct, "application/json") {
		return rt.callChatWithTools(ctx, messages, tools, toolChoice, emit)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)

	var content strings.Builder
	var reasoningContent strings.Builder
	toolNames := map[int]string{}
	toolIDs := map[int]string{}
	toolArgs := map[int]*strings.Builder{}
	toolInputStarted := map[int]bool{}
	reasoningStarted := false
	finishReason := ""
	const maxToolArgBytes = 64 * 1024

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		evt, err := profile.ParseStreamLine(line)
		if err != nil {
			continue
		}
		if evt == nil {
			continue
		}

		if evt.Type == "done" {
			rt.logger.Info("SSE stream [DONE] received")
			for idx := range toolNames {
				if emit != nil {
					_ = emit("tool_input_end", map[string]any{"index": idx, "tool": toolNames[idx], "call_id": toolIDs[idx]})
				}
			}
			if reasoningStarted && emit != nil {
				_ = emit("reasoning_end", map[string]any{"text": reasoningContent.String()})
			}
			break
		}

		if evt.Type == "reasoning_start" {
			reasoningStarted = true
			reasoningContent.Reset()
			rt.logger.Info("Reasoning start", zap.String("id", evt.ReasoningID))
			if emit != nil {
				_ = emit("reasoning_start", map[string]any{"id": evt.ReasoningID})
			}
		} else if evt.Type == "reasoning_delta" {
			reasoningContent.WriteString(evt.Text)
			if emit != nil {
				_ = emit("reasoning_delta", map[string]any{"id": evt.ReasoningID, "text": evt.Text})
			}
		} else if evt.Type == "text_start" {
			if emit != nil {
				_ = emit("text_start", map[string]any{"index": evt.Index})
			}
		} else if evt.Type == "text_end" {
			if emit != nil {
				_ = emit("text_end", map[string]any{"index": evt.Index})
			}
		} else if evt.Type == "content_delta" {
			content.WriteString(evt.Text)
			if emit != nil {
				_ = emit("assistant_delta", map[string]string{"text": evt.Text})
			}
		} else if evt.Type == "tool_call" {
			toolNames[evt.Index] = evt.ToolName
			toolIDs[evt.Index] = evt.ToolID
			rt.logger.Info("Tool call name received", zap.String("name", evt.ToolName), zap.Int("index", evt.Index))
			if emit != nil {
				_ = emit("tool_input_start", map[string]any{"index": evt.Index, "tool": evt.ToolName, "call_id": evt.ToolID})
				toolInputStarted[evt.Index] = true
			}
		} else if evt.Type == "tool_call_delta" {
			b, ok := toolArgs[evt.Index]
			if !ok {
				b = &strings.Builder{}
				toolArgs[evt.Index] = b
				if emit != nil && !toolInputStarted[evt.Index] {
					_ = emit("tool_input_start", map[string]any{"index": evt.Index, "tool": toolNames[evt.Index], "call_id": toolIDs[evt.Index]})
					toolInputStarted[evt.Index] = true
				}
			}
			argLen := len(evt.ToolArgs)
			b.WriteString(evt.ToolArgs)
			if emit != nil {
				_ = emit("tool_input_delta", map[string]any{"index": evt.Index, "tool": toolNames[evt.Index], "delta": evt.ToolArgs})
			}
			if b.Len() > maxToolArgBytes {
				return chatResponse{}, fmt.Errorf("tool call arguments exceeded %d bytes for %s", maxToolArgBytes, toolNames[evt.Index])
			}
			rt.logger.Info("Tool call args received", zap.Int("index", evt.Index), zap.Int("argLen", argLen), zap.Int("totalLen", b.Len()))
		}

		if evt.FinishReason != "" && finishReason == "" {
			finishReason = evt.FinishReason
			rt.logger.Info("Finish reason received", zap.String("reason", finishReason), zap.Int("toolNamesLen", len(toolNames)), zap.Int("toolArgsLen", len(toolArgs)))
		}
	}
	if err := scanner.Err(); err != nil {
		return chatResponse{}, err
	}

	respOut := chatResponse{Content: content.String(), FinishReason: finishReason}
	rt.logger.Info("callChatWithToolsStream returning", zap.Duration("streamDuration", time.Since(streamStart)), zap.Int("eventCount", eventCount), zap.String("contentLength", fmt.Sprintf("%d", len(respOut.Content))), zap.String("finishReason", respOut.FinishReason), zap.Int("toolNamesLen", len(toolNames)), zap.Int("toolIDsLen", len(toolIDs)), zap.Int("toolArgsLen", len(toolArgs)))
	if len(toolNames) > 0 || len(toolArgs) > 0 {
		rt.logger.Info("Building tool calls from SSE events", zap.Any("toolNames", toolNames), zap.Any("toolIDs", toolIDs), zap.Any("toolArgsKeys", func() []int {
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
			if name == "" {
				rt.logger.Warn("Skipping tool call with empty name", zap.Int("index", i), zap.Bool("hasArgs", hasArgs))
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
	rt.logger.Info("Final tool calls count", zap.Int("count", len(respOut.ToolCalls)), zap.String("model", model), zap.Bool("hasTextToolCallRegex", profile.TextToolCallRegex != ""), zap.Bool("requiresTextToolCalls", profile.RequiresTextToolCalls))

	validToolCalls := 0
	hasValidName := false
	for _, tc := range respOut.ToolCalls {
		if tc.ID != "" && tc.Name != "" {
			validToolCalls++
			hasValidName = true
		}
	}

	shouldTryTextParsing := !hasValidName && (profile.TextToolCallRegex != "" || profile.RequiresTextToolCalls)
	if shouldTryTextParsing && respOut.Content != "" {
		rt.logger.Info("No valid tool calls from SSE events, trying text parsing", zap.String("contentPreview", func() string {
			if len(respOut.Content) > 200 {
				return respOut.Content[:200] + "..."
			}
			return respOut.Content
		}()))
		textCalls := profile.ParseTextToolCalls(respOut.Content)
		rt.logger.Info("ParseTextToolCalls result", zap.Int("count", len(textCalls)))
		if len(textCalls) > 0 {
			rt.logger.Info("Parsed text tool calls", zap.Int("count", len(textCalls)), zap.Any("calls", textCalls))
			var tc []toolCall
			for _, t := range textCalls {
				normalizedName := sdk.NormalizeToolName(t.Name)
				rt.logger.Info("Text tool call parsed", zap.String("id", t.ID), zap.String("name", t.Name), zap.String("normalized", normalizedName))
				tc = append(tc, toolCall{ID: t.ID, Name: normalizedName, Arguments: t.Arguments})
			}
			respOut.ToolCalls = tc
			respOut.FinishReason = "tool_calls"
		} else {
			rt.logger.Warn("No text tool calls found in content")
		}
	}
	return respOut, nil
}
