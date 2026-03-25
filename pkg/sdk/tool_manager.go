package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ToolCallInput struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type ToolExecutorFunc func(ctx context.Context, input ToolCallInput) ToolResult

type ToolCallResult struct {
	ID      string
	Name    string
	Success bool
	Result  any
	Error   string
	Output  map[string]any
}

type ToolManager struct {
	registry       ToolRegistry
	executor       ToolExecutorFunc
	activeTools    map[string]bool
	nameNormalizer func(string) string
}

func NewToolManager(registry ToolRegistry, executor ToolExecutorFunc) *ToolManager {
	return &ToolManager{
		registry:       registry,
		executor:       executor,
		activeTools:    make(map[string]bool),
		nameNormalizer: NormalizeToolName,
	}
}

func (tm *ToolManager) SetNameNormalizer(f func(string) string) {
	tm.nameNormalizer = f
}

func (tm *ToolManager) SetActiveTools(tools []string) {
	tm.activeTools = make(map[string]bool)
	for _, t := range tools {
		tm.activeTools[t] = true
	}
}

func (tm *ToolManager) IsToolAllowed(name string) bool {
	if len(tm.activeTools) == 0 {
		return true
	}
	return tm.activeTools[name]
}

func (tm *ToolManager) NormalizeName(name string) string {
	if tm.nameNormalizer != nil {
		return tm.nameNormalizer(name)
	}
	return NormalizeToolName(name)
}

func (tm *ToolManager) ResolveTool(name string) (string, Tool, bool) {
	normalized := tm.NormalizeName(name)

	if tm.IsToolAllowed(normalized) {
		if tool, ok := tm.registry.Get(normalized); ok {
			return normalized, tool, true
		}
	}

	original := name
	if name != normalized {
		if tm.IsToolAllowed(name) {
			if tool, ok := tm.registry.Get(name); ok {
				return name, tool, true
			}
		}

		lower := strings.ToLower(name)
		if tm.IsToolAllowed(lower) {
			if tool, ok := tm.registry.Get(lower); ok {
				return lower, tool, true
			}
		}
	}

	return original, nil, false
}

func (tm *ToolManager) RepairToolCall(input ToolCallInput) (ToolCallInput, error) {
	name := tm.NormalizeName(input.Name)

	actualName, _, found := tm.ResolveTool(name)
	if !found {
		return ToolCallInput{
			ID:   input.ID,
			Name: "invalid",
			Arguments: map[string]any{
				"tool":  input.Name,
				"error": fmt.Sprintf("tool %s not found (tried: %s, %s)", input.Name, name, actualName),
			},
		}, fmt.Errorf("tool %s not found", input.Name)
	}

	if actualName != input.Name {
		actualName = tm.NormalizeName(actualName)
	}

	return ToolCallInput{
		ID:        input.ID,
		Name:      actualName,
		Arguments: input.Arguments,
	}, nil
}

func (tm *ToolManager) ExecuteTool(ctx context.Context, input ToolCallInput) ToolResult {
	repaired, err := tm.RepairToolCall(input)
	if err != nil {
		return ToolResult{
			StepID:  input.ID,
			Success: false,
			Error:   err.Error(),
		}
	}

	if repaired.Name == "invalid" {
		return ToolResult{
			StepID:  input.ID,
			Success: false,
			Data:    repaired.Arguments,
			Error:   "tool not found or not allowed",
		}
	}

	actualName, tool, found := tm.ResolveTool(repaired.Name)
	if !found {
		return ToolResult{
			StepID:  input.ID,
			Success: false,
			Error:   fmt.Sprintf("tool %s not found after repair", repaired.Name),
		}
	}

	if tm.executor != nil {
		return tm.executor(ctx, ToolCallInput{
			ID:        input.ID,
			Name:      actualName,
			Arguments: repaired.Arguments,
		})
	}

	toolResult, err := tool.Invoke(ctx, repaired.Arguments)
	toolResult.StepID = input.ID
	return toolResult
}

func (tm *ToolManager) ExecuteTools(ctx context.Context, inputs []ToolCallInput) []ToolCallResult {
	results := make([]ToolCallResult, len(inputs))
	for i, input := range inputs {
		result := tm.ExecuteTool(ctx, input)
		results[i] = ToolCallResult{
			ID:      input.ID,
			Name:    input.Name,
			Success: result.Success,
			Result:  result.Data,
			Error:   result.Error,
			Output:  result.Data,
		}
	}
	return results
}

type ToolCallParser struct {
	textToolCallRegex string
	requiresTextMode  bool
	normalizer        func(string) string
}

func NewToolCallParser(textRegex string, requiresTextMode bool) *ToolCallParser {
	return &ToolCallParser{
		textToolCallRegex: textRegex,
		requiresTextMode:  requiresTextMode,
		normalizer:        NormalizeToolName,
	}
}

func (p *ToolCallParser) SetNormalizer(f func(string) string) {
	p.normalizer = f
}

func (p *ToolCallParser) NormalizeName(name string) string {
	if p.normalizer != nil {
		return p.normalizer(name)
	}
	return NormalizeToolName(name)
}

func (p *ToolCallParser) ParseTextToolCalls(content string) []ToolCallInput {
	if content == "" {
		return nil
	}

	normalized := normalizeJSONContent(content)
	jsonStr := extractToolCallsJSON(normalized)
	if jsonStr == "" {
		return nil
	}

	var raw struct {
		ToolCalls []struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"tool_calls"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return p.parseRawToolCalls(jsonStr)
	}

	inputs := make([]ToolCallInput, 0, len(raw.ToolCalls))
	for i, tc := range raw.ToolCalls {
		name := p.NormalizeName(tc.Name)
		var args map[string]any
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			args = map[string]any{"raw": string(tc.Arguments)}
		}
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		inputs = append(inputs, ToolCallInput{
			ID:        id,
			Name:      name,
			Arguments: args,
		})
	}

	return inputs
}

func normalizeJSONContent(content string) string {
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

func extractToolCallsJSON(content string) string {
	patterns := []string{`"tool_calls"`, `"tool_calls"`}

	var startIdx int = -1
	for _, pattern := range patterns {
		idx := strings.Index(content, pattern)
		if idx == -1 {
			continue
		}

		start := idx
		for start > 0 && content[start] != '{' {
			start--
		}
		startIdx = start
		break
	}

	if startIdx == -1 {
		return ""
	}

	braceCount := 0
	for i := startIdx; i < len(content); i++ {
		if content[i] == '{' {
			braceCount++
		} else if content[i] == '}' {
			braceCount--
			if braceCount == 0 {
				return content[startIdx : i+1]
			}
		}
	}

	return ""
}

func (p *ToolCallParser) parseRawToolCalls(content string) []ToolCallInput {
	namePattern := `"name"\s*:\s*"([^"]+)"`
	argPattern := `"arguments"\s*:\s*(\{[^}]*(?:\{[^}]*\}[^}]*)*\})`

	nameRegex := regexp.MustCompile(namePattern)
	argRegex := regexp.MustCompile(argPattern)

	nameMatches := nameRegex.FindAllStringSubmatchIndex(content, -1)
	argMatches := argRegex.FindAllStringSubmatchIndex(content, -1)

	if len(nameMatches) == 0 {
		return nil
	}

	inputs := make([]ToolCallInput, 0)
	minLen := len(nameMatches)
	if len(argMatches) < minLen {
		minLen = len(argMatches)
	}

	for i := 0; i < minLen; i++ {
		nameStart := nameMatches[i][2]
		nameEnd := nameMatches[i][3]
		name := content[nameStart:nameEnd]
		name = p.NormalizeName(name)

		argStart := argMatches[i][2]
		argEnd := argMatches[i][3]
		argStr := content[argStart:argEnd]

		var args map[string]any
		if err := json.Unmarshal([]byte(argStr), &args); err != nil {
			args = map[string]any{"raw": argStr}
		}

		inputs = append(inputs, ToolCallInput{
			ID:        fmt.Sprintf("call_%d", i),
			Name:      name,
			Arguments: args,
		})
	}

	return inputs
}
