package registry

import (
	"context"
	"errors"
	"testing"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type mockTool struct {
	nameVal string
	result  string
	err     error
}

func (m *mockTool) Name() string { return m.nameVal }
func (m *mockTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	return sdk.ToolResult{Success: true, Data: map[string]any{"output": m.result}}, m.err
}

type errorTool struct{}

func (e *errorTool) Name() string { return "error-tool" }
func (e *errorTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	return sdk.ToolResult{Success: false, Error: "tool error"}, errors.New("tool error")
}

func TestNewRegistry(t *testing.T) {
	r := NewRegistry()
	if r == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if r.tools == nil {
		t.Error("tools map is nil")
	}
}

func TestRegister(t *testing.T) {
	r := NewRegistry()

	err := r.Register(nil)
	if err == nil {
		t.Error("Register(nil) should return error")
	}

	err = r.Register(&mockTool{nameVal: ""})
	if err == nil {
		t.Error("Register tool with empty name should return error")
	}

	tool := &mockTool{nameVal: "test-tool"}
	err = r.Register(tool)
	if err != nil {
		t.Errorf("Register() returned error: %v", err)
	}
}

func TestGet(t *testing.T) {
	r := NewRegistry()

	tool := &mockTool{nameVal: "test-tool"}
	_ = r.Register(tool)

	got, ok := r.Get("test-tool")
	if !ok {
		t.Error("Get() returned false, expected true")
	}
	if got.Name() != "test-tool" {
		t.Errorf("Get() returned tool with name %q, want %q", got.Name(), "test-tool")
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("Get() for nonexistent tool returned true, expected false")
	}
}

func TestAll(t *testing.T) {
	r := NewRegistry()

	tools := r.All()
	if len(tools) != 0 {
		t.Errorf("All() on empty registry returned %d tools, want 0", len(tools))
	}

	_ = r.Register(&mockTool{nameVal: "tool1"})
	_ = r.Register(&mockTool{nameVal: "tool2"})

	tools = r.All()
	if len(tools) != 2 {
		t.Errorf("All() returned %d tools, want 2", len(tools))
	}
}

func TestRegisterDuplicate(t *testing.T) {
	r := NewRegistry()

	tool1 := &mockTool{nameVal: "test-tool"}
	tool2 := &mockTool{nameVal: "test-tool"}

	_ = r.Register(tool1)
	err := r.Register(tool2)
	if err != nil {
		t.Errorf("Register() duplicate returned error: %v", err)
	}

	tools := r.All()
	if len(tools) != 1 {
		t.Errorf("All() after duplicate register returned %d tools, want 1", len(tools))
	}
}

func TestConcurrentAccess(t *testing.T) {
	r := NewRegistry()
	done := make(chan bool)

	for i := 0; i < 50; i++ {
		go func(n int) {
			tool := &mockTool{nameVal: "tool"}
			r.Register(tool)
			done <- true
		}(i)
	}

	for i := 0; i < 50; i++ {
		go func(n int) {
			r.Get("tool")
			r.All()
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}
