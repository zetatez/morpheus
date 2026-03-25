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
		plan.Steps = []sdk.PlanStep{newStep("question", map[string]any{"question": "Please provide an instruction.", "options": []string{"Continue"}}, "Ask user")}
		return plan, nil
	}
	lower := strings.ToLower(trimmed)
	switch {
	case strings.HasPrefix(lower, "run ") || strings.HasPrefix(lower, "cmd "):
		command := strings.TrimSpace(trimmed[strings.Index(trimmed, " ")+1:])
		plan.Steps = []sdk.PlanStep{newStep("bash", map[string]any{"command": command}, "bash "+command)}
	case strings.HasPrefix(lower, "read "):
		path := strings.TrimSpace(trimmed[4:])
		plan.Steps = []sdk.PlanStep{newStep("read", map[string]any{"path": path}, "read "+path)}
	case strings.HasPrefix(lower, "write "):
		remainder := strings.TrimSpace(trimmed[5:])
		parts := strings.SplitN(remainder, "--", 2)
		path := strings.TrimSpace(parts[0])
		content := ""
		if len(parts) > 1 {
			content = strings.TrimSpace(parts[1])
		}
		plan.Steps = []sdk.PlanStep{newStep("write", map[string]any{"path": path, "content": content}, "write "+path)}
	case strings.HasPrefix(lower, "search "):
		query := strings.TrimSpace(trimmed[6:])
		plan.Steps = []sdk.PlanStep{newStep("grep", map[string]any{"pattern": query}, "grep "+query)}
	case strings.Contains(lower, "analy") || strings.Contains(lower, "analysis") || strings.Contains(lower, "structure") || strings.Contains(lower, "架构") || strings.Contains(lower, "结构") || strings.Contains(lower, "分析"):
		plan.Steps = []sdk.PlanStep{
			newStep("glob", map[string]any{"pattern": "**/*"}, "glob **/*"),
		}
	default:
		plan.Steps = []sdk.PlanStep{newStep("question", map[string]any{"question": "How can I help you?", "options": []string{"Continue"}}, "Ask user")}
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
