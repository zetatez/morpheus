package app

import (
	"context"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type ToolHandlerAdapter struct {
	ExecuteFunc        func(ctx context.Context, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error)
	TruncateResultFunc func(ctx context.Context, sessionID, toolName string, data map[string]interface{}) map[string]interface{}
}

func (a *ToolHandlerAdapter) Execute(ctx context.Context, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error) {
	if a.ExecuteFunc != nil {
		return a.ExecuteFunc(ctx, sessionID, step)
	}
	return sdk.ToolResult{Success: false, Error: "Execute not implemented"}, nil
}

func (a *ToolHandlerAdapter) TruncateResult(ctx context.Context, sessionID, toolName string, data map[string]interface{}) map[string]interface{} {
	if a.TruncateResultFunc != nil {
		return a.TruncateResultFunc(ctx, sessionID, toolName, data)
	}
	return data
}

type CallbacksAdapter struct {
	Streaming         bool
	EmitFunc          func(event string, data interface{}) error
	RunEventFunc      func(eventType string, data map[string]interface{})
	ToolResultFunc    func(toolName string, callID string, result sdk.ToolResult)
	MessageAppendFunc func(ctx context.Context, sessionID, role, content string, parts []sdk.MessagePart) error
	CallChatFunc      func(ctx context.Context, messages []map[string]interface{}, tools []map[string]interface{}, toolChoice interface{}, emit func(string, interface{}) error) (ChatResponse, error)
}

type ChatResponse struct {
	Content      string
	ToolCalls    []ToolCallInfo
	FinishReason string
	Usage        TokenUsage
}

type ToolCallInfo struct {
	ID        string
	Name      string
	Arguments map[string]interface{}
}

func (a *CallbacksAdapter) ToLoopCallbacks() LoopCallbacks {
	return LoopCallbacks{
		OnEmit: a.EmitFunc,
		OnRunEvent: func(eventType string, data map[string]interface{}) {
			if a.RunEventFunc != nil {
				a.RunEventFunc(eventType, data)
			}
		},
		OnToolResult: func(toolName string, callID string, result sdk.ToolResult) {
			if a.ToolResultFunc != nil {
				a.ToolResultFunc(toolName, callID, result)
			}
		},
		OnMessageAppend: func(ctx context.Context, sessionID, role, content string, parts []sdk.MessagePart) error {
			if a.MessageAppendFunc != nil {
				return a.MessageAppendFunc(ctx, sessionID, role, content, parts)
			}
			return nil
		},
		OnCallChat: a.CallChatFunc,
	}
}
