package planner

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zetatez/morpheus/internal/planner/llm"
	"github.com/zetatez/morpheus/pkg/sdk"
)

// PlanExecutor manages explicit planning before execution
type PlanExecutor struct {
	planner llm.Planner
}

// PlanValidator validates plan steps before execution
type PlanValidator struct {
	toolRegistry interface {
		Get(name string) (interface {
			Invoke(context.Context, map[string]interface{}) (sdk.ToolResult, error)
		}, bool)
	}
	maxSteps int
}

// ValidationResult contains the result of plan validation
type ValidationResult struct {
	Valid           bool
	StepResults     []StepValidation
	TotalRisks      []string
	RequiresConfirm []int // indices of steps requiring confirmation
}

// StepValidation is the validation result for a single step
type StepValidation struct {
	StepID       string
	Allowed      bool
	RiskLevel    sdk.RiskLevel
	Reason       string
	NeedsConfirm bool
}

// NewPlanExecutor creates a new PlanExecutor
func NewPlanExecutor(planner llm.Planner) *PlanExecutor {
	return &PlanExecutor{
		planner: planner,
	}
}

// NewPlanValidator creates a new PlanValidator
func NewPlanValidator() *PlanValidator {
	return &PlanValidator{
		maxSteps: 50,
	}
}

// CreatePlan creates an explicit plan from user input
func (pe *PlanExecutor) CreatePlan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	// Use the underlying planner to generate the plan
	plan, err := pe.planner.Plan(ctx, req)
	if err != nil {
		return sdk.Plan{}, fmt.Errorf("planning failed: %w", err)
	}

	// Enhance plan with metadata
	if plan.ID == "" {
		plan.ID = uuid.NewString()
	}

	return plan, nil
}

// Validate validates all steps in a plan
func (pv *PlanValidator) Validate(plan *sdk.Plan) (*ValidationResult, error) {
	result := &ValidationResult{
		Valid:           true,
		StepResults:     make([]StepValidation, len(plan.Steps)),
		RequiresConfirm: make([]int, 0),
	}

	if len(plan.Steps) > pv.maxSteps {
		result.Valid = false
		result.TotalRisks = append(result.TotalRisks, fmt.Sprintf("plan exceeds maximum steps (%d > %d)", len(plan.Steps), pv.maxSteps))
		return result, nil
	}

	for i, step := range plan.Steps {
		sv := pv.validateStep(&step)
		result.StepResults[i] = sv

		if !sv.Allowed {
			result.Valid = false
		}
		if sv.NeedsConfirm {
			result.RequiresConfirm = append(result.RequiresConfirm, i)
		}
		if sv.Reason != "" {
			result.TotalRisks = append(result.TotalRisks, fmt.Sprintf("step %d: %s", i+1, sv.Reason))
		}
	}

	return result, nil
}

// validateStep validates a single plan step
func (pv *PlanValidator) validateStep(step *sdk.PlanStep) StepValidation {
	sv := StepValidation{
		StepID:    step.ID,
		Allowed:   true,
		RiskLevel: sdk.RiskUnknown,
	}

	// Check if tool is specified
	if step.Tool == "" {
		sv.Allowed = false
		sv.Reason = "no tool specified for step"
		return sv
	}

	return sv
}

// isRiskyTool returns true if the tool is considered risky and needs validation
func isRiskyTool(toolName string) bool {
	riskyTools := map[string]bool{
		"bash":             true,
		"write":            true,
		"edit":             false, // less risky
		"fs.remove":        true,
		"task":             true,
		"agent.coordinate": true,
	}
	return riskyTools[toolName]
}

// PlanTracker tracks progress through a plan
type PlanTracker struct {
	Plan           *sdk.Plan
	CurrentStep    int
	CompletedSteps map[string]sdk.ToolResult
	StartedAt      time.Time
}

// NewPlanTracker creates a new plan tracker
func NewPlanTracker(plan *sdk.Plan) *PlanTracker {
	return &PlanTracker{
		Plan:           plan,
		CurrentStep:    0,
		CompletedSteps: make(map[string]sdk.ToolResult),
		StartedAt:      time.Now(),
	}
}

// NextStep returns the next step to execute, or nil if plan is complete
func (pt *PlanTracker) NextStep() *sdk.PlanStep {
	if pt.CurrentStep >= len(pt.Plan.Steps) {
		return nil
	}
	return &pt.Plan.Steps[pt.CurrentStep]
}

// MarkStepComplete marks a step as completed and advances to the next
func (pt *PlanTracker) MarkStepComplete(stepID string, result sdk.ToolResult) {
	pt.CompletedSteps[stepID] = result
	pt.CurrentStep++
}

// MarkStepFailed marks a step as failed
func (pt *PlanTracker) MarkStepFailed(stepID string, err error) {
	pt.CompletedSteps[stepID] = sdk.ToolResult{
		StepID:  stepID,
		Success: false,
		Error:   err.Error(),
	}
}

// IsComplete returns true if all steps have been executed
func (pt *PlanTracker) IsComplete() bool {
	return pt.CurrentStep >= len(pt.Plan.Steps)
}

// Progress returns the current progress as a fraction
func (pt *PlanTracker) Progress() (completed, total int) {
	return pt.CurrentStep, len(pt.Plan.Steps)
}

// CanParallelize returns true if the next step can run in parallel with current steps
func (pt *PlanTracker) CanParallelize(step *sdk.PlanStep) bool {
	if len(step.DependsOn) == 0 {
		return true
	}
	// Check if all dependencies are complete
	for _, dep := range step.DependsOn {
		if _, ok := pt.CompletedSteps[dep]; !ok {
			return false
		}
	}
	return true
}
