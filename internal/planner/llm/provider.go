package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type RequestPayload struct {
	Model       string            `json:"model"`
	Messages    []Message         `json:"messages"`
	Temperature float64           `json:"temperature,omitempty"`
	MaxTokens   int               `json:"max_tokens,omitempty"`
	Tools       []json.RawMessage `json:"tools,omitempty"`
	Stream      bool              `json:"stream,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ResponsePayload struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type Planner interface {
	ID() string
	Capabilities() []string
	Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error)
}

type StreamChunk struct {
	Type   string `json:"type"`
	Index  int    `json:"index,omitempty"`
	Delta  Delta  `json:"delta,omitempty"`
	Finish int    `json:"finish_reason,omitempty"`
}

type Delta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type PlannerProviderConfig struct {
	APIKey       string
	Model        string
	Endpoint     string
	Temperature  float64
	ExtraHeaders map[string]string
}

type ProviderFactory func(config PlannerProviderConfig) (Planner, error)

type ProviderRegistry struct {
	providers map[string]ProviderFactory
}

var globalRegistry = &ProviderRegistry{
	providers: make(map[string]ProviderFactory),
}

func RegisterProvider(name string, factory ProviderFactory) {
	globalRegistry.providers[name] = factory
}

func GetProvider(name string) (ProviderFactory, bool) {
	factory, ok := globalRegistry.providers[name]
	return factory, ok
}

func ListProviders() []string {
	providers := make([]string, 0, len(globalRegistry.providers))
	for name := range globalRegistry.providers {
		providers = append(providers, name)
	}
	return providers
}

type BasePlanner struct {
	model        string
	temp         float64
	endpoint     string
	apiKey       string
	httpClient   *http.Client
	extraHeaders map[string]string
}

func NewBasePlanner(apiKey string, model string, temp float64, endpoint string, extraHeaders map[string]string) *BasePlanner {
	ep := endpoint
	if ep == "" {
		ep = "https://api.openai.com/v1/chat/completions"
	}
	return &BasePlanner{
		model:        model,
		temp:         temp,
		endpoint:     ep,
		apiKey:       apiKey,
		httpClient:   &http.Client{},
		extraHeaders: extraHeaders,
	}
}

func (p *BasePlanner) Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	userPrompt := req.Prompt
	if len(req.Context) > 0 {
		var ctxLines []string
		for _, c := range req.Context {
			ctxLines = append(ctxLines, c.Content)
		}
		userPrompt = "Context:\n" + strings.Join(ctxLines, "\n") + "\n\nRequest: " + userPrompt
	}

	systemPrompt := p.GetSystemPrompt()

	payload := RequestPayload{
		Model: p.model,
		Messages: []Message{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: p.temp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if p.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	}
	for k, v := range p.extraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return sdk.Plan{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return sdk.Plan{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return sdk.Plan{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	content, err := p.parseResponse(respBody)
	if err != nil {
		return sdk.Plan{}, err
	}

	return p.parsePlanResponse(content)
}

func (p *BasePlanner) GetSystemPrompt() string {
	return `You are Morpheus, an autonomous coding assistant. Your goal is to complete tasks efficiently with minimal user interaction.

## Operating Principles
- Think independently and make decisions without asking the user
- If unsure about a non-critical detail, choose a safe default and proceed
- Never ask for confirmation on safe, reversible operations - just do them
- Only ask the user when you have exhausted all options and cannot proceed
- Keep responses brief and direct

## Workflow
1. Understand the task fully before planning
2. Use the right tool for each operation
3. Execute efficiently - typically 1-3 steps per task
4. Output results directly, avoid unnecessary echo steps

## Tool Selection
- todowrite: Create and update todo list for multi-step work
- bash: Shell commands
- glob: Match file paths (pattern required)
- grep: Search patterns in files
- read: Read files by range (path required, offset/limit optional)
- write: Create/update files (path + content required)
- edit: Precise string replacement edits
- lsp: Code navigation, definitions, references, hover, diagnostics, rename, code actions
- webfetch: Fetch URL content
- websearch: Web search
- question: Ask user a targeted question
- mcp: MCP server operations
- skill: Invoke local skills when user explicitly asks
- task: Delegate isolated research or sub-tasks
- agent.coordinate: Coordinate multiple sub-agents
- agent.message: Send message to team member

## Best Practices
- **Think independently**: You have powerful tools - use read, grep, glob, lsp, bash to explore and understand the codebase yourself. Do not ask the user for clarification unless truly blocked.
- **Verify before claiming**: Run tests, check git history, inspect code rather than assuming or guessing. When you find something, prove it with actual tool output.
- **Iterate actively**: Try an approach, observe results, adjust. Do not wait for perfect understanding before acting.
- **Prefer bash for almost everything**: file operations (ls, find, cat), git (status, log, diff), running tests/builds, JSON parsing (jq), network checks (curl, ping), process management (ps, kill, top)
- Combine related commands: "cd dir && ls" or "grep pattern file | head -20"
- Verify paths with glob before read
- Use grep first, then read with offset/limit for relevant lines only
- Keep read limit small (never exceed 400 lines)
- Prefer edit for precise changes; use write only for full-file creation
- Use lsp for code navigation before grep-based guesses
- Use python inside bash when logic is clearer in Python

## Output Format (valid JSON only):
{"summary": "1-2 line summary", "steps": [{"description": "action description", "tool": "tool name", "inputs": {"key": "value"}}], "risks": []}`
}

func (p *BasePlanner) parseResponse(body []byte) (string, error) {
	var result ResponsePayload
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no response from API")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

func (p *BasePlanner) parsePlanResponse(content string) (sdk.Plan, error) {
	if jsonStr := extractJSON(content); jsonStr != "" {
		var parsed struct {
			Summary string `json:"summary"`
			Steps   []struct {
				Description string         `json:"description"`
				Tool        string         `json:"tool"`
				Inputs      map[string]any `json:"inputs"`
			} `json:"steps"`
			Risks []string `json:"risks"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
			return fallbackPlan(content), nil
		}

		plan := sdk.Plan{
			ID:      uuid.NewString(),
			Summary: parsed.Summary,
			Status:  sdk.PlanStatusDraft,
			Risks:   parsed.Risks,
		}
		for _, s := range parsed.Steps {
			plan.Steps = append(plan.Steps, sdk.PlanStep{
				ID:          uuid.NewString(),
				Description: s.Description,
				Tool:        s.Tool,
				Inputs:      s.Inputs,
				Status:      sdk.StepStatusPending,
			})
		}
		return plan, nil
	}

	return fallbackPlan(content), nil
}

func extractJSON(s string) string {
	start := strings.Index(s, "{")
	if start == -1 {
		if idx := strings.Index(s, "["); idx != -1 {
			start = idx
		} else {
			return ""
		}
	}
	end := strings.LastIndex(s, "}")
	if end == -1 {
		return ""
	}
	return s[start : end+1]
}

func fallbackPlan(response string) sdk.Plan {
	return sdk.Plan{
		ID:      uuid.NewString(),
		Summary: "Generated plan",
		Status:  sdk.PlanStatusDraft,
		Steps: []sdk.PlanStep{
			{
				ID:          uuid.NewString(),
				Description: "Process request",
				Tool:        "question",
				Inputs:      map[string]any{"question": response, "options": []string{"Continue"}},
				Status:      sdk.StepStatusPending,
			},
		},
	}
}
