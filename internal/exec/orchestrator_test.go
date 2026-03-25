package exec

import (
	"context"
	"testing"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type mockTool struct {
	name string
}

func (t *mockTool) Name() string { return t.name }
func (t *mockTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	return sdk.ToolResult{Success: true, Data: input}, nil
}

type mockRegistry struct {
	tools map[string]sdk.Tool
}

func (r *mockRegistry) Register(tool sdk.Tool) error {
	if r.tools == nil {
		r.tools = make(map[string]sdk.Tool)
	}
	r.tools[tool.Name()] = tool
	return nil
}

func (r *mockRegistry) Get(name string) (sdk.Tool, bool) {
	if r.tools == nil {
		return nil, false
	}
	tool, ok := r.tools[name]
	return tool, ok
}

func TestFindTool(t *testing.T) {
	registry := &mockRegistry{}
	registry.tools = map[string]sdk.Tool{
		"web_fetch":  &mockTool{name: "web_fetch"},
		"todo_write": &mockTool{name: "todo_write"},
		"fs_read":    &mockTool{name: "fs_read"},
	}

	orch := NewOrchestrator(registry, nil, "/tmp", nil)

	tests := []struct {
		input    string
		expected string
		found    bool
	}{
		{"web_fetch", "web_fetch", true},
		{"web\nfetch", "web_fetch", true},
		{"web fetch", "web_fetch", true},
		{"WEB_FETCH", "web_fetch", true},
		{"WebFetch", "web_fetch", true},
		{"Web_Fetch", "web_fetch", true},
		{"unknown_tool", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			name, _, found := orch.findTool(tt.input)
			if found != tt.found {
				t.Errorf("findTool(%q) found=%v, want %v", tt.input, found, tt.found)
			}
			if found && name != tt.expected {
				t.Errorf("findTool(%q) = %q, want %q", tt.input, name, tt.expected)
			}
		})
	}
}

func TestExecuteStepWithNormalizedName(t *testing.T) {
	registry := &mockRegistry{}
	registry.tools = map[string]sdk.Tool{
		"web_fetch": &mockTool{name: "web_fetch"},
	}

	orch := NewOrchestrator(registry, nil, "/tmp", nil)

	step := sdk.PlanStep{
		ID:     "step1",
		Tool:   "web\nfetch",
		Inputs: map[string]any{"url": "https://example.com"},
	}

	result, err := orch.ExecuteStep(context.Background(), "session1", step)
	if err != nil {
		t.Errorf("ExecuteStep failed: %v", err)
	}
	if !result.Success {
		t.Errorf("ExecuteStep result.Success=false, want true")
	}
}

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"web_fetch", "web_fetch"},
		{"web\nfetch", "web_fetch"},
		{"web\rfetch", "web_fetch"},
		{"web fetch", "web_fetch"},
		{"WEB_FETCH", "web_fetch"},
		{"Web_Fetch", "web_fetch"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sdk.NormalizeToolName(tt.input)
			if result != tt.expected {
				t.Errorf("sdk.NormalizeToolName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
