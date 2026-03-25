package llm

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type ProviderProfile struct {
	Name                  string
	AcceptsSystemMessages bool
	SupportsAPITools      bool
	RequiresTextToolCalls bool
	UseAnthropicFormat    bool
	UseOpenAIFormat       bool
	DefaultMaxTokens      int
	DefaultTemperature    float64
	TextToolCallRegex     string
	Endpoint              string
	MergeSystemMessages   bool
}

func DetectProviderProfile(provider, model string) ProviderProfile {
	switch provider {
	case "openai", "deepseek", "glm", "openrouter", "groq", "mistral", "togetherai", "perplexity", "ollama", "cohere":
		return ProviderProfile{
			Name:                  provider,
			AcceptsSystemMessages: true,
			SupportsAPITools:      true,
			RequiresTextToolCalls: false,
			UseAnthropicFormat:    false,
			UseOpenAIFormat:       true,
			DefaultMaxTokens:      4096,
			DefaultTemperature:    0.7,
		}
	case "anthropic":
		return ProviderProfile{
			Name:                  provider,
			AcceptsSystemMessages: true,
			SupportsAPITools:      true,
			RequiresTextToolCalls: false,
			UseAnthropicFormat:    true,
			UseOpenAIFormat:       false,
			DefaultMaxTokens:      4096,
			DefaultTemperature:    0.7,
		}
	case "minimax":
		return ProviderProfile{
			Name:                  provider,
			AcceptsSystemMessages: true,
			SupportsAPITools:      true,
			RequiresTextToolCalls: false,
			UseAnthropicFormat:    false,
			UseOpenAIFormat:       true,
			DefaultMaxTokens:      1024,
			DefaultTemperature:    1.0,
			Endpoint:              "https://api.minimaxi.com/v1/chat/completions",
			MergeSystemMessages:   true,
		}
	case "azure":
		return ProviderProfile{
			Name:                  provider,
			AcceptsSystemMessages: true,
			SupportsAPITools:      true,
			RequiresTextToolCalls: false,
			UseAnthropicFormat:    false,
			UseOpenAIFormat:       true,
			DefaultMaxTokens:      4096,
			DefaultTemperature:    0.7,
		}
	case "builtin", "keyword":
		return ProviderProfile{
			Name:                  provider,
			AcceptsSystemMessages: true,
			SupportsAPITools:      false,
			RequiresTextToolCalls: false,
			UseAnthropicFormat:    false,
			UseOpenAIFormat:       false,
			DefaultMaxTokens:      4096,
			DefaultTemperature:    0.7,
		}
	default:
		return ProviderProfile{
			Name:                  provider,
			AcceptsSystemMessages: true,
			SupportsAPITools:      true,
			RequiresTextToolCalls: false,
			UseAnthropicFormat:    false,
			UseOpenAIFormat:       true,
			DefaultMaxTokens:      4096,
			DefaultTemperature:    0.7,
		}
	}
}

func (p ProviderProfile) NeedsToolTextInPrompt() bool {
	return p.RequiresTextToolCalls
}

func (p ProviderProfile) ShouldSendToolsInAPI() bool {
	return p.SupportsAPITools && !p.RequiresTextToolCalls
}

func (p ProviderProfile) IsAnthropicFormat() bool {
	return p.UseAnthropicFormat && !p.UseOpenAIFormat
}

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ToolDef struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

func (p ProviderProfile) BuildMessages(messages []map[string]any) ([]map[string]any, string) {
	if p.AcceptsSystemMessages && !p.MergeSystemMessages {
		return messages, ""
	}

	var systemContent string
	var cleaned []map[string]any
	var lastUser string

	for i, msg := range messages {
		role, _ := msg["role"].(string)
		content, _ := msg["content"].(string)

		if role == "system" {
			systemContent += "\n" + content
			continue
		}
		if i > 0 && role == "user" && content == lastUser {
			continue
		}
		cleaned = append(cleaned, msg)
		if role == "user" {
			lastUser = content
		}
	}

	if systemContent != "" && len(cleaned) > 0 {
		firstMsg := cleaned[0]
		if firstMsg["role"] == "user" {
			cleaned[0] = map[string]any{
				"role":    "user",
				"content": strings.TrimSpace(systemContent) + "\n\n" + firstMsg["content"].(string),
			}
		}
	}

	return cleaned, systemContent
}

func (p ProviderProfile) BuildPayload(model string, messages []map[string]any, tools []map[string]any, temperature, maxTokens int) (map[string]any, string) {
	payload := map[string]any{
		"model":    model,
		"messages": messages,
	}

	if temperature > 0 {
		payload["temperature"] = float64(temperature)
	} else {
		payload["temperature"] = p.DefaultTemperature
	}

	if p.IsAnthropicFormat() {
		if p.UseOpenAIFormat {
			payload["tokens_to_generate"] = maxTokens
		} else {
			payload["max_tokens"] = maxTokens
		}
	} else if maxTokens > 0 {
		payload["max_tokens"] = maxTokens
	}

	var toolPrompt string
	if len(tools) > 0 && p.NeedsToolTextInPrompt() {
		toolPrompt = p.BuildToolTextPrompt(tools)
		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			if lastMsg["role"] == "user" {
				messages[len(messages)-1] = map[string]any{
					"role":    "user",
					"content": lastMsg["content"].(string) + toolPrompt,
				}
			}
		}
	}

	if len(tools) > 0 && p.ShouldSendToolsInAPI() {
		payload["tools"] = tools
		payload["tool_choice"] = "auto"
	}

	return payload, toolPrompt
}

func (p ProviderProfile) BuildToolTextPrompt(tools []map[string]any) string {
	var b strings.Builder
	b.WriteString("\n\n## TOOL CALLING (CRITICAL)\n")
	b.WriteString("When you need to use a tool, you MUST respond with ONLY this exact JSON format:\n")
	b.WriteString("{\"tool_calls\":[{\"name\":\"TOOL_NAME\",\"arguments\":{\"param\":\"value\"}}]}\n")
	b.WriteString("Do NOT write anything else. Do NOT use markdown. The response must be valid JSON starting with `{`.\n\n")
	b.WriteString("Available tools:\n")
	for _, tool := range tools {
		if fn, ok := tool["function"].(map[string]any); ok {
			name, _ := fn["name"].(string)
			desc, _ := fn["description"].(string)
			params, _ := fn["parameters"].(map[string]any)
			b.WriteString(fmt.Sprintf("- %s: %s\n", name, desc))
			if props, ok := params["properties"].(map[string]any); ok {
				for pname, pdesc := range props {
					if pd, ok := pdesc.(map[string]any); ok {
						ptype, _ := pd["type"].(string)
						b.WriteString(fmt.Sprintf("  - %s (%s)\n", pname, ptype))
					}
				}
			}
		}
	}

	b.WriteString("\n## EXAMPLES\n")
	b.WriteString("Weather: {\"tool_calls\":[{\"name\":\"web_fetch\",\"arguments\":{\"url\":\"https://wttr.in/Shanghai?format=3\"}}]}\n")
	b.WriteString("Read file: {\"tool_calls\":[{\"name\":\"fs_read\",\"arguments\":{\"path\":\"/path/file\",\"offset\":0,\"limit\":100}}]}\n")
	b.WriteString("Run command: {\"tool_calls\":[{\"name\":\"cmd_exec\",\"arguments\":{\"command\":\"ls -la\"}}]}\n")
	b.WriteString("If no tool needed, respond with plain text.\n")

	return b.String()
}

type ToolCallResult struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type ParsedResponse struct {
	Content      string
	ToolCalls    []ToolCallResult
	FinishReason string
	Usage        TokenUsage
}

type TokenUsage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

func (p ProviderProfile) ParseResponse(body []byte) (*ParsedResponse, error) {
	if p.IsAnthropicFormat() && !p.UseOpenAIFormat {
		return p.parseAnthropicResponse(body)
	}
	return p.parseOpenAIResponse(body)
}

func (p ProviderProfile) parseAnthropicResponse(body []byte) (*ParsedResponse, error) {
	var raw struct {
		Type    string `json:"type"`
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			ID    string `json:"id"`
			Name  string `json:"name"`
			Input any    `json:"input"`
		} `json:"content"`
		StopReason   string `json:"stop_reason"`
		StopSequence string `json:"stop_sequence"`
		Usage        struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	resp := &ParsedResponse{
		FinishReason: raw.StopReason,
		Usage: TokenUsage{
			PromptTokens:     raw.Usage.InputTokens,
			CompletionTokens: raw.Usage.OutputTokens,
			TotalTokens:      raw.Usage.InputTokens + raw.Usage.OutputTokens,
		},
	}

	for _, c := range raw.Content {
		if c.Type == "text" {
			resp.Content += c.Text
		} else if c.Type == "tool_use" {
			args, _ := json.Marshal(c.Input)
			var argsMap map[string]any
			json.Unmarshal(args, &argsMap)
			resp.ToolCalls = append(resp.ToolCalls, ToolCallResult{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: argsMap,
			})
		}
	}

	return resp, nil
}

func (p ProviderProfile) parseOpenAIResponse(body []byte) (*ParsedResponse, error) {
	var raw struct {
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
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	if len(raw.Choices) == 0 {
		return &ParsedResponse{}, nil
	}

	choice := raw.Choices[0]
	resp := &ParsedResponse{
		Content:      choice.Message.Content,
		FinishReason: choice.FinishReason,
		Usage: TokenUsage{
			PromptTokens:     raw.Usage.PromptTokens,
			CompletionTokens: raw.Usage.CompletionTokens,
			TotalTokens:      raw.Usage.TotalTokens,
		},
	}

	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		json.Unmarshal([]byte(tc.Function.Arguments), &args)
		resp.ToolCalls = append(resp.ToolCalls, ToolCallResult{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: args,
		})
	}

	return resp, nil
}

type StreamEvent struct {
	Type          string
	Index         int
	Text          string
	ToolName      string
	ToolID        string
	ToolArgs      string
	FinishReason  string
	Error         string
	ReasoningID   string
	ProviderMeta  any
	HasInputStart bool
}

func (p ProviderProfile) ParseStreamLine(line string) (*StreamEvent, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "event:") {
		return nil, nil
	}
	if !strings.HasPrefix(line, "data:") {
		return nil, nil
	}
	data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if data == "[DONE]" {
		return &StreamEvent{Type: "done"}, nil
	}

	if p.IsAnthropicFormat() && !p.UseOpenAIFormat {
		return p.parseAnthropicStreamEvent(data)
	}
	return p.parseOpenAIStreamEvent(data)
}

func (p ProviderProfile) parseAnthropicStreamEvent(data string) (*StreamEvent, error) {
	if !strings.Contains(data, `"type"`) {
		return nil, nil
	}

	var base struct {
		Type  string `json:"type"`
		Index int    `json:"index"`
	}
	if err := json.Unmarshal([]byte(data), &base); err != nil {
		return nil, err
	}

	if base.Type == "content_block_start" {
		var event struct {
			Index        int `json:"index"`
			ContentBlock struct {
				Type string `json:"type"`
				Name string `json:"name,omitempty"`
			} `json:"content_block"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, err
		}
		if event.ContentBlock.Type == "thinking" {
			return &StreamEvent{
				Type:        "reasoning_start",
				Index:       event.Index,
				ReasoningID: fmt.Sprintf("reasoning_%d", event.Index),
			}, nil
		}
		if event.ContentBlock.Type == "text" {
			return &StreamEvent{
				Type:  "text_start",
				Index: event.Index,
			}, nil
		}
		return nil, nil
	}

	if base.Type == "content_block_delta" {
		var event struct {
			Index int `json:"index"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, err
		}
		if event.Delta.Type == "text_delta" {
			return &StreamEvent{
				Type:  "content_delta",
				Index: event.Index,
				Text:  event.Delta.Text,
			}, nil
		}
		if event.Delta.Type == "thinking_delta" {
			return &StreamEvent{
				Type:        "reasoning_delta",
				Index:       event.Index,
				ReasoningID: fmt.Sprintf("reasoning_%d", event.Index),
				Text:        event.Delta.Text,
			}, nil
		}
		return nil, nil
	}

	if base.Type == "content_block_stop" {
		return &StreamEvent{
			Type:  "text_end",
			Index: base.Index,
		}, nil
	}

	if base.Type == "message_delta" {
		var event struct {
			Delta struct {
				StopReason string `json:"stop_reason,omitempty"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, err
		}
		if event.Delta.StopReason != "" {
			return &StreamEvent{
				Type:         "reasoning_end",
				FinishReason: event.Delta.StopReason,
			}, nil
		}
	}

	if strings.Contains(data, `"text_delta"`) {
		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"delta"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return nil, err
		}
		if event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			return &StreamEvent{
				Type:  "content_delta",
				Index: event.Index,
				Text:  event.Delta.Text,
			}, nil
		}
	}

	return nil, nil
}

func (p ProviderProfile) parseOpenAIStreamEvent(data string) (*StreamEvent, error) {
	var chunk struct {
		Choices []struct {
			Index int `json:"index"`
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
		return nil, err
	}

	if len(chunk.Choices) == 0 {
		return nil, nil
	}

	choice := chunk.Choices[0]
	evt := &StreamEvent{
		Index:        choice.Index,
		FinishReason: choice.FinishReason,
	}

	if choice.Delta.Content != "" {
		evt.Type = "content_delta"
		evt.Text = choice.Delta.Content
	}

	for _, tc := range choice.Delta.ToolCalls {
		evt.Type = "tool_call"
		evt.ToolName = tc.Function.Name
		evt.ToolID = tc.ID
		evt.Index = tc.Index
		if tc.Function.Arguments != "" {
			evt.Type = "tool_call_delta"
			evt.ToolArgs = tc.Function.Arguments
		}
	}

	return evt, nil
}

func (p ProviderProfile) ParseTextToolCalls(content string) []ToolCallResult {
	// If no text tool call support configured, skip
	if p.TextToolCallRegex == "" && !strings.Contains(content, "tool_calls") {
		// Still try to find JSON patterns even without explicit config
		if !strings.Contains(content, "{\"tool") && !strings.Contains(content, "{'tool") {
			return nil
		}
	}

	normalizedContent := normalizeToolCallJSON(content)

	// Try multiple extraction strategies
	var calls []ToolCallResult

	// Strategy 1: Use configured regex if available
	if p.TextToolCallRegex != "" {
		if calls = p.extractWithNestedRegex(normalizedContent, p.TextToolCallRegex); len(calls) > 0 {
			return calls
		}
	}

	// Strategy 2: Try to find tool_calls anywhere in content with nested JSON support
	if calls = p.extractToolCallsFromText(normalizedContent); len(calls) > 0 {
		return calls
	}

	// Strategy 3: Look for common tool patterns and extract
	if calls = p.extractFromToolPatterns(normalizedContent); len(calls) > 0 {
		return calls
	}

	// Strategy 4: Try JSON object parsing as fallback
	return p.parseToolCallsFromJSONObject(normalizedContent)
}

// extractWithNestedRegex handles nested JSON structures
func (p ProviderProfile) extractWithNestedRegex(content, regexPattern string) []ToolCallResult {
	// Use a more permissive pattern that handles nested braces
	// Original: \{[^}]*tool_calls[^}]*\}
	// Improved: matches balanced braces around tool_calls
	improvedPattern := `\{[^{}]*(?:\{[^{}]*\}[^{}]*)*tool_calls[^{}]*(?:\{[^{}]*\}[^{}]*)*\}`

	regex := regexp.MustCompile(improvedPattern)
	matches := regex.FindAllString(content, -1)

	for _, match := range matches {
		if calls := p.parseToolCallsFromJSON(content, match); len(calls) > 0 {
			return calls
		}
	}

	// Fallback to original pattern
	originalRegex := regexp.MustCompile(regexPattern)
	if matches = originalRegex.FindAllString(content, -1); len(matches) > 0 {
		if calls := p.parseToolCallsFromJSON(content, matches[0]); len(calls) > 0 {
			return calls
		}
	}

	return nil
}

// extractToolCallsFromText extracts tool_calls from anywhere in text
func (p ProviderProfile) extractToolCallsFromText(content string) []ToolCallResult {
	// Find the position of "tool_calls" in the content
	toolCallsIdx := strings.Index(content, "tool_calls")
	if toolCallsIdx == -1 {
		// Try variations
		toolCallsIdx = strings.Index(content, "tool calls")
		if toolCallsIdx == -1 {
			toolCallsIdx = strings.Index(content, "tool-calls")
		}
	}
	if toolCallsIdx == -1 {
		return nil
	}

	// Find the JSON object containing tool_calls
	// Search backwards for the opening brace
	start := toolCallsIdx
	for start > 0 && content[start] != '{' {
		start--
	}
	if start == 0 {
		return nil
	}

	// Find the closing brace by counting braces
	braceCount := 0
	end := start
	for end < len(content) {
		if content[end] == '{' {
			braceCount++
		} else if content[end] == '}' {
			braceCount--
			if braceCount == 0 {
				end++
				break
			}
		}
		end++
	}

	if braceCount != 0 {
		return nil
	}

	jsonStr := content[start:end]
	return p.parseToolCallsFromJSON(content, jsonStr)
}

// extractFromToolPatterns looks for common tool call patterns
func (p ProviderProfile) extractFromToolPatterns(content string) []ToolCallResult {
	// Pattern 1: Look for {"name": "tool_name", "arguments": {...}}
	pattern1 := regexp.MustCompile(`\{\s*"name"\s*:\s*"([a-zA-Z_][a-zA-Z0-9_]*)"[^}]*"arguments"\s*:\s*(\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\})`)

	// Pattern 2: Look for tool_name followed by arguments
	pattern2 := regexp.MustCompile(`"([a-zA-Z_][a-zA-Z0-9_]*)"\s*:\s*(\{[^{}]*(?:\{[^{}]*\}[^{}]*)*\})`)

	var calls []ToolCallResult

	// Try pattern 1
	matches1 := pattern1.FindAllStringSubmatchIndex(content, -1)
	for _, match := range matches1 {
		if len(match) >= 6 {
			name := content[match[2]:match[3]]
			argsStr := content[match[4]:match[5]]
			var args map[string]any
			if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
				name = sdk.NormalizeToolName(name)
				calls = append(calls, ToolCallResult{
					ID:        fmt.Sprintf("call_%d", len(calls)),
					Name:      name,
					Arguments: args,
				})
			}
		}
	}

	// Try pattern 2 - look for tool names followed by arguments
	matches2 := pattern2.FindAllStringSubmatchIndex(content, -1)
	knownTools := []string{"web_fetch", "cmd_exec", "fs_read", "fs_write", "fs_edit", "fs_glob", "fs_grep", "mcp_query", "lsp_query", "todo_write", "skill_invoke", "agent_run", "conversation_ask"}

	for _, match := range matches2 {
		if len(match) >= 4 {
			name := content[match[2]:match[3]]
			argsStr := content[match[4]:match[5]]

			// Only accept if it looks like a known tool or has arguments
			isKnownTool := false
			for _, tool := range knownTools {
				if strings.Contains(strings.ToLower(name), tool) {
					isKnownTool = true
					break
				}
			}

			if !isKnownTool {
				continue
			}

			var args map[string]any
			if err := json.Unmarshal([]byte(argsStr), &args); err == nil {
				name = sdk.NormalizeToolName(name)
				calls = append(calls, ToolCallResult{
					ID:        fmt.Sprintf("call_%d", len(calls)),
					Name:      name,
					Arguments: args,
				})
			}
		}
	}

	return calls
}

func normalizeToolCallJSON(content string) string {
	cleaned := strings.ReplaceAll(content, "\r\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\r", " ")
	cleaned = strings.ReplaceAll(cleaned, "\t", " ")

	cleaned = strings.ReplaceAll(cleaned, "tool calls", "tool_calls")
	cleaned = strings.ReplaceAll(cleaned, "web fetch", "web_fetch")

	cleaned = strings.ReplaceAll(cleaned, "\\n", "_")
	cleaned = strings.ReplaceAll(cleaned, "\\r", "_")
	cleaned = strings.ReplaceAll(cleaned, "\\\\n", "_")
	cleaned = strings.ReplaceAll(cleaned, `"`, "\"")

	return cleaned
}

func (p ProviderProfile) parseToolCallsFromJSON(content, jsonStr string) []ToolCallResult {
	var result struct {
		ToolCalls []struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"tool_calls"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil
	}

	var calls []ToolCallResult
	for i, tc := range result.ToolCalls {
		name := sdk.NormalizeToolName(tc.Name)
		var args map[string]any
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			args = map[string]any{"raw": string(tc.Arguments)}
		}
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		calls = append(calls, ToolCallResult{
			ID:        id,
			Name:      name,
			Arguments: args,
		})
	}
	return calls
}

func (p ProviderProfile) parseToolCallsFromJSONObject(content string) []ToolCallResult {
	searchTerms := []string{`"tool_calls"`, `"tool_calls"`}

	var jsonStart, jsonEnd int
	found := false

	for _, term := range searchTerms {
		idx := strings.Index(content, term)
		if idx == -1 {
			continue
		}
		start := strings.LastIndex(content[:idx], "{")
		if start == -1 {
			continue
		}

		braceCount := 0
		end := start
		for i := start; i < len(content); i++ {
			if content[i] == '{' {
				braceCount++
			} else if content[i] == '}' {
				braceCount--
				if braceCount == 0 {
					end = i + 1
					found = true
					jsonStart = start
					jsonEnd = end
					break
				}
			}
		}
		if found {
			break
		}
	}

	if !found {
		toolIdx := strings.Index(content, `"tool`)
		if toolIdx == -1 {
			return nil
		}
		start := toolIdx
		for start > 0 && content[start-1] != '\n' && content[start-1] != '{' {
			start--
		}
		jsonStart = start
		jsonEnd = len(content)
	}

	jsonStr := content[jsonStart:jsonEnd]

	if strings.Contains(jsonStr, `"tool_calls"`) {
		var result struct {
			ToolCalls []struct {
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
			return p.parseToolCallsRaw(jsonStr)
		}

		var calls []ToolCallResult
		for i, tc := range result.ToolCalls {
			name := sdk.NormalizeToolName(tc.Name)
			var args map[string]any
			if err := json.Unmarshal(tc.Arguments, &args); err != nil {
				args = map[string]any{"raw": string(tc.Arguments)}
			}
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", i)
			}
			calls = append(calls, ToolCallResult{
				ID:        id,
				Name:      name,
				Arguments: args,
			})
		}
		return calls
	}

	return p.parseToolCallsRaw(jsonStr)
}

func (p ProviderProfile) parseToolCallsRaw(content string) []ToolCallResult {
	toolPattern := regexp.MustCompile(`"name"\s*:\s*"([^"]+)"`)
	argPattern := regexp.MustCompile(`"arguments"\s*:\s*(\{[^}]*(?:\{[^}]*\}[^}]*)*\})`)

	nameMatches := toolPattern.FindAllStringSubmatchIndex(content, -1)
	argMatches := argPattern.FindAllStringSubmatchIndex(content, -1)

	if len(nameMatches) == 0 {
		return nil
	}

	var calls []ToolCallResult
	minLen := len(nameMatches)
	if len(argMatches) < minLen {
		minLen = len(argMatches)
	}

	for i := 0; i < minLen; i++ {
		nameStart := nameMatches[i][2]
		nameEnd := nameMatches[i][3]
		name := content[nameStart:nameEnd]
		name = sdk.NormalizeToolName(name)

		argStart := argMatches[i][2]
		argEnd := argMatches[i][3]
		argStr := content[argStart:argEnd]

		var args map[string]any
		if err := json.Unmarshal([]byte(argStr), &args); err != nil {
			args = map[string]any{"raw": argStr}
		}

		id := fmt.Sprintf("call_%d", i)
		calls = append(calls, ToolCallResult{
			ID:        id,
			Name:      name,
			Arguments: args,
		})
	}

	return calls
}

func (p ProviderProfile) GetEndpoint(customEndpoint string) string {
	if customEndpoint != "" {
		return customEndpoint
	}
	if p.Endpoint != "" {
		return p.Endpoint
	}
	switch p.Name {
	case "openai":
		return "https://api.openai.com/v1/chat/completions"
	case "deepseek":
		return "https://api.deepseek.com/v1/chat/completions"
	case "anthropic":
		return "https://api.anthropic.com/v1/messages"
	case "minimax":
		return "https://api.minimaxi.com/v1/chat/completions"
	case "glm":
		return "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/models"
	default:
		return ""
	}
}
