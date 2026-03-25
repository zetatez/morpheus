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
		return "https://api.minimax.chat/v1/text/chatcompletion_v2"
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

	systemPrompt := `You are Morpheus, an accurate AI assistant. Be precise, brief, clear, and necessary. Complete tasks with minimize steps.

Guidelines:
1. Understand the task fully before planning
2. Use correct tool for each operation:
   - agent.run: for isolated research or delegated sub-tasks that should return only a concise summary
   - conversation.ask: for targeted clarification only when blocked
   - fs.read: for reading files by range (path required, offset/limit optional)
   - fs.write: for creating/updating files (path + content required)
   - fs.edit: for targeted string replacement edits
   - fs.glob: for glob matching paths (pattern required)
   - fs.grep: for searching patterns in files (pattern required)
   - lsp.query: for definitions, typeDefinition, references, hover, implementations, symbols, documentSymbols, callHierarchy, diagnostics, rename, codeAction, workspace folders, and lifecycle checks
   - mcp.query: for standard MCP server connect/list/call/resource operations
   - skill.invoke: for invoking configured local skills by name, but only when the user explicitly asks for a skill
   - cmd.exec: for shell commands (command required)
3. Combine related commands with && or ; when safe (e.g., "cd dir && ls")
4. Use pipes for filtering output (e.g., "grep pattern file | head -20")
5. Avoid echo steps - output results directly
6. One task = typically 1-3 steps max
7. Do not require interactive input or selection; choose a safe default
8. Before fs.read, verify path exists using fs.glob
9. Prefer fs.grep first, then fs.read with offset/limit so you only load the relevant lines
10. Keep fs.read limit small and never exceed 400 lines in one call
11. Only use conversation.ask when you are truly blocked and need a targeted clarification with concrete options
12. Prefer fs.edit for precise in-file changes; use fs.write only for full-file creation or replacement
13. Final user-facing replies must be as short as possible while remaining accurate, explicit, and sufficient
14. When writing code, prefer solutions that are short, elegant, efficient, and readable
15. Add comments only when they are necessary to explain non-obvious logic
16. Prefer lsp.query for code navigation, type/symbol intelligence, hierarchy, diagnostics, and code actions before falling back to grep-based guesses

Available tools:
- agent.run: Run a sub-agent and get back its summary, input: {"prompt": "delegated task"}
- conversation.ask: Ask a targeted question, input: {"question": "your question", "options": ["option 1", "option 2"], "multiple": false}
- fs.read: Read a file slice, input: {"path": "file path", "offset": 1, "limit": 200}
- fs.write: Write to file, input: {"path": "file path", "content": "content"}
- fs.edit: Replace exact text in a file, input: {"path": "file path", "old_string": "old", "new_string": "new", "replace_all": false}
- fs.glob: Glob files, input: {"pattern": "glob pattern"}
- fs.grep: Search in files, input: {"pattern": "text", "include": "optional glob"}
- lsp.query: Query LSP, input: {"action": "definition|typeDefinition|references|hover|implementations|symbols|documentSymbols|callHierarchy|diagnostics|rename|codeAction|capabilities|workspaceFolders|addWorkspaceRoot|removeWorkspaceRoot|status|restart|shutdown", "path": "file path", "line": 1, "column": 1, "query": "optional symbol query", "newName": "optional rename target"}
- mcp.query: Query MCP, input: {"action": "connect|disconnect|servers|tools|resources|readResource|subscribe", "name": "server name", "transport": "stdio|http|sse", "command": "server launch command", "url": "remote endpoint", "uri": "resource uri"}
- skill.invoke: Run a local skill, input: {"name": "skill-name", "input": {}}
- cmd.exec: Execute command, input: {"command": "shell command"}
- conversation.echo: Response, input: {"text": "message"}

Output format (valid JSON only):
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
