package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
)

type RecoveryLevel int

const (
	LevelRetry RecoveryLevel = iota
	LevelCompression
	LevelCredentialFailover
	LevelProviderFallback
	LevelBudgetSummary
)

func (r RecoveryLevel) String() string {
	switch r {
	case LevelRetry:
		return "retry"
	case LevelCompression:
		return "compression"
	case LevelCredentialFailover:
		return "credential_failover"
	case LevelProviderFallback:
		return "provider_fallback"
	case LevelBudgetSummary:
		return "budget_summary"
	default:
		return "unknown"
	}
}

type RecoveryStrategy struct {
	Level         RecoveryLevel
	Attempt       int
	MaxAttempts   int
	BackoffMs     int
	ShouldRecover bool
	Summary       string
}

func DefaultRecoveryStrategy() *RecoveryStrategy {
	return &RecoveryStrategy{
		Level:       LevelRetry,
		Attempt:     0,
		MaxAttempts: 3,
		BackoffMs:   500,
	}
}

type ErrorClassifier struct {
	logger *zap.Logger
}

func NewErrorClassifier(logger *zap.Logger) *ErrorClassifier {
	return &ErrorClassifier{logger: logger}
}

type ErrorCategory string

const (
	CategoryRetryable   ErrorCategory = "retryable"
	CategoryCompression ErrorCategory = "compression"
	CategoryCredential  ErrorCategory = "credential"
	CategoryProvider    ErrorCategory = "provider"
	CategoryBudget      ErrorCategory = "budget"
	CategoryFatal       ErrorCategory = "fatal"
)

func (e *ErrorClassifier) Classify(err error) ErrorCategory {
	if err == nil {
		return CategoryRetryable
	}

	errStr := err.Error()

	if isContextDeadline(err) || isContextCanceled(err) {
		return CategoryFatal
	}

	if isQuotaError(errStr) {
		return CategoryCredential
	}

	if isAuthError(errStr) {
		return CategoryCredential
	}

	if isCompressionError(errStr) {
		return CategoryCompression
	}

	if isProviderError(errStr) {
		return CategoryProvider
	}

	if isBudgetError(errStr) {
		return CategoryBudget
	}

	if isRetryableErrorForRecovery(errStr) {
		return CategoryRetryable
	}

	return CategoryRetryable
}

func isContextDeadline(err error) bool {
	return errors.Is(err, context.DeadlineExceeded)
}

func isContextCanceled(err error) bool {
	return errors.Is(err, context.Canceled)
}

func isQuotaError(errStr string) bool {
	quotaPatterns := []string{
		"quota",
		"rate limit",
		"rate_limit",
		"too many requests",
		"429",
		"402",
	}
	for _, pattern := range quotaPatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}
	return false
}

func isAuthError(errStr string) bool {
	authPatterns := []string{
		"401",
		"403",
		"unauthorized",
		"forbidden",
		"invalid api key",
		"authentication",
		"token",
	}
	for _, pattern := range authPatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}
	return false
}

func isCompressionError(errStr string) bool {
	compressionPatterns := []string{
		"context length",
		"maximum context",
		"token limit",
		"too many tokens",
		"context_window",
		"max_tokens",
	}
	for _, pattern := range compressionPatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}
	return false
}

func isProviderError(errStr string) bool {
	providerPatterns := []string{
		"connection",
		"timeout",
		"unavailable",
		"503",
		"502",
		"504",
		"network",
		"reset",
	}
	for _, pattern := range providerPatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}
	return false
}

func isBudgetError(errStr string) bool {
	budgetPatterns := []string{
		"iteration budget",
		"max iterations",
		"budget exhausted",
		"step limit",
	}
	for _, pattern := range budgetPatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}
	return false
}

func isRetryableErrorForRecovery(errStr string) bool {
	retryablePatterns := []string{
		"temporary",
		"transient",
		"retry",
		"try again",
	}
	for _, pattern := range retryablePatterns {
		if containsIgnoreCase(errStr, pattern) {
			return true
		}
	}
	return false
}

type RecoveryEngine struct {
	strategy        *RecoveryStrategy
	errorClassifier *ErrorClassifier
	compactor       *Compactor
	compactionFn    func(context.Context) error
	logger          *zap.Logger
}

func NewRecoveryEngine(logger *zap.Logger) *RecoveryEngine {
	return &RecoveryEngine{
		strategy:        DefaultRecoveryStrategy(),
		errorClassifier: NewErrorClassifier(logger),
		logger:          logger,
	}
}

func (r *RecoveryEngine) SetCompactionFn(fn func(context.Context) error) {
	r.compactionFn = fn
}

func (r *RecoveryEngine) NextRecoveryLevel(ctx context.Context, err error) *RecoveryStrategy {
	category := r.errorClassifier.Classify(err)

	r.strategy.Attempt++

	switch category {
	case CategoryRetryable:
		if r.strategy.Attempt <= r.strategy.MaxAttempts {
			r.strategy.Level = LevelRetry
			r.strategy.BackoffMs *= 2
			if r.strategy.BackoffMs > 8000 {
				r.strategy.BackoffMs = 8000
			}
			r.strategy.ShouldRecover = true
			r.strategy.Summary = fmt.Sprintf("retrying after %dms (attempt %d/%d)", r.strategy.BackoffMs, r.strategy.Attempt, r.strategy.MaxAttempts)
			return r.strategy
		}

	case CategoryCompression:
		r.strategy.Level = LevelCompression
		r.strategy.ShouldRecover = true
		r.strategy.Summary = "triggering context compression"
		return r.strategy

	case CategoryCredential:
		r.strategy.Level = LevelCredentialFailover
		r.strategy.ShouldRecover = true
		r.strategy.Summary = "rotating credentials"
		return r.strategy

	case CategoryProvider:
		r.strategy.Level = LevelProviderFallback
		r.strategy.ShouldRecover = true
		r.strategy.Summary = "falling back to alternative provider"
		return r.strategy

	case CategoryBudget:
		r.strategy.Level = LevelBudgetSummary
		r.strategy.ShouldRecover = false
		r.strategy.Summary = "budget exhausted, generating summary"
		return r.strategy

	default:
		r.strategy.Level = LevelRetry
		if r.strategy.Attempt <= r.strategy.MaxAttempts {
			r.strategy.ShouldRecover = true
			return r.strategy
		}
		r.strategy.ShouldRecover = false
		r.strategy.Summary = "max retries exceeded"
		return r.strategy
	}

	r.strategy.ShouldRecover = false
	return r.strategy
}

func (r *RecoveryEngine) ExecuteRecovery(ctx context.Context, strategy *RecoveryStrategy) error {
	if !strategy.ShouldRecover {
		return nil
	}

	switch strategy.Level {
	case LevelRetry:
		time.Sleep(time.Duration(strategy.BackoffMs) * time.Millisecond)
		return nil

	case LevelCompression:
		if r.compactionFn != nil {
			return r.compactionFn(ctx)
		}
		return nil

	case LevelCredentialFailover:
		r.logger.Info("credential failover requested")
		return nil

	case LevelProviderFallback:
		r.logger.Info("provider fallback requested")
		return nil

	default:
		return nil
	}
}

func (r *RecoveryEngine) Reset() {
	r.strategy = DefaultRecoveryStrategy()
}

func (r *RecoveryEngine) CurrentLevel() RecoveryLevel {
	return r.strategy.Level
}
