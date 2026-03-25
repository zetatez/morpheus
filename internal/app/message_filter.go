package app

import (
	"strings"
	"sync"
)

type MessageFilter struct {
	mu        sync.RWMutex
	compacted map[string]map[string]bool
}

func NewMessageFilter() *MessageFilter {
	return &MessageFilter{
		compacted: make(map[string]map[string]bool),
	}
}

func (mf *MessageFilter) MarkCompacted(sessionID, messageID string) {
	mf.mu.Lock()
	defer mf.mu.Unlock()

	if mf.compacted[sessionID] == nil {
		mf.compacted[sessionID] = make(map[string]bool)
	}
	mf.compacted[sessionID][messageID] = true
}

func (mf *MessageFilter) IsCompacted(sessionID, messageID string) bool {
	mf.mu.RLock()
	defer mf.mu.RUnlock()

	if mf.compacted[sessionID] == nil {
		return false
	}
	return mf.compacted[sessionID][messageID]
}

func (mf *MessageFilter) FilterCompacted(messages []map[string]any) []map[string]any {
	mf.mu.RLock()
	defer mf.mu.RUnlock()

	sessionID := ""
	if len(messages) > 0 {
		if sid, ok := messages[0]["session_id"].(string); ok {
			sessionID = sid
		}
	}

	var compactedIDs map[string]bool
	if sessionID != "" {
		compactedIDs = mf.compacted[sessionID]
	}

	var filtered []map[string]any
	for _, msg := range messages {
		if compactedIDs != nil {
			if id, ok := msg["id"].(string); ok && compactedIDs[id] {
				continue
			}
		}
		filtered = append(filtered, msg)
	}

	return filtered
}

func (mf *MessageFilter) Reset(sessionID string) {
	mf.mu.Lock()
	defer mf.mu.Unlock()
	delete(mf.compacted, sessionID)
}

type TokenBudget struct {
	mu        sync.RWMutex
	budget    int
	used      int
	sessionID string
}

func NewTokenBudget(sessionID string, budget int) *TokenBudget {
	return &TokenBudget{
		budget:    budget,
		used:      0,
		sessionID: sessionID,
	}
}

func (tb *TokenBudget) EstimateTokens(text string) int {
	return len(text) / 4
}

func (tb *TokenBudget) CanFit(tokens int) bool {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	return tb.used+tokens <= tb.budget
}

func (tb *TokenBudget) Use(tokens int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.used += tokens
}

func (tb *TokenBudget) Remaining() int {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	return tb.budget - tb.used
}

func (tb *TokenBudget) UsagePercent() float64 {
	tb.mu.RLock()
	defer tb.mu.RUnlock()
	if tb.budget == 0 {
		return 0
	}
	return float64(tb.used) / float64(tb.budget) * 100
}

type SmartTruncator struct {
	maxOutputTokens int
	protectRecent   int
}

func NewSmartTruncator(maxOutputTokens, protectRecent int) *SmartTruncator {
	return &SmartTruncator{
		maxOutputTokens: maxOutputTokens,
		protectRecent:   protectRecent,
	}
}

func (st *SmartTruncator) TruncateToolOutput(output string, isRecent bool) string {
	if isRecent && st.protectRecent > 0 {
		return output
	}

	estimatedTokens := st.EstimateTokens(output)
	if estimatedTokens <= st.maxOutputTokens {
		return output
	}

	maxChars := st.maxOutputTokens * 4
	if maxChars < len(output) {
		truncated := output[:maxChars]
		lastNewline := strings.LastIndex(truncated, "\n")
		if lastNewline > maxChars/2 {
			truncated = truncated[:lastNewline]
		}
		return strings.TrimSpace(truncated) + "\n\n[Output truncated due to length...]"
	}

	return output
}

func (st *SmartTruncator) EstimateTokens(text string) int {
	return len(text) / 4
}

func TruncateContent(content string, maxChars int) string {
	if len(content) <= maxChars {
		return content
	}

	truncated := content[:maxChars]
	lastNewline := strings.LastIndex(truncated, "\n")
	if lastNewline > maxChars/2 {
		truncated = truncated[:lastNewline]
	}
	return strings.TrimSpace(truncated) + "\n\n[Content truncated...]"
}

func BuildFilteredMessages(messages []map[string]any, sessionID string, filter *MessageFilter, budget *TokenBudget) []map[string]any {
	if filter != nil {
		messages = filter.FilterCompacted(messages)
	}

	var filtered []map[string]any
	for _, msg := range messages {
		msgCopy := make(map[string]any)
		for k, v := range msg {
			msgCopy[k] = v
		}

		if content, ok := msgCopy["content"].(string); ok {
			tokens := budget.EstimateTokens(content)
			if !budget.CanFit(tokens) {
				truncated := TruncateContent(content, budget.Remaining()*4)
				msgCopy["content"] = truncated
				msgCopy["_truncated"] = true
			} else {
				budget.Use(tokens)
			}
		}

		filtered = append(filtered, msgCopy)
	}

	return filtered
}
