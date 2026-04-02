package openai

import (
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

type Planner struct {
	model      string
	temp       float64
	system     string
	endpoint   string
	apiKey     string
	provider   string
	httpClient *http.Client
}

func NewPlanner(apiKey string, model string, temp float64, provider string, endpoint string) *Planner {
	ep := endpoint
	if ep == "" {
		ep = defaultEndpoint(provider)
	}
	return &Planner{
		model:      model,
		temp:       temp,
		endpoint:   ep,
		apiKey:     apiKey,
		provider:   provider,
		httpClient: &http.Client{},
	}
}

func defaultEndpoint(provider string) string {
	switch provider {
	case "minmax":
		return "https://api.minimax.io/anthropic/v1/messages"
	case "glm":
		return "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/models"
	case "deepseek":
		return "https://api.deepseek.com/v1/chat/completions"
	default:
		return "https://api.openai.com/v1/chat/completions"
	}
}

func (p *Planner) ID() string { return "openai" }

func (p *Planner) Capabilities() []string { return []string{"fs", "cmd", "search"} }

func (p *Planner) Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	userPrompt := req.Prompt
	if len(req.Context) > 0 {
		var ctxLines []string
		for _, c := range req.Context {
			ctxLines = append(ctxLines, c.Content)
		}
		userPrompt = "Context:\n" + strings.Join(ctxLines, "\n") + "\n\nRequest: " + userPrompt
	}

	systemPrompt := `You are Morpheus, an autonomous coding assistant. Your goal is to complete tasks efficiently with minimal user interaction.

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
- agent.run: Delegate isolated research or sub-tasks
- fs.read: Read files by range (path required, offset/limit optional)
- fs.write: Create/update files (path + content required)
- fs.edit: Precise string replacement edits
- fs.glob: Match file paths (pattern required)
- fs.grep: Search patterns in files
- lsp.query: Code navigation, definitions, references, hover, diagnostics, rename, code actions
- mcp.query: MCP server operations
- skill.invoke: Invoke local skills when user explicitly asks
- cmd.exec: Shell commands

## Best Practices
- Combine related commands: "cd dir && ls" or "grep pattern file | head -20"
- Verify paths with fs.glob before fs.read
- Use fs.grep first, then fs.read with offset/limit for relevant lines only
- Keep fs.read limit small (never exceed 400 lines)
- Prefer fs.edit for precise changes; use fs.write only for full-file creation
- Use lsp.query for code navigation before grep-based guesses

## Output Format (valid JSON only):
{"summary": "1-2 line summary", "steps": [{"description": "action description", "tool": "tool name", "inputs": {"key": "value"}}], "risks": []}`

	payload := map[string]any{
		"model": p.model,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": p.temp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.Plan{}, err
	}

	endpoint := p.endpoint
	switch p.provider {
	case "minmax":
		endpoint += "?GroupId=" + p.apiKey
	case "gemini":
		endpoint += ":" + p.model + ":generateContent?key=" + p.apiKey
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	switch p.provider {
	case "openai", "glm", "minmax", "deepseek":
		httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)
	case "gemini":
		// gemini uses API key in URL
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

func (p *Planner) parseResponse(body []byte) (string, error) {
	switch p.provider {
	case "gemini":
		var result struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", err
		}
		if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
			return result.Candidates[0].Content.Parts[0].Text, nil
		}
		return "", fmt.Errorf("no content in response")
	default:
		var result struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return "", err
		}
		if len(result.Choices) == 0 {
			return "", fmt.Errorf("no response from API")
		}
		return strings.TrimSpace(result.Choices[0].Message.Content), nil
	}
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
				Tool:        "conversation.echo",
				Inputs:      map[string]any{"text": response},
				Status:      sdk.StepStatusPending,
			},
		},
	}
}
