package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/internal/tools/todotool"
)

const (
	defaultTodoPriority = "medium"
	maxAutoTodos        = 6
	maxTodoPlannerRetry = 2
)

type todoPlan struct {
	Todos []todoPlanItem `json:"todos"`
}

type todoPlanItem struct {
	ID       string `json:"id"`
	Content  string `json:"content"`
	Status   string `json:"status,omitempty"`
	Priority string `json:"priority,omitempty"`
	Active   bool   `json:"active,omitempty"`
}

func (rt *Runtime) planTodosFromInput(ctx context.Context, sessionID, input string) []RunTodo {
	if !shouldCreateTodos(input) {
		return nil
	}
	if todos, ok := rt.planTodosWithModel(ctx, sessionID, input); ok {
		return todos
	}
	return fallbackTodosFromInput(input)
}

func shouldCreateTodos(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	if lower == "" {
		return false
	}
	if len(strings.Fields(lower)) >= 18 {
		return true
	}
	markers := []string{"\n", " then ", " and ", " also ", " first ", " next ", " finally ", "todo", "tasks", "step by step"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func (rt *Runtime) planTodosWithModel(ctx context.Context, sessionID, input string) ([]RunTodo, bool) {
	if rt == nil || rt.cfg.Planner.Provider == "builtin" || rt.cfg.Planner.APIKey == "" {
		return nil, false
	}
	messages := []map[string]any{{"role": "system", "content": todoPlannerSystemPrompt}}
	if context := rt.coordinatorContext(sessionID, input); context != "" {
		messages = append(messages, map[string]any{"role": "system", "content": context})
	}
	messages = append(messages, map[string]any{"role": "user", "content": input})
	for attempt := 0; attempt < maxTodoPlannerRetry; attempt++ {
		resp, err := rt.callChatWithTools(ctx, messages, nil, nil, nil)
		if err != nil {
			return nil, false
		}
		plan, err := parseTodoPlan(strings.TrimSpace(resp.Content))
		if err == nil {
			todos := normalizeTodoPlan(plan)
			if len(todos) > 0 {
				return todos, true
			}
		}
		messages = append(messages, map[string]any{"role": "system", "content": "Return valid JSON only. Include 3-6 todos with concise content."})
	}
	return nil, false
}

func parseTodoPlan(content string) (todoPlan, error) {
	if content == "" {
		return todoPlan{}, fmt.Errorf("empty todo plan")
	}
	jsonPayload := extractJSON(content)
	var plan todoPlan
	if err := json.Unmarshal([]byte(jsonPayload), &plan); err != nil {
		return todoPlan{}, err
	}
	return plan, nil
}

func normalizeTodoPlan(plan todoPlan) []RunTodo {
	if len(plan.Todos) == 0 {
		return nil
	}
	if len(plan.Todos) > maxAutoTodos {
		plan.Todos = plan.Todos[:maxAutoTodos]
	}
	seen := map[string]struct{}{}
	out := make([]RunTodo, 0, len(plan.Todos))
	for i, item := range plan.Todos {
		content := tidyTodoContent(item.Content)
		if content == "" {
			continue
		}
		id := strings.TrimSpace(item.ID)
		if id == "" {
			id = fmt.Sprintf("todo-%d", i+1)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		status := normalizeTodoStatus(item.Status)
		priority := normalizeTodoPriority(item.Priority)
		out = append(out, RunTodo{
			ID:       id,
			Content:  content,
			Status:   status,
			Priority: priority,
			Active:   status == "in_progress" || item.Active,
		})
	}
	ensureSingleActiveTodo(out)
	return out
}

func fallbackTodosFromInput(input string) []RunTodo {
	segments := splitTodoSegments(input)
	if len(segments) == 0 {
		segments = []string{
			"Inspect the current context and constraints",
			strings.TrimSpace(input),
			"Verify the result and report what changed",
		}
	}
	if len(segments) > maxAutoTodos {
		segments = segments[:maxAutoTodos]
	}
	todos := make([]RunTodo, 0, len(segments))
	for i, segment := range segments {
		content := tidyTodoContent(segment)
		if content == "" {
			continue
		}
		status := "pending"
		active := false
		if len(todos) == 0 {
			status = "in_progress"
			active = true
		}
		todos = append(todos, RunTodo{ID: fmt.Sprintf("todo-%d", i+1), Content: content, Status: status, Priority: defaultTodoPriority, Active: active})
	}
	return todos
}

func splitTodoSegments(input string) []string {
	text := strings.ReplaceAll(input, "\r\n", "\n")
	var segments []string
	if strings.Contains(text, "\n") {
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			line = strings.TrimLeft(line, "-*0123456789. )")
			if line != "" {
				segments = append(segments, line)
			}
		}
		if len(segments) > 1 {
			return segments
		}
	}
	flat := text
	for _, marker := range []string{" then ", " also ", " finally ", " next ", " and then "} {
		flat = strings.ReplaceAll(flat, marker, "|")
	}
	for _, part := range strings.Split(flat, "|") {
		part = strings.TrimSpace(part)
		if part != "" {
			segments = append(segments, part)
		}
	}
	if len(segments) <= 1 {
		return nil
	}
	return segments
}

func tidyTodoContent(text string) string {
	text = strings.TrimSpace(strings.TrimSuffix(text, "."))
	if text == "" {
		return ""
	}
	return strings.ToUpper(text[:1]) + text[1:]
}

func renderTodoSystemPrompt(todos []RunTodo) string {
	if len(todos) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("This task is being tracked with a todo list. Work through the items in order, keep one item in progress when practical, and align tool usage with the active todo.\n\nCurrent todos:\n")
	for _, todo := range todos {
		marker := "[ ]"
		if todo.Status == "completed" {
			marker = "[x]"
		} else if todo.Status == "in_progress" {
			marker = "[>]"
		} else if todo.Status == "failed" {
			marker = "[!]"
		}
		b.WriteString(fmt.Sprintf("- %s %s\n", marker, todo.Content))
	}
	b.WriteString("\nBefore major work, acknowledge the todo structure briefly. When you choose a tool, make sure it advances the active todo.")
	return b.String()
}

func ensureSingleActiveTodo(todos []RunTodo) {
	if len(todos) == 0 {
		return
	}
	activeFound := false
	for i := range todos {
		if todos[i].Status == "completed" || todos[i].Status == "cancelled" || todos[i].Status == "failed" {
			todos[i].Active = false
			continue
		}
		if !activeFound && (todos[i].Status == "in_progress" || todos[i].Active) {
			todos[i].Status = "in_progress"
			todos[i].Active = true
			activeFound = true
			continue
		}
		if todos[i].Status == "in_progress" {
			todos[i].Status = "pending"
		}
		todos[i].Active = false
	}
	if !activeFound {
		for i := range todos {
			if todos[i].Status == "pending" {
				todos[i].Status = "in_progress"
				todos[i].Active = true
				return
			}
		}
	}
}

func advanceTodosFromResponse(rt *Runtime, run *RunState, content string, emit replEmitter) {
	if rt == nil || run == nil || len(run.Todos) == 0 || strings.TrimSpace(content) == "" {
		return
	}
	next := append([]RunTodo(nil), run.Todos...)
	changed := false
	for i := range next {
		if next[i].Status == "completed" || next[i].Status == "cancelled" {
			continue
		}
		next[i].Status = "completed"
		next[i].Active = false
		changed = true
	}
	if changed {
		rt.updateRunTodos(run, next, emit)
	}
}

func advanceTodosFromTool(rt *Runtime, run *RunState, toolName string, success bool, emit replEmitter) {
	if rt == nil || run == nil || len(run.Todos) == 0 {
		return
	}
	next := append([]RunTodo(nil), run.Todos...)
	idx := currentTodoIndex(next)
	if idx < 0 {
		return
	}
	next[idx].Tool = toolName
	if success {
		next[idx].Status = "completed"
		next[idx].Active = false
		next[idx].Note = "Step finished successfully"
		if idx+1 < len(next) && next[idx+1].Status == "pending" {
			next[idx+1].Status = "in_progress"
			next[idx+1].Active = true
			next[idx+1].Note = "Ready for the next step"
		}
	} else {
		next[idx].Status = "failed"
		next[idx].Active = true
		next[idx].Note = "Last tool failed; retry or update the plan"
	}
	rt.updateRunTodos(run, next, emit)
}

func markTodoInProgress(rt *Runtime, run *RunState, toolName string, emit replEmitter) {
	if rt == nil || run == nil || len(run.Todos) == 0 {
		return
	}
	next := append([]RunTodo(nil), run.Todos...)
	idx := currentTodoIndex(next)
	if idx < 0 {
		return
	}
	for i := range next {
		next[i].Active = false
		if i != idx && next[i].Status == "in_progress" {
			next[i].Status = "pending"
		}
	}
	next[idx].Status = "in_progress"
	next[idx].Active = true
	next[idx].Tool = toolName
	next[idx].Note = "Running current step"
	rt.updateRunTodos(run, next, emit)
}

func currentTodoIndex(todos []RunTodo) int {
	for i, todo := range todos {
		if todo.Status == "in_progress" || todo.Active {
			return i
		}
	}
	for i, todo := range todos {
		if todo.Status == "pending" {
			return i
		}
	}
	return -1
}

func containsTodoWriteCall(nameMap map[string]string, calls []toolCall) bool {
	for _, call := range calls {
		toolName := nameMap[call.Name]
		if toolName == "" {
			toolName = call.Name
		}
		if toolName == "todo.write" || call.Name == normalizeToolName("todo.write") {
			return true
		}
	}
	return false
}

func hasCompletedTodo(todos []RunTodo) bool {
	for _, todo := range todos {
		if todo.Status == "completed" {
			return true
		}
	}
	return false
}

const todoPlannerSystemPrompt = `You are a task planner for a coding agent.

Return JSON only with this schema:
{
  "todos": [
    {"id": "todo-1", "content": "...", "status": "in_progress|pending", "priority": "high|medium|low", "active": true|false}
  ]
}

Rules:
- Produce 3 to 6 todos for complex tasks.
- Make todos concrete, execution-oriented, and ordered.
- Mark exactly one todo as in_progress and active=true.
- Use pending for the rest.
- Keep content short and specific.
- Focus on investigation, implementation, verification, and follow-up as needed.
- Return valid JSON only.`

func (rt *Runtime) ReplaceTodos(sessionID string, todos []todotool.Todo) ([]todotool.Todo, error) {
	if rt == nil {
		return nil, fmt.Errorf("runtime not configured")
	}
	run, ok := rt.runs.latestBySession(sessionID)
	if !ok || run == nil {
		return nil, fmt.Errorf("run not found for session %s", sessionID)
	}
	raw := make([]map[string]any, 0, len(todos))
	for _, todo := range todos {
		raw = append(raw, map[string]any{
			"id":       todo.ID,
			"content":  todo.Content,
			"status":   todo.Status,
			"priority": todo.Priority,
			"active":   todo.Active,
			"tool":     todo.Tool,
			"note":     todo.Note,
		})
	}
	normalized := normalizeTodos(raw)
	ensureSingleActiveTodo(normalized)
	rt.updateRunTodos(run, normalized, nil)
	out := make([]todotool.Todo, 0, len(normalized))
	for _, todo := range normalized {
		out = append(out, todotool.Todo{
			ID:       todo.ID,
			Content:  todo.Content,
			Status:   todo.Status,
			Priority: todo.Priority,
			Active:   todo.Active,
			Tool:     todo.Tool,
			Note:     todo.Note,
		})
	}
	return out, nil
}
