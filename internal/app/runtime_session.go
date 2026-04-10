package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.uber.org/zap"

	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/planner/llm"
)

type ConfirmationDecision struct {
	Reason       string   `json:"reason,omitempty"`
	RuleName     string   `json:"rule_name,omitempty"`
	RiskLevel    string   `json:"risk_level,omitempty"`
	RiskScore    int      `json:"risk_score,omitempty"`
	Alternatives []string `json:"alternatives,omitempty"`
	Suggestions  []string `json:"suggestions,omitempty"`
}

type ConfirmationPayload struct {
	Tool         string               `json:"tool"`
	Inputs       map[string]any       `json:"inputs,omitempty"`
	Decision     ConfirmationDecision `json:"decision,omitempty"`
	ReplyOptions []string             `json:"reply_options,omitempty"`
}

type IntentDecision int

const (
	IntentApprove IntentDecision = iota
	IntentDeny
	IntentUnclear
)

func ClassifyConfirmationIntent(ctx context.Context, logger *zap.Logger, plannerCfg *config.PlannerConfig, userInput string, toolName string, reason string) (IntentDecision, string) {
	lower := strings.ToLower(strings.TrimSpace(userInput))

	if lower == "" {
		return IntentUnclear, "empty input"
	}

	if strings.HasPrefix(lower, "/") {
		return IntentUnclear, "command input"
	}

	simpleApproval := []string{
		"yes", "y", "approve", "approved", "allow", "ok", "confirm", "proceed", "continue",
		"好的", "好", "可以", "行", "是的", "对", "没错", "同意", "批准", "ok", "yep", "yeah", "yup", "sure",
	}
	for _, token := range simpleApproval {
		if lower == token {
			return IntentApprove, "direct approval word"
		}
	}

	simpleDenial := []string{
		"no", "n", "deny", "denied", "cancel", "stop", "nope", "nah",
		"不", "不要", "算了", "不用", "取消", "拒绝", "不同意", "不行",
	}
	for _, token := range simpleDenial {
		if lower == token {
			return IntentDeny, "direct denial word"
		}
	}

	if plannerCfg == nil || plannerCfg.APIKey == "" || plannerCfg.Provider == "builtin" {
		if strings.Contains(lower, "不") || strings.Contains(lower, "no") || strings.Contains(lower, "don't") || strings.Contains(lower, "dont") {
			return IntentDeny, "contains denial keyword"
		}
		if strings.Contains(lower, "好") || strings.Contains(lower, "ok") || strings.Contains(lower, "yes") || strings.Contains(lower, "do") {
			return IntentApprove, "contains approval keyword"
		}
		return IntentUnclear, "fallback - cannot determine"
	}

	prompt := fmt.Sprintf(`You are a classifier that determines user intent for a confirmation prompt.

Tool being confirmed: %s
Reason for confirmation: %s
User's response: "%s"

Analyze the user's response and classify it as one of:
- "approve": User wants to proceed with the action (even if they add conditions, questions, or modifications, as long as they basically agree)
- "deny": User explicitly refuses or cancels the action

Rules:
- If user says "yes", "ok", "do it", "proceed", "continue", "approve", "好的", "可以", "同意", "执行" → approve
- If user says "no", "cancel", "stop", "don't", "不", "不要", "算了", "拒绝" → deny
- If user asks a question about the action, they are likely NOT denying → approve
- If user says "but what about X?" or "can you first Y?" → they want to proceed but have concerns → approve
- If user says "wait" or "hold on" → they want to think → approve (not a denial)
- If user says "sounds good", "that makes sense", "I see" → approve
- If user just says "thanks" or "okay thanks" → approve
- If user says "never mind" or "forget it" → deny
- If user says "let's think about this more" → approve (they want to discuss, not deny)

Respond with ONLY a JSON object in this format:
{"decision": "approve" or "deny", "reason": "brief explanation in English"}`, toolName, reason, userInput)

	messages := []map[string]any{
		{"role": "user", "content": prompt},
	}

	profile := llm.DetectProviderProfile(plannerCfg.Provider, plannerCfg.Model)
	cleanedMessages, _ := profile.BuildMessages(messages)
	payload, _ := profile.BuildPayload(plannerCfg.Model, cleanedMessages, nil, 0, 100)

	body, err := json.Marshal(payload)
	if err != nil {
		logger.Warn("failed to marshal LLM request", zap.Error(err))
		return IntentUnclear, fmt.Sprintf("failed to marshal: %v", err)
	}

	endpoint := profile.GetEndpoint(plannerCfg.Endpoint)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		logger.Warn("failed to create LLM request", zap.Error(err))
		return IntentUnclear, fmt.Sprintf("failed to create request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	switch plannerCfg.Provider {
	case "openai", "glm", "deepseek", "anthropic", "openrouter", "groq", "mistral", "togetherai", "perplexity", "minimax":
		httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
	}

	resp, err := llmHTTPClient.Do(httpReq)
	if err != nil {
		logger.Warn("failed to call LLM for intent classification", zap.Error(err))
		return IntentUnclear, fmt.Sprintf("LLM call failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		logger.Warn("LLM returned error status", zap.Int("status", resp.StatusCode), zap.String("body", string(respBody)))
		return IntentUnclear, fmt.Sprintf("LLM error status %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Warn("failed to read LLM response", zap.Error(err))
		return IntentUnclear, fmt.Sprintf("failed to read response: %v", err)
	}

	parsed, err := profile.ParseResponse(respBody)
	if err != nil {
		logger.Warn("failed to parse LLM response", zap.Error(err), zap.String("body", string(respBody)))
		return IntentUnclear, fmt.Sprintf("failed to parse: %v", err)
	}

	content := parsed.Content
	if content == "" {
		logger.Warn("empty LLM response")
		return IntentUnclear, "empty LLM response"
	}

	content = strings.TrimSpace(content)

	var result struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}

	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		logger.Warn("failed to parse LLM JSON response", zap.Error(err), zap.String("content", content))
		if strings.Contains(strings.ToLower(content), "approve") {
			return IntentApprove, "LLM indicated approve (parse failed)"
		}
		if strings.Contains(strings.ToLower(content), "deny") {
			return IntentDeny, "LLM indicated deny (parse failed)"
		}
		return IntentUnclear, fmt.Sprintf("cannot parse LLM response: %v", err)
	}

	switch strings.ToLower(result.Decision) {
	case "approve":
		return IntentApprove, "LLM: " + result.Reason
	case "deny":
		return IntentDeny, "LLM: " + result.Reason
	default:
		return IntentUnclear, fmt.Sprintf("unknown LLM decision: %s", result.Decision)
	}
}

func isReservedCommand(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "new", "sessions", "skills", "models", "monitor", "plan", "vim", "ssh", "connect", "help", "exit", "checkpoint":
		return true
	case "team":
		return true
	default:
		return false
	}
}
