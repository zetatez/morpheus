package keyword

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/zetatez/morpheus/pkg/sdk"
)

// Planner is a heuristic planner used for the first pass implementation.
type Planner struct{}

// NewPlanner returns a keyword planner.
func NewPlanner() *Planner { return &Planner{} }

func (p *Planner) ID() string { return "keyword" }

func (p *Planner) Capabilities() []string { return []string{"fs", "cmd"} }

func (p *Planner) Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	trimmed := strings.TrimSpace(req.Prompt)
	plan := sdk.Plan{
		ID:      uuid.NewString(),
		Summary: "Baseline plan",
		Status:  sdk.PlanStatusDraft,
	}
	if trimmed == "" {
		plan.Steps = []sdk.PlanStep{newEchoStep("Please provide an instruction.")}
		return plan, nil
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "run ") || strings.HasPrefix(lower, "cmd "):
		command := strings.TrimSpace(trimmed[strings.Index(trimmed, " ")+1:])
		plan.Steps = []sdk.PlanStep{newStep("cmd.exec", map[string]any{"command": command}, "fs.exec "+command)}
	case strings.HasPrefix(lower, "read "):
		path := strings.TrimSpace(trimmed[4:])
		plan.Steps = []sdk.PlanStep{newStep("fs.read", map[string]any{"path": path}, "fs.read "+path)}
	case strings.HasPrefix(lower, "write "):
		remainder := strings.TrimSpace(trimmed[5:])
		parts := strings.SplitN(remainder, "--", 2)
		path := strings.TrimSpace(parts[0])
		content := ""
		if len(parts) > 1 {
			content = strings.TrimSpace(parts[1])
		}
		plan.Steps = []sdk.PlanStep{newStep("fs.write", map[string]any{"path": path, "content": content}, "fs.write "+path)}
	case strings.HasPrefix(lower, "search "):
		query := strings.TrimSpace(trimmed[6:])
		plan.Steps = []sdk.PlanStep{newStep("fs.grep", map[string]any{"pattern": query}, "fs.grep "+query)}
	case strings.Contains(lower, "analy") || strings.Contains(lower, "analysis") || strings.Contains(lower, "structure") || strings.Contains(lower, "架构") || strings.Contains(lower, "结构") || strings.Contains(lower, "分析"):
		plan.Steps = []sdk.PlanStep{
			newStep("fs.tree", map[string]any{"path": ".", "depth": 3}, "fs.tree ."),
		}
	default:
		plan.Steps = []sdk.PlanStep{newEchoStep("Received: " + trimmed)}
	}
	return plan, nil
}

func newStep(tool string, inputs map[string]any, description string) sdk.PlanStep {
	return sdk.PlanStep{
		ID:          uuid.NewString(),
		Description: description,
		Tool:        tool,
		Inputs:      inputs,
		Status:      sdk.StepStatusPending,
	}
}

func newEchoStep(text string) sdk.PlanStep {
	return newStep("conversation.echo", map[string]any{"text": text}, "Respond to user")
}
