package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	DoomLoopThreshold = 3
)

type DoomLoopKey struct {
	ToolName string
	Input    string
}

type DoomLoopTracker struct {
	mu      sync.RWMutex
	history map[string][]DoomLoopKey
}

type DoomLoopDetector struct {
	tracker *DoomLoopTracker
}

func NewDoomLoopDetector() *DoomLoopDetector {
	return &DoomLoopDetector{
		tracker: &DoomLoopTracker{
			history: make(map[string][]DoomLoopKey),
		},
	}
}

func (d *DoomLoopDetector) sessionKey(sessionID string) string {
	return sessionID
}

func (d *DoomLoopDetector) RecordToolCall(sessionID, toolName string, args map[string]any) (isDoomLoop bool, needsConfirmation bool) {
	key := DoomLoopKey{
		ToolName: toolName,
	}

	inputBytes, _ := json.Marshal(args)
	key.Input = string(inputBytes)

	d.tracker.mu.Lock()
	defer d.tracker.mu.Unlock()

	sk := d.sessionKey(sessionID)
	history, exists := d.tracker.history[sk]
	if !exists {
		history = []DoomLoopKey{}
	}

	if len(history) > 0 {
		last := history[len(history)-1]
		if last.ToolName == key.ToolName && last.Input == key.Input {
			history = append(history, key)
		} else {
			history = []DoomLoopKey{key}
		}
	} else {
		history = []DoomLoopKey{key}
	}

	d.tracker.history[sk] = history

	consecutiveCount := 0
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].ToolName == key.ToolName && history[i].Input == key.Input {
			consecutiveCount++
		} else {
			break
		}
	}

	if consecutiveCount >= DoomLoopThreshold {
		return true, true
	}

	return false, false
}

func (d *DoomLoopDetector) Reset(sessionID string) {
	d.tracker.mu.Lock()
	defer d.tracker.mu.Unlock()

	sk := d.sessionKey(sessionID)
	delete(d.tracker.history, sk)
}

func (d *DoomLoopDetector) GetRecentToolCalls(sessionID string, count int) []DoomLoopKey {
	d.tracker.mu.RLock()
	defer d.tracker.mu.RUnlock()

	sk := d.sessionKey(sessionID)
	history, exists := d.tracker.history[sk]
	if !exists {
		return nil
	}

	if len(history) <= count {
		return history
	}
	return history[len(history)-count:]
}

func FormatDoomLoopPrompt(toolName string, attempts int) string {
	parts := []string{"# Doom Loop Detected"}
	parts = append(parts, "", "The same tool call has been detected multiple times consecutively:")
	parts = append(parts, "", fmt.Sprintf("- **Tool**: %s", toolName))
	parts = append(parts, fmt.Sprintf("- **Attempts**: %d", attempts))
	parts = append(parts, "", "This may indicate the agent is stuck in a repetitive pattern.")
	parts = append(parts, "", "## Options")
	parts = append(parts, "", "1. **Continue anyway** - Allow the agent to continue trying")
	parts = append(parts, "2. **Stop** - Halt the agent loop to avoid further wasted effort")
	parts = append(parts, "", "Please confirm how you'd like to proceed.")
	return strings.Join(parts, "\n")
}

func CreateDoomLoopPendingConfirmation(toolName string, inputs map[string]any) PendingConfirmation {
	return PendingConfirmation{
		Tool:   toolName,
		Inputs: inputs,
		Decision: sdk.PolicyDecision{
			Reason:    "Doom loop detected: same tool called multiple times with identical arguments",
			RiskLevel: sdk.RiskMedium,
		},
		Kind:      "doom_loop",
		CreatedAt: time.Now(),
	}
}

type ActionFingerprint struct {
	mu        sync.RWMutex
	sequences map[string][]string
}

func NewActionFingerprint() *ActionFingerprint {
	return &ActionFingerprint{
		sequences: make(map[string][]string),
	}
}

func (af *ActionFingerprint) sessionKey(sessionID string) string {
	return sessionID
}

func (af *ActionFingerprint) RecordSequence(sessionID string, fingerprints []string) {
	af.mu.Lock()
	defer af.mu.Unlock()

	sk := af.sessionKey(sessionID)
	af.sequences[sk] = append(af.sequences[sk], fingerprints...)
}

func (af *ActionFingerprint) DetectLoop(sessionID string, threshold int) bool {
	af.mu.RLock()
	defer af.mu.RUnlock()

	sk := af.sessionKey(sessionID)
	seq, exists := af.sequences[sk]
	if !exists || len(seq) < threshold*2 {
		return false
	}

	recent := seq[len(seq)-threshold*2:]
	firstHalf := recent[:threshold]
	secondHalf := recent[threshold:]

	for i := 0; i < threshold; i++ {
		if firstHalf[i] != secondHalf[i] {
			return false
		}
	}

	return true
}

func (af *ActionFingerprint) Reset(sessionID string) {
	af.mu.Lock()
	defer af.mu.Unlock()

	sk := af.sessionKey(sessionID)
	delete(af.sequences, sk)
}
