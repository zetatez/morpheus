package app

import (
	"context"
	"net/http"
	"time"
)

const (
	RetryInitialDelayMs     = 2000
	RetryBackoffFactor      = 2
	RetryMaxDelayMs         = 30000
	RetryMaxDelayNoHeaderMs = 30000
)

type RetryableError struct {
	Message         string
	IsRetryable     bool
	RetryAfterMs    int64
	ResponseBody    string
	ResponseHeaders http.Header
}

type RetryPolicy struct {
	maxDelayMs int64
}

func NewRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		maxDelayMs: RetryMaxDelayMs,
	}
}

func (rp *RetryPolicy) WithMaxDelay(maxDelayMs int64) *RetryPolicy {
	return &RetryPolicy{
		maxDelayMs: maxDelayMs,
	}
}

func (rp *RetryPolicy) CalculateDelay(attempt int, err *RetryableError) time.Duration {
	if err != nil && err.RetryAfterMs > 0 {
		delay := err.RetryAfterMs
		if delay > rp.maxDelayMs {
			delay = rp.maxDelayMs
		}
		return time.Duration(delay) * time.Millisecond
	}

	baseDelay := int64(RetryInitialDelayMs)
	for i := 1; i < attempt; i++ {
		baseDelay *= RetryBackoffFactor
		if baseDelay > RetryMaxDelayNoHeaderMs {
			baseDelay = RetryMaxDelayNoHeaderMs
			break
		}
	}

	if err != nil && err.ResponseHeaders != nil {
		if retryAfter := err.ResponseHeaders.Get("Retry-After"); retryAfter != "" {
			delay := rp.parseRetryAfter(retryAfter)
			if delay > 0 && delay < rp.maxDelayMs {
				return time.Duration(delay) * time.Millisecond
			}
		}
	}

	if baseDelay > rp.maxDelayMs {
		baseDelay = rp.maxDelayMs
	}

	return time.Duration(baseDelay) * time.Millisecond
}

func (rp *RetryPolicy) parseRetryAfter(value string) int64 {
	parsedMs, ok := parseRetryAfterMs(value)
	if ok && parsedMs > 0 {
		return parsedMs
	}

	parsedSeconds, ok := parseRetryAfterSeconds(value)
	if ok && parsedSeconds > 0 {
		return parsedSeconds * 1000
	}

	return 0
}

func parseRetryAfterMs(value string) (int64, bool) {
	var ms float64
	_, err := parseFloatCommon(value, &ms)
	if err != nil {
		return 0, false
	}
	return int64(ms), true
}

func parseRetryAfterSeconds(value string) (int64, bool) {
	var seconds float64
	_, err := parseFloatCommon(value, &seconds)
	if err != nil {
		return 0, false
	}
	return int64(seconds), true
}

func parseFloatCommon(value string, result *float64) (bool, error) {
	var n int
	for _, c := range value {
		if c == '.' || c == 'e' || c == 'E' {
			return false, nil
		}
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		} else {
			return false, nil
		}
	}
	*result = float64(n)
	return true, nil
}

func IsRetryableError(err error) *RetryableError {
	if err == nil {
		return nil
	}

	return &RetryableError{
		Message:     err.Error(),
		IsRetryable: true,
	}
}

func ShouldRetry(err *RetryableError, attempt int, maxAttempts int) bool {
	if attempt >= maxAttempts {
		return false
	}
	if err == nil {
		return false
	}
	return err.IsRetryable
}

func ExecuteWithRetry(ctx context.Context, fn func() error, policy *RetryPolicy, maxAttempts int) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		retryable := IsRetryableError(lastErr)
		if !ShouldRetry(retryable, attempt, maxAttempts) {
			return lastErr
		}

		delay := policy.CalculateDelay(attempt+1, retryable)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return lastErr
}
