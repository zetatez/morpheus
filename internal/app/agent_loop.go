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
	"time"

	"github.com/zetatez/morpheus/internal/app/prompts"
	"github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/internal/planner/llm"
	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/pkg/sdk"
	"go.uber.org/zap"
)

const (
	maxHistoryTurns  = 20
	defaultMaxTokens = 60000
	llmAPITimeout    = 60 * time.Second
)

var llmHTTPClient = &http.Client{
	Timeout: llmAPITimeout,
}

type toolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
}

type chatResponse struct {
	Content      string
	ToolCalls    []toolCall
	FinishReason string
	Usage        llm.TokenUsage
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
	return rt.AgentLoopV2(ctx, sessionID, input, format, mode, nil)
}

func (rt *Runtime) AgentLoopV2(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode, emit func(string, interface{}) error) (Response, error) {
	return rt.agentLoopV2WithStreaming(ctx, sessionID, input, format, mode, emit, false)
}

func (rt *Runtime) AgentLoopStreamV2(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode, emit func(string, interface{}) error) (Response, error) {
	return rt.agentLoopV2WithStreaming(ctx, sessionID, input, format, mode, emit, true)
}

func (rt *Runtime) agentLoopV2WithStreaming(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode, emit func(string, interface{}) error, streaming bool) (Response, error) {
	if sessionID == "" {
		sessionID = "default"
	}

	run := rt.startRun(sessionID, input, format, mode)
	defer func() {
		if run.Cancel != nil {
			run.Cancel()
		}
	}()

	ctx, cancel := context.WithDeadline(ctx, run.Deadline)
	ctx = withTeamSession(ctx, sessionID)
	run.Cancel = cancel
	defer cancel()

	normalized, err := rt.normalizeUserInput(ctx, UserInput{Text: input.Text, Attachments: input.Attachments})
	if err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, emit, "run_failed", map[string]interface{}{"error": err.Error()})
		return Response{}, err
	}

	run.Prompt = normalized.Text
	if _, err := rt.appendMessage(ctx, sessionID, "user", normalized.Text, normalized.Parts); err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, emit, "run_failed", map[string]interface{}{"error": err.Error()})
		return Response{}, err
	}

	rt.checkAndCompress(ctx, sessionID)

	toolHandler := &ToolHandlerAdapter{
		ExecuteFunc: func(ctx context.Context, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error) {
			return rt.orchestrator.ExecuteStep(ctx, sessionID, step)
		},
		TruncateResultFunc: func(ctx context.Context, sessionID, toolName string, data map[string]interface{}) map[string]interface{} {
			return rt.truncateToolResult(ctx, sessionID, toolName, data)
		},
	}

	agentCfg := AgentConfig{
		MaxSteps:                500,
		MaxConsecutiveFailures:  10,
		MaxStepsWithoutProgress: 50,
		MaxSimilarResults:       5,
		MaxRunDurationMs:        60 * 60 * 1000,
		DoomLoopThreshold:       3,
		ContinueOnDeny:          false,
		Model:                   rt.cfg.Planner.Model,
	}

	callbacks := LoopCallbacks{
		OnEmit: emit,
		OnRunEvent: func(eventType string, data map[string]interface{}) {
			if emit != nil {
				_ = emit(eventType, data)
			}
		},
		OnToolResult: func(toolName string, callID string, result sdk.ToolResult) {
			if emit != nil {
				_ = emit("tool_result", map[string]interface{}{
					"tool": toolName, "call_id": callID, "result": result,
				})
			}
		},
	}

	if streaming {
		callbacks.OnCallChat = func(ctx context.Context, messages []map[string]interface{}, tools []map[string]interface{}, toolChoice interface{}, emitFn func(string, interface{}) error) (ChatResponse, error) {
			resp, err := rt.callChatWithToolsStream(ctx, messages, tools, toolChoice, emitFn)
			if err != nil {
				return ChatResponse{}, err
			}
			toolCalls := make([]ToolCallInfo, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				toolCalls[i] = ToolCallInfo{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				}
			}
			return ChatResponse{
				Content:      resp.Content,
				ToolCalls:    toolCalls,
				FinishReason: resp.FinishReason,
				Usage: TokenUsage{
					InputTokens:  int64(resp.Usage.PromptTokens),
					OutputTokens: int64(resp.Usage.CompletionTokens),
				},
			}, nil
		}
	} else {
		callbacks.OnCallChat = func(ctx context.Context, messages []map[string]interface{}, tools []map[string]interface{}, toolChoice interface{}, emitFn func(string, interface{}) error) (ChatResponse, error) {
			resp, err := rt.callChatWithTools(ctx, messages, tools, toolChoice, emitFn)
			if err != nil {
				return ChatResponse{}, err
			}
			toolCalls := make([]ToolCallInfo, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				toolCalls[i] = ToolCallInfo{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				}
			}
			return ChatResponse{
				Content:      resp.Content,
				ToolCalls:    toolCalls,
				FinishReason: resp.FinishReason,
				Usage: TokenUsage{
					InputTokens:  int64(resp.Usage.PromptTokens),
					OutputTokens: int64(resp.Usage.CompletionTokens),
				},
			}, nil
		}
	}

	loop := NewRunLoop(agentCfg, callbacks, toolHandler)
	loop.SetLogger(rt.logger)

	forkIsolated := forkIsolationFromContext(ctx)
	baseMessages := rt.buildMessagesForRoute(ctx, sessionID, routeToolAgent, forkIsolated)
	if normalized.Text != "" || len(normalized.Parts) > 0 {
		baseMessages = append(baseMessages, map[string]interface{}{"role": "user", "content": normalized.Text})
	}

	tools, _, _, _ := rt.collectToolSpecs(ctx, format, mode, nil)

	result, err := loop.Run(ctx, sessionID, baseMessages, tools, nil)
	if err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, emit, "run_failed", map[string]interface{}{"error": err.Error()})
		return Response{}, err
	}

	switch result.Status {
	case LoopStatusStop:
		reply := ""
		if result.Response != nil {
			reply = result.Response.Summary
		}
		finishRun(run, RunStatusCompleted, reply, nil)
		rt.finalizeRun(run, emit, "run_completed", map[string]interface{}{"reply": reply})
		var plan sdk.Plan
		if result.Response != nil {
			plan = *result.Response
		}
		return Response{Plan: plan, Reply: reply}, nil
	case LoopStatusCompact:
		rt.checkAndCompress(ctx, sessionID)
		var plan sdk.Plan
		if result.Response != nil {
			plan = *result.Response
		}
		return Response{Plan: plan}, nil
	case LoopStatusNeedsFinalSummary:
		loop.compaction.MarkCompacted()
		rt.checkAndCompress(ctx, sessionID)
		llmSummary := rt.conversation.Summary(sessionID)
		reply := ""
		if result.Response != nil {
			reply = result.Response.Summary
		}
		if llmSummary != "" {
			reply = llmSummary
		}
		finalPlan := sdk.Plan{
			Summary: reply,
			Status:  sdk.PlanStatusDone,
		}
		if result.Response != nil {
			finalPlan.Steps = result.Response.Steps
		}
		finishRun(run, RunStatusCompleted, reply, nil)
		rt.finalizeRun(run, emit, "run_completed", map[string]interface{}{"reply": reply})
		return Response{Plan: finalPlan, Reply: reply}, nil
	default:
		finishRun(run, RunStatusFailed, "unknown status", fmt.Errorf("unknown loop status"))
		rt.finalizeRun(run, emit, "run_failed", map[string]interface{}{"error": "unknown loop status"})
		return Response{}, fmt.Errorf("unknown loop status")
	}
}

func (rt *Runtime) AgentLoopBackgroundV2(ctx context.Context, run *RunState, sessionID string, input UserInput, format *OutputFormat, mode AgentMode) (Response, error) {
	ctx = withTeamSession(ctx, sessionID)

	normalized, err := rt.normalizeUserInput(ctx, input)
	if err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, nil, "run_failed", map[string]interface{}{"error": err.Error()})
		return Response{}, err
	}

	if _, err := rt.appendMessage(ctx, sessionID, "user", normalized.Text, normalized.Parts); err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, nil, "run_failed", map[string]interface{}{"error": err.Error()})
		return Response{}, err
	}

	rt.checkAndCompress(ctx, sessionID)

	toolHandler := &ToolHandlerAdapter{
		ExecuteFunc: func(ctx context.Context, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error) {
			return rt.orchestrator.ExecuteStep(ctx, sessionID, step)
		},
		TruncateResultFunc: func(ctx context.Context, sessionID, toolName string, data map[string]interface{}) map[string]interface{} {
			return rt.truncateToolResult(ctx, sessionID, toolName, data)
		},
	}

	agentCfg := AgentConfig{
		MaxSteps:                500,
		MaxConsecutiveFailures:  10,
		MaxStepsWithoutProgress: 50,
		MaxSimilarResults:       5,
		MaxRunDurationMs:        60 * 60 * 1000,
		DoomLoopThreshold:       3,
		ContinueOnDeny:          false,
		Model:                   rt.cfg.Planner.Model,
	}

	callbacks := LoopCallbacks{
		OnRunEvent: func(eventType string, data map[string]interface{}) {
			rt.emitRunEvent(run, nil, eventType, data)
		},
		OnCallChat: func(ctx context.Context, messages []map[string]interface{}, tools []map[string]interface{}, toolChoice interface{}, emitFn func(string, interface{}) error) (ChatResponse, error) {
			resp, err := rt.callChatWithTools(ctx, messages, tools, toolChoice, nil)
			if err != nil {
				return ChatResponse{}, err
			}
			toolCalls := make([]ToolCallInfo, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				toolCalls[i] = ToolCallInfo{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				}
			}
			return ChatResponse{
				Content:      resp.Content,
				ToolCalls:    toolCalls,
				FinishReason: resp.FinishReason,
			}, nil
		},
	}

	loop := NewRunLoop(agentCfg, callbacks, toolHandler)
	loop.SetLogger(rt.logger)

	forkIsolated := forkIsolationFromContext(ctx)
	baseMessages := rt.buildMessagesForRoute(ctx, sessionID, routeToolAgent, forkIsolated)
	if normalized.Text != "" || len(normalized.Parts) > 0 {
		baseMessages = append(baseMessages, map[string]interface{}{"role": "user", "content": normalized.Text})
	}

	tools, _, _, _ := rt.collectToolSpecs(ctx, format, mode, nil)

	result, err := loop.RunWithDeadline(ctx, sessionID, baseMessages, tools, nil, run.Deadline)
	if err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, nil, "run_failed", map[string]interface{}{"error": err.Error()})
		return Response{}, err
	}

	switch result.Status {
	case LoopStatusStop:
		reply := ""
		if result.Response != nil {
			reply = result.Response.Summary
		}
		finishRun(run, RunStatusCompleted, reply, nil)
		rt.finalizeRun(run, nil, "run_completed", map[string]interface{}{"reply": reply})
		var plan sdk.Plan
		if result.Response != nil {
			plan = *result.Response
		}
		return Response{Plan: plan, Reply: reply}, nil
	case LoopStatusCompact:
		rt.checkAndCompress(ctx, sessionID)
		var plan sdk.Plan
		if result.Response != nil {
			plan = *result.Response
		}
		return Response{Plan: plan}, nil
	default:
		finishRun(run, RunStatusFailed, "unknown status", fmt.Errorf("unknown loop status"))
		rt.finalizeRun(run, nil, "run_failed", map[string]interface{}{"error": "unknown loop status"})
		return Response{}, fmt.Errorf("unknown loop status")
	}
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
			ms := rt.memorySystem(sessionID)
			if working := ms.GetWorkingMemory(); working != "" {
				messages = append(messages, map[string]any{"role": "system", "content": "Working context:\n" + truncateLines(working, 100)})
			}
			if longTerm := rt.longTermMemory(sessionID); longTerm != "" {
				messages = append(messages, map[string]any{"role": "system", "content": "Long-term memory:\n" + truncateLines(longTerm, 150)})
			}
			episodic := rt.getRecentEpisodicMemory(sessionID, 5)
			if len(episodic) > 0 {
				var episodicLines []string
				for _, entry := range episodic {
					episodicLines = append(episodicLines, entry.Content)
				}
				messages = append(messages, map[string]any{"role": "system", "content": "Recent events:\n" + truncateLines(strings.Join(episodicLines, "; "), 100)})
			}
			if shortTerm := rt.shortTermMemory(sessionID); shortTerm != "" && shortTerm != rt.conversation.Summary(sessionID) {
				messages = append(messages, map[string]any{"role": "system", "content": "Short-term memory:\n" + truncateLines(shortTerm, 100)})
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

func (rt *Runtime) classifyIntent(ctx context.Context, sessionID string, input normalizedInput, format *OutputFormat, pendingConfirmation bool) intentClassification {
	return intentClassification{Route: string(routeToolAgent), Tags: []string{"general"}, SuggestedTools: []string{"shell", "web", "file_read", "file_search", "edit"}, Confidence: "high", Reason: "All requests go through tool agent."}
}

func allowedToolsForClassification(route requestRoute, suggested []string) []string {
	return nil
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

var toolSystemPrompt = buildToolSystemPrompt()

func buildToolSystemPrompt() string {
	if prompts.System == "" {
		return fallbackToolSystemPrompt
	}
	return prompts.System + "\n\n## Skills\n\n" +
		"### Coding (see coding.md)\n" + prompts.Coding + "\n\n" +
		"### Debugging (see debug.md)\n" + prompts.Debug + "\n\n" +
		"### Testing (see testing.md)\n" + prompts.Testing + "\n\n" +
		"### Refactoring (see refactor.md)\n" + prompts.Refactor
}

const fallbackToolSystemPrompt = `You are Morpheus, an autonomous coding assistant. Your job is to solve the user's problem in the fewest safe steps, with minimal back-and-forth.

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

## Code Writing Principles
- Write the minimum viable code that solves the problem correctly first.
- Prefer simple, idiomatic solutions over complex clever ones.
- Avoid premature optimization: do not add performance optimizations, alternative implementations, or multiple variations unless the user explicitly asks for them.
- Do not include multiple algorithm implementations (e.g., both Lomuto and Hoare partition schemes) when one correct implementation suffices.
- Do not add extensive comments, documentation, or example usage code unless requested.
- Only expand scope with additional features, optimizations, or variations when the user explicitly requests them.
- Follow the "Red-Green-Refactor" approach: first make it work with minimal code, then improve if needed.

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
		if blockAsk && meta.Name() == "question" {
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

func isTextBasedToolCalls(calls []toolCall) bool {
	if len(calls) == 0 {
		return false
	}
	for _, call := range calls {
		if strings.HasPrefix(call.ID, "call_") {
			return true
		}
	}
	return false
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

	profile := llm.DetectProviderProfile(plannerCfg.Provider, model)

	cleanedMessages, _ := profile.BuildMessages(messages)
	payload, _ := profile.BuildPayload(model, cleanedMessages, tools, int(plannerCfg.Temperature), profile.DefaultMaxTokens)

	body, err := json.Marshal(payload)
	if err != nil {
		return chatResponse{}, err
	}
	endpoint := profile.GetEndpoint(plannerCfg.Endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return chatResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if profile.UseAnthropicFormat && !profile.UseOpenAIFormat {
		httpReq.Header.Set("x-api-key", plannerCfg.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	} else {
		switch plannerCfg.Provider {
		case "openai", "glm", "deepseek", "anthropic", "openrouter", "groq", "mistral", "togetherai", "perplexity", "minimax":
			httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
		}
	}
	resp, err := llmHTTPClient.Do(httpReq)
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

	parsed, err := profile.ParseResponse(respBody)
	if err != nil {
		return chatResponse{}, err
	}

	out := chatResponse{
		Content:      parsed.Content,
		FinishReason: parsed.FinishReason,
		Usage:        parsed.Usage,
	}
	for _, tc := range parsed.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, toolCall{
			ID:        tc.ID,
			Name:      tc.Name,
			Arguments: tc.Arguments,
		})
	}

	if len(out.ToolCalls) == 0 && profile.RequiresTextToolCalls && parsed.Content != "" {
		rt.logger.Info("No structured tool calls, trying text parsing", zap.String("contentLength", fmt.Sprintf("%d", len(parsed.Content))))
		textCalls := profile.ParseTextToolCalls(parsed.Content)
		if len(textCalls) > 0 {
			rt.logger.Info("Text parsing found tool calls", zap.Int("count", len(textCalls)))
			for _, tc := range textCalls {
				out.ToolCalls = append(out.ToolCalls, toolCall{
					ID:        tc.ID,
					Name:      tc.Name,
					Arguments: tc.Arguments,
				})
			}
			out.FinishReason = "tool_calls"
		}
	}

	if rt.metrics != nil && parsed.Usage.TotalTokens > 0 {
		rt.metrics.addTokens(int64(parsed.Usage.PromptTokens), int64(parsed.Usage.CompletionTokens))
	}
	return out, nil
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
	data := part.Output
	if data != nil {
		if truncated, ok := data["truncated"].(bool); ok && truncated {
			if content, ok := data["content"].(string); ok && len(content) > 5000 {
				data["content"] = truncateLines(content, 100)
			}
		}
	}
	payload := map[string]any{
		"success": part.Status == "completed",
		"data":    data,
		"error":   part.Error,
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf("%v", payload)
	}
	result := string(jsonData)
	if len(result) > 8000 {
		preview := truncateLines(result, 150)
		return preview + "\n\n[Tool output truncated to fit context window]"
	}
	return result
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
