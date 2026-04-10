package session

import (
	"encoding/json"
	"sync"
	"time"
)

const (
	DefaultDoomLoopThreshold = 5
	DefaultDoomLoopWindow   = 5 * time.Minute
)

type DoomLoopConfig struct {
	Threshold int
	Window   time.Duration
}

type ToolCall struct {
	ToolName string
	Input    map[string]interface{}
	CallID   string
	StartTime time.Time
	Status   ToolCallStatus
}

type ToolCallStatus string

const (
	ToolCallStatusPending   ToolCallStatus = "pending"
	ToolCallStatusRunning   ToolCallStatus = "running"
	ToolCallStatusCompleted ToolCallStatus = "completed"
	ToolCallStatusError     ToolCallStatus = "error"
)

type DoomLoopDetector struct {
	mu       sync.RWMutex
	config   DoomLoopConfig
	history  map[string][]ToolCall
	onDetect func(toolName string, input map[string]interface{}) bool
}

type DoomLoopDetection struct {
	ToolName   string
	Input     map[string]interface{}
	Count     int
	LastSeen  time.Time
	Window    time.Duration
	Calls     []ToolCall
}

func NewDoomLoopDetector(config DoomLoopConfig) *DoomLoopDetector {
	if config.Threshold <= 0 {
		config.Threshold = DefaultDoomLoopThreshold
	}
	if config.Window <= 0 {
		config.Window = DefaultDoomLoopWindow
	}
	return &DoomLoopDetector{
		config:  config,
		history: make(map[string][]ToolCall),
	}
}

func (d *DoomLoopDetector) SetOnDetect(fn func(toolName string, input map[string]interface{}) bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.onDetect = fn
}

func (d *DoomLoopDetector) RecordCall(toolName, callID string, input map[string]interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	call := ToolCall{
		ToolName: toolName,
		CallID:   callID,
		Input:    input,
		StartTime: now,
		Status:    ToolCallStatusPending,
	}

	key := d.makeKey(toolName, input)
	d.history[key] = append(d.history[key], call)
	d.cleanup(key, now)
}

func (d *DoomLoopDetector) UpdateStatus(callID string, status ToolCallStatus) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for _, calls := range d.history {
		for i := range calls {
			if calls[i].CallID == callID {
				calls[i].Status = status
				return
			}
		}
	}
}

func (d *DoomLoopDetector) Detect(toolName string, input map[string]interface{}) (*DoomLoopDetection, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := d.makeKey(toolName, input)
	calls := d.history[key]
	if len(calls) < d.config.Threshold {
		return nil, false
	}

	d.cleanup(key, time.Now())
	calls = d.history[key]
	if len(calls) < d.config.Threshold {
		return nil, false
	}

	recentCalls := calls[len(calls)-d.config.Threshold:]
	for _, call := range recentCalls {
		if call.Status == ToolCallStatusPending {
			return nil, false
		}
	}

	if !d.allSameInput(recentCalls) {
		return nil, false
	}

	detection := &DoomLoopDetection{
		ToolName:  toolName,
		Input:    input,
		Count:    len(recentCalls),
		LastSeen:  recentCalls[len(recentCalls)-1].StartTime,
		Window:    d.config.Window,
		Calls:    recentCalls,
	}

	return detection, true
}

func (d *DoomLoopDetector) IsDoomLoop(toolName string, input map[string]interface{}) bool {
	detection, ok := d.Detect(toolName, input)
	if !ok {
		return false
	}

	if d.onDetect != nil {
		return d.onDetect(detection.ToolName, detection.Input)
	}

	return true
}

func (d *DoomLoopDetector) Reset(toolName string, input map[string]interface{}) {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := d.makeKey(toolName, input)
	delete(d.history, key)
}

func (d *DoomLoopDetector) ResetAll() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.history = make(map[string][]ToolCall)
}

func (d *DoomLoopDetector) GetHistory(toolName string, input map[string]interface{}) []ToolCall {
	d.mu.RLock()
	defer d.mu.RUnlock()

	key := d.makeKey(toolName, input)
	calls := d.history[key]
	result := make([]ToolCall, len(calls))
	copy(result, calls)
	return result
}

func (d *DoomLoopDetector) GetDetection() []DoomLoopDetection {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	var detections []DoomLoopDetection

	for key, calls := range d.history {
		if len(calls) < d.config.Threshold {
			continue
		}

		d.cleanupLocked(key, now)
		calls = d.history[key]
		if len(calls) < d.config.Threshold {
			continue
		}

		recentCalls := calls[len(calls)-d.config.Threshold:]
		if !d.allSameInput(recentCalls) {
			continue
		}

		for _, call := range recentCalls {
			if call.Status == ToolCallStatusPending {
				goto nextKey
			}
		}

		detections = append(detections, DoomLoopDetection{
			ToolName: recentCalls[0].ToolName,
			Input:   recentCalls[0].Input,
			Count:   len(recentCalls),
			LastSeen: recentCalls[len(recentCalls)-1].StartTime,
			Window:   d.config.Window,
			Calls:   recentCalls,
		})

	nextKey:
	}

	return detections
}

func (d *DoomLoopDetector) makeKey(toolName string, input map[string]interface{}) string {
	data, _ := json.Marshal(input)
	return toolName + ":" + string(data)
}

func (d *DoomLoopDetector) cleanup(key string, now time.Time) {
	d.cleanupLocked(key, now)
}

func (d *DoomLoopDetector) cleanupLocked(key string, now time.Time) {
	calls := d.history[key]
	if calls == nil {
		return
	}

	cutoff := now.Add(-d.config.Window)
	var valid []ToolCall
	for _, call := range calls {
		if call.StartTime.After(cutoff) {
			valid = append(valid, call)
		}
	}

	if len(valid) == 0 {
		delete(d.history, key)
	} else {
		d.history[key] = valid
	}
}

func (d *DoomLoopDetector) allSameInput(calls []ToolCall) bool {
	if len(calls) < 2 {
		return true
	}

	first := calls[0]
	for _, call := range calls[1:] {
		if call.ToolName != first.ToolName {
			return false
		}
		if !jsonEqual(call.Input, first.Input) {
			return false
		}
	}
	return true
}

func jsonEqual(a, b map[string]interface{}) bool {
	aJSON, errA := json.Marshal(a)
	bJSON, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(aJSON) == string(bJSON)
}

type DoomLoopHandler struct {
	mu            sync.RWMutex
	detector      *DoomLoopDetector
	permissionAsk func(toolName string, input map[string]interface{}) bool
}

func NewDoomLoopHandler(config DoomLoopConfig) *DoomLoopHandler {
	h := &DoomLoopHandler{
		detector: NewDoomLoopDetector(config),
	}

	h.detector.SetOnDetect(func(toolName string, input map[string]interface{}) bool {
		h.mu.RLock()
		callback := h.permissionAsk
		h.mu.RUnlock()

		if callback != nil {
			return callback(toolName, input)
		}
		return false
	})

	return h
}

func (h *DoomLoopHandler) SetPermissionAsk(fn func(toolName string, input map[string]interface{}) bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.permissionAsk = fn
}

func (h *DoomLoopHandler) RecordCall(toolName, callID string, input map[string]interface{}) {
	h.detector.RecordCall(toolName, callID, input)
}

func (h *DoomLoopHandler) UpdateStatus(callID string, status ToolCallStatus) {
	h.detector.UpdateStatus(callID, status)
}

func (h *DoomLoopHandler) CheckDoomLoop(toolName string, input map[string]interface{}) bool {
	return h.detector.IsDoomLoop(toolName, input)
}

func (h *DoomLoopHandler) GetDetection() *DoomLoopDetection {
	detections := h.detector.GetDetection()
	if len(detections) > 0 {
		return &detections[0]
	}
	return nil
}

func (h *DoomLoopHandler) Reset() {
	h.detector.ResetAll()
}
