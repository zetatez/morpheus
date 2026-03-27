package app

import (
	"fmt"
	"strings"
)

const (
	defaultTodoPriority = "medium"
	maxAutoTodos        = 6
)

func planTodosFromInput(input string) []RunTodo {
	if !shouldCreateTodos(input) {
		return nil
	}
	segments := splitTodoSegments(input)
	if len(segments) == 0 {
		segments = fallbackTodoSegments(input)
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
		todos = append(todos, RunTodo{
			ID:       fmt.Sprintf("todo-%d", i+1),
			Content:  content,
			Status:   status,
			Priority: defaultTodoPriority,
			Active:   active,
		})
	}
	return todos
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
	lower := text
	replacements := []string{" then ", " also ", " finally ", " next ", " and then "}
	for _, marker := range replacements {
		lower = strings.ReplaceAll(lower, marker, "|")
	}
	parts := strings.Split(lower, "|")
	if len(parts) <= 1 {
		return nil
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			segments = append(segments, part)
		}
	}
	return segments
}

func fallbackTodoSegments(input string) []string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return nil
	}
	return []string{
		"Inspect the current context and constraints",
		trimmed,
		"Verify the result and report what changed",
	}
}

func tidyTodoContent(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimSuffix(text, ".")
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
	b.WriteString("This task looks complex. Start by following and updating this todo list. Keep exactly one todo in_progress when possible. Complete items as you finish them.\n\n")
	b.WriteString("Current todos:\n")
	for _, todo := range todos {
		marker := "[ ]"
		if todo.Status == "completed" {
			marker = "[x]"
		} else if todo.Status == "in_progress" {
			marker = "[>]"
		}
		b.WriteString(fmt.Sprintf("- %s %s\n", marker, todo.Content))
	}
	b.WriteString("\nBefore starting major work, acknowledge the todo structure briefly, then execute tasks in order and keep the list current in run events.")
	return b.String()
}

func advanceTodosFromTool(rt *Runtime, run *RunState, toolName string, success bool, emit replEmitter) {
	if rt == nil || run == nil || len(run.Todos) == 0 || !success {
		return
	}
	next := append([]RunTodo(nil), run.Todos...)
	idx := currentTodoIndex(next)
	if idx < 0 {
		return
	}
	if shouldCompleteTodoFromTool(toolName) {
		next[idx].Status = "completed"
		next[idx].Active = false
		if idx+1 < len(next) {
			next[idx+1].Status = "in_progress"
			next[idx+1].Active = true
		}
		rt.updateRunTodos(run, next, emit)
	}
}

func advanceTodosFromResponse(rt *Runtime, run *RunState, content string, emit replEmitter) {
	if rt == nil || run == nil || len(run.Todos) == 0 {
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

func shouldCompleteTodoFromTool(toolName string) bool {
	lower := strings.ToLower(strings.TrimSpace(toolName))
	if lower == "" {
		return false
	}
	markers := []string{"write", "edit", "exec", "fetch", "agent.", "git."}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
