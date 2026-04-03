package app

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	MaxWorkingMemoryBytes  = 8 * 1024
	MaxSemanticMemoryBytes = 24 * 1024
	MaxEpisodicMemoryItems = 100

	WorkingMemoryCooldown       = 2 * time.Minute
	SemanticExtractionThreshold = 5
)

type MemoryLayer string

const (
	MemoryLayerWorking  MemoryLayer = "working"
	MemoryLayerEpisodic MemoryLayer = "episodic"
	MemoryLayerSemantic MemoryLayer = "semantic"
)

type MemoryEntry struct {
	ID        string
	Layer     MemoryLayer
	Content   string
	Timestamp time.Time
	Tags      []string
	Metadata  map[string]any
	Accessed  int
}

type ReflectionResult struct {
	Success        bool
	Feedback       string
	Suggestions    []string
	ConstraintsMet bool
	LoopDetected   bool
	NeedsRetry     bool
}

type SemanticMemory struct {
	mu       sync.RWMutex
	facts    []MemoryEntry
	index    map[string][]int
	maxBytes int
}

type EpisodicMemory struct {
	mu       sync.RWMutex
	events   []MemoryEntry
	maxItems int
	index    map[string][]int
}

type WorkingMemory struct {
	mu          sync.RWMutex
	pointers    []string
	lastUpdated time.Time
	maxBytes    int
}

type MemorySystem struct {
	semantic *SemanticMemory
	episodic *EpisodicMemory
	working  *WorkingMemory

	mu              sync.RWMutex
	sessionID       string
	lastExtraction  time.Time
	extractionCount int
}

func NewMemorySystem(sessionID string) *MemorySystem {
	return &MemorySystem{
		semantic: &SemanticMemory{
			index:    make(map[string][]int),
			maxBytes: MaxSemanticMemoryBytes,
		},
		episodic: &EpisodicMemory{
			index:    make(map[string][]int),
			maxItems: MaxEpisodicMemoryItems,
		},
		working: &WorkingMemory{
			maxBytes: MaxWorkingMemoryBytes,
		},
		sessionID: sessionID,
	}
}

func (m *MemorySystem) SetWorkingMemory(content string) {
	m.working.mu.Lock()
	defer m.working.mu.Unlock()

	if len(content) > m.working.maxBytes {
		lines := strings.Split(content, "\n")
		var kept []string
		total := 0
		for i := len(lines) - 1; i >= 0; i-- {
			lineLen := len(lines[i])
			if total+lineLen+1 <= m.working.maxBytes {
				kept = append([]string{lines[i]}, kept...)
				total += lineLen + 1
			} else {
				break
			}
		}
		m.working.pointers = kept
		m.working.lastUpdated = time.Now()
		return
	}

	if content == "" {
		m.working.pointers = nil
	} else {
		m.working.pointers = strings.Split(content, "\n")
	}
	m.working.lastUpdated = time.Now()
}

func (m *MemorySystem) GetWorkingMemory() string {
	m.working.mu.RLock()
	defer m.working.mu.RUnlock()

	if len(m.working.pointers) == 0 {
		return ""
	}

	content := strings.Join(m.working.pointers, "\n")
	if m.working.lastUpdated.IsZero() || time.Since(m.working.lastUpdated) > WorkingMemoryCooldown {
		return content
	}

	return "[Working Memory may be stale]\n" + content
}

func (m *MemorySystem) AddEpisodic(entry MemoryEntry) {
	m.episodic.mu.Lock()
	defer m.episodic.mu.Unlock()

	entry.Layer = MemoryLayerEpisodic
	entry.Timestamp = time.Now()
	entry.ID = generateMemoryID()

	if len(m.episodic.events) >= m.episodic.maxItems {
		removed := m.episodic.events[0]
		m.removeFromIndex(removed)
		m.episodic.events = m.episodic.events[1:]
	}

	m.episodic.events = append(m.episodic.events, entry)
	m.addToIndex(entry)
}

func (m *MemorySystem) GetEpisodic(query string, limit int) []MemoryEntry {
	m.episodic.mu.RLock()
	defer m.episodic.mu.RUnlock()

	if query == "" {
		start := 0
		if len(m.episodic.events) > limit {
			start = len(m.episodic.events) - limit
		}
		return m.episodic.events[start:]
	}

	var results []MemoryEntry
	indices := m.episodic.index[query]
	for _, idx := range indices {
		if len(results) >= limit {
			break
		}
		results = append(results, m.episodic.events[idx])
	}
	return results
}

func (m *MemorySystem) AddSemantic(entry MemoryEntry) {
	m.semantic.mu.Lock()
	defer m.semantic.mu.Unlock()

	entry.Layer = MemoryLayerSemantic
	entry.Timestamp = time.Now()
	entry.ID = generateMemoryID()

	m.semantic.facts = append(m.semantic.facts, entry)
	m.addSemanticToIndex(entry)
	m.pruneSemanticIfNeeded()
}

func (m *MemorySystem) GetSemantic(query string, limit int) []MemoryEntry {
	m.semantic.mu.RLock()
	defer m.semantic.mu.RUnlock()

	if query == "" {
		start := 0
		if len(m.semantic.facts) > limit {
			start = len(m.semantic.facts) - limit
		}
		return m.semantic.facts[start:]
	}

	var results []MemoryEntry
	for _, word := range strings.Fields(strings.ToLower(query)) {
		indices := m.semantic.index[word]
		for _, idx := range indices {
			found := false
			for _, r := range results {
				if r.ID == m.semantic.facts[idx].ID {
					found = true
					break
				}
			}
			if !found {
				results = append(results, m.semantic.facts[idx])
				if len(results) >= limit {
					break
				}
			}
		}
		if len(results) >= limit {
			break
		}
	}
	return results
}

func (m *MemorySystem) Reflect(ctx context.Context, recentActions []sdk.ToolResult, currentGoal string) ReflectionResult {
	result := ReflectionResult{
		Success:     true,
		Feedback:    "All checks passed.",
		Suggestions: []string{},
	}

	failedCount := 0
	successCount := 0
	for _, action := range recentActions {
		if action.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	if failedCount == 0 && successCount == 0 {
		result.Success = true
		return result
	}

	if failedCount > successCount {
		result.Success = false
		result.Feedback = "More failures than successes. Consider a different approach."
		result.NeedsRetry = true
		result.Suggestions = append(result.Suggestions, "Try breaking down the task into smaller steps")
		result.Suggestions = append(result.Suggestions, "Check if prerequisites are met before retrying")
	}

	actionTypes := make(map[string]int)
	for _, action := range recentActions {
		if tool, ok := action.Data["tool"].(string); ok {
			actionTypes[tool]++
			if actionTypes[tool] > 3 {
				result.LoopDetected = true
				result.Feedback = "Detected repeated actions. You may be in a loop."
				result.Suggestions = append(result.Suggestions, "Stop repeating the same approach")
				result.Suggestions = append(result.Suggestions, "Try an alternative strategy")
				break
			}
		}
	}

	constraintsMet := true
	if strings.Contains(strings.ToLower(currentGoal), "without") {
		result.ConstraintsMet = constraintsMet
	}

	return result
}

func (m *MemorySystem) ExtractSemanticFromEpisodic(ctx context.Context) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.extractionCount++
	if m.extractionCount < SemanticExtractionThreshold {
		return 0
	}

	m.episodic.mu.RLock()
	defer m.episodic.mu.RUnlock()

	if len(m.episodic.events) < 3 {
		return 0
	}

	patterns := m.findPatterns()
	count := 0
	for _, pattern := range patterns {
		m.AddSemantic(MemoryEntry{
			Content: pattern,
			Tags:    []string{"extracted", "pattern"},
		})
		count++
	}

	m.extractionCount = 0
	m.lastExtraction = time.Now()
	return count
}

func (m *MemorySystem) findPatterns() []string {
	m.episodic.mu.RLock()
	defer m.episodic.mu.RUnlock()

	var patterns []string
	toolFrequency := make(map[string]int)

	for _, event := range m.episodic.events {
		if event.Tags != nil {
			for _, tag := range event.Tags {
				toolFrequency[tag]++
			}
		}
	}

	for tool, freq := range toolFrequency {
		if freq >= 3 {
			patterns = append(patterns, "Frequently used: "+tool)
		}
	}

	return patterns
}

func (m *MemorySystem) ShouldExtract() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.extractionCount >= SemanticExtractionThreshold
}

func (m *MemorySystem) addToIndex(entry MemoryEntry) {
	words := strings.Fields(strings.ToLower(entry.Content))
	for _, word := range words {
		if len(word) < 3 {
			continue
		}
		m.episodic.index[word] = append(m.episodic.index[word], len(m.episodic.events)-1)
	}
}

func (m *MemorySystem) removeFromIndex(entry MemoryEntry) {
	words := strings.Fields(strings.ToLower(entry.Content))
	for _, word := range words {
		if indices, ok := m.episodic.index[word]; ok {
			var newIndices []int
			for _, idx := range indices {
				if m.episodic.events[idx].ID != entry.ID {
					newIndices = append(newIndices, idx)
				}
			}
			if len(newIndices) == 0 {
				delete(m.episodic.index, word)
			} else {
				m.episodic.index[word] = newIndices
			}
		}
	}
}

func (m *MemorySystem) addSemanticToIndex(entry MemoryEntry) {
	words := strings.Fields(strings.ToLower(entry.Content))
	for _, word := range words {
		if len(word) < 3 {
			continue
		}
		m.semantic.index[word] = append(m.semantic.index[word], len(m.semantic.facts)-1)
	}
}

func (m *MemorySystem) pruneSemanticIfNeeded() {
	totalSize := 0
	for _, fact := range m.semantic.facts {
		totalSize += len(fact.Content)
	}

	if totalSize <= m.semantic.maxBytes {
		return
	}

	var toRemove []int
	for totalSize > m.semantic.maxBytes && len(m.semantic.facts) > 1 {
		totalSize -= len(m.semantic.facts[0].Content)
		toRemove = append(toRemove, 0)
		m.semantic.facts = m.semantic.facts[1:]
	}

	for _, idx := range toRemove {
		removed := m.semantic.facts[idx]
		m.removeSemanticFromIndex(removed)
	}
}

func (m *MemorySystem) removeSemanticFromIndex(entry MemoryEntry) {
	words := strings.Fields(strings.ToLower(entry.Content))
	for _, word := range words {
		if indices, ok := m.semantic.index[word]; ok {
			var newIndices []int
			for _, idx := range indices {
				if m.semantic.facts[idx].ID != entry.ID {
					newIndices = append(newIndices, idx)
				}
			}
			if len(newIndices) == 0 {
				delete(m.semantic.index, word)
			} else {
				m.semantic.index[word] = newIndices
			}
		}
	}
}

func generateMemoryID() string {
	return time.Now().Format("20060102150405") + "-" + randomString(8)
}

func randomString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
	}
	return string(b)
}

type MemoryStats struct {
	WorkingMemoryBytes  int
	EpisodicMemoryCount int
	SemanticMemoryCount int
	LastExtraction      time.Time
}

func (m *MemorySystem) Stats() MemoryStats {
	m.working.mu.RLock()
	workingBytes := 0
	for _, line := range m.working.pointers {
		workingBytes += len(line)
	}
	m.working.mu.RUnlock()

	m.episodic.mu.RLock()
	episodicCount := len(m.episodic.events)
	m.episodic.mu.RUnlock()

	m.semantic.mu.RLock()
	semanticCount := len(m.semantic.facts)
	m.semantic.mu.RUnlock()

	return MemoryStats{
		WorkingMemoryBytes:  workingBytes,
		EpisodicMemoryCount: episodicCount,
		SemanticMemoryCount: semanticCount,
		LastExtraction:      m.lastExtraction,
	}
}
