package todotool

import (
	"context"
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type Todo struct {
	ID       string
	Content  string
	Status   string
	Priority string
	Active   bool
	Tool     string
	Note     string
}

type Store interface {
	ReplaceTodos(sessionID string, todos []Todo) ([]Todo, error)
}

type Tool struct {
	store Store
}

func New(store Store) *Tool {
	return &Tool{store: store}
}

func (t *Tool) Name() string { return "todo.write" }

func (t *Tool) Describe() string {
	return "Create or update the active todo list for the current task. Use this for complex multi-step work and keep exactly one item in progress when practical."
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":       map[string]any{"type": "string"},
						"content":  map[string]any{"type": "string"},
						"status":   map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "completed", "failed", "cancelled"}},
						"priority": map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
						"active":   map[string]any{"type": "boolean"},
						"note":     map[string]any{"type": "string"},
					},
					"required": []string{"content", "status", "priority"},
				},
			},
		},
		"required": []string{"todos"},
	}
}

func (t *Tool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	if t.store == nil {
		return sdk.ToolResult{Success: false}, fmt.Errorf("todo store not configured")
	}
	sessionID, _ := input["session_id"].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("session_id is required")
	}
	rawTodos, _ := input["todos"].([]any)
	if len(rawTodos) == 0 {
		return sdk.ToolResult{Success: false}, fmt.Errorf("todos is required")
	}
	todos := make([]Todo, 0, len(rawTodos))
	for i, raw := range rawTodos {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		content, _ := item["content"].(string)
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}
		id, _ := item["id"].(string)
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("todo-%d", i+1)
		}
		status, _ := item["status"].(string)
		priority, _ := item["priority"].(string)
		active, _ := item["active"].(bool)
		note, _ := item["note"].(string)
		todos = append(todos, Todo{ID: id, Content: content, Status: status, Priority: priority, Active: active, Note: note})
	}
	updated, err := t.store.ReplaceTodos(sessionID, todos)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	out := make([]map[string]any, 0, len(updated))
	for _, todo := range updated {
		out = append(out, map[string]any{
			"id":       todo.ID,
			"content":  todo.Content,
			"status":   todo.Status,
			"priority": todo.Priority,
			"active":   todo.Active,
			"tool":     todo.Tool,
			"note":     todo.Note,
		})
	}
	return sdk.ToolResult{Success: true, Data: map[string]any{"todos": out, "count": len(out)}}, nil
}
