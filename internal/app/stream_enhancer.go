package app

import (
	"fmt"
	"sync"
	"time"
)

// StreamingEnhancer adds Claude Code-style streaming enhancements:
// 1. Parallel tool execution while model outputs
// 2. Fallback model on failure
// 3. User interrupt handling with tombstone results

// InterruptHandler handles user interrupts during streaming
type InterruptHandler struct {
	mu           sync.Mutex
	interrupted  bool
	pendingCalls map[string]bool
}

func NewInterruptHandler() *InterruptHandler {
	return &InterruptHandler{
		pendingCalls: make(map[string]bool),
	}
}

// MarkInterrupted marks that the user has interrupted the operation
func (ih *InterruptHandler) MarkInterrupted() {
	ih.mu.Lock()
	defer ih.mu.Unlock()
	ih.interrupted = true
}

// IsInterrupted returns whether the operation was interrupted
func (ih *InterruptHandler) IsInterrupted() bool {
	ih.mu.Lock()
	defer ih.mu.Unlock()
	return ih.interrupted
}

// RegisterPendingCall marks a tool call as pending
func (ih *InterruptHandler) RegisterPendingCall(callID string) {
	ih.mu.Lock()
	defer ih.mu.Unlock()
	ih.pendingCalls[callID] = true
}

// CreateTombstones creates error results for all pending tool calls
// This ensures protocol consistency when user interrupts
func (ih *InterruptHandler) CreateTombstones() []map[string]any {
	ih.mu.Lock()
	defer ih.mu.Unlock()

	var tombstones []map[string]any
	for callID := range ih.pendingCalls {
		tombstones = append(tombstones, map[string]any{
			"role":         "tool",
			"tool_call_id": callID,
			"content":      fmt.Sprintf(`{"success": false, "error": "interrupted by user"}`),
			"is_error":     true,
		})
	}
	ih.pendingCalls = make(map[string]bool)
	ih.interrupted = false

	return tombstones
}

// Reset clears the interrupt state
func (ih *InterruptHandler) Reset() {
	ih.mu.Lock()
	defer ih.mu.Unlock()
	ih.interrupted = false
	ih.pendingCalls = make(map[string]bool)
}

// ModelFallbackConfig configures fallback models
type ModelFallbackConfig struct {
	// FallbackModels lists models to try in order of preference
	FallbackModels []string
	// MaxRetries is the maximum number of retries per model
	MaxRetries int
	// RetryDelay is the delay between retries
	RetryDelay time.Duration
}

// ModelFallback handles model fallback when primary model fails
type ModelFallback struct {
	config ModelFallbackConfig
}

func NewModelFallback(cfg ModelFallbackConfig) *ModelFallback {
	if len(cfg.FallbackModels) == 0 {
		cfg.FallbackModels = []string{"gpt-4o", "gpt-3.5-turbo"}
	}
	if cfg.MaxRetries == 0 {
		cfg.MaxRetries = 2
	}
	if cfg.RetryDelay == 0 {
		cfg.RetryDelay = time.Second
	}
	return &ModelFallback{config: cfg}
}

// ShouldRetry determines if we should try a fallback model
func (mf *ModelFallback) ShouldRetry(err error, attemptCount int) bool {
	if attemptCount >= mf.config.MaxRetries {
		return false
	}
	re := IsRetryableError(err)
	return re != nil && re.IsRetryable
}

// GetNextFallback returns the next fallback model to try
func (mf *ModelFallback) GetNextFallback(currentModel string, tried []string) string {
	for _, model := range mf.config.FallbackModels {
		if model == currentModel {
			continue
		}
		alreadyTried := false
		for _, t := range tried {
			if t == model {
				alreadyTried = true
				break
			}
		}
		if !alreadyTried {
			return model
		}
	}
	return ""
}

// ParallelToolExecutor manages parallel tool execution during streaming
type ParallelToolExecutor struct {
	mu       sync.Mutex
	pending  map[string]*PendingToolCall
	complete map[string]*CompletedToolCall
}

type PendingToolCall struct {
	CallID    string
	Name      string
	Arguments map[string]any
	Done      bool
}

type CompletedToolCall struct {
	CallID string
	Name   string
	Result map[string]any
	Error  error
}

// NewParallelToolExecutor creates a new parallel tool executor
func NewParallelToolExecutor() *ParallelToolExecutor {
	return &ParallelToolExecutor{
		pending:  make(map[string]*PendingToolCall),
		complete: make(map[string]*CompletedToolCall),
	}
}

// Register registers a tool call as pending
func (pte *ParallelToolExecutor) Register(callID, name string, args map[string]any) {
	pte.mu.Lock()
	defer pte.mu.Unlock()
	pte.pending[callID] = &PendingToolCall{
		CallID:    callID,
		Name:      name,
		Arguments: args,
		Done:      false,
	}
}

// MarkDone marks a tool call as having all its input
func (pte *ParallelToolExecutor) MarkDone(callID string) {
	pte.mu.Lock()
	defer pte.mu.Unlock()
	if pt, ok := pte.pending[callID]; ok {
		pt.Done = true
	}
}

// GetPending returns all pending tool calls that are done
func (pte *ParallelToolExecutor) GetPending() []*PendingToolCall {
	pte.mu.Lock()
	defer pte.mu.Unlock()

	var result []*PendingToolCall
	for _, pt := range pte.pending {
		if pt.Done {
			result = append(result, pt)
		}
	}
	return result
}

// Complete marks a tool call as completed
func (pte *ParallelToolExecutor) Complete(callID string, result map[string]any, err error) {
	pte.mu.Lock()
	defer pte.mu.Unlock()

	pte.complete[callID] = &CompletedToolCall{
		CallID: callID,
		Name:   pte.pending[callID].Name,
		Result: result,
		Error:  err,
	}
	delete(pte.pending, callID)
}

// GetCompleted returns a completed tool call
func (pte *ParallelToolExecutor) GetCompleted(callID string) (*CompletedToolCall, bool) {
	pte.mu.Lock()
	defer pte.mu.Unlock()
	c, ok := pte.complete[callID]
	return c, ok
}

// CancelAll cancels all pending tool calls
func (pte *ParallelToolExecutor) CancelAll() {
	pte.mu.Lock()
	defer pte.mu.Unlock()

	for callID, pt := range pte.pending {
		pte.complete[callID] = &CompletedToolCall{
			CallID: callID,
			Name:   pt.Name,
			Result: nil,
			Error:  fmt.Errorf("cancelled"),
		}
	}
	pte.pending = make(map[string]*PendingToolCall)
}

// Count returns the number of pending and completed calls
func (pte *ParallelToolExecutor) Count() (pending int, completed int) {
	pte.mu.Lock()
	defer pte.mu.Unlock()
	return len(pte.pending), len(pte.complete)
}

// StreamEvent represents an event from the streaming LLM response
type StreamEvent struct {
	Type         string
	Index        int
	Text         string
	ToolName     string
	ToolID       string
	ToolArgs     string
	ReasoningID  string
	FinishReason string
}

// StreamProcessor coordinates streaming output with tool execution
type StreamProcessor struct {
	executor        *ParallelToolExecutor
	interruptHandler *InterruptHandler
	modelFallback   *ModelFallback
}

func NewStreamProcessor(cfg ModelFallbackConfig) *StreamProcessor {
	return &StreamProcessor{
		executor:        NewParallelToolExecutor(),
		interruptHandler: NewInterruptHandler(),
		modelFallback:   NewModelFallback(cfg),
	}
}

// ProcessEvent handles a streaming event from the LLM
func (sp *StreamProcessor) ProcessEvent(evt *StreamEvent) {
	sp.interruptHandler.mu.Lock()
	interrupted := sp.interruptHandler.interrupted
	sp.interruptHandler.mu.Unlock()

	if interrupted {
		return
	}

	switch evt.Type {
	case "tool_call":
		sp.executor.Register(evt.ToolID, evt.ToolName, nil)
		sp.interruptHandler.RegisterPendingCall(evt.ToolID)

	case "tool_call_delta":
		sp.executor.MarkDone(evt.ToolID)

	case "tool_input_end":
		// Tool input is complete, can now execute

	case "interrupt":
		sp.interruptHandler.MarkInterrupted()
	}
}

// HandleInterrupt handles user interrupt, creating tombstones for pending calls
func (sp *StreamProcessor) HandleInterrupt() []map[string]any {
	sp.executor.CancelAll()
	return sp.interruptHandler.CreateTombstones()
}

// ShouldRetryFallback checks if we should try a fallback model
func (sp *StreamProcessor) ShouldRetryFallback(err error, attemptCount int) bool {
	return sp.modelFallback.ShouldRetry(err, attemptCount)
}

// GetNextFallback returns the next fallback model to try
func (sp *StreamProcessor) GetNextFallback(currentModel string, tried []string) string {
	return sp.modelFallback.GetNextFallback(currentModel, tried)
}
