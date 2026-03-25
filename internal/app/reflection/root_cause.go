package reflection

import (
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

// FailureType categorizes the type of failure
type FailureType int

const (
	// UnknownFailure is an unrecognized failure
	UnknownFailure FailureType = iota
	// PermissionFailure is access/permission denied
	PermissionFailure
	// ResourceFailure is system resource exhaustion
	ResourceFailure
	// DependencyFailure is missing prerequisite
	DependencyFailure
	// InputValidationFailure is invalid input format
	InputValidationFailure
	// ToolNotFoundFailure is tool doesn't exist
	ToolNotFoundFailure
	// TimeoutFailure is operation timed out
	TimeoutFailure
	// NetworkFailure is network connectivity issue
	NetworkFailure
	// SyntaxFailure is code/command syntax error
	SyntaxFailure
)

// FailurePattern describes a pattern of failure
type FailurePattern struct {
	Type          FailureType
	RootCause     string
	Context       []string
	Resolution    string
	OccurrenceCount int
}

// Diagnosis contains the result of root cause analysis
type Diagnosis struct {
	Type            FailureType
	Diagnosis       string
	RootCause       string
	RiskLevel       sdk.RiskLevel
	CanRetry        bool
	NeedsUserInput  bool
	Resolution      string
	OccurrenceCount int
}

// RootCauseAnalyzer analyzes failures to determine root causes
type RootCauseAnalyzer struct{}

// NewRootCauseAnalyzer creates a new root cause analyzer
func NewRootCauseAnalyzer() *RootCauseAnalyzer {
	return &RootCauseAnalyzer{}
}

// Analyze performs root cause analysis on a list of failures
func (a *RootCauseAnalyzer) Analyze(failures []sdk.ToolResult) *Diagnosis {
	if len(failures) == 0 {
		return &Diagnosis{
			Type:      UnknownFailure,
			Diagnosis: "No failures to analyze",
			RiskLevel: sdk.RiskUnknown,
		}
	}

	// Aggregate error types
	typeCounts := make(map[FailureType]int)

	for _, f := range failures {
		if !f.Success {
			failureType := a.classifyFailure(f)
			typeCounts[failureType]++
		}
	}

	// Find the most common failure type
	var dominantType FailureType
	maxCount := 0
	for t, c := range typeCounts {
		if c > maxCount {
			maxCount = c
			dominantType = t
		}
	}

	diagnosis := &Diagnosis{
		Type:            dominantType,
		OccurrenceCount: maxCount,
	}

	// Generate diagnosis based on failure type
	switch dominantType {
	case PermissionFailure:
		diagnosis.Diagnosis = "Permission denied or access control issue"
		diagnosis.RootCause = "The operation requires permissions that are not available"
		diagnosis.RiskLevel = sdk.RiskHigh
		diagnosis.CanRetry = false
		diagnosis.NeedsUserInput = true
		diagnosis.Resolution = "Grant appropriate permissions or use an alternative approach"

	case ResourceFailure:
		diagnosis.Diagnosis = "System resource exhaustion"
		diagnosis.RootCause = "The system has insufficient resources (memory, disk, etc.)"
		diagnosis.RiskLevel = sdk.RiskMedium
		diagnosis.CanRetry = true
		diagnosis.NeedsUserInput = false
		diagnosis.Resolution = "Free up resources or wait and retry"

	case DependencyFailure:
		diagnosis.Diagnosis = "Missing or unavailable dependency"
		diagnosis.RootCause = "A required tool, file, or resource is not available"
		diagnosis.RiskLevel = sdk.RiskMedium
		diagnosis.CanRetry = false
		diagnosis.NeedsUserInput = false
		diagnosis.Resolution = "Install missing dependencies or use alternative tools"

	case InputValidationFailure:
		diagnosis.Diagnosis = "Invalid input or parameters"
		diagnosis.RootCause = "The provided input does not match expected format"
		diagnosis.RiskLevel = sdk.RiskLow
		diagnosis.CanRetry = true
		diagnosis.NeedsUserInput = true
		diagnosis.Resolution = "Fix the input format and retry"

	case ToolNotFoundFailure:
		diagnosis.Diagnosis = "Requested tool not found"
		diagnosis.RootCause = "The tool being called does not exist or is not available"
		diagnosis.RiskLevel = sdk.RiskMedium
		diagnosis.CanRetry = false
		diagnosis.NeedsUserInput = false
		diagnosis.Resolution = "Use an alternative tool or check MCP server status"

	case TimeoutFailure:
		diagnosis.Diagnosis = "Operation timed out"
		diagnosis.RootCause = "The operation took longer than the allowed time"
		diagnosis.RiskLevel = sdk.RiskLow
		diagnosis.CanRetry = true
		diagnosis.NeedsUserInput = false
		diagnosis.Resolution = "Increase timeout or break operation into smaller steps"

	case NetworkFailure:
		diagnosis.Diagnosis = "Network connectivity issue"
		diagnosis.RootCause = "Unable to reach remote service or resource"
		diagnosis.RiskLevel = sdk.RiskMedium
		diagnosis.CanRetry = true
		diagnosis.NeedsUserInput = false
		diagnosis.Resolution = "Check network connection and retry"

	case SyntaxFailure:
		diagnosis.Diagnosis = "Syntax error in code or command"
		diagnosis.RootCause = "The command or code contains syntax errors"
		diagnosis.RiskLevel = sdk.RiskLow
		diagnosis.CanRetry = false
		diagnosis.NeedsUserInput = true
		diagnosis.Resolution = "Fix the syntax error"

	default:
		diagnosis.Diagnosis = "Unknown failure"
		diagnosis.RootCause = "Unable to determine root cause"
		diagnosis.RiskLevel = sdk.RiskUnknown
		diagnosis.CanRetry = false
		diagnosis.NeedsUserInput = true
		diagnosis.Resolution = "Review the error details and try a different approach"
	}

	return diagnosis
}

// classifyFailure determines the failure type from a tool result
func (a *RootCauseAnalyzer) classifyFailure(failure sdk.ToolResult) FailureType {
	err := strings.ToLower(failure.Error)

	// Permission failures
	permPatterns := []string{
		"permission denied",
		"access denied",
		"forbidden",
		"unauthorized",
		"eacces",
	}
	for _, p := range permPatterns {
		if strings.Contains(err, p) {
			return PermissionFailure
		}
	}

	// Resource failures
	resourcePatterns := []string{
		"no space left",
		"out of memory",
		"oom",
		"disk full",
		"resource exhausted",
		"enospc",
	}
	for _, p := range resourcePatterns {
		if strings.Contains(err, p) {
			return ResourceFailure
		}
	}

	// Timeout failures
	timeoutPatterns := []string{
		"timeout",
		"timed out",
		"deadline exceeded",
		"context deadline",
	}
	for _, p := range timeoutPatterns {
		if strings.Contains(err, p) {
			return TimeoutFailure
		}
	}

	// Network failures
	networkPatterns := []string{
		"network",
		"connection refused",
		"connection reset",
		"no route to host",
		"dns",
		"refused",
	}
	for _, p := range networkPatterns {
		if strings.Contains(err, p) {
			return NetworkFailure
		}
	}

	// Tool not found failures
	notFoundPatterns := []string{
		"not found",
		"does not exist",
		"command not found",
		"tool not found",
		"no such file",
		"enoent",
	}
	for _, p := range notFoundPatterns {
		if strings.Contains(err, p) {
			return ToolNotFoundFailure
		}
	}

	// Dependency failures (tool exists but something it depends on doesn't)
	depPatterns := []string{
		"depends on",
		"dependency",
		"required",
		"cannot find module",
		"import error",
		"no such module",
	}
	for _, p := range depPatterns {
		if strings.Contains(err, p) {
			return DependencyFailure
		}
	}

	// Syntax failures
	syntaxPatterns := []string{
		"syntax error",
		"parse error",
		"invalid syntax",
		"unexpected token",
		"unexpected character",
	}
	for _, p := range syntaxPatterns {
		if strings.Contains(err, p) {
			return SyntaxFailure
		}
	}

	// Input validation failures
	validationPatterns := []string{
		"invalid",
		"malformed",
		"wrong format",
		"invalid argument",
		"invalid input",
	}
	for _, p := range validationPatterns {
		if strings.Contains(err, p) {
			return InputValidationFailure
		}
	}

	return UnknownFailure
}

// IsRetryable returns true if a failure type is typically retryable
func IsRetryable(t FailureType) bool {
	switch t {
	case TimeoutFailure, NetworkFailure, ResourceFailure:
		return true
	default:
		return false
	}
}

// NeedsUserIntervention returns true if the failure requires user input
func NeedsUserIntervention(t FailureType) bool {
	switch t {
	case PermissionFailure, InputValidationFailure, UnknownFailure:
		return true
	default:
		return false
	}
}
