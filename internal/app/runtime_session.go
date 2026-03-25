package app

import (
	"strings"
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

func isConfirmationApproval(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	approved := []string{"yes", "y", "approve", "approved", "allow", "ok", "confirm", "proceed", "continue"}
	for _, token := range approved {
		if lower == token {
			return true
		}
	}
	return false
}

func isConfirmationDenial(input string) bool {
	lower := strings.ToLower(strings.TrimSpace(input))
	denied := []string{"no", "n", "deny", "denied", "cancel", "stop"}
	for _, token := range denied {
		if lower == token {
			return true
		}
	}
	return false
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
