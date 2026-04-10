package session

import (
	"context"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	DefaultCompactionThresholdTokens = 150000
	DefaultCompactionRatio           = 0.7
	DefaultCompactionWindowMessages  = 50
)

type CompactionConfig struct {
	ThresholdTokens int
	Ratio           float64
	WindowMessages  int
	Enabled         bool
}

type CompactionService struct {
	config    CompactionConfig
	backend   SessionBackend
	onPrune   func(sessionID string, prunedCount int) error
	onCompact func(sessionID string, summary string) error
}

type CompactionResult struct {
	Pruned       bool
	PrunedCount  int
	Compacted    bool
	Summary      string
	MessageCount int
}

func NewCompactionService(config CompactionConfig, backend SessionBackend) *CompactionService {
	if config.ThresholdTokens <= 0 {
		config.ThresholdTokens = DefaultCompactionThresholdTokens
	}
	if config.Ratio <= 0 {
		config.Ratio = DefaultCompactionRatio
	}
	if config.WindowMessages <= 0 {
		config.WindowMessages = DefaultCompactionWindowMessages
	}
	return &CompactionService{
		config:  config,
		backend: backend,
	}
}

func (s *CompactionService) SetOnPrune(fn func(sessionID string, prunedCount int) error) {
	s.onPrune = fn
}

func (s *CompactionService) SetOnCompact(fn func(sessionID string, summary string) error) {
	s.onCompact = fn
}

func (s *CompactionService) IsOverflow(ctx context.Context, sessionID string, tokens int) (bool, error) {
	if !s.config.Enabled {
		return false, nil
	}
	if s.backend == nil {
		return false, nil
	}
	return tokens >= s.config.ThresholdTokens, nil
}

func (s *CompactionService) EstimateTokens(messages []sdk.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateMessageTokens(msg)
	}
	return total
}

func estimateMessageTokens(msg sdk.Message) int {
	charCount := len(msg.Role) + len(msg.Content)
	for _, part := range msg.Parts {
		charCount += len(part.Type) + len(part.Text) + len(part.Tool)
	}
	return (charCount / 4) + 20
}

func (s *CompactionService) Prune(ctx context.Context, sessionID string) (int, error) {
	if s.backend == nil {
		return 0, nil
	}

	stored, err := s.backend.LoadSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}

	messages := stored.Messages
	if len(messages) <= s.config.WindowMessages {
		return 0, nil
	}

	pruneCount := len(messages) - s.config.WindowMessages
	messages = messages[pruneCount:]

	if s.onPrune != nil {
		if err := s.onPrune(sessionID, pruneCount); err != nil {
			return 0, err
		}
	}

	err = s.backend.SaveSession(ctx, sessionID, messages, stored.Summary, stored.Metadata)
	if err != nil {
		return pruneCount, err
	}

	return pruneCount, nil
}

func (s *CompactionService) Compact(ctx context.Context, sessionID string, messages []sdk.Message) (*CompactionResult, error) {
	result := &CompactionResult{
		MessageCount: len(messages),
	}

	if len(messages) <= 5 {
		return result, nil
	}

	var textContent []string
	for _, msg := range messages {
		content := extractMessageContent(msg)
		if content != "" {
			textContent = append(textContent, content)
		}
	}

	summary := generateSummary(textContent)
	result.Summary = summary
	result.Compacted = true

	if s.onCompact != nil {
		if err := s.onCompact(sessionID, summary); err != nil {
			return result, err
		}
	}

	return result, nil
}

func extractMessageContent(msg sdk.Message) string {
	if len(msg.Parts) == 0 {
		return msg.Content
	}
	var b string
	for _, part := range msg.Parts {
		if part.Type == "text" && part.Text != "" {
			b += part.Text + "\n"
		}
	}
	if b == "" && msg.Content != "" {
		return msg.Content
	}
	return b
}

func generateSummary(contents []string) string {
	if len(contents) == 0 {
		return ""
	}
	var totalLen int
	for _, c := range contents {
		totalLen += len(c)
	}

	if totalLen < 500 {
		return ""
	}

	const maxSummaryLen = 2000
	var summary string
	for i, c := range contents {
		if len(summary)+len(c) > maxSummaryLen {
			break
		}
		if i > 0 {
			summary += "\n---\n"
		}
		summary += c
	}

	if len(summary) > maxSummaryLen {
		summary = summary[:maxSummaryLen] + "..."
	}

	return summary
}

func (s *CompactionService) Process(ctx context.Context, sessionID string, messages []sdk.Message, auto bool) (string, error) {
	if !s.config.Enabled && !auto {
		return "continue", nil
	}

	tokens := s.EstimateTokens(messages)
	overflow, err := s.IsOverflow(ctx, sessionID, tokens)
	if err != nil {
		return "stop", err
	}

	if overflow {
		pruned, err := s.Prune(ctx, sessionID)
		if err != nil {
			return "stop", err
		}

		if pruned > 0 && auto {
			result, err := s.Compact(ctx, sessionID, messages[pruned:])
			if err != nil {
				return "stop", err
			}
			if result.Compacted {
				return "stop", nil
			}
		}

		if auto {
			return "continue", nil
		}
		return "stop", nil
	}

	return "continue", nil
}

type CompactionEvent struct {
	SessionID string
	Type      string
	Timestamp time.Time
	Pruned    int
	Summary   string
	Tokens    int
}
