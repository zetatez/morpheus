package plugin

import (
	"sync"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type MessagePayload struct {
	Role    string
	Content string
	Parts   []sdk.MessagePart
}

type MessageContext struct {
	SessionID string
}

type SystemContext struct {
	SessionID string
}

type ToolContext struct {
	SessionID string
	Tool      string
}

type MessageHook func(ctx MessageContext, payload MessagePayload) MessagePayload
type SystemHook func(ctx SystemContext, system string) string
type ToolBeforeHook func(ctx ToolContext, input map[string]any) map[string]any
type ToolAfterHook func(ctx ToolContext, result sdk.ToolResult) sdk.ToolResult

type Registry struct {
	mu           sync.RWMutex
	messageHooks []MessageHook
	systemHooks  []SystemHook
	toolBefore   []ToolBeforeHook
	toolAfter    []ToolAfterHook
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) RegisterMessage(hook MessageHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.messageHooks = append(r.messageHooks, hook)
}

func (r *Registry) RegisterSystem(hook SystemHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.systemHooks = append(r.systemHooks, hook)
}

func (r *Registry) RegisterToolBefore(hook ToolBeforeHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolBefore = append(r.toolBefore, hook)
}

func (r *Registry) RegisterToolAfter(hook ToolAfterHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolAfter = append(r.toolAfter, hook)
}

func (r *Registry) ApplyMessage(ctx MessageContext, payload MessagePayload) MessagePayload {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.messageHooks {
		payload = hook(ctx, payload)
	}
	return payload
}

func (r *Registry) ApplySystem(ctx SystemContext, system string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.systemHooks {
		system = hook(ctx, system)
	}
	return system
}

func (r *Registry) ApplyToolBefore(ctx ToolContext, input map[string]any) map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.toolBefore {
		input = hook(ctx, input)
	}
	return input
}

func (r *Registry) ApplyToolAfter(ctx ToolContext, result sdk.ToolResult) sdk.ToolResult {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.toolAfter {
		result = hook(ctx, result)
	}
	return result
}
