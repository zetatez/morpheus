package exec

import (
	"context"
	"fmt"

	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/internal/policy"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type Orchestrator struct {
	registry sdk.ToolRegistry
	policy   *policy.Engine
	workdir  string
	plugins  *plugin.Registry
}

func NewOrchestrator(reg sdk.ToolRegistry, pol *policy.Engine, workdir string, plugins *plugin.Registry) *Orchestrator {
	return &Orchestrator{registry: reg, policy: pol, workdir: workdir, plugins: plugins}
}

func (o *Orchestrator) ExecuteStep(ctx context.Context, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error) {
	tool, ok := o.registry.Get(step.Tool)
	if !ok {
		return sdk.ToolResult{StepID: step.ID}, fmt.Errorf("tool %s not registered", step.Tool)
	}
	if err := o.checkPolicy(ctx, step); err != nil {
		return sdk.ToolResult{StepID: step.ID}, err
	}
	inputs := step.Inputs
	if step.Tool == "skill.invoke" {
		if inputs == nil {
			inputs = map[string]any{}
		}
		inputs["session_id"] = sessionID
	}
	if step.Tool == "mcp.query" {
		if inputs == nil {
			inputs = map[string]any{}
		}
		inputs["session_id"] = sessionID
	}
	if o.plugins != nil {
		inputs = o.plugins.ApplyToolBefore(plugin.ToolContext{SessionID: sessionID, Tool: step.Tool}, inputs)
	}
	res, err := tool.Invoke(ctx, inputs)
	if o.plugins != nil {
		res = o.plugins.ApplyToolAfter(plugin.ToolContext{SessionID: sessionID, Tool: step.Tool}, res)
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
		if decision.RequiresConfirm {
			fmt.Printf("warning: %s\n", decision.Reason)
		}
	case "fs.read", "fs.write":
		path, _ := step.Inputs["path"].(string)
		decision := o.policy.EvaluateCommand(ctx, step.Tool, path)
		if !decision.Allowed {
			return fmt.Errorf("policy rejected: %s", decision.Reason)
		}
		if decision.RequiresConfirm {
			fmt.Printf("warning: %s\n", decision.Reason)
		}
	}
	return nil
}
