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
	Agent     string
	Model     *ModelInfo
	MessageID string
	Variant   string
}

type ModelInfo struct {
	ProviderID string
	ModelID    string
}

type SystemContext struct {
	SessionID string
}

type ToolContext struct {
	SessionID string
	Tool      string
	CallID    string
}

type ChatParamsContext struct {
	SessionID string
	Agent     string
	Model     ModelInfo
	Message   MessagePayload
}

type ChatParamsOutput struct {
	Temperature     float64
	TopP            float64
	TopK            int
	MaxOutputTokens *int
	Options         map[string]any
}

type ChatHeadersContext struct {
	SessionID string
	Agent     string
	Model     ModelInfo
	Message   MessagePayload
}

type ChatHeadersOutput struct {
	Headers map[string]string
}

type PermissionContext struct {
	Permission     Permission
	Justification  string
	RiskLevel      RiskLevel
	TimeoutSeconds int
	SessionID      string
	AgentName      string
}

type RiskLevel string

const (
	RiskLevelLow      RiskLevel = "low"
	RiskLevelMedium   RiskLevel = "medium"
	RiskLevelHigh     RiskLevel = "high"
	RiskLevelCritical RiskLevel = "critical"
)

type Permission struct {
	ID       string
	Tool     string
	Command  string
	Args     map[string]any
	Risk     string
	Resource string
}

type PermissionOutput struct {
	Status        string
	Justification string
	AutoGrant     bool
	GrantUntil    int64
}

type CommandExecuteContext struct {
	Command   string
	SessionID string
	Arguments string
}

type CommandExecuteOutput struct {
	Parts []sdk.MessagePart
}

type ShellEnvContext struct {
	Cwd       string
	SessionID string
	CallID    string
}

type ShellEnvOutput struct {
	Env map[string]string
}

type ToolExecuteContext struct {
	Tool      string
	SessionID string
	CallID    string
	Args      any
}

type ToolExecuteOutput struct {
	Title    string
	Output   string
	Metadata any
}

type ChatMessagesTransformContext struct{}

type ChatMessagesTransformOutput struct {
	Messages []ChatMessageInfo
}

type ChatMessageInfo struct {
	Info  sdk.Message
	Parts []sdk.MessagePart
}

type ChatSystemTransformContext struct {
	SessionID string
	Model     ModelInfo
}

type ChatSystemTransformOutput struct {
	System []string
}

type SessionCompactingContext struct {
	SessionID string
}

type SessionCompactingOutput struct {
	Context []string
	Prompt  string
}

type TextCompleteContext struct {
	SessionID string
	MessageID string
	PartID    string
}

type TextCompleteOutput struct {
	Text string
}

type ToolDefinitionContext struct {
	ToolID string
}

type ToolDefinitionOutput struct {
	Description string
	Parameters  any
}

type MessageHook func(ctx MessageContext, payload MessagePayload) MessagePayload
type SystemHook func(ctx SystemContext, system string) string
type ToolBeforeHook func(ctx ToolContext, input map[string]any) map[string]any
type ToolAfterHook func(ctx ToolContext, result sdk.ToolResult) sdk.ToolResult

type ChatMessageHook func(ctx MessageContext, payload *MessagePayload) (*MessagePayload, error)
type ChatParamsHook func(ctx ChatParamsContext, output *ChatParamsOutput) error
type ChatHeadersHook func(ctx ChatHeadersContext, output *ChatHeadersOutput) error
type PermissionAskHook func(ctx PermissionContext, output *PermissionOutput) error
type CommandExecuteBeforeHook func(ctx CommandExecuteContext, output *CommandExecuteOutput) error
type ShellEnvHook func(ctx ShellEnvContext, output *ShellEnvOutput) error
type ToolExecuteBeforeHook func(ctx ToolExecuteContext, output *ToolExecuteOutput) error
type ToolExecuteAfterHook func(ctx ToolExecuteContext, output *ToolExecuteOutput) error
type ChatMessagesTransformHook func(ctx ChatMessagesTransformContext, output *ChatMessagesTransformOutput) error
type ChatSystemTransformHook func(ctx ChatSystemTransformContext, output *ChatSystemTransformOutput) error
type SessionCompactingHook func(ctx SessionCompactingContext, output *SessionCompactingOutput) error
type TextCompleteHook func(ctx TextCompleteContext, output *TextCompleteOutput) error
type ToolDefinitionHook func(ctx ToolDefinitionContext, output *ToolDefinitionOutput) error

type Registry struct {
	mu                         sync.RWMutex
	messageHooks               []MessageHook
	systemHooks                []SystemHook
	toolBefore                 []ToolBeforeHook
	toolAfter                  []ToolAfterHook
	chatMessageHooks           []ChatMessageHook
	chatParamsHooks            []ChatParamsHook
	chatHeadersHooks           []ChatHeadersHook
	permissionAskHooks         []PermissionAskHook
	commandExecuteBeforeHooks  []CommandExecuteBeforeHook
	shellEnvHooks              []ShellEnvHook
	toolExecuteBeforeHooks     []ToolExecuteBeforeHook
	toolExecuteAfterHooks      []ToolExecuteAfterHook
	chatMessagesTransformHooks []ChatMessagesTransformHook
	chatSystemTransformHooks   []ChatSystemTransformHook
	sessionCompactingHooks     []SessionCompactingHook
	textCompleteHooks          []TextCompleteHook
	toolDefinitionHooks        []ToolDefinitionHook
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

func (r *Registry) RegisterChatMessage(hook ChatMessageHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatMessageHooks = append(r.chatMessageHooks, hook)
}

func (r *Registry) RegisterChatParams(hook ChatParamsHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatParamsHooks = append(r.chatParamsHooks, hook)
}

func (r *Registry) RegisterChatHeaders(hook ChatHeadersHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatHeadersHooks = append(r.chatHeadersHooks, hook)
}

func (r *Registry) RegisterPermissionAsk(hook PermissionAskHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissionAskHooks = append(r.permissionAskHooks, hook)
}

func (r *Registry) RegisterCommandExecuteBefore(hook CommandExecuteBeforeHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commandExecuteBeforeHooks = append(r.commandExecuteBeforeHooks, hook)
}

func (r *Registry) RegisterShellEnv(hook ShellEnvHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shellEnvHooks = append(r.shellEnvHooks, hook)
}

func (r *Registry) RegisterToolExecuteBefore(hook ToolExecuteBeforeHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolExecuteBeforeHooks = append(r.toolExecuteBeforeHooks, hook)
}

func (r *Registry) RegisterToolExecuteAfter(hook ToolExecuteAfterHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolExecuteAfterHooks = append(r.toolExecuteAfterHooks, hook)
}

func (r *Registry) RegisterChatMessagesTransform(hook ChatMessagesTransformHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatMessagesTransformHooks = append(r.chatMessagesTransformHooks, hook)
}

func (r *Registry) RegisterChatSystemTransform(hook ChatSystemTransformHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chatSystemTransformHooks = append(r.chatSystemTransformHooks, hook)
}

func (r *Registry) RegisterSessionCompacting(hook SessionCompactingHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionCompactingHooks = append(r.sessionCompactingHooks, hook)
}

func (r *Registry) RegisterTextComplete(hook TextCompleteHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.textCompleteHooks = append(r.textCompleteHooks, hook)
}

func (r *Registry) RegisterToolDefinition(hook ToolDefinitionHook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.toolDefinitionHooks = append(r.toolDefinitionHooks, hook)
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

func (r *Registry) ApplyChatMessage(ctx MessageContext, payload *MessagePayload) (*MessagePayload, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var err error
	for _, hook := range r.chatMessageHooks {
		payload, err = hook(ctx, payload)
		if err != nil {
			return payload, err
		}
	}
	return payload, nil
}

func (r *Registry) ApplyChatParams(ctx ChatParamsContext, output *ChatParamsOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.chatParamsHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyChatHeaders(ctx ChatHeadersContext, output *ChatHeadersOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.chatHeadersHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyPermissionAsk(ctx PermissionContext, output *PermissionOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.permissionAskHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyCommandExecuteBefore(ctx CommandExecuteContext, output *CommandExecuteOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.commandExecuteBeforeHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyShellEnv(ctx ShellEnvContext, output *ShellEnvOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.shellEnvHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyToolExecuteBefore(ctx ToolExecuteContext, output *ToolExecuteOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.toolExecuteBeforeHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyToolExecuteAfter(ctx ToolExecuteContext, output *ToolExecuteOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.toolExecuteAfterHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyChatMessagesTransform(ctx ChatMessagesTransformContext, output *ChatMessagesTransformOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.chatMessagesTransformHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyChatSystemTransform(ctx ChatSystemTransformContext, output *ChatSystemTransformOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.chatSystemTransformHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplySessionCompacting(ctx SessionCompactingContext, output *SessionCompactingOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.sessionCompactingHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyTextComplete(ctx TextCompleteContext, output *TextCompleteOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.textCompleteHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}

func (r *Registry) ApplyToolDefinition(ctx ToolDefinitionContext, output *ToolDefinitionOutput) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, hook := range r.toolDefinitionHooks {
		if err := hook(ctx, output); err != nil {
			return err
		}
	}
	return nil
}
