package reflection

import (
	"context"
	"fmt"

	"github.com/zetatez/morpheus/pkg/sdk"
)

// RecoveryPlanner creates recovery plans when tasks fail
type RecoveryPlanner struct {
	rootCauseAnalyzer *RootCauseAnalyzer
	strategyLibrary   []RecoveryStrategy
}

// RecoveryStrategy describes a strategy for recovering from a failure
type RecoveryStrategy struct {
	Pattern     FailurePattern
	Actions     []RecoveryAction
	SuccessRate float64
	Description string
}

// RecoveryAction is a single action to take for recovery
type RecoveryAction struct {
	Tool       string
	Parameters map[string]any
	Reason     string
}

// RecoveryPlan is a structured plan for recovering from failures
type RecoveryPlan struct {
	Diagnosis      string
	Strategies     []RecoveryStrategy
	SelectedAction *RecoveryAction
	EstimatedRisk  sdk.RiskLevel
	Suggestions    []string
}

// NewRecoveryPlanner creates a new recovery planner
func NewRecoveryPlanner() *RecoveryPlanner {
	return &RecoveryPlanner{
		rootCauseAnalyzer: NewRootCauseAnalyzer(),
		strategyLibrary:   buildStrategyLibrary(),
	}
}

// CreateRecoveryPlan creates a recovery plan based on failures
func (rp *RecoveryPlanner) CreateRecoveryPlan(
	ctx context.Context,
	failures []sdk.ToolResult,
	currentGoal string,
) (*RecoveryPlan, error) {
	if len(failures) == 0 {
		return nil, fmt.Errorf("no failures provided")
	}

	plan := &RecoveryPlan{}

	// Step 1: Root cause analysis
	diagnosis := rp.rootCauseAnalyzer.Analyze(failures)
	plan.Diagnosis = diagnosis.Diagnosis
	plan.EstimatedRisk = diagnosis.RiskLevel

	// Step 2: Match against strategy library
	candidates := rp.matchStrategies(diagnosis)
	plan.Strategies = candidates

	// Step 3: Select the best action based on success rate
	if len(candidates) > 0 {
		best := rp.selectBestStrategy(candidates)
		if len(best.Actions) > 0 {
			plan.SelectedAction = &best.Actions[0]
		}
	}

	// Step 4: Generate suggestions
	plan.Suggestions = rp.generateSuggestions(diagnosis, currentGoal)

	return plan, nil
}

// matchStrategies finds strategies matching the failure pattern
func (rp *RecoveryPlanner) matchStrategies(diagnosis *Diagnosis) []RecoveryStrategy {
	var matched []RecoveryStrategy

	for _, strategy := range rp.strategyLibrary {
		if strategy.Pattern.Type == diagnosis.Type {
			matched = append(matched, strategy)
		}
	}

	// Sort by success rate
	if len(matched) > 1 {
		// Could sort here if needed
	}

	return matched
}

// selectBestStrategy selects the strategy with highest success rate
func (rp *RecoveryPlanner) selectBestStrategy(strategies []RecoveryStrategy) RecoveryStrategy {
	var best RecoveryStrategy
	best.SuccessRate = -1

	for _, s := range strategies {
		if s.SuccessRate > best.SuccessRate {
			best = s
		}
	}

	return best
}

// generateSuggestions generates helpful suggestions based on diagnosis
func (rp *RecoveryPlanner) generateSuggestions(diagnosis *Diagnosis, goal string) []string {
	var suggestions []string

	switch diagnosis.Type {
	case PermissionFailure:
		suggestions = append(suggestions,
			"Check if the required permissions are available",
			"Try running with appropriate permissions",
			"Consider using a different tool that doesn't require elevated access")
	case ResourceFailure:
		suggestions = append(suggestions,
			"Check system resource availability (disk, memory)",
			"Try reducing the scope of the operation",
			"Wait and retry if resources are temporarily unavailable")
	case DependencyFailure:
		suggestions = append(suggestions,
			"Ensure dependencies are installed/available",
			"Run prerequisite steps first",
			"Check the order of operations in your plan")
	case InputValidationFailure:
		suggestions = append(suggestions,
			"Review the input format and parameters",
			"Check for typos in tool names or arguments",
			"Consult the tool schema for correct usage")
	case ToolNotFoundFailure:
		suggestions = append(suggestions,
			"Verify the tool is available in your environment",
			"Check if the MCP server is connected",
			"Consider using an alternative tool")
	case TimeoutFailure:
		suggestions = append(suggestions,
			"Increase timeout if the operation is legitimately slow",
			"Break down the task into smaller steps",
			"Check network connectivity if remote resources are involved")
	default:
		suggestions = append(suggestions,
			"Review the error message and context",
			"Try a different approach",
			"Break down the task into simpler steps")
	}

	// Add goal-specific suggestions
	if goal != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("Current goal: %s", truncateString(goal, 100)))
	}

	return suggestions
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// buildStrategyLibrary creates the default strategy library
func buildStrategyLibrary() []RecoveryStrategy {
	return []RecoveryStrategy{
		{
			Pattern: FailurePattern{Type: PermissionFailure},
			Actions: []RecoveryAction{
				{Tool: "question", Parameters: map[string]any{
					"question": "Permission denied. How would you like to proceed?",
					"options":  []string{"retry", "skip", "cancel"},
				}},
			},
			SuccessRate: 0.7,
			Description: "Request user guidance for permission issues",
		},
		{
			Pattern: FailurePattern{Type: TimeoutFailure},
			Actions: []RecoveryAction{
				{Tool: "cmd.exec", Parameters: map[string]any{
					"command": "echo 'Retrying with extended timeout...'",
				}},
			},
			SuccessRate: 0.5,
			Description: "Retry the operation",
		},
		{
			Pattern: FailurePattern{Type: DependencyFailure},
			Actions: []RecoveryAction{
				{Tool: "glob", Parameters: map[string]any{
					"pattern": "**/requirements*.txt",
				}},
				{Tool: "glob", Parameters: map[string]any{
					"pattern": "**/package.json",
				}},
			},
			SuccessRate: 0.6,
			Description: "Check for dependency files",
		},
		{
			Pattern: FailurePattern{Type: InputValidationFailure},
			Actions: []RecoveryAction{
				{Tool: "question", Parameters: map[string]any{
					"question": "Invalid input detected. How would you like to proceed?",
					"options":  []string{"fix", "skip", "cancel"},
				}},
			},
			SuccessRate: 0.8,
			Description: "Request user input for corrections",
		},
		{
			Pattern: FailurePattern{Type: ToolNotFoundFailure},
			Actions: []RecoveryAction{
				{Tool: "mcp", Parameters: map[string]any{
					"action": "tools",
				}},
			},
			SuccessRate: 0.9,
			Description: "List available tools",
		},
	}
}
