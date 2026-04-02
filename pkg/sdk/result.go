package sdk

import "time"

type Result[T any] struct {
	Value   T
	Error   *ToolError
	Metrics ExecutionMetrics
}

type ToolError struct {
	Code      ErrorCode
	Message   string
	Retryable bool
}

type ErrorCode string

const (
	ErrorCodeUnknown        ErrorCode = "unknown"
	ErrorCodeNotFound       ErrorCode = "not_found"
	ErrorCodePermission     ErrorCode = "permission"
	ErrorCodeInvalidInput   ErrorCode = "invalid_input"
	ErrorCodeTimeout        ErrorCode = "timeout"
	ErrorCodeInternal       ErrorCode = "internal"
	ErrorCodePolicyRejected ErrorCode = "policy_rejected"
	ErrorCodeConfirmation   ErrorCode = "confirmation_required"
)

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *ToolError) IsRetryable() bool {
	if e == nil {
		return false
	}
	return e.Retryable
}

type ExecutionMetrics struct {
	StartTime  time.Time
	EndTime    time.Time
	DurationMS int64
	TokensUsed int
	ModelName  string
	ToolName   string
	StepID     string
}

func (m ExecutionMetrics) Duration() time.Duration {
	if m.EndTime.IsZero() || m.StartTime.IsZero() {
		return 0
	}
	return m.EndTime.Sub(m.StartTime)
}

func NewSuccessResult[T any](value T, metrics ExecutionMetrics) Result[T] {
	return Result[T]{Value: value, Metrics: metrics}
}

func NewErrorResult[T any](err *ToolError, metrics ExecutionMetrics) Result[T] {
	return Result[T]{Error: err, Metrics: metrics}
}

func (r *Result[T]) IsSuccess() bool {
	return r.Error == nil
}
