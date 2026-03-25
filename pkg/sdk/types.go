package sdk

import "time"

// Message represents a conversation turn captured by the Conversation Manager.
type Message struct {
	ID        string
	Role      string
	Content   string
	Parts     []MessagePart
	Timestamp time.Time
}

// MessagePart represents structured content for a message.
type MessagePart struct {
	Type   string
	Text   string
	Tool   string
	CallID string
	Input  map[string]any
	Output map[string]any
	Error  string
	Status string
}

// PlanStatus enumerates the lifecycle states for a plan.
type PlanStatus int

const (
	PlanStatusDraft PlanStatus = iota
	PlanStatusConfirmed
	PlanStatusInProgress
	PlanStatusBlocked
	PlanStatusDone
)

// StepStatus tracks execution progress for a plan step.
type StepStatus int

const (
	StepStatusPending StepStatus = iota
	StepStatusRunning
	StepStatusSucceeded
	StepStatusFailed
)

func (s PlanStatus) String() string {
	switch s {
	case PlanStatusDraft:
		return "draft"
	case PlanStatusConfirmed:
		return "confirmed"
	case PlanStatusInProgress:
		return "in_progress"
	case PlanStatusBlocked:
		return "blocked"
	case PlanStatusDone:
		return "done"
	default:
		return "unknown"
	}
}

func (s StepStatus) String() string {
	switch s {
	case StepStatusPending:
		return "pending"
	case StepStatusRunning:
		return "running"
	case StepStatusSucceeded:
		return "succeeded"
	case StepStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// Plan describes the structured strategy that BruteCode executes.
type Plan struct {
	ID          string
	Summary     string
	Steps       []PlanStep
	Risks       []string
	Status      PlanStatus
	Context     []SummaryChunk
	Permissions SnapshotPermissions
}

// PlanStep codifies a single executable action within a plan.
type PlanStep struct {
	ID          string
	Description string
	Tool        string
	Inputs      map[string]any
	Outputs     []string
	Status      StepStatus
	DependsOn   []string
}

// ToolResult captures the structured output of a Tool invocation.
type ToolResult struct {
	StepID  string
	Success bool
	Data    map[string]any
	Error   string
}

// SummaryChunk stores condensed context material that can be injected back
// into planner or conversation prompts.
type SummaryChunk struct {
	ID               string
	Scope            string
	Content          string
	SourceMessageIDs []string
	TokenCost        int
}

// SnapshotPermissions represent the guardrails active for a plan.
type SnapshotPermissions struct {
	Commands        []string
	CommandPatterns []string
	WritablePaths   []string
	ReadOnlyPaths   []string
	MaxWriteSizeKB  int
	NetworkAllow    []string
	NetworkDeny     []string
}

// PlanRequest is the payload passed to planners to request structured plans.
type PlanRequest struct {
	ConversationID string
	Prompt         string
	Context        []SummaryChunk
	Intent         string
}

// RiskLevel represents the danger level of an operation.
type RiskLevel int

const (
	RiskUnknown RiskLevel = iota
	RiskLow
	RiskMedium
	RiskHigh
	RiskCritical
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	case RiskCritical:
		return "critical"
	default:
		return "unknown"
	}
}

// Action represents the policy decision action.
type Action string

const (
	ActionAllow   Action = "allow"
	ActionDeny    Action = "deny"
	ActionConfirm Action = "confirm"
	ActionWarn    Action = "warn"
)

// PolicyDecision encodes allow/deny responses from the PolicyEngine.
type PolicyDecision struct {
	Allowed         bool
	Action          Action
	Reason          string
	RuleName        string
	RiskLevel       RiskLevel
	RiskScore       int
	RequiresConfirm bool
	ConfirmToken    string
	Alternatives    []string
	Suggestions     []string
	LogEnabled      bool
	LogMessage      string
	MatchedFactors  []string
}

// PolicyQuery describes the action being evaluated.
type PolicyQuery struct {
	Command string
	Path    string
	Tool    string
	Mode    string
}
