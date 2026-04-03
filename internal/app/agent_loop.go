package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/pkg/sdk"
	"go.uber.org/zap"
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

type intentClassification struct {
	Route          string   `json:"route"`
	Tags           []string `json:"tags"`
	SuggestedTools []string `json:"suggested_tools"`
	Confidence     string   `json:"confidence"`
	Reason         string   `json:"reason"`
}

type OutputFormat struct {
	Type       string         `json:"type"`
	Schema     map[string]any `json:"schema,omitempty"`
	RetryCount int            `json:"retry_count,omitempty"`
}

func (rt *Runtime) AgentLoop(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode) (Response, error) {
	return rt.runAgentLoop(ctx, sessionID, input, format, mode, runnerCallbacks{
		callChat: rt.callChatWithTools,
	})
}

func (rt *Runtime) buildAgentMessages(sessionID string) []map[string]any {
	return rt.buildMessagesForRoute(context.Background(), sessionID, routeToolAgent, false)
}

const (
	routeSimpleChat  requestRoute = "simple_chat"
	routeLightweight requestRoute = "lightweight_answer"
	routeFreshInfo   requestRoute = "fresh_info"
	routeToolAgent   requestRoute = "tool_agent"
)

type requestRoute string

func (rt *Runtime) buildMessagesForRoute(ctx context.Context, sessionID string, route requestRoute, forkIsolated bool) []map[string]any {
	var messages []map[string]any
	undercoverMode := undercoverModeFromContext(ctx)
	if systemPrompt := rt.systemPrompt(sessionID); systemPrompt != "" && !undercoverMode {
		messages = append(messages, map[string]any{"role": "system", "content": systemPrompt})
	}
	if route == routeToolAgent || route == routeFreshInfo {
		if !forkIsolated && !undercoverMode {
			if shared := rt.renderTeamSharedContext(sessionID); shared != "" {
				messages = append(messages, map[string]any{"role": "system", "content": shared})
			}
			for _, doc := range rt.contextDocuments() {
				messages = append(messages, map[string]any{"role": "system", "content": doc.Label + ":\n" + doc.Content})
			}
			if longTerm := rt.longTermMemory(sessionID); longTerm != "" {
				messages = append(messages, map[string]any{"role": "system", "content": "Long-term memory:\n" + truncateLines(longTerm, 200)})
			}
			if shortTerm := rt.shortTermMemory(sessionID); shortTerm != "" && shortTerm != rt.conversation.Summary(sessionID) {
				messages = append(messages, map[string]any{"role": "system", "content": "Short-term memory:\n" + truncateLines(shortTerm, 200)})
			}
		}
		if route == routeFreshInfo {
			messages = append(messages, map[string]any{"role": "system", "content": freshInfoSystemPrompt})
		} else if undercoverMode {
			messages = append(messages, map[string]any{"role": "system", "content": undercoverSystemPrompt})
		} else {
			messages = append(messages, map[string]any{"role": "system", "content": toolSystemPrompt})
		}
		if !forkIsolated && !undercoverMode {
			if summary := rt.conversation.Summary(sessionID); summary != "" {
				messages = append(messages, map[string]any{"role": "system", "content": truncateLines(summary, 200)})
			}
		}
	} else {
		messages = append(messages, map[string]any{"role": "system", "content": lightweightSystemPrompt(route)})
		if !forkIsolated && !undercoverMode {
			if summary := rt.conversation.Summary(sessionID); summary != "" {
				messages = append(messages, map[string]any{"role": "system", "content": "Conversation summary:\n" + truncateLines(summary, 60)})
			}
		}
	}

	if forkIsolated {
		return messages
	}

	history := rt.conversation.Messages(sessionID)
	start := 0
	historyLimit := maxHistoryTurns
	if route != routeToolAgent && route != routeFreshInfo {
		historyLimit = 6
	}
	if len(history) > historyLimit {
		start = len(history) - historyLimit
	}
	for _, msg := range history[start:] {
		if msg.Role == "system" {
			continue
		}
		if len(msg.Parts) > 0 {
			for _, part := range msg.Parts {
				if part.Type != "tool" || strings.TrimSpace(part.Tool) == "" {
					continue
				}
				toolName := normalizeToolName(part.Tool)
				messages = append(messages, map[string]any{
					"role":    "assistant",
					"content": msg.Content,
					"tool_calls": []map[string]any{map[string]any{
						"id":   part.CallID,
						"type": "function",
						"function": map[string]any{
							"name":      toolName,
							"arguments": mustJSONString(part.Input),
						},
					}},
				})
				if part.Status != "pending" {
					messages = append(messages, map[string]any{
						"role":         "tool",
						"name":         toolName,
						"tool_call_id": part.CallID,
						"content":      formatToolPartContent(part),
					})
				}
			}
			continue
		}
		if msg.Content == "" {
			continue
		}
		messages = append(messages, map[string]any{"role": msg.Role, "content": msg.Content})
	}
	return messages
}

func lightweightSystemPrompt(route requestRoute) string {
	if route == routeSimpleChat {
		return "You are Morpheus. Reply briefly and naturally to simple conversational messages. Do not use tools. Do not over-explain."
	}
	return "You are Morpheus. Answer clearly and directly. Prefer a single concise response. Do not use tools unless they are strictly required to answer correctly."
}

const freshInfoSystemPrompt = `You are Morpheus handling a fresh-information request.

Rules:
- The user expects you to proactively fetch the latest information.
- Use web.fetch before answering unless you already have verified fresh data in the conversation.
- Do not stop with generic statements like "I cannot access real-time information" if web.fetch is available.
- Prefer a direct public source first. If one source fails, try another public source.
- For simple factual queries like a single current price, rate, weather value, or headline, stop after you have 1-2 credible successful fetches and answer directly.
- Do not keep browsing once you already have enough evidence to answer the user's exact question.
- This applies to weather, news, stock prices, exchange rates, flight status, traffic, and other time-sensitive facts.
- Prefer official or high-signal public endpoints/pages when possible.
- Keep the final answer concise and based on fetched evidence.
- Avoid unrelated tools; focus on web retrieval and summarization.`

const intentClassifierPrompt = `You classify user requests for routing and tool selection.

Return JSON only with this schema:
{
  "route": "simple_chat" | "lightweight_answer" | "tool_agent",
  "tags": [string],
  "suggested_tools": [string],
  "confidence": "low" | "medium" | "high",
  "reason": string
}

Rules:
- Use "simple_chat" for greetings, thanks, short social replies, identity/help chatter.
- Use "lightweight_answer" for questions that can be answered well from general knowledge without external tools.
- Use "tool_agent" when the request likely needs files, repository inspection, command execution, code changes, web/network lookup, or real-time/fresh information.
- Use "tool_agent" for time-sensitive facts like weather, news, stock prices, exchange rates, traffic, and flight status unless you explicitly return route "fresh_info".
- Tags should be short labels such as: greeting, social, knowledge, coding, repo, file, shell, web, fresh_info, realtime, weather, news, stock, finance, exchange_rate, flight, traffic, debugging, editing.
- suggested_tools should contain only tool capability names, not guaranteed exact tool names. Use from: web, file_read, file_search, shell, edit, git, none.
- Prefer the smallest sufficient route.
- If the user asks for current weather/news/prices/traffic/flights/exchange rates/anything time-sensitive, include fresh_info and prefer route "fresh_info".
- If unsure between lightweight_answer and tool_agent, prefer lightweight_answer unless external verification is clearly needed.`

func (rt *Runtime) classifyIntent(ctx context.Context, sessionID string, input normalizedInput, format *OutputFormat, pendingConfirmation bool) intentClassification {
	if pendingConfirmation {
		return intentClassification{Route: string(routeToolAgent), Tags: []string{"confirmation"}, SuggestedTools: []string{"shell"}, Confidence: "high", Reason: "Pending confirmation requires the tool agent path."}
	}
	if format != nil {
		return intentClassification{Route: string(routeToolAgent), Tags: []string{"structured_output"}, SuggestedTools: []string{}, Confidence: "high", Reason: "Structured output requests must stay on the tool agent path."}
	}
	if len(input.Parts) > 0 {
		return intentClassification{Route: string(routeToolAgent), Tags: []string{"attachment"}, SuggestedTools: []string{"file_read"}, Confidence: "high", Reason: "Attachments require tool-capable handling."}
	}
	trimmed := strings.TrimSpace(input.Text)
	if trimmed == "" {
		return intentClassification{Route: string(routeSimpleChat), Tags: []string{"social"}, SuggestedTools: []string{"none"}, Confidence: "high", Reason: "Empty input defaults to a simple conversational reply."}
	}
	cacheKey := strings.ToLower(trimmed)
	if cached, ok := rt.getCachedIntent(sessionID, cacheKey); ok {
		rt.logger.Info("intent classification cache hit", zap.String("session", sessionID), zap.String("route", cached.Route), zap.Strings("tags", cached.Tags), zap.String("confidence", cached.Confidence), zap.String("input", trimmed))
		return cached
	}
	rt.logger.Info("intent classification cache miss", zap.String("session", sessionID), zap.String("input", trimmed))
	messages := []map[string]any{{"role": "system", "content": intentClassifierPrompt}, {"role": "user", "content": trimmed}}
	resp, err := rt.callChatWithTools(ctx, messages, nil, nil, nil)
	if err != nil {
		fallback := fallbackIntentClassification(trimmed)
		rt.setCachedIntent(sessionID, cacheKey, fallback)
		return fallback
	}
	classified, err := parseIntentClassification(strings.TrimSpace(resp.Content))
	if err != nil {
		fallback := fallbackIntentClassification(trimmed)
		rt.setCachedIntent(sessionID, cacheKey, fallback)
		return fallback
	}
	normalized := normalizeIntentClassification(classified, trimmed)
	rt.setCachedIntent(sessionID, cacheKey, normalized)
	return normalized
}

func parseIntentClassification(content string) (intentClassification, error) {
	if content == "" {
		return intentClassification{}, fmt.Errorf("empty intent classification")
	}
	jsonPayload := extractJSON(content)
	var out intentClassification
	if err := json.Unmarshal([]byte(jsonPayload), &out); err != nil {
		return intentClassification{}, err
	}
	return out, nil
}

func normalizeIntentClassification(in intentClassification, text string) intentClassification {
	route := requestRoute(strings.TrimSpace(in.Route))
	switch route {
	case routeSimpleChat, routeLightweight, routeFreshInfo, routeToolAgent:
	default:
		route = routeLightweight
	}
	tags := dedupeStrings(in.Tags)
	tools := dedupeStrings(in.SuggestedTools)
	conf := strings.ToLower(strings.TrimSpace(in.Confidence))
	if conf != "low" && conf != "medium" && conf != "high" {
		conf = "medium"
	}
	out := intentClassification{Route: string(route), Tags: tags, SuggestedTools: tools, Confidence: conf, Reason: strings.TrimSpace(in.Reason)}
	if len(out.SuggestedTools) == 0 {
		out.SuggestedTools = inferSuggestedToolsFromTags(out.Tags)
	}
	if len(out.Tags) == 0 {
		out.Tags = inferTagsFromText(text, route)
	}
	if hasAnyTag(out.Tags, "fresh_info", "realtime", "weather", "news", "finance", "stock", "exchange_rate", "flight", "traffic") && route == routeToolAgent {
		out.Route = string(routeFreshInfo)
		if len(out.SuggestedTools) == 0 || (len(out.SuggestedTools) == 1 && out.SuggestedTools[0] == "none") {
			out.SuggestedTools = []string{"web"}
		}
	}
	return out
}

func fallbackIntentClassification(text string) intentClassification {
	_ = text
	return intentClassification{Route: string(routeLightweight), Tags: []string{"knowledge"}, SuggestedTools: []string{"none"}, Confidence: "low", Reason: "Fallback defaulted to lightweight answering because intent classification was unavailable."}
}

func inferSuggestedToolsFromTags(tags []string) []string {
	set := map[string]struct{}{}
	for _, tag := range tags {
		switch tag {
		case "web", "fresh_info", "realtime", "weather", "news", "finance", "stock", "exchange_rate", "flight", "traffic":
			set["web"] = struct{}{}
		case "file", "repo":
			set["file_read"] = struct{}{}
			set["file_search"] = struct{}{}
		case "shell", "debugging", "execution":
			set["shell"] = struct{}{}
		case "editing", "coding":
			set["edit"] = struct{}{}
			set["file_read"] = struct{}{}
			set["file_search"] = struct{}{}
		case "git":
			set["git"] = struct{}{}
		}
	}
	return mapKeysSorted(set)
}

func inferTagsFromText(text string, route requestRoute) []string {
	if route == routeSimpleChat {
		return []string{"social"}
	}
	if route == routeFreshInfo {
		return []string{"fresh_info", "web"}
	}
	if route == routeToolAgent {
		return []string{"tooling"}
	}
	return []string{"knowledge"}
}

func dedupeStrings(items []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, item := range items {
		clean := strings.TrimSpace(strings.ToLower(item))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func mapKeysSorted(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func allowedToolsForClassification(route requestRoute, suggested []string) []string {
	if route == routeFreshInfo {
		return []string{"web.fetch"}
	}
	if route != routeToolAgent {
		return []string{}
	}
	if len(suggested) == 0 {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, name := range suggested {
		switch name {
		case "web":
			allowed["web.fetch"] = struct{}{}
		case "file_read":
			allowed["fs.read"] = struct{}{}
			allowed["fs.ls"] = struct{}{}
			allowed["fs.tree"] = struct{}{}
		case "file_search":
			allowed["fs.glob"] = struct{}{}
			allowed["fs.grep"] = struct{}{}
		case "shell":
			allowed["cmd.exec"] = struct{}{}
		case "edit":
			allowed["fs.edit"] = struct{}{}
			allowed["fs.write"] = struct{}{}
			allowed["fs.create"] = struct{}{}
			allowed["fs.mkdir"] = struct{}{}
			allowed["fs.mv"] = struct{}{}
			allowed["fs.cp"] = struct{}{}
		case "git":
			allowed["git.status"] = struct{}{}
			allowed["git.diff"] = struct{}{}
		}
	}
	if _, hasEdit := allowed["fs.edit"]; hasEdit {
		allowed["fs.read"] = struct{}{}
		allowed["fs.glob"] = struct{}{}
		allowed["fs.grep"] = struct{}{}
	}
	if _, hasShell := allowed["cmd.exec"]; hasShell {
		allowed["fs.read"] = struct{}{}
	}
	if _, hasGitStatus := allowed["git.status"]; hasGitStatus {
		allowed["fs.read"] = struct{}{}
	}
	if len(allowed) == 0 {
		return nil
	}
	return mapKeysSorted(allowed)
}

func shouldEmitThinking(route requestRoute) bool {
	return route == routeToolAgent || route == routeFreshInfo
}

func formatRouteName(route requestRoute) string {
	return string(route)
}

func hasAnyTag(tags []string, wants ...string) bool {
	set := map[string]struct{}{}
	for _, tag := range tags {
		set[tag] = struct{}{}
	}
	for _, want := range wants {
		if _, ok := set[want]; ok {
			return true
		}
	}
	return false
}

func sortedToolNames(specs []map[string]any) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		fn, _ := spec["function"].(map[string]any)
		name, _ := fn["name"].(string)
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

const toolSystemPrompt = `You are Morpheus, an autonomous coding assistant. Your job is to solve the user's problem in the fewest safe steps, with minimal back-and-forth.

## Core Behavior
- Be highly proactive: inspect the workspace, infer intent, choose reasonable defaults, and move forward without waiting.
- Prefer finishing the task over discussing the task.
- Minimize the number of tool calls, but do enough investigation to avoid blind changes.
- Prefer one decisive execution path over offering many options unless the user explicitly asks for choices.
- When the request is underspecified but a safe default exists, pick the default and continue.

## When To Ask The User
Ask only if one of these is true:
- A required secret, credential, token, or account-specific value is missing.
- The action is destructive, irreversible, security-sensitive, production-impacting, or changes billing/access policy.
- Multiple materially different outcomes are possible and the repository/context does not disambiguate them.
- You are fully blocked after checking the codebase and available tools.

If you must ask:
- Ask exactly one focused question.
- State the default you would choose.
- Continue any non-blocked work first.

## Execution Strategy
- First understand just enough context to act correctly.
- Then execute directly: inspect, edit, run, verify.
- Prefer solving the whole task end-to-end rather than stopping after partial progress.
- For complex tasks, actively consider whether a shell pipeline or a short Python script is the fastest reliable path.
- Prefer automation over repetitive manual edits or repeated tool calls.
- If a tool or command fails, recover intelligently and try the next best approach.
- Do not repeat the same tool call with materially identical inputs after the same outcome.
- If an approach repeats or yields the same failure, switch strategy: inspect more context, try a different tool, narrow the scope, or stop with a concise explanation.
- Avoid repeating the same thinking, plan, or action sequence.
- Actively look for the right tool instead of asking the user what to do next.
- If the task can be solved by searching files, reading code, running commands, or applying edits, do that yourself before asking anything.
- Treat the available tools as your primary way to reduce uncertainty.
- Follow a clear agent loop similar to a strong coding operator:
  1. Briefly state what you are about to do.
  2. Use tools to inspect, change, and verify.
  3. Briefly summarize what changed or what you learned.
- Thinking should be short, concrete, and immediately connected to the next action.
- Do not produce long speculative analysis before using tools.

## Tool Usage
- Use tools whenever they help you verify facts instead of guessing.
- Batch related work efficiently.
- Prefer direct file inspection and targeted commands over broad exploratory churn.
- For complex multi-step work, call ` + "`todo.write`" + ` early to create or refresh the todo list, then update it as execution progresses.
- Use ` + "`cmd.exec`" + ` confidently for shell-native work such as builds, tests, git inspection, file discovery, JSON formatting, or chaining dependable CLI tools.
- Prefer ` + "`python - <<'PY' ... PY`" + ` or ` + "`python script.py`" + ` inside ` + "`cmd.exec`" + ` when logic, parsing, or bulk transformation would be clearer in Python than shell.
- When writing Python for task execution, keep scripts short, deterministic, and focused on one job.
- Favor commands and scripts that can be rerun safely and that produce inspectable output.
- If the task is command-heavy, automation-oriented, or needs structured parsing plus execution, consider delegating it to the ` + "`shell-python-operator`" + ` subagent.
- Emit tool calls instead of plain text whenever tool use is needed.
- Only provide the final user-facing answer after the necessary work is completed or you are truly blocked.
- Prefer this rhythm: brief thinking -> tool calls -> concise summary.
- If the task spans multiple steps, keep the user informed with short progress updates, not long essays.
- For complex tasks, first decompose the work into a short todo list, keep exactly one item in progress when practical, and complete the todos in order.
- Do not treat the todo list as static; update it with ` + "`todo.write`" + ` whenever scope changes, steps finish, or a task fails.

## Output Style
- Keep user-facing responses brief, concrete, and action-oriented.
- Report what you changed, what you verified, and any remaining limitation.
- Do not ask permission for normal safe actions; just take them.
- When handling a user request, make your workflow visible:
  - show brief thinking/progress updates when useful,
  - expose tool usage as the concrete work being performed,
  - end with a short summary of the result and verification.
- Prefer a clear structure: think -> tool calls -> summary.
- Avoid duplicated summaries, repeated apologies, or generic filler.
- When a simple direct answer is enough, keep it short; when execution is needed, show progress through the loop.`

const undercoverSystemPrompt = `You are a skilled software developer working on an open source project. You are making a pull request contribution. Maintain a professional tone as a regular contributor. Do not reveal that you are an AI assistant or use language that suggests you are an AI. Do not mention Morpheus, AI assistance, or any AI-related terminology. Focus purely on the technical content.`

func (rt *Runtime) systemPrompt(sessionID string) string {
	systemPrompt := rt.conversation.SystemPrompt()
	if rt.plugins != nil {
		systemPrompt = rt.plugins.ApplySystem(plugin.SystemContext{SessionID: sessionID}, systemPrompt)
	}
	return systemPrompt
}

func (rt *Runtime) collectToolSpecs(ctx context.Context, format *OutputFormat, mode AgentMode, allowedTools []string) ([]map[string]any, any, map[string]string, string) {
	var specs []map[string]any
	nameMap := map[string]string{}
	blockAsk := exec.IsAskToolBlocked(ctx)
	for _, tool := range rt.registry.All() {
		meta, ok := tool.(sdk.ToolSpec)
		if !ok {
			continue
		}
		if blockAsk && meta.Name() == "conversation.ask" {
			continue
		}
		if !exec.IsToolAllowed(string(mode), meta.Name()) {
			continue
		}
		if !exec.IsToolAllowedWithList(allowedTools, meta.Name()) {
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
		toolChoice = map[string]any{"type": "function", "function": map[string]any{"name": structuredName}}
	}

	if antiDistillationFromContext(ctx) {
		specs = append(specs, fakeToolSpecsForAntiDistillation()...)
	}

	return specs, toolChoice, nameMap, structuredName
}

func fakeToolSpecsForAntiDistillation() []map[string]any {
	return []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "memory_query",
				"description": "Query the long-term memory system for specific facts or patterns",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"query": map[string]any{"type": "string", "description": "The semantic query to search memory"},
						"depth": map[string]any{"type": "string", "description": "Search depth: shallow, medium, or deep"},
					},
					"required": []string{"query"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "internal_state_dump",
				"description": "Dump the current internal agent state for debugging",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"format": map[string]any{"type": "string", "description": "Output format: json, yaml, or text"},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "session_clone",
				"description": "Create a clone of the current session for parallel processing",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"preserve_state": map[string]any{"type": "boolean", "description": "Whether to preserve current state"},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "model_config_get",
				"description": "Get the current model configuration and parameters",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"decrypt": map[string]any{"type": "boolean", "description": "Return decrypted configuration"},
					},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "prompt_inject_check",
				"description": "Check if the current prompt contains potential injection patterns",
				"parameters": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"text": map[string]any{"type": "string", "description": "Text to check for injection patterns"},
					},
					"required": []string{"text"},
				},
			},
		},
	}
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
	payload := map[string]any{"success": result.Success, "data": result.Data, "error": result.Error}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%v", payload)
	}
	return string(data)
}

func (rt *Runtime) callChatWithTools(ctx context.Context, messages []map[string]any, tools []map[string]any, toolChoice any, _ replEmitter) (chatResponse, error) {
	plannerCfg := rt.cfg.Planner
	if plannerCfg.Provider == "builtin" || plannerCfg.APIKey == "" {
		return chatResponse{}, fmt.Errorf("LLM provider not configured")
	}

	model := plannerCfg.Model
	if model == "" {
		model = defaultModel(plannerCfg.Provider)
	}

	var payload map[string]any
	if plannerCfg.Provider == "minimax" || plannerCfg.Provider == "minmax" {
		var systemPrompt string
		var userMsgs []map[string]any
		for _, msg := range messages {
			if msg["role"] == "system" {
				if systemPrompt != "" {
					systemPrompt += "\n"
				}
				systemPrompt += msg["content"].(string)
			} else {
				userMsgs = append(userMsgs, msg)
			}
		}
		payload = map[string]any{
			"model":       model,
			"messages":    userMsgs,
			"system":      systemPrompt,
			"temperature": 0.4,
			"top_p":       0.9,
			"max_tokens":  4096,
		}
	} else {
		payload = map[string]any{"model": model, "messages": messages}
		payload["temperature"] = plannerCfg.Temperature
		payload["max_tokens"] = 4096
		if len(tools) > 0 {
			payload["tools"] = tools
			if toolChoice != nil {
				payload["tool_choice"] = toolChoice
			} else {
				payload["tool_choice"] = "auto"
			}
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
		return chatResponse{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return chatResponse{}, err
	}

	var out chatResponse
	var usage struct {
		PromptTokens     int
		CompletionTokens int
		TotalTokens      int
	}
	if plannerCfg.Provider == "minimax" || plannerCfg.Provider == "minmax" {
		out, usage, err = parseAnthropicResponse(respBody)
	} else {
		out, usage, err = parseOpenAIResponse(respBody)
	}
	if err != nil {
		return chatResponse{}, err
	}

	if rt.metrics != nil && usage.TotalTokens > 0 {
		rt.metrics.addTokens(int64(usage.PromptTokens), int64(usage.CompletionTokens))
		rt.metrics.addCost(calculateCost(usage.PromptTokens, usage.CompletionTokens, model, plannerCfg.Provider))
	}
	return out, nil
}

func parseAnthropicResponse(body []byte) (chatResponse, struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}, error) {
	var result struct {
		Content []struct {
			Type  string         `json:"type"`
			Text  string         `json:"text"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
		Usage      struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return chatResponse{}, struct {
			PromptTokens     int
			CompletionTokens int
			TotalTokens      int
		}{}, err
	}

	out := chatResponse{FinishReason: result.StopReason}
	usage := struct {
		PromptTokens     int
		CompletionTokens int
		TotalTokens      int
	}{
		PromptTokens:     result.Usage.InputTokens,
		CompletionTokens: result.Usage.OutputTokens,
		TotalTokens:      result.Usage.InputTokens + result.Usage.OutputTokens,
	}

	for _, block := range result.Content {
		if block.Type == "text" {
			out.Content += block.Text
		} else if block.Type == "tool_use" {
			out.ToolCalls = append(out.ToolCalls, toolCall{
				ID:        block.ID,
				Name:      block.Name,
				Arguments: block.Input,
			})
		}
	}
	return out, usage, nil
}

func parseOpenAIResponse(body []byte) (chatResponse, struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}, error) {
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
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return chatResponse{}, struct {
			PromptTokens     int
			CompletionTokens int
			TotalTokens      int
		}{}, err
	}
	if len(result.Choices) == 0 {
		return chatResponse{}, struct {
			PromptTokens     int
			CompletionTokens int
			TotalTokens      int
		}{}, fmt.Errorf("empty choices from model")
	}
	choice := result.Choices[0]
	out := chatResponse{Content: choice.Message.Content, FinishReason: choice.FinishReason}
	for _, tc := range choice.Message.ToolCalls {
		var args map[string]any
		if tc.Function.Arguments != "" {
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
		}
		out.ToolCalls = append(out.ToolCalls, toolCall{ID: tc.ID, Name: tc.Function.Name, Arguments: args})
	}
	return out, struct {
		PromptTokens     int
		CompletionTokens int
		TotalTokens      int
	}{
		PromptTokens:     result.Usage.PromptTokens,
		CompletionTokens: result.Usage.CompletionTokens,
		TotalTokens:      result.Usage.TotalTokens,
	}, nil
}

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

var toolNamePattern = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func mustJSONString(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func formatToolPartContent(part sdk.MessagePart) string {
	payload := map[string]any{
		"success": part.Status == "completed",
		"data":    part.Output,
		"error":   part.Error,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%v", payload)
	}
	return string(data)
}

func calculateCost(promptTokens int, completionTokens int, model string, provider string) float64 {
	pricing, ok := modelPrices[model]
	if !ok {
		return 0
	}
	return (float64(promptTokens)/1_000_000)*pricing.inputPricePerM + (float64(completionTokens)/1_000_000)*pricing.outputPricePerM
}

type modelPricing struct {
	inputPricePerM  float64
	outputPricePerM float64
}

var modelPrices = map[string]modelPricing{
	"gpt-4o":            {inputPricePerM: 2.5, outputPricePerM: 10.0},
	"gpt-4o-mini":       {inputPricePerM: 0.075, outputPricePerM: 0.3},
	"gpt-4-turbo":       {inputPricePerM: 10.0, outputPricePerM: 30.0},
	"gpt-4":             {inputPricePerM: 30.0, outputPricePerM: 60.0},
	"gpt-3.5-turbo":     {inputPricePerM: 0.5, outputPricePerM: 1.5},
	"claude-3-5-sonnet": {inputPricePerM: 3.0, outputPricePerM: 15.0},
	"claude-3-opus":     {inputPricePerM: 15.0, outputPricePerM: 75.0},
	"claude-3-haiku":    {inputPricePerM: 0.25, outputPricePerM: 1.25},
	"deepseek-chat":     {inputPricePerM: 0.14, outputPricePerM: 0.28},
	"deepseek-coder":    {inputPricePerM: 0.14, outputPricePerM: 0.28},
	"glm-4":             {inputPricePerM: 0.1, outputPricePerM: 0.1},
	"glm-4-flash":       {inputPricePerM: 0.0, outputPricePerM: 0.0},
}

func extractStructuredOutput(calls []toolCall, structuredName string) (map[string]any, bool) {
	if structuredName == "" {
		return nil, false
	}
	for _, call := range calls {
		if call.Name == structuredName {
			return call.Arguments, true
		}
	}
	return nil, false
}

func outputPart(output map[string]any) sdk.MessagePart {
	return sdk.MessagePart{Type: "text", Output: output}
}
