package registry

import (
	"fmt"
	"sync"

	"github.com/zetatez/morpheus/pkg/sdk"
)

// Registry satisfies sdk.ToolRegistry using an in-memory map.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]sdk.Tool
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{tools: make(map[string]sdk.Tool)}
}

// Register adds a tool to the registry.
func (r *Registry) Register(tool sdk.Tool) error {
	if tool == nil {
		return fmt.Errorf("tool is nil")
	}
	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tools[name] = tool
	return nil
}

// Get looks up a tool by name.
func (r *Registry) Get(name string) (sdk.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tool, ok := r.tools[name]
	return tool, ok
}

// All returns all registered tools.
func (r *Registry) All() []sdk.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]sdk.Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		tools = append(tools, tool)
	}
	return tools
}
