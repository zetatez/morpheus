package app

import (
	"strings"
)

func normalizeSessionID(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		return "default"
	}
	return sessionID
}

func isEmpty(s string) bool {
	return strings.TrimSpace(s) == ""
}

func isNotEmpty(s string) bool {
	return strings.TrimSpace(s) != ""
}

func normalizeString(s string) string {
	return strings.TrimSpace(s)
}

func containsIgnoreCase(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

func equalsIgnoreCase(a, b string) bool {
	return strings.EqualFold(a, b)
}
