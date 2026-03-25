package sdk

import "context"

// Planner produces structured plans from natural language intents.
type Planner interface {
	ID() string
	Capabilities() []string
	Plan(ctx context.Context, req PlanRequest) (Plan, error)
}

// PlannerFactory spawns configured planner instances.
type PlannerFactory interface {
	Provider() string
	New(ctx context.Context, cfg map[string]any) (Planner, error)
}

// Tool defines a discrete capability BruteCode can invoke via the orchestrator.
type Tool interface {
	Name() string
	Invoke(ctx context.Context, input map[string]any) (ToolResult, error)
}

// ToolSpec provides tool metadata for LLM tool-calling.
type ToolSpec interface {
	Name() string
	Describe() string
	Schema() map[string]any
}

// ToolRegistry manages tool registration and lookup.
type ToolRegistry interface {
	Register(tool Tool) error
	Get(name string) (Tool, bool)
}

// ConversationStore persists conversation transcripts or summaries.
type ConversationStore interface {
	Append(ctx context.Context, sessionID string, msg Message) error
	History(ctx context.Context, sessionID string) ([]Message, error)
}

// TranscriptWriter writes markdown transcripts to disk or alternative stores.
type TranscriptWriter interface {
	Write(ctx context.Context, sessionID string, markdown string) error
	Prune(ctx context.Context, olderThanSeconds int64) error
}

// Module is the shared interface implemented by pluggable components.
type Module interface {
	ID() string
	Init(ctx context.Context, deps ModuleDeps) error
	Shutdown(ctx context.Context) error
}

// ModuleDeps lists the dependencies passed during module initialization.
type ModuleDeps struct {
	ToolRegistry ToolRegistry
}

// Retriever fetches contextual documents for planners or tools.
type Retriever interface {
	Name() string
	Retrieve(ctx context.Context, query string, opts RetrieveOptions) ([]Document, error)
}

// RetrieveOptions allows retrievers to tune search behavior.
type RetrieveOptions struct {
	Limit int
}

// Document is a retrieval unit returned to the planner.
type Document struct {
	ID      string
	Title   string
	Content string
	Source  string
	Score   float64
}

// Skill exposes lightweight user-authored logic.
type Skill interface {
	Describe() SkillMetadata
	Warmup(ctx context.Context) error
	Invoke(ctx context.Context, input map[string]any) (map[string]any, error)
}

// SkillMetadata provides discovery details for skills.
type SkillMetadata struct {
	Name              string
	Description       string
	Capabilities      []string
	ExpectedTokenCost int
}

// PolicyProvider evaluates guardrails for requested actions.
type PolicyProvider interface {
	Describe() string
	Evaluate(ctx context.Context, query PolicyQuery) (PolicyDecision, error)
}
