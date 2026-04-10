package app

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	DOOM_LOOP_THRESHOLD = 3
)

type LoopStatus string

const (
	LoopStatusContinue          LoopStatus = "continue"
	LoopStatusStop              LoopStatus = "stop"
	LoopStatusCompact           LoopStatus = "compact"
	LoopStatusNeedsFinalSummary LoopStatus = "needs_final_summary"
	LoopStatusNeedsConfirmation LoopStatus = "needs_confirmation"
)

type LoopResult struct {
	Status       LoopStatus
	Response     *sdk.Plan
	Error        error
	Confirmation *PendingConfirmation
}

func (r LoopResult) IsContinue() bool          { return r.Status == LoopStatusContinue }
func (r LoopResult) IsStop() bool              { return r.Status == LoopStatusStop }
func (r LoopResult) IsCompact() bool           { return r.Status == LoopStatusCompact }
func (r LoopResult) IsNeedsConfirmation() bool { return r.Status == LoopStatusNeedsConfirmation }
func (r LoopResult) HasError() bool            { return r.Error != nil }

type ProcessorEventType string

const (
	EventStart           ProcessorEventType = "start"
	EventFinish          ProcessorEventType = "finish"
	EventTextDelta       ProcessorEventType = "text_delta"
	EventReasoningStart  ProcessorEventType = "reasoning_start"
	EventReasoningDelta  ProcessorEventType = "reasoning_delta"
	EventReasoningEnd    ProcessorEventType = "reasoning_end"
	EventToolCall        ProcessorEventType = "tool_call"
	EventToolResult      ProcessorEventType = "tool_result"
	EventToolError       ProcessorEventType = "tool_error"
	EventNeedsCompaction ProcessorEventType = "needs_compaction"
	EventError           ProcessorEventType = "error"
)

type ProcessorEvent struct {
	Type     ProcessorEventType
	Index    int
	Text     string
	ToolName string
	ToolID   string
	ToolArgs string
	Result   sdk.ToolResult
	Error    error
}

type ToolCallState struct {
	ID        string                 `json:"id"`
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
	Status    string                 `json:"status"`
	Result    *sdk.ToolResult        `json:"result,omitempty"`
	Error     string                 `json:"error,omitempty"`
	Time      struct {
		Start int64 `json:"start"`
		End   int64 `json:"end"`
	} `json:"time"`
}

func (t *ToolCallState) IsPending() bool   { return t.Status == "pending" }
func (t *ToolCallState) IsRunning() bool   { return t.Status == "running" }
func (t *ToolCallState) IsCompleted() bool { return t.Status == "completed" }
func (t *ToolCallState) IsError() bool     { return t.Status == "error" }

type ProcessorCallbacks struct {
	OnTextDelta      func(text string)
	OnReasoningStart func(id string)
	OnReasoningDelta func(id string, text string)
	OnReasoningEnd   func(text string)
	OnToolCall       func(index int, name, id string, args map[string]interface{})
	OnToolResult     func(id string, result sdk.ToolResult)
	OnToolError      func(id string, err error)
	OnFinish         func(content string, finishReason string, toolCalls []*ToolCallState)
	OnError          func(err error)
}

type SessionProcessor struct {
	mu sync.RWMutex

	steps    int
	maxSteps int

	content      strings.Builder
	reasoning    strings.Builder
	reasoningID  string
	toolCalls    map[int]*ToolCallState
	currentIndex int

	blocked         bool
	needsCompaction bool
	error           error

	callbacks ProcessorCallbacks
}

func NewSessionProcessor(maxSteps int) *SessionProcessor {
	return &SessionProcessor{
		maxSteps:  maxSteps,
		toolCalls: make(map[int]*ToolCallState),
	}
}

func (p *SessionProcessor) SetCallbacks(cb ProcessorCallbacks) {
	p.callbacks = cb
}

func (p *SessionProcessor) IncrementStep() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps++
}

func (p *SessionProcessor) Step() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.steps
}

func (p *SessionProcessor) IsLastStep() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.maxSteps > 0 && p.steps >= p.maxSteps
}

func (p *SessionProcessor) ShouldContinue() bool {
	if p.IsLastStep() {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.blocked || p.error != nil {
		return false
	}
	return true
}

func (p *SessionProcessor) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.content.Reset()
	p.reasoning.Reset()
	p.reasoningID = ""
	p.toolCalls = make(map[int]*ToolCallState)
	p.currentIndex = 0
	p.blocked = false
	p.needsCompaction = false
	p.error = nil
}

func (p *SessionProcessor) Content() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.content.String()
}

func (p *SessionProcessor) Reasoning() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.reasoning.String()
}

func (p *SessionProcessor) ToolCallsAll() []*ToolCallState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var calls []*ToolCallState
	for _, tc := range p.toolCalls {
		calls = append(calls, tc)
	}
	return calls
}

func (p *SessionProcessor) HasToolCalls() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.toolCalls) > 0
}

func (p *SessionProcessor) IsBlocked() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.blocked
}

func (p *SessionProcessor) NeedsCompaction() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.needsCompaction
}

func (p *SessionProcessor) DetermineResult() LoopStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.needsCompaction {
		return LoopStatusCompact
	}
	if p.blocked || p.error != nil {
		return LoopStatusStop
	}
	return LoopStatusContinue
}

func (p *SessionProcessor) HandleEvent(evt ProcessorEvent) {
	p.mu.Lock()
	defer p.mu.Unlock()

	switch evt.Type {
	case EventStart:
		p.Reset()

	case EventTextDelta:
		p.content.WriteString(evt.Text)
		if p.callbacks.OnTextDelta != nil {
			p.callbacks.OnTextDelta(evt.Text)
		}

	case EventReasoningStart:
		p.reasoningID = evt.ToolID
		p.reasoning.Reset()
		if p.callbacks.OnReasoningStart != nil {
			p.callbacks.OnReasoningStart(evt.ToolID)
		}

	case EventReasoningDelta:
		p.reasoning.WriteString(evt.Text)
		if p.callbacks.OnReasoningDelta != nil {
			p.callbacks.OnReasoningDelta(p.reasoningID, evt.Text)
		}

	case EventReasoningEnd:
		if p.callbacks.OnReasoningEnd != nil {
			p.callbacks.OnReasoningEnd(p.reasoning.String())
		}

	case EventToolCall:
		p.currentIndex = evt.Index
		state := &ToolCallState{
			ID:        evt.ToolID,
			Name:      evt.ToolName,
			Arguments: make(map[string]interface{}),
			Status:    "running",
		}
		state.Time.Start = time.Now().UnixMilli()
		p.toolCalls[evt.Index] = state
		if p.callbacks.OnToolCall != nil {
			p.callbacks.OnToolCall(evt.Index, evt.ToolName, evt.ToolID, state.Arguments)
		}

	case EventToolResult:
		if state, ok := p.toolCalls[p.currentIndex]; ok {
			state.Status = "completed"
			state.Result = &evt.Result
			state.Time.End = time.Now().UnixMilli()
			if p.callbacks.OnToolResult != nil {
				p.callbacks.OnToolResult(state.ID, evt.Result)
			}
		}

	case EventToolError:
		if state, ok := p.toolCalls[p.currentIndex]; ok {
			state.Status = "error"
			state.Error = evt.Error.Error()
			state.Time.End = time.Now().UnixMilli()
			if p.callbacks.OnToolError != nil {
				p.callbacks.OnToolError(state.ID, evt.Error)
			}
		}
		p.blocked = true

	case EventNeedsCompaction:
		p.needsCompaction = true

	case EventFinish:
		var calls []*ToolCallState
		for _, tc := range p.toolCalls {
			calls = append(calls, tc)
		}
		if p.callbacks.OnFinish != nil {
			p.callbacks.OnFinish(p.content.String(), "", calls)
		}

	case EventError:
		p.error = evt.Error
		if p.callbacks.OnError != nil {
			p.callbacks.OnError(evt.Error)
		}
	}
}

type LoopDoomKey struct {
	ToolName string `json:"tool_name"`
	Input    string `json:"input"`
	Count    int    `json:"count"`
}

type DoomLoopState struct {
	Detected      bool   `json:"detected"`
	RecoveryCount int    `json:"recovery_count"`
	LastDoomTool  string `json:"last_doom_tool"`
	LastDoomInput string `json:"last_doom_input"`
}

type LoopDoomDetector struct {
	mu         sync.RWMutex
	threshold  int
	history    map[string][]LoopDoomKey
	states     map[string]*DoomLoopState
	maxRecover int
}

func NewLoopDoomDetector(threshold int) *LoopDoomDetector {
	if threshold <= 0 {
		threshold = DOOM_LOOP_THRESHOLD
	}
	return &LoopDoomDetector{
		threshold:  threshold,
		history:    make(map[string][]LoopDoomKey),
		states:     make(map[string]*DoomLoopState),
		maxRecover: 2,
	}
}

func (d *LoopDoomDetector) SetMaxRecovery(max int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.maxRecover = max
}

func (d *LoopDoomDetector) sessionKey(sessionID string) string {
	return sessionID
}

func (d *LoopDoomDetector) Record(sessionID, toolName string, args map[string]interface{}) (isDoomLoop bool, needsConfirmation bool) {
	inputKey := hashArgs(args)

	d.mu.Lock()
	defer d.mu.Unlock()

	sk := d.sessionKey(sessionID)
	history := d.history[sk]

	state := d.states[sk]
	if state == nil {
		state = &DoomLoopState{}
		d.states[sk] = state
	}

	var newHistory []LoopDoomKey
	if len(history) > 0 {
		last := history[len(history)-1]
		if last.ToolName == toolName && last.Input == inputKey {
			newHistory = append(history, LoopDoomKey{
				ToolName: toolName,
				Input:    inputKey,
				Count:    last.Count + 1,
			})
		} else {
			newHistory = []LoopDoomKey{{ToolName: toolName, Input: inputKey, Count: 1}}
			state.Detected = false
			state.RecoveryCount = 0
		}
	} else {
		newHistory = []LoopDoomKey{{ToolName: toolName, Input: inputKey, Count: 1}}
		state.Detected = false
		state.RecoveryCount = 0
	}

	d.history[sk] = newHistory

	if newHistory[len(newHistory)-1].Count >= d.threshold {
		if state.RecoveryCount >= d.maxRecover {
			return true, true
		}
		state.Detected = true
		state.LastDoomTool = toolName
		state.LastDoomInput = inputKey
		return true, true
	}
	return false, false
}

func (d *LoopDoomDetector) RecordRecovery(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	sk := d.sessionKey(sessionID)
	state := d.states[sk]
	if state != nil {
		state.RecoveryCount++
		state.Detected = false
	}
}

func (d *LoopDoomDetector) Reset(sessionID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.history, d.sessionKey(sessionID))
	delete(d.states, d.sessionKey(sessionID))
}

func (d *LoopDoomDetector) GetRecent(sessionID string, count int) []LoopDoomKey {
	d.mu.RLock()
	defer d.mu.RUnlock()
	sk := d.sessionKey(sessionID)
	history := d.history[sk]
	if len(history) <= count {
		return history
	}
	return history[len(history)-count:]
}

func (d *LoopDoomDetector) GetState(sessionID string) *DoomLoopState {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.states[d.sessionKey(sessionID)]
}

func (d *LoopDoomDetector) IsRecoverable(sessionID string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	state := d.states[d.sessionKey(sessionID)]
	if state == nil {
		return true
	}
	return state.RecoveryCount < d.maxRecover
}

func hashArgs(args map[string]interface{}) string {
	if args == nil {
		return ""
	}
	data, _ := json.Marshal(args)
	return string(data)
}
