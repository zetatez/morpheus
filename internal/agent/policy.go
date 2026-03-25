package agent

import (
	"context"
	"strings"
)

type Policy struct {
	Name        string
	Description string
	Mode        AgentMode
	Tools       []string
	DeniedTools []string
	Prompt      string
	Constraints []Constraint
}

type AgentMode string

const (
	ModeBuild   AgentMode = "build"
	ModePlan    AgentMode = "plan"
	ModeExplore AgentMode = "explore"
	ModeCustom  AgentMode = "custom"
	ModeGeneral AgentMode = "general"
)

type Constraint struct {
	Type    ConstraintType
	Pattern string
	Action  ConstraintAction
}

type ConstraintType string

const (
	ConstraintTool       ConstraintType = "tool"
	ConstraintPath       ConstraintType = "path"
	ConstraintContent    ConstraintType = "content"
	ConstraintPermission ConstraintType = "permission"
)

type ConstraintAction string

const (
	ActionAllow ConstraintAction = "allow"
	ActionDeny  ConstraintAction = "deny"
	ActionAsk   ConstraintAction = "ask"
)

type DecisionInput struct {
	Task           string
	ContextSize    int
	AvailableTools []string
	SessionTools   []string
	UserTools      []string
	Mode           AgentMode
}

type Decision struct {
	SelectedAgent string
	SelectedTools []string
	DeniedTools   []string
	Mode          AgentMode
	Reason        string
	Score         float64
}

type DecisionMaker struct {
	policies map[string]*Policy
}

func NewDecisionMaker() *DecisionMaker {
	return &DecisionMaker{
		policies: make(map[string]*Policy),
	}
}

func (d *DecisionMaker) Register(policy *Policy) {
	d.policies[strings.ToLower(policy.Name)] = policy
}

func (d *DecisionMaker) Get(name string) (*Policy, bool) {
	policy, ok := d.policies[strings.ToLower(name)]
	return policy, ok
}

func (d *DecisionMaker) List() []*Policy {
	result := make([]*Policy, 0, len(d.policies))
	for _, p := range d.policies {
		result = append(result, p)
	}
	return result
}

func (d *DecisionMaker) Decide(ctx context.Context, input DecisionInput) Decision {
	var bestScore float64
	var bestPolicy *Policy

	for _, policy := range d.policies {
		score := d.calculateScore(policy, input)
		if score > bestScore {
			bestScore = score
			bestPolicy = policy
		}
	}

	if bestPolicy == nil {
		return Decision{
			SelectedAgent: "build",
			Mode:          ModeBuild,
			Reason:        "default",
			Score:         0,
		}
	}

	selectedTools := d.filterTools(bestPolicy, input)

	return Decision{
		SelectedAgent: bestPolicy.Name,
		SelectedTools: selectedTools,
		DeniedTools:   bestPolicy.DeniedTools,
		Mode:          bestPolicy.Mode,
		Reason:        "best_match",
		Score:         bestScore,
	}
}

func (d *DecisionMaker) calculateScore(policy *Policy, input DecisionInput) float64 {
	var score float64

	taskLower := strings.ToLower(input.Task)

	if strings.Contains(taskLower, "plan") || strings.Contains(taskLower, "think") || strings.Contains(taskLower, "分析") {
		if policy.Mode == ModePlan {
			score += 10
		}
	}

	if strings.Contains(taskLower, "explore") || strings.Contains(taskLower, "find") || strings.Contains(taskLower, "search") || strings.Contains(taskLower, "查找") {
		if policy.Mode == ModeExplore {
			score += 10
		}
	}

	if strings.Contains(taskLower, "build") || strings.Contains(taskLower, "implement") || strings.Contains(taskLower, "fix") || strings.Contains(taskLower, "write") || strings.Contains(taskLower, "实现") || strings.Contains(taskLower, "修复") {
		if policy.Mode == ModeBuild {
			score += 10
		}
	}

	if policy.Mode == input.Mode {
		score += 5
	}

	if len(input.AvailableTools) > 0 {
		matchingTools := 0
		for _, tool := range input.AvailableTools {
			for _, allowed := range policy.Tools {
				if tool == allowed {
					matchingTools++
					break
				}
			}
		}
		toolScore := float64(matchingTools) / float64(len(input.AvailableTools))
		score += toolScore * 3
	}

	if input.ContextSize > 50000 {
		if policy.Mode == ModePlan || policy.Mode == ModeExplore {
			score += 2
		}
	}

	return score
}

func (d *DecisionMaker) filterTools(policy *Policy, input DecisionInput) []string {
	if len(policy.Tools) == 0 && len(policy.DeniedTools) == 0 {
		return input.AvailableTools
	}

	var allowed []string
	for _, tool := range input.AvailableTools {
		if d.isToolDenied(tool, policy.DeniedTools) {
			continue
		}
		if len(policy.Tools) == 0 || d.isToolAllowed(tool, policy.Tools) {
			allowed = append(allowed, tool)
		}
	}

	return allowed
}

func (d *DecisionMaker) isToolAllowed(tool string, allowed []string) bool {
	for _, a := range allowed {
		if a == tool || a == "*" {
			return true
		}
	}
	return false
}

func (d *DecisionMaker) isToolDenied(tool string, denied []string) bool {
	for _, n := range denied {
		if n == tool {
			return true
		}
	}
	return false
}

func BuiltinPolicies() []*Policy {
	return []*Policy{
		{
			Name:        "build",
			Description: "Default agent for implementing code changes",
			Mode:        ModeBuild,
			Tools:       []string{"*"},
			DeniedTools: []string{},
			Constraints: []Constraint{},
		},
		{
			Name:        "plan",
			Description: "Plan mode - analyze and plan without making changes",
			Mode:        ModePlan,
			Tools:       []string{"read", "glob", "grep", "question", "lsp"},
			DeniedTools: []string{"write", "edit", "bash", "task"},
			Constraints: []Constraint{
				{Type: ConstraintTool, Pattern: "write", Action: ActionDeny},
				{Type: ConstraintTool, Pattern: "edit", Action: ActionDeny},
				{Type: ConstraintTool, Pattern: "bash", Action: ActionAsk},
			},
		},
		{
			Name:        "explore",
			Description: "Fast file search and codebase exploration",
			Mode:        ModeExplore,
			Tools:       []string{"read", "glob", "grep", "lsp"},
			DeniedTools: []string{"write", "edit", "bash", "task"},
			Constraints: []Constraint{
				{Type: ConstraintTool, Pattern: "write", Action: ActionDeny},
				{Type: ConstraintTool, Pattern: "edit", Action: ActionDeny},
				{Type: ConstraintTool, Pattern: "bash", Action: ActionDeny},
			},
		},
		{
			Name:        "general",
			Description: "General purpose for research and multi-step tasks",
			Mode:        ModeGeneral,
			Tools:       []string{"read", "glob", "grep", "bash", "question", "lsp"},
			DeniedTools: []string{},
			Constraints: []Constraint{
				{Type: ConstraintTool, Pattern: "bash", Action: ActionAsk},
			},
		},
	}
}

func (d *DecisionMaker) LoadBuiltin() {
	for _, policy := range BuiltinPolicies() {
		d.Register(policy)
	}
}
