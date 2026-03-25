package exec

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/internal/policy"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type contextKey string

const AllowedToolsKey contextKey = "allowed_tools"
const confirmationKey contextKey = "confirmed_action"
const blockAskToolKey contextKey = "block_ask_tool"

func WithAllowedTools(ctx context.Context, tools []string) context.Context {
	return context.WithValue(ctx, AllowedToolsKey, tools)
}

func GetAllowedTools(ctx context.Context) []string {
	if tools, ok := ctx.Value(AllowedToolsKey).([]string); ok {
		return tools
	}
	return nil
}

func WithConfirmation(ctx context.Context) context.Context {
	return context.WithValue(ctx, confirmationKey, true)
}

func IsConfirmationApproved(ctx context.Context) bool {
	confirmed, _ := ctx.Value(confirmationKey).(bool)
	return confirmed
}

func BlockAskTool(ctx context.Context) context.Context {
	return context.WithValue(ctx, blockAskToolKey, true)
}

func IsAskToolBlocked(ctx context.Context) bool {
	blocked, _ := ctx.Value(blockAskToolKey).(bool)
	return blocked
}

type Orchestrator struct {
	registry sdk.ToolRegistry
	policy   *policy.Engine
	workdir  string
	plugins  *plugin.Registry
}

type agentModeKey struct{}

const AgentModePlan = "plan"

type ConfirmationRequiredError struct {
	Tool     string
	Inputs   map[string]any
	Decision sdk.PolicyDecision
}

func (e ConfirmationRequiredError) Error() string {
	if e.Decision.Reason != "" {
		return "confirmation required: " + e.Decision.Reason
	}
	return "confirmation required"
}

func IsConfirmationRequired(err error) (ConfirmationRequiredError, bool) {
	var target ConfirmationRequiredError
	if err == nil {
		return target, false
	}
	if errors.As(err, &target) {
		return target, true
	}
	return target, false
}

func WithAgentMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, agentModeKey{}, strings.ToLower(strings.TrimSpace(mode)))
}

func AgentModeFromContext(ctx context.Context) string {
	mode, _ := ctx.Value(agentModeKey{}).(string)
	return strings.ToLower(strings.TrimSpace(mode))
}

func IsToolAllowed(mode, tool string) bool {
	mode = strings.ToLower(strings.TrimSpace(mode))

	// Plan mode only allows read-only tools
	if mode == AgentModePlan {
		allowed := map[string]struct{}{
			"read":      {},
			"glob":      {},
			"grep":      {},
			"lsp":       {},
			"todowrite": {},
			"webfetch":  {},
			"question":  {},
		}
		if _, ok := allowed[tool]; ok {
			return true
		}
		return false
	}

	return true
}

func IsToolAllowedWithList(allowed []string, tool string) bool {
	if len(allowed) == 0 {
		return true
	}

	tool = strings.ToLower(tool)
	for _, pattern := range allowed {
		pattern = strings.ToLower(strings.TrimSpace(pattern))
		// Support wildcard matching
		if strings.HasSuffix(pattern, ".*") {
			prefix := strings.TrimSuffix(pattern, ".*")
			if strings.HasPrefix(tool, prefix) {
				return true
			}
		}
		if tool == pattern {
			return true
		}
	}
	return false
}

func NewOrchestrator(reg sdk.ToolRegistry, pol *policy.Engine, workdir string, plugins *plugin.Registry) *Orchestrator {
	return &Orchestrator{registry: reg, policy: pol, workdir: workdir, plugins: plugins}
}

func (o *Orchestrator) findTool(name string) (string, sdk.Tool, bool) {
	actualName, toolInterface, ok := sdk.RepairToolName(name, func(n string) (any, bool) {
		return o.registry.Get(n)
	})
	if !ok {
		return name, nil, false
	}
	tool, ok := toolInterface.(sdk.Tool)
	if !ok {
		return name, nil, false
	}
	return actualName, tool, true
}

func (o *Orchestrator) ExecuteStep(ctx context.Context, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error) {
	toolName := step.Tool
	actualName, tool, ok := o.findTool(toolName)
	if !ok {
		return sdk.ToolResult{StepID: step.ID}, fmt.Errorf("tool %s not registered", step.Tool)
	}
	if actualName != toolName {
		step = sdk.PlanStep{
			ID:          step.ID,
			Description: step.Description,
			Tool:        actualName,
			Inputs:      step.Inputs,
			Outputs:     step.Outputs,
			Status:      step.Status,
			DependsOn:   step.DependsOn,
		}
	}
	if err := o.checkPolicy(ctx, step); err != nil {
		return sdk.ToolResult{StepID: step.ID}, err
	}
	inputs := step.Inputs
	if toolName == "skill" {
		if inputs == nil {
			inputs = map[string]any{}
		}
		inputs["session_id"] = sessionID
	}
	if toolName == "todowrite" {
		if inputs == nil {
			inputs = map[string]any{}
		}
		inputs["session_id"] = sessionID
	}
	if toolName == "mcp.query" {
		if inputs == nil {
			inputs = map[string]any{}
		}
		inputs["session_id"] = sessionID
	}
	if o.plugins != nil {
		inputs = o.plugins.ApplyToolBefore(plugin.ToolContext{SessionID: sessionID, Tool: toolName}, inputs)
	}
	res, err := tool.Invoke(ctx, inputs)
	if o.plugins != nil {
		res = o.plugins.ApplyToolAfter(plugin.ToolContext{SessionID: sessionID, Tool: toolName}, res)
	}
	res.StepID = step.ID
	if err != nil || !res.Success {
		if err != nil {
			res.Success = false
			res.Error = err.Error()
		}
		return res, nil
	}
	return res, nil
}

func (o *Orchestrator) checkPolicy(ctx context.Context, step sdk.PlanStep) error {
	if !IsToolAllowed(AgentModeFromContext(ctx), step.Tool) {
		return fmt.Errorf("tool %s is not allowed in plan mode", step.Tool)
	}
	switch step.Tool {
	case "cmd.exec":
		command, _ := step.Inputs["command"].(string)
		workdir, _ := step.Inputs["workdir"].(string)
		if workdir == "" {
			workdir = o.workdir
		}
		decision := o.policy.EvaluateCommand(ctx, command, workdir)
		if !decision.Allowed {
			return fmt.Errorf("policy rejected command: %s", decision.Reason)
		}
		if decision.RequiresConfirm && !IsConfirmationApproved(ctx) {
			return ConfirmationRequiredError{Tool: step.Tool, Inputs: step.Inputs, Decision: decision}
		}
	case "read", "write", "edit":
		path, _ := step.Inputs["path"].(string)
		decision := o.policy.EvaluateCommand(ctx, step.Tool, path)
		if !decision.Allowed {
			return fmt.Errorf("policy rejected: %s", decision.Reason)
		}
		if decision.RequiresConfirm && !IsConfirmationApproved(ctx) {
			return ConfirmationRequiredError{Tool: step.Tool, Inputs: step.Inputs, Decision: decision}
		}
	}
	return nil
}
