package registry

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type CheckFn func() bool

type ToolMetadata struct {
	Category      string
	Tags          []string
	Deprecated    bool
	DeprecatedMsg string
	Version       string
	Author        string
	RegisteredAt  time.Time
	LastUsedAt    time.Time
	UseCount      int64

	CheckFn     CheckFn
	RequiresEnv []string
}

type ToolEntry struct {
	Tool     sdk.Tool
	Metadata ToolMetadata
}

type Registry struct {
	mu         sync.RWMutex
	tools      map[string]ToolEntry
	categories map[string][]string
}

func NewRegistry() *Registry {
	return &Registry{
		tools:      make(map[string]ToolEntry),
		categories: make(map[string][]string),
	}
}

func (r *Registry) Register(tool sdk.Tool) error {
	return r.RegisterWithMetadata(tool, ToolMetadata{RegisteredAt: time.Now()})
}

func (r *Registry) RegisterWithMetadata(tool sdk.Tool, metadata ToolMetadata) error {
	if tool == nil {
		return fmt.Errorf("tool is nil")
	}
	name := tool.Name()
	if name == "" {
		return fmt.Errorf("tool name required")
	}

	if err := r.validateEnvRequirements(metadata.RequiresEnv); err != nil {
		return fmt.Errorf("tool %s: %w", name, err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.tools[name] = ToolEntry{
		Tool:     tool,
		Metadata: metadata,
	}

	if metadata.Category != "" {
		r.categories[metadata.Category] = append(r.categories[metadata.Category], name)
	}

	return nil
}

func (r *Registry) validateEnvRequirements(envs []string) error {
	for _, env := range envs {
		if strings.TrimSpace(env) == "" {
			continue
		}
		if _, exists := os.LookupEnv(env); !exists {
			return fmt.Errorf("required environment variable %s is not set", env)
		}
	}
	return nil
}

func (r *Registry) IsToolAvailable(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.tools[name]
	if !ok {
		return false
	}

	if entry.Metadata.CheckFn != nil && !entry.Metadata.CheckFn() {
		return false
	}

	for _, env := range entry.Metadata.RequiresEnv {
		if strings.TrimSpace(env) != "" {
			if _, exists := os.LookupEnv(env); !exists {
				return false
			}
		}
	}

	return true
}

func (r *Registry) GetUnavailableReason(name string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.tools[name]
	if !ok {
		return "tool not found"
	}

	if entry.Metadata.CheckFn != nil && !entry.Metadata.CheckFn() {
		return "tool runtime check failed"
	}

	for _, env := range entry.Metadata.RequiresEnv {
		if strings.TrimSpace(env) != "" {
			if _, exists := os.LookupEnv(env); !exists {
				return fmt.Sprintf("required environment variable %s is not set", env)
			}
		}
	}

	return ""
}

func (r *Registry) ListAvailable() []sdk.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var tools []sdk.Tool
	for _, entry := range r.tools {
		if !entry.Metadata.Deprecated && r.isAvailableUnlocked(entry) {
			tools = append(tools, entry.Tool)
		}
	}
	return tools
}

func (r *Registry) isAvailableUnlocked(entry ToolEntry) bool {
	if entry.Metadata.CheckFn != nil && !entry.Metadata.CheckFn() {
		return false
	}
	for _, env := range entry.Metadata.RequiresEnv {
		if strings.TrimSpace(env) != "" {
			if _, exists := os.LookupEnv(env); !exists {
				return false
			}
		}
	}
	return true
}

func (r *Registry) RegisterDeprecated(tool sdk.Tool, category, deprecatedMsg string) error {
	return r.RegisterWithMetadata(tool, ToolMetadata{
		Category:      category,
		Deprecated:    true,
		DeprecatedMsg: deprecatedMsg,
		RegisteredAt:  time.Now(),
	})
}

func (r *Registry) Get(name string) (sdk.Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.tools[name]
	if !ok {
		return nil, false
	}
	return entry.Tool, true
}

func (r *Registry) GetWithMetadata(name string) (ToolEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.tools[name]
	return entry, ok
}

func (r *Registry) All() []sdk.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]sdk.Tool, 0, len(r.tools))
	for _, entry := range r.tools {
		tools = append(tools, entry.Tool)
	}
	return tools
}

func (r *Registry) AllWithMetadata() []ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := make([]ToolEntry, 0, len(r.tools))
	for _, entry := range r.tools {
		entries = append(entries, entry)
	}
	return entries
}

func (r *Registry) ListByCategory(category string) []sdk.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names, ok := r.categories[category]
	if !ok {
		return nil
	}
	tools := make([]sdk.Tool, 0, len(names))
	for _, name := range names {
		if entry, ok := r.tools[name]; ok {
			tools = append(tools, entry.Tool)
		}
	}
	return tools
}

func (r *Registry) ListActive() []sdk.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]sdk.Tool, 0, len(r.tools))
	for _, entry := range r.tools {
		if !entry.Metadata.Deprecated {
			tools = append(tools, entry.Tool)
		}
	}
	return tools
}

func (r *Registry) ListDeprecated() []ToolEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var deprecated []ToolEntry
	for _, entry := range r.tools {
		if entry.Metadata.Deprecated {
			deprecated = append(deprecated, entry)
		}
	}
	return deprecated
}

func (r *Registry) Categories() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cats := make([]string, 0, len(r.categories))
	for cat := range r.categories {
		cats = append(cats, cat)
	}
	return cats
}

func (r *Registry) GetCategoryForTool(name string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entry, ok := r.tools[name]
	if !ok {
		return "", false
	}
	return entry.Metadata.Category, entry.Metadata.Category != ""
}

func (r *Registry) RecordUse(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry, ok := r.tools[name]; ok {
		entry.Metadata.UseCount++
		entry.Metadata.LastUsedAt = time.Now()
		r.tools[name] = entry
	}
}

func (r *Registry) Filter(predicate func(ToolEntry) bool) []sdk.Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var tools []sdk.Tool
	for _, entry := range r.tools {
		if predicate(entry) {
			tools = append(tools, entry.Tool)
		}
	}
	return tools
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

func (r *Registry) CountByCategory() map[string]int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]int)
	for cat, names := range r.categories {
		result[cat] = len(names)
	}
	return result
}

var _ sdk.ToolRegistry = (*Registry)(nil)
