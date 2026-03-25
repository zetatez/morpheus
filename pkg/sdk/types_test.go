package sdk

import (
	"testing"
	"time"
)

func TestPlanStatusString(t *testing.T) {
	tests := []struct {
		status   PlanStatus
		expected string
	}{
		{PlanStatusDraft, "draft"},
		{PlanStatusConfirmed, "confirmed"},
		{PlanStatusInProgress, "in_progress"},
		{PlanStatusBlocked, "blocked"},
		{PlanStatusDone, "done"},
		{PlanStatus(100), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("PlanStatus.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStepStatusString(t *testing.T) {
	tests := []struct {
		status   StepStatus
		expected string
	}{
		{StepStatusPending, "pending"},
		{StepStatusRunning, "running"},
		{StepStatusSucceeded, "succeeded"},
		{StepStatusFailed, "failed"},
		{StepStatus(100), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.status.String(); got != tt.expected {
				t.Errorf("StepStatus.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRiskLevelString(t *testing.T) {
	tests := []struct {
		level    RiskLevel
		expected string
	}{
		{RiskUnknown, "unknown"},
		{RiskLow, "low"},
		{RiskMedium, "medium"},
		{RiskHigh, "high"},
		{RiskCritical, "critical"},
		{RiskLevel(100), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.level.String(); got != tt.expected {
				t.Errorf("RiskLevel.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestMessageParts(t *testing.T) {
	msg := Message{
		ID:        "msg1",
		Role:      "user",
		Content:   "hello",
		Timestamp: time.Now(),
		Parts: []MessagePart{
			{Type: "text", Text: "hello"},
		},
	}

	if msg.ID != "msg1" {
		t.Errorf("msg.ID = %q, want %q", msg.ID, "msg1")
	}
	if msg.Role != "user" {
		t.Errorf("msg.Role = %q, want %q", msg.Role, "user")
	}
	if len(msg.Parts) != 1 {
		t.Errorf("len(msg.Parts) = %d, want 1", len(msg.Parts))
	}
}

func TestMessagePartTypes(t *testing.T) {
	part := MessagePart{
		Type:   "tool",
		Text:   "result",
		Tool:   "test_tool",
		CallID: "call_123",
		Input:  map[string]any{"arg": "value"},
		Output: map[string]any{"result": "success"},
		Error:  "",
		Status: "completed",
	}

	if part.Type != "tool" {
		t.Errorf("part.Type = %q, want %q", part.Type, "tool")
	}
	if part.Tool != "test_tool" {
		t.Errorf("part.Tool = %q, want %q", part.Tool, "test_tool")
	}
	if part.Input["arg"] != "value" {
		t.Errorf("part.Input[arg] = %v, want %v", part.Input["arg"], "value")
	}
}

func TestPlanStatusConstants(t *testing.T) {
	if PlanStatusDraft != 0 {
		t.Errorf("PlanStatusDraft = %d, want 0", PlanStatusDraft)
	}
	if PlanStatusConfirmed != 1 {
		t.Errorf("PlanStatusConfirmed = %d, want 1", PlanStatusConfirmed)
	}
	if PlanStatusInProgress != 2 {
		t.Errorf("PlanStatusInProgress = %d, want 2", PlanStatusInProgress)
	}
	if PlanStatusBlocked != 3 {
		t.Errorf("PlanStatusBlocked = %d, want 3", PlanStatusBlocked)
	}
	if PlanStatusDone != 4 {
		t.Errorf("PlanStatusDone = %d, want 4", PlanStatusDone)
	}
}

func TestStepStatusConstants(t *testing.T) {
	if StepStatusPending != 0 {
		t.Errorf("StepStatusPending = %d, want 0", StepStatusPending)
	}
	if StepStatusRunning != 1 {
		t.Errorf("StepStatusRunning = %d, want 1", StepStatusRunning)
	}
	if StepStatusSucceeded != 2 {
		t.Errorf("StepStatusSucceeded = %d, want 2", StepStatusSucceeded)
	}
	if StepStatusFailed != 3 {
		t.Errorf("StepStatusFailed = %d, want 3", StepStatusFailed)
	}
}

func TestRiskLevelConstants(t *testing.T) {
	if RiskUnknown != 0 {
		t.Errorf("RiskUnknown = %d, want 0", RiskUnknown)
	}
	if RiskLow != 1 {
		t.Errorf("RiskLow = %d, want 1", RiskLow)
	}
	if RiskMedium != 2 {
		t.Errorf("RiskMedium = %d, want 2", RiskMedium)
	}
	if RiskHigh != 3 {
		t.Errorf("RiskHigh = %d, want 3", RiskHigh)
	}
	if RiskCritical != 4 {
		t.Errorf("RiskCritical = %d, want 4", RiskCritical)
	}
}
