package sdk

import (
	"time"
)

type Result[T any] struct {
	Value   T
	Error   *ToolError
	Metrics ExecutionMetrics
}

type ToolError struct {
	code       ErrorCode
	message    string
	retryable  bool
	errorName  string
	cause      error
	details    map[string]any
	retryAfter time.Duration
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

type NamedError interface {
	error
	ErrorName() string
	ErrorCode() ErrorCode
	ErrorCause() error
	Unwrap() error
}

func (e *ToolError) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

func (e *ToolError) IsRetryable() bool {
	if e == nil {
		return false
	}
	return e.retryable
}

func (e *ToolError) ErrorName() string {
	return e.errorName
}

func (e *ToolError) ErrorCode() ErrorCode {
	return e.code
}

func (e *ToolError) ErrorCause() error {
	return e.cause
}

func (e *ToolError) Unwrap() error {
	return e.cause
}

func (e *ToolError) WithName(name string) *ToolError {
	e.errorName = name
	return e
}

func (e *ToolError) WithCause(err error) *ToolError {
	e.cause = err
	return e
}

func (e *ToolError) WithDetail(key string, value any) *ToolError {
	if e.details == nil {
		e.details = make(map[string]any)
	}
	e.details[key] = value
	return e
}

func (e *ToolError) WithRetryAfter(d time.Duration) *ToolError {
	e.retryAfter = d
	e.retryable = true
	return e
}

func (e *ToolError) Is(name string) bool {
	return e.errorName == name
}

func (e *ToolError) HasCode(code ErrorCode) bool {
	return e.code == code
}

func (e *ToolError) GetDetail(key string) (any, bool) {
	if e.details == nil {
		return nil, false
	}
	val, ok := e.details[key]
	return val, ok
}

func NewToolError(code ErrorCode, message string) *ToolError {
	return &ToolError{
		code:    code,
		message: message,
	}
}

func WrapError(err error, code ErrorCode, message string) *ToolError {
	if err == nil {
		return nil
	}
	return &ToolError{
		code:    code,
		message: message,
		cause:   err,
	}
}

func (e *ToolError) IsNamedError() bool {
	return e.errorName != ""
}

func (e *ToolError) Cause() error {
	return e.cause
}

func (e *ToolError) CauseChain() []error {
	var chain []error
	current := e.cause
	for current != nil {
		chain = append(chain, current)
		if unwrap, ok := current.(interface{ Unwrap() error }); ok {
			current = unwrap.Unwrap()
		} else {
			current = nil
		}
	}
	return chain
}

func (e *ToolError) RootCause() error {
	chain := e.CauseChain()
	if len(chain) == 0 {
		return nil
	}
	return chain[len(chain)-1]
}

func (e *ToolError) FullMessage() string {
	if e.cause == nil {
		return e.message
	}
	return e.message + ": " + e.cause.Error()
}

func (e *ToolError) ToObject() map[string]any {
	obj := map[string]any{
		"type":      "ToolError",
		"name":      e.errorName,
		"code":      e.code,
		"message":   e.message,
		"retryable": e.retryable,
	}

	if e.cause != nil {
		if namedErr, ok := e.cause.(NamedError); ok {
			obj["cause"] = map[string]any{
				"name":    namedErr.ErrorName(),
				"code":    namedErr.ErrorCode(),
				"message": namedErr.Error(),
			}
		} else {
			obj["cause"] = map[string]any{
				"message": e.cause.Error(),
			}
		}
	}

	if e.details != nil {
		obj["details"] = e.details
	}

	if e.retryAfter > 0 {
		obj["retryAfterMs"] = e.retryAfter.Milliseconds()
	}

	return obj
}

func (e *ToolError) GetAllDetails() map[string]any {
	return e.details
}

func (e *ToolError) HasDetail(key string) bool {
	if e.details == nil {
		return false
	}
	_, ok := e.details[key]
	return ok
}

func (e *ToolError) Merge(other *ToolError) *ToolError {
	if other == nil {
		return e
	}
	if e == nil {
		return other
	}

	merged := &ToolError{
		code:       other.code,
		message:    other.message,
		retryable:  other.retryable || e.retryable,
		errorName:  other.errorName,
		cause:      e,
		details:    e.details,
		retryAfter: other.retryAfter,
	}

	if other.details != nil {
		if merged.details == nil {
			merged.details = make(map[string]any)
		}
		for k, v := range other.details {
			merged.details[k] = v
		}
	}

	return merged
}

func (e *ToolError) WithCode(code ErrorCode) *ToolError {
	e.code = code
	return e
}

func (e *ToolError) WithMessage(msg string) *ToolError {
	e.message = msg
	return e
}

func (e *ToolError) WithRetryable(retryable bool) *ToolError {
	e.retryable = retryable
	return e
}

func (e *ToolError) RetryAfter() time.Duration {
	return e.retryAfter
}

func GetRetryAfter(err error) time.Duration {
	if err == nil {
		return 0
	}
	if te, ok := err.(*ToolError); ok {
		return te.RetryAfter()
	}
	return 0
}

func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	if te, ok := err.(*ToolError); ok {
		return te.IsRetryable()
	}
	return false
}

var _ NamedError = (*ToolError)(nil)

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
