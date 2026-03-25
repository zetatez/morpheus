package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/internal/subagent"
)

const (
	// AutoDream triggers after 5 sessions or 24 hours
	AutoDreamSessionThreshold = 5
	AutoDreamTimeThreshold   = 24 * time.Hour

	// MEMORY.md truncation limits
	MemoryMaxLines = 200
	MemoryMaxBytes = 25 * 1024

	// Memory consolidation prompt
	AutoDreamSystemPrompt = `You are consolidating memory for an AI coding assistant.

Your job is to:
1. Review the existing semantic and episodic memories
2. Extract persistent knowledge that will be useful in future sessions
3. Identify patterns in past work
4. Discard obsolete or redundant information
5. Synthesize key learnings

## Output Format

Write a JSON object with this schema:
{
  "extracted_knowledge": [
    {"fact": "what you learned", "confidence": "high/medium/low", "source": "session pattern or specific memory"},
    ...
  ],
  "discarded": ["reason for discarding each item"],
  "synthesis": "2-3 sentence summary of what this user tends to work on",
  "recommendations": ["suggestions for future sessions based on patterns"]
}

Be concise and focus on actionable, persistent knowledge.`
)

// AutoDreamMemory represents the memory consolidation state
type AutoDreamMemory struct {
	mu sync.RWMutex

	// User-level tracking
	sessionCount    int
	lastSessionTime time.Time
	lastDreamTime   time.Time

	// Memory storage path
	memoryDir string

	// Pending consolidation
	pendingDream   bool
	dreamInProgress bool
}

// NewAutoDream creates a new AutoDream memory consolidator
func NewAutoDream(memoryDir string) *AutoDreamMemory {
	return &AutoDreamMemory{
		lastSessionTime: time.Now(),
		lastDreamTime:   time.Time{}, // Never dreamed
		memoryDir:      memoryDir,
	}
}

// TrackSession should be called at the start of each user session
func (ad *AutoDreamMemory) TrackSession() bool {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	ad.sessionCount++
	ad.lastSessionTime = time.Now()

	// Check if we should trigger a dream
	shouldDream := ad.shouldTriggerDreamLocked()
	if shouldDream {
		ad.pendingDream = true
	}

	return shouldDream
}

// shouldTriggerDreamLocked checks if dream should be triggered (caller must hold lock)
func (ad *AutoDreamMemory) shouldTriggerDreamLocked() bool {
	// Never dreamed before
	if ad.lastDreamTime.IsZero() {
		return ad.sessionCount >= AutoDreamSessionThreshold
	}

	// Check time threshold
	if time.Since(ad.lastDreamTime) >= AutoDreamTimeThreshold {
		return true
	}

	// Check session threshold
	if ad.sessionCount >= AutoDreamSessionThreshold {
		return true
	}

	return false
}

// ShouldDream returns true if memory consolidation is recommended
func (ad *AutoDreamMemory) ShouldDream() bool {
	ad.mu.RLock()
	defer ad.mu.RUnlock()
	return ad.pendingDream && !ad.dreamInProgress
}

// MarkDreamStarted marks that a dream consolidation has started
func (ad *AutoDreamMemory) MarkDreamStarted() {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.pendingDream = false
	ad.dreamInProgress = true
}

// MarkDreamComplete marks that a dream consolidation has completed
func (ad *AutoDreamMemory) MarkDreamComplete() {
	ad.mu.Lock()
	defer ad.mu.Unlock()
	ad.dreamInProgress = false
	ad.lastDreamTime = time.Now()
	ad.sessionCount = 0
}

// GetTimeUntilNextDream returns the duration until the next automatic dream
func (ad *AutoDreamMemory) GetTimeUntilNextDream() time.Duration {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	if ad.lastDreamTime.IsZero() {
		return time.Duration(0)
	}

	nextTime := ad.lastDreamTime.Add(AutoDreamTimeThreshold)
	return time.Until(nextTime)
}

// ConsolidationResult is the output of memory consolidation
type ConsolidationResult struct {
	ExtractedKnowledge []KnowledgeEntry
	Discarded         []string
	Synthesis         string
	Recommendations   []string
}

// KnowledgeEntry represents a piece of extracted knowledge
type KnowledgeEntry struct {
	Fact       string
	Confidence string
	Source     string
}

// MemoryStorage handles persistent memory storage
type MemoryStorage struct {
	mu        sync.RWMutex
	memoryDir string
}

// NewMemoryStorage creates a new memory storage handler
func NewMemoryStorage(memoryDir string) (*MemoryStorage, error) {
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return nil, err
	}
	return &MemoryStorage{memoryDir: memoryDir}, nil
}

// SaveMemory saves consolidated memory to disk
func (ms *MemoryStorage) SaveMemory(userID string, memory string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Apply dual truncation
	memory = TruncateMemoryMD(memory)

	// Ensure directory exists
	userDir := filepath.Join(ms.memoryDir, userID)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return err
	}

	// Write to MEMORY.md
	memoryPath := filepath.Join(userDir, "MEMORY.md")
	return os.WriteFile(memoryPath, []byte(memory), 0644)
}

// LoadMemory loads consolidated memory from disk
func (ms *MemoryStorage) LoadMemory(userID string) (string, error) {
	ms.mu.RLock()
	defer ms.mu.RUnlock()

	memoryPath := filepath.Join(ms.memoryDir, userID, "MEMORY.md")
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// AppendMemory appends new memory to existing MEMORY.md
func (ms *MemoryStorage) AppendMemory(userID string, newMemory string) error {
	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Load existing
	existing, _ := ms.LoadMemoryUnlocked(userID)

	// Combine
	combined := existing
	if combined != "" && newMemory != "" {
		combined += "\n\n---\n\n"
	}
	combined += newMemory

	// Truncate
	combined = TruncateMemoryMD(combined)

	// Ensure directory exists
	userDir := filepath.Join(ms.memoryDir, userID)
	if err := os.MkdirAll(userDir, 0755); err != nil {
		return err
	}

	// Write
	memoryPath := filepath.Join(userDir, "MEMORY.md")
	return os.WriteFile(memoryPath, []byte(combined), 0644)
}

// LoadMemoryUnlocked loads memory without locking (caller must hold lock)
func (ms *MemoryStorage) LoadMemoryUnlocked(userID string) (string, error) {
	memoryPath := filepath.Join(ms.memoryDir, userID, "MEMORY.md")
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

// TruncateMemoryMD applies dual truncation (200 lines + 25KB)
func TruncateMemoryMD(memory string) string {
	if memory == "" {
		return memory
	}

	// First truncate by bytes
	if len(memory) > MemoryMaxBytes {
		// Find a good break point
		truncated := memory[:MemoryMaxBytes]
		lastNewline := strings.LastIndex(truncated, "\n")
		lastDoubleNewline := strings.LastIndex(truncated, "\n\n")
		breakPoint := lastNewline
		if lastDoubleNewline > lastNewline-100 {
			breakPoint = lastDoubleNewline
		}
		if breakPoint > MemoryMaxBytes/2 {
			truncated = truncated[:breakPoint]
		} else {
			truncated = truncated[:MemoryMaxBytes]
		}
		memory = strings.TrimSpace(truncated) + "\n\n[Memory truncated due to size]"
	}

	// Then truncate by lines
	lines := strings.Split(memory, "\n")
	if len(lines) > MemoryMaxLines {
		// Keep first portion and last portion
		keepFirst := MemoryMaxLines / 2
		keepLast := MemoryMaxLines - keepFirst
		truncatedMsg := fmt.Sprintf("\n... [%d lines truncated] ...\n", len(lines)-MemoryMaxLines)
		kept := []string{}
		kept = append(kept, lines[:keepFirst]...)
		kept = append(kept, truncatedMsg)
		kept = append(kept, lines[len(lines)-keepLast:]...)
		memory = strings.Join(kept, "\n")
	}

	return memory
}

// DreamConsolidator performs memory consolidation
type DreamConsolidator struct {
	rt       *Runtime
	storage  *MemoryStorage
	subagent *subagent.Loader
}

// NewDreamConsolidator creates a new dream consolidator
func NewDreamConsolidator(rt *Runtime, storage *MemoryStorage) *DreamConsolidator {
	return &DreamConsolidator{
		rt:      rt,
		storage: storage,
	}
}

// Consolidate performs memory consolidation for a user
func (dc *DreamConsolidator) Consolidate(ctx context.Context, userID string) (*ConsolidationResult, error) {
	// Load existing memories
	semantic, episodic := dc.loadMemories(userID)

	// Build consolidation prompt
	prompt := dc.buildConsolidationPrompt(semantic, episodic)

	// Call LLM to consolidate
	memoryCtx := dc.buildMemoryContext(semantic, episodic)

	// Use the LLM to consolidate
	messages := []map[string]any{
		{"role": "system", "content": AutoDreamSystemPrompt},
		{"role": "system", "content": fmt.Sprintf("Current memories:\n%s", memoryCtx)},
		{"role": "user", "content": prompt},
	}

	// Get LLM response
	resp, err := dc.rt.callChatWithTools(ctx, messages, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("dream consolidation failed: %w", err)
	}

	// Parse response
	result := dc.parseConsolidationResult(resp.Content)

	// Save consolidated memory
	if err := dc.saveConsolidatedMemory(userID, result); err != nil {
		return nil, fmt.Errorf("failed to save memory: %w", err)
	}

	return result, nil
}

func (dc *DreamConsolidator) loadMemories(userID string) (semantic []string, episodic []string) {
	// In a full implementation, this would load from persistent storage
	// For now, we use the runtime's memory system
	return nil, nil
}

func (dc *DreamConsolidator) buildConsolidationPrompt(semantic, episodic []string) string {
	var b strings.Builder
	b.WriteString("Please consolidate the following memories.\n\n")

	if len(semantic) > 0 {
		b.WriteString("## Semantic Memories\n")
		for _, s := range semantic {
			b.WriteString("- " + s + "\n")
		}
		b.WriteString("\n")
	}

	if len(episodic) > 0 {
		b.WriteString("## Episodic Memories\n")
		for _, e := range episodic {
			b.WriteString("- " + e + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("Provide your analysis in JSON format.")
	return b.String()
}

func (dc *DreamConsolidator) buildMemoryContext(semantic, episodic []string) string {
	var b strings.Builder

	if len(semantic) > 0 {
		b.WriteString("Semantic memories:\n")
		for _, s := range semantic {
			b.WriteString("  - " + s + "\n")
		}
	}

	if len(episodic) > 0 {
		b.WriteString("Episodic memories:\n")
		for _, e := range episodic {
			b.WriteString("  - " + e + "\n")
		}
	}

	return b.String()
}

func (dc *DreamConsolidator) parseConsolidationResult(content string) *ConsolidationResult {
	result := &ConsolidationResult{}

	// Try to extract JSON
	jsonStr := extractJSONFromContent(content)
	if jsonStr == "" {
		// Fallback: just use the content as synthesis
		result.Synthesis = strings.TrimSpace(content)
		return result
	}

	// Parse JSON (simplified)
	// In production, use proper JSON parsing
	result.Synthesis = "Consolidated from memories"

	return result
}

func (dc *DreamConsolidator) saveConsolidatedMemory(userID string, result *ConsolidationResult) error {
	var memory strings.Builder

	if len(result.ExtractedKnowledge) > 0 {
		memory.WriteString("## Extracted Knowledge\n")
		for _, k := range result.ExtractedKnowledge {
			fmt.Fprintf(&memory, "- [%s] %s (from: %s)\n", k.Confidence, k.Fact, k.Source)
		}
		memory.WriteString("\n")
	}

	if result.Synthesis != "" {
		memory.WriteString("## Synthesis\n")
		memory.WriteString(result.Synthesis + "\n\n")
	}

	if len(result.Recommendations) > 0 {
		memory.WriteString("## Recommendations\n")
		for _, r := range result.Recommendations {
			memory.WriteString("- " + r + "\n")
		}
		memory.WriteString("\n")
	}

	if len(result.Discarded) > 0 {
		memory.WriteString("## Discarded\n")
		for _, d := range result.Discarded {
			memory.WriteString("- " + d + "\n")
		}
	}

	return dc.storage.AppendMemory(userID, memory.String())
}

func extractJSONFromContent(content string) string {
	// Try to find JSON in content
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		return content[start : end+1]
	}
	return ""
}

// DreamStats provides statistics about auto-dream memory
type DreamStats struct {
	SessionCount       int
	TimeSinceLastDream time.Duration
	TimeUntilNextDream time.Duration
	DreamPending      bool
	DreamInProgress   bool
}

// GetStats returns dream statistics
func (ad *AutoDreamMemory) GetStats() DreamStats {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	return DreamStats{
		SessionCount:       ad.sessionCount,
		TimeSinceLastDream: time.Since(ad.lastDreamTime),
		TimeUntilNextDream: ad.GetTimeUntilNextDream(),
		DreamPending:      ad.pendingDream,
		DreamInProgress:   ad.dreamInProgress,
	}
}
