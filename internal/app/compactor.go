package app

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type CompressionMetrics struct {
	SessionID        string
	Layer            CompressionLayer
	OriginalTokens   int
	CompressedTokens int
	CompressionRatio float64
	Duration         time.Duration
	Timestamp        time.Time
	ToolCount        int
	Success          bool
}

type CompressionLayer int

const (
	LayerMicro CompressionLayer = iota
	LayerFolding
	LayerAuto
	LayerMemory
)

func (l CompressionLayer) String() string {
	switch l {
	case LayerMicro:
		return "micro"
	case LayerFolding:
		return "folding"
	case LayerAuto:
		return "auto"
	case LayerMemory:
		return "memory"
	default:
		return "unknown"
	}
}

type CompressionTracker struct {
	mu         sync.RWMutex
	history    map[string][]CompressionMetrics
	maxHistory int
}

func NewCompressionTracker(maxHistory int) *CompressionTracker {
	if maxHistory <= 0 {
		maxHistory = 100
	}
	return &CompressionTracker{
		history:    make(map[string][]CompressionMetrics),
		maxHistory: maxHistory,
	}
}

func (t *CompressionTracker) Record(sessionID string, metrics CompressionMetrics) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if metrics.CompressionRatio == 0 && metrics.OriginalTokens > 0 {
		metrics.CompressionRatio = float64(metrics.CompressedTokens) / float64(metrics.OriginalTokens)
	}
	if metrics.Timestamp.IsZero() {
		metrics.Timestamp = time.Now()
	}

	history := t.history[sessionID]
	history = append(history, metrics)

	if len(history) > t.maxHistory {
		history = history[len(history)-t.maxHistory:]
	}
	t.history[sessionID] = history
}

func (t *CompressionTracker) GetHistory(sessionID string) []CompressionMetrics {
	t.mu.RLock()
	defer t.mu.RUnlock()
	history := t.history[sessionID]
	result := make([]CompressionMetrics, len(history))
	copy(result, history)
	return result
}

func (t *CompressionTracker) GetStats(sessionID string) CompressionStats {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var stats CompressionStats
	history := t.history[sessionID]
	if len(history) == 0 {
		return stats
	}

	var totalOriginal, totalCompressed int
	var totalDuration time.Duration
	successCount := 0

	for _, m := range history {
		totalOriginal += m.OriginalTokens
		totalCompressed += m.CompressedTokens
		totalDuration += m.Duration
		if m.Success {
			successCount++
		}
	}

	stats.TotalCompactions = len(history)
	stats.SuccessCount = successCount
	stats.FailureCount = len(history) - successCount
	stats.AverageRatio = float64(totalCompressed) / float64(totalOriginal)
	stats.TotalTokensSaved = totalOriginal - totalCompressed
	stats.TotalDuration = totalDuration
	stats.AverageDuration = totalDuration / time.Duration(len(history))

	if len(history) > 0 {
		stats.LastCompression = history[len(history)-1]
	}

	var minRatio float64 = 1.0
	var maxRatio float64
	for _, m := range history {
		if m.CompressionRatio < minRatio {
			minRatio = m.CompressionRatio
		}
		if m.CompressionRatio > maxRatio {
			maxRatio = m.CompressionRatio
		}
	}
	stats.MinRatio = minRatio
	stats.MaxRatio = maxRatio

	return stats
}

type CompressionStats struct {
	TotalCompactions int
	SuccessCount     int
	FailureCount     int
	AverageRatio     float64
	MinRatio         float64
	MaxRatio         float64
	TotalTokensSaved int
	TotalDuration    time.Duration
	AverageDuration  time.Duration
	LastCompression  CompressionMetrics
}

var globalTracker = NewCompressionTracker(100)

func GetCompressionTracker() *CompressionTracker {
	return globalTracker
}

var protectedTools = map[string]bool{
	"skill": true,
}

type CompactionState struct {
	mu            sync.RWMutex
	sessionID     string
	lastCompacted time.Time
	tokenEstimate int64
}

type Compactor struct {
	states sync.Map
}

func NewCompactor() *Compactor {
	return &Compactor{}
}

func (c *Compactor) sessionState(sessionID string) *CompactionState {
	if existing, ok := c.states.Load(sessionID); ok {
		return existing.(*CompactionState)
	}

	state := &CompactionState{
		sessionID: sessionID,
	}
	actual, _ := c.states.LoadOrStore(sessionID, state)
	return actual.(*CompactionState)
}

func (c *Compactor) RecordTokens(sessionID string, tokenCount int64) {
	state := c.sessionState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.tokenEstimate = tokenCount
}

func (c *Compactor) IsOverflow(sessionID string, modelMaxTokens int64) bool {
	state := c.sessionState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()

	return state.tokenEstimate > modelMaxTokens
}

func (c *Compactor) NeedsCompaction(sessionID string) bool {
	state := c.sessionState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()

	if state.lastCompacted.IsZero() {
		return state.tokenEstimate > PruneProtectTokens
	}

	if time.Since(state.lastCompacted) < 30*time.Second {
		return false
	}

	return state.tokenEstimate > PruneProtectTokens
}

func (c *Compactor) MarkCompacted(sessionID string) {
	state := c.sessionState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.lastCompacted = time.Now()
}

func (c *Compactor) Reset(sessionID string) {
	c.states.Delete(sessionID)
}

type ToolCallPruner struct {
	protectedTools map[string]bool
}

func NewToolCallPruner() *ToolCallPruner {
	return &ToolCallPruner{
		protectedTools: protectedTools,
	}
}

func (tp *ToolCallPruner) IsProtected(toolName string) bool {
	return tp.protectedTools[toolName]
}

type PruneCandidate struct {
	ToolName     string
	OutputLen    int
	Timestamp    time.Time
	IsProtected  bool
	WasCompacted bool
}

func (tp *ToolCallPruner) FindPruneCandidates(toolOutputs []ToolOutputInfo, protectRecent int) []int {
	if len(toolOutputs) <= protectRecent*2 {
		return nil
	}

	var candidates []int
	var totalTokens int
	protectedCount := 0

	for i := 0; i < len(toolOutputs)-protectRecent; i++ {
		output := toolOutputs[i]
		if tp.IsProtected(output.ToolName) {
			protectedCount++
			continue
		}
		if output.WasCompacted {
			continue
		}

		totalTokens += output.OutputLen
		if totalTokens > PruneProtectTokens {
			candidates = append(candidates, i)
		}
	}

	if totalTokens < PruneMinimumTokens {
		return nil
	}

	return candidates
}

type ToolOutputInfo struct {
	ToolName     string
	OutputLen    int
	Timestamp    time.Time
	IsProtected  bool
	WasCompacted bool
}

// EstimateTokens is a quick approximation (4 chars per token)
func EstimateTokens(text string) int {
	return len(text) / 4
}

// ============================================================================
// Four-Layer Compression Pipeline (aligned with Claude Code)
// ============================================================================
//
// Layer 1: Micro-compaction    - Tool output in-place reduction, keep task-relevant parts
// Layer 2: Context folding     - Fold related messages before compression triggers
// Layer 3: Auto-compaction    - LLM summary when token count approaches window - 20K
// Layer 4: Memory persistence - Preserve critical constraints across compression boundaries

const (
	// Layer 3 thresholds
	AutoCompactReserveTokens = 20000 // Reserve 20K for compact summary output
	AutoCompactTriggerRatio  = 0.85  // Trigger at 85% of available window

	// Layer 1: Micro-compaction
	MicroCompactMaxToolOutput = 8000 // Max chars per tool output before micro-compaction
	MicroCompactPreviewLines  = 30   // Preview lines when compacting
	MicroCompactMaxPreview    = 1500 // Max preview chars

	// Layer 2: Context folding
	FoldWindowMessages      = 3    // Group messages within this window
	FoldSimilarityThreshold = 0.75 // Similarity score to consider folding

	// Layer 4: Constraint preservation
	ConstraintPreserveTokens = 3000 // Tokens to preserve for critical constraints
	ConstraintMaxAge         = 10   // Max turns to preserve constraint across compression
)

// CompactionPipeline implements the 4-layer compression system
type CompactionPipeline struct {
	compactor *Compactor
	micro     *MicroCompactor
	fold      *ContextFolder
	auto      *AutoCompactor
	memory    *MemoryPreserver
}

// NewCompactionPipeline creates a new 4-layer compaction pipeline
func NewCompactionPipeline() *CompactionPipeline {
	return &CompactionPipeline{
		compactor: NewCompactor(),
		micro:     NewMicroCompactor(),
		fold:      NewContextFolder(),
		auto:      NewAutoCompactor(),
		memory:    NewMemoryPreserver(),
	}
}

// Pipeline stages
type PipelineStage int

const (
	StageMicroCompaction PipelineStage = iota
	StageContextFolding
	StageAutoCompaction
	StageMemoryPersistence
)

// CompactRequest is the input for compaction pipeline
type CompactRequest struct {
	SessionID           string
	Messages            []sdk.Message
	MemorySystem        *MemorySystem
	ModelMaxTokens      int64
	TurnCount           int
	CriticalConstraints []Constraint
}

// Constraint represents a critical constraint that should survive compression
type Constraint struct {
	Content    string
	TurnNumber int
	Source     string // "user", "system", "agent"
	Priority   int    // Higher = more important
}

// CompactResult is the output of compaction pipeline
type CompactResult struct {
	Messages               []sdk.Message
	Summary                string
	TokensRemoved          int
	ConstraintsUpdated     []Constraint
	StageReached           PipelineStage
	NeedsFurtherCompaction bool
}

// MicroCompactor implements Layer 1: In-place tool output reduction
type MicroCompactor struct{}

func NewMicroCompactor() *MicroCompactor {
	return &MicroCompactor{}
}

// CompactToolOutput reduces tool output while preserving task-relevant content
func (mc *MicroCompactor) CompactToolOutput(content string, taskContext string) string {
	if content == "" {
		return content
	}

	trimmed := strings.TrimSpace(content)
	if len(trimmed) <= MicroCompactMaxToolOutput {
		return trimmed
	}

	// Extract task-relevant lines based on context
	lines := strings.Split(trimmed, "\n")
	relevantLines := mc.extractRelevantLines(lines, taskContext)

	if len(relevantLines) > MicroCompactPreviewLines {
		// Keep first portion and last portion (most likely to have relevant info)
		keep := MicroCompactPreviewLines / 2
		kept := append(relevantLines[:keep], relevantLines[len(relevantLines)-keep:]...)
		relevantLines = kept
	}

	preview := strings.Join(relevantLines, "\n")
	if len(preview) > MicroCompactMaxPreview {
		preview = preview[:MicroCompactMaxPreview] + "..."
	}

	return preview + fmt.Sprintf("\n\n[Tool output compacted: %d lines, original %d chars]",
		len(relevantLines), len(trimmed))
}

func (mc *MicroCompactor) extractRelevantLines(lines []string, taskContext string) []string {
	if taskContext == "" {
		return mc.defaultExtract(lines)
	}

	taskKeywords := extractKeywords(taskContext)
	if len(taskKeywords) == 0 {
		return mc.defaultExtract(lines)
	}

	var relevant []string
	var skipped int

	for _, line := range lines {
		lower := strings.ToLower(line)
		score := 0
		for _, kw := range taskKeywords {
			if strings.Contains(lower, kw) {
				score++
			}
		}

		// Keep lines with keyword matches or structure
		if score > 0 || mc.isStructuralLine(line) {
			if skipped > 0 && skipped > 3 {
				relevant = append(relevant, fmt.Sprintf("... [%d lines skipped]", skipped))
			}
			relevant = append(relevant, line)
			skipped = 0
		} else if len(relevant) > 0 && !mc.isBlankLine(line) {
			skipped++
			if skipped <= 3 {
				relevant = append(relevant, line)
			}
		}
	}

	return relevant
}

func (mc *MicroCompactor) defaultExtract(lines []string) []string {
	// Default: keep first 15 lines and last 10 lines
	keepFirst := 15
	keepLast := 10

	if len(lines) <= keepFirst+keepLast {
		return lines
	}

	result := append(lines[:keepFirst], fmt.Sprintf("... [%d lines skipped]", len(lines)-keepFirst-keepLast))
	return append(result, lines[len(lines)-keepLast:]...)
}

func (mc *MicroCompactor) isStructuralLine(line string) bool {
	structuralPrefixes := []string{
		"## ", "### ", "# ", "// ", "/* ", "-- ", "package ",
		"import ", "func ", "class ", "def ", "struct ",
		"ERROR", "FAIL", "PASS", "OK", "WARN", "INFO",
		"---", "===", "***",
	}
	lower := strings.ToLower(strings.TrimSpace(line))
	for _, prefix := range structuralPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func (mc *MicroCompactor) isBlankLine(line string) bool {
	return strings.TrimSpace(line) == ""
}

// ApplyToMessages applies micro-compaction to all tool outputs in messages
func (mc *MicroCompactor) ApplyToMessages(messages []sdk.Message, taskContext string) []sdk.Message {
	compacted := make([]sdk.Message, len(messages))
	for i, msg := range messages {
		compacted[i] = msg
		if len(msg.Parts) > 0 {
			for j, part := range msg.Parts {
				if part.Type == "tool" && part.Output != nil {
					// Compact the output
					if content, ok := part.Output["content"].(string); ok {
						compactedOutput := mc.CompactToolOutput(content, taskContext)
						if compactedOutput != content {
							compacted[i].Parts[j].Output["content"] = compactedOutput
							compacted[i].Parts[j].Output["compacted"] = true
						}
					}
				}
			}
		}
		// Also compact main content if it's a tool result
		if msg.Content != "" && isToolLikeContent(msg.Content) && len(msg.Content) > MicroCompactMaxToolOutput {
			compacted[i].Content = mc.CompactToolOutput(msg.Content, taskContext)
		}
	}
	return compacted
}

// extractKeywords extracts important keywords from task context
func extractKeywords(context string) []string {
	// Remove common stop words
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "must": true, "shall": true, "can": true,
		"to": true, "of": true, "in": true, "for": true, "on": true,
		"with": true, "at": true, "by": true, "from": true, "as": true,
		"and": true, "or": true, "but": true, "if": true, "then": true,
		"else": true, "when": true, "up": true, "down": true, "out": true,
		"all": true, "each": true, "every": true, "both": true, "few": true,
		"more": true, "most": true, "other": true, "some": true, "such": true,
		"no": true, "nor": true, "not": true, "only": true, "own": true,
		"same": true, "so": true, "than": true, "too": true, "very": true,
		"s": true, "t": true, "just": true, "don": true, "now": true,
	}

	words := strings.Fields(strings.ToLower(context))
	var keywords []string
	for _, word := range words {
		word = strings.Trim(word, ".,!?;:\"'()[]{}\\//")
		if len(word) < 3 || stopWords[word] || len(word) > 20 {
			continue
		}
		keywords = append(keywords, word)
	}

	// Dedupe and limit
	seen := map[string]bool{}
	var unique []string
	for _, kw := range keywords {
		if !seen[kw] {
			seen[kw] = true
			unique = append(unique, kw)
		}
	}
	if len(unique) > 20 {
		unique = unique[:20]
	}
	return unique
}

// ContextFolder implements Layer 2: Context folding
type ContextFolder struct{}

func NewContextFolder() *ContextFolder {
	return &ContextFolder{}
}

// FoldResult represents a folded context group
type FoldResult struct {
	OriginalCount int
	FoldedLines   []string
	Type          string // "sequential", "similar", "loop"
}

// FoldMessages groups related messages to reduce context while preserving meaning
func (cf *ContextFolder) FoldMessages(messages []sdk.Message) ([]sdk.Message, []FoldResult) {
	if len(messages) < FoldWindowMessages*2 {
		return messages, nil
	}

	var folded []sdk.Message
	var results []FoldResult

	i := 0
	for i < len(messages) {
		group, foldType := cf.findFoldableGroup(messages, i)
		if group == nil {
			folded = append(folded, messages[i])
			i++
			continue
		}

		foldedLines := cf.summarizeGroup(group, foldType)
		foldedMsg := sdk.Message{
			ID:        fmt.Sprintf("fold-%d", time.Now().UnixNano()),
			Role:      "system",
			Content:   strings.Join(foldedLines, "\n"),
			Timestamp: group[0].Timestamp,
		}
		folded = append(folded, foldedMsg)
		results = append(results, FoldResult{
			OriginalCount: len(group),
			FoldedLines:   foldedLines,
			Type:          foldType,
		})
		i += len(group)
	}

	return folded, results
}

func (cf *ContextFolder) findFoldableGroup(messages []sdk.Message, start int) ([]sdk.Message, string) {
	windowEnd := start + FoldWindowMessages
	if windowEnd > len(messages) {
		windowEnd = len(messages)
	}
	window := messages[start:windowEnd]

	// Check for sequential tool calls (same tool pattern)
	if cf.isSequentialToolPattern(window) {
		return window, "sequential"
	}

	// Check for similar messages
	if start+1 < len(messages) && cf.isSimilarPattern(window[:2]) {
		return window[:2], "similar"
	}

	return nil, ""
}

func (cf *ContextFolder) isSequentialToolPattern(window []sdk.Message) bool {
	if len(window) < 3 {
		return false
	}

	var toolCount int
	var assistantCount int
	for _, msg := range window {
		if msg.Role == "assistant" {
			assistantCount++
			if len(msg.Parts) > 0 {
				for _, part := range msg.Parts {
					if part.Type == "tool" {
						toolCount++
					}
				}
			}
		}
	}

	return toolCount >= 3 && assistantCount == toolCount
}

func (cf *ContextFolder) isSimilarPattern(window []sdk.Message) bool {
	if len(window) != 2 {
		return false
	}

	msg1 := window[0]
	msg2 := window[1]

	// Same role and similar length
	if msg1.Role != msg2.Role {
		return false
	}

	len1 := len(msg1.Content)
	len2 := len(msg2.Content)
	if len1 == 0 || len2 == 0 {
		return false
	}

	ratio := float64(min(len1, len2)) / float64(max(len1, len2))
	return ratio >= FoldSimilarityThreshold
}

func (cf *ContextFolder) summarizeGroup(group []sdk.Message, foldType string) []string {
	switch foldType {
	case "sequential":
		return cf.summarizeSequential(group)
	case "similar":
		return cf.summarizeSimilar(group)
	default:
		return []string{"[folded group]"}
	}
}

func (cf *ContextFolder) summarizeSequential(group []sdk.Message) []string {
	var lines []string
	toolCalls := 0
	errors := 0

	for _, msg := range group {
		if msg.Role == "assistant" && len(msg.Parts) > 0 {
			for _, part := range msg.Parts {
				if part.Type == "tool" {
					toolCalls++
					if part.Error != "" {
						errors++
					}
				}
			}
		}
	}

	lines = append(lines, fmt.Sprintf("[%d sequential tool calls]", toolCalls))
	if errors > 0 {
		lines = append(lines, fmt.Sprintf("  - %d with errors", errors))
	}

	// Keep first and last results
	if len(group) >= 2 {
		if last := cf.extractKeyResult(group[len(group)-1]); last != "" {
			lines = append(lines, "  Last result: "+last)
		}
	}

	return lines
}

func (cf *ContextFolder) summarizeSimilar(group []sdk.Message) []string {
	var lines []string
	first := group[0]
	lines = append(lines, fmt.Sprintf("[2 similar %s messages, %d chars total]",
		first.Role, len(first.Content)*2))

	// Keep first 50 chars of first message
	preview := first.Content
	if len(preview) > 50 {
		preview = preview[:50] + "..."
	}
	lines = append(lines, "  Preview: "+preview)

	return lines
}

func (cf *ContextFolder) extractKeyResult(msg sdk.Message) string {
	if len(msg.Parts) == 0 {
		return ""
	}
	for _, part := range msg.Parts {
		if part.Type == "tool" && part.Output != nil {
			if data, ok := part.Output["content"].(string); ok {
				if len(data) > 100 {
					return data[:100] + "..."
				}
				return data
			}
		}
	}
	return ""
}

// AutoCompactor implements Layer 3: LLM-based summarization
type AutoCompactor struct{}

func NewAutoCompactor() *AutoCompactor {
	return &AutoCompactor{}
}

// ShouldTrigger checks if auto-compaction should trigger
func (ac *AutoCompactor) ShouldTrigger(currentTokens, modelMaxTokens int64) bool {
	available := modelMaxTokens - AutoCompactReserveTokens
	threshold := int64(float64(available) * AutoCompactTriggerRatio)
	return currentTokens >= threshold
}

// BuildCompactPrompt creates the prompt for LLM summarization
func (ac *AutoCompactor) BuildCompactPrompt(messages []sdk.Message, constraints []Constraint, memoryContext string) string {
	var b strings.Builder

	b.WriteString("You are performing context compression for an AI coding assistant.\n\n")
	b.WriteString("## Task\n")
	b.WriteString("Compress the conversation history into a concise summary that preserves:\n")
	b.WriteString("1. Key decisions and their rationale\n")
	b.WriteString("2. Important constraints or requirements\n")
	b.WriteString("3. Current work in progress\n")
	b.WriteString("4. Any unresolved issues or next steps\n\n")

	if len(constraints) > 0 {
		b.WriteString("## Critical Constraints to Preserve\n")
		for _, c := range constraints {
			b.WriteString(fmt.Sprintf("- [%s] %s (turn %d)\n", c.Source, c.Content, c.TurnNumber))
		}
		b.WriteString("\n")
	}

	if memoryContext != "" {
		b.WriteString("## Memory Context\n")
		b.WriteString(memoryContext)
		b.WriteString("\n\n")
	}

	b.WriteString("## Conversation to Compress\n")
	for i, msg := range messages {
		role := msg.Role
		if role == "system" {
			continue // Skip system messages
		}
		content := msg.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		b.WriteString(fmt.Sprintf("[%d] %s: %s\n", i, role, content))

		if len(msg.Parts) > 0 {
			for j, part := range msg.Parts {
				if part.Type == "tool" {
					output := part.Output
					if output != nil {
						if data, ok := output["content"].(string); ok {
							if len(data) > 200 {
								data = data[:200] + "..."
							}
							b.WriteString(fmt.Sprintf("      tool[%d] output: %s\n", j, data))
						}
					}
				}
			}
		}
	}
	b.WriteString("\n## Output Format\n")
	b.WriteString("Provide a markdown summary with sections:\n")
	b.WriteString("- ## Summary: <2-3 sentence overview>\n")
	b.WriteString("- ## Decisions: <key decisions made>\n")
	b.WriteString("- ## Current State: <what's being worked on>\n")
	b.WriteString("- ## Open Items: <remaining tasks>\n")

	return b.String()
}

// EstimateCompressedSize estimates the size of the compressed output
func (ac *AutoCompactor) EstimateCompressedSize(messages []sdk.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content)
		for _, part := range msg.Parts {
			total += len(part.Text)
			if part.Output != nil {
				if data, ok := part.Output["content"].(string); ok {
					total += len(data)
				}
			}
		}
	}
	// Rough estimate: 10x compression ratio
	return total / 10
}

// MemoryPreserver implements Layer 4: Critical constraint preservation
type MemoryPreserver struct{}

func NewMemoryPreserver() *MemoryPreserver {
	return &MemoryPreserver{}
}

// PreserveConstraints extracts and preserves critical constraints across compression
func (mp *MemoryPreserver) PreserveConstraints(messages []sdk.Message, currentTurn int) []Constraint {
	var constraints []Constraint

	for i, msg := range messages {
		if mp.isConstraintMessage(msg) {
			constraint := Constraint{
				Content:    mp.extractConstraintContent(msg),
				TurnNumber: currentTurn - (len(messages) - i),
				Source:     msg.Role,
				Priority:   mp.evaluateConstraintPriority(msg),
			}
			if constraint.Priority > 0 {
				constraints = append(constraints, constraint)
			}
		}
	}

	// Sort by priority (highest first)
	sort.Slice(constraints, func(i, j int) bool {
		return constraints[i].Priority > constraints[j].Priority
	})

	// Limit to most important
	if len(constraints) > 10 {
		constraints = constraints[:10]
	}

	return constraints
}

func (mp *MemoryPreserver) isConstraintMessage(msg sdk.Message) bool {
	constraintPatterns := []string{
		"must", "require", "should not", "do not", "don't",
		"important", "critical", "essential", "must-have",
		"without", "except", "only", "never",
		"always", "never", "only", "exactly",
		"limitation", "restriction", "constraint",
	}

	content := strings.ToLower(msg.Content)
	for _, pattern := range constraintPatterns {
		if strings.Contains(content, pattern) {
			return true
		}
	}

	return false
}

func (mp *MemoryPreserver) extractConstraintContent(msg sdk.Message) string {
	content := msg.Content

	// For user messages, extract the constraint portion
	if msg.Role == "user" && len(content) > 200 {
		// Try to find the constraint portion
		sentences := strings.Split(content, ".")
		if len(sentences) > 2 {
			// Keep sentences with constraint keywords
			var constraintSentences []string
			for _, s := range sentences {
				if mp.isConstraintMessage(sdk.Message{Content: s}) {
					constraintSentences = append(constraintSentences, strings.TrimSpace(s))
				}
			}
			if len(constraintSentences) > 0 {
				return strings.Join(constraintSentences, ". ") + "."
			}
		}
	}

	// Truncate long constraints
	if len(content) > ConstraintPreserveTokens*4 {
		return content[:ConstraintPreserveTokens*4] + "..."
	}
	return content
}

func (mp *MemoryPreserver) evaluateConstraintPriority(msg sdk.Message) int {
	content := strings.ToLower(msg.Content)
	priority := 0

	// High priority patterns
	highPriority := []string{"must not", "never", "critical", "security", "secret", "password", "key"}
	for _, pattern := range highPriority {
		if strings.Contains(content, pattern) {
			priority += 5
		}
	}

	// Medium priority patterns
	medPriority := []string{"should not", "do not", "don't", "important", "must", "require"}
	for _, pattern := range medPriority {
		if strings.Contains(content, pattern) {
			priority += 2
		}
	}

	// Length factor (longer constraints are often more specific)
	priority += len(msg.Content) / 500

	return priority
}

// FilterValidConstraints removes constraints that are too old
func (mp *MemoryPreserver) FilterValidConstraints(constraints []Constraint, currentTurn int) []Constraint {
	var valid []Constraint
	for _, c := range constraints {
		if currentTurn-c.TurnNumber <= ConstraintMaxAge {
			valid = append(valid, c)
		}
	}
	return valid
}

// CompactToolOutputToFit reduces tool output to fit within token budget
func CompactToolOutputToFit(output string, maxTokens int, taskContext string) string {
	compactor := NewMicroCompactor()
	return compactor.CompactToolOutput(output, taskContext)
}

// Helper functions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// AdvancedTokenEstimator provides more accurate token estimation
// based on character patterns (aligned with Claude Code's approach)
var jsonRE = regexp.MustCompile(`\{[^{}]*"[^"]*":\s*[^{}]*\}`)

// SmartTokenBudget provides model-aware token budgeting
type SmartTokenBudget struct {
	ModelMaxTokens    int64
	ReserveTokens     int64   // Reserve for response (default 20K)
	CompressThreshold float64 // Trigger compression at this ratio (default 0.85)

	// Dynamic budgets per content type
	SystemPromptBudget   int64
	MemoryBudget         int64
	WorkingContextBudget int64
}

// TokenAllocation represents the allocated budget for each content type
type TokenAllocation struct {
	SystemPrompt   int64
	Memory         int64
	History        int64
	WorkingContext int64
}

// Model tokenization rates (chars per token, varies by model family)
var modelRates = map[string]float64{
	// Claude models (Anthropic) - efficient tokenization
	"claude":            3.5,
	"claude-3-opus":     3.2,
	"claude-3-sonnet":   3.2,
	"claude-3-5-sonnet": 3.2,
	"claude-3-haiku":    3.2,
	"claude-4-opus":     3.2,
	"claude-4-sonnet":   3.2,
	"claude-4-haiku":    3.2,

	// GPT-4 models (OpenAI) - tiktoken-based
	"gpt-4":         3.8,
	"gpt-4-turbo":   3.8,
	"gpt-4o":        3.5,
	"gpt-4o-mini":   3.5,
	"gpt-3.5-turbo": 3.8,

	// DeepSeek models
	"deepseek":       3.8,
	"deepseek-chat":  3.8,
	"deepseek-coder": 3.8,

	// Gemini
	"gemini":     4.0,
	"gemini-pro": 4.0,

	// MiniMax
	"minimax":    3.8,
	"minimax-m2": 3.5,

	// GLM
	"glm": 3.8,

	// Default for unknown models
	"default": 4.0,
}

// NewSmartTokenBudget creates a token budget configured for the given model
func NewSmartTokenBudget(modelName string, modelMaxTokens int64) *SmartTokenBudget {
	// Default allocations based on typical usage patterns
	// These can be overridden via config
	budget := &SmartTokenBudget{
		ModelMaxTokens:       modelMaxTokens,
		ReserveTokens:        AutoCompactReserveTokens,
		CompressThreshold:    AutoCompactTriggerRatio,
		SystemPromptBudget:   8000,  // ~8K for system prompt
		MemoryBudget:         5000,  // ~5K for memory context
		WorkingContextBudget: 10000, // ~10K for working context
	}

	// Adjust based on model family
	if isClaudeModel(modelName) {
		// Claude models have larger effective context
		budget.MemoryBudget = 6000
		budget.WorkingContextBudget = 12000
	}

	return budget
}

// Calculate returns the token allocation for each content type
func (b *SmartTokenBudget) Calculate() TokenAllocation {
	available := b.ModelMaxTokens - b.ReserveTokens
	historyBudget := available - b.SystemPromptBudget - b.MemoryBudget - b.WorkingContextBudget
	if historyBudget < 0 {
		historyBudget = 0
	}

	return TokenAllocation{
		SystemPrompt:   b.SystemPromptBudget,
		Memory:         b.MemoryBudget,
		History:        historyBudget,
		WorkingContext: b.WorkingContextBudget,
	}
}

// AvailableForHistory returns how many tokens are available for history
func (b *SmartTokenBudget) AvailableForHistory() int64 {
	alloc := b.Calculate()
	return alloc.History
}

// ShouldCompress returns true if compression should be triggered
func (b *SmartTokenBudget) ShouldCompress(currentTokens int64) bool {
	available := b.ModelMaxTokens - b.ReserveTokens
	threshold := int64(float64(available) * b.CompressThreshold)
	return currentTokens >= threshold
}

// GetTokenRate returns the chars-per-token rate for a model
func GetTokenRate(modelName string) float64 {
	modelLower := strings.ToLower(modelName)

	// Check exact match first
	if rate, ok := modelRates[modelLower]; ok {
		return rate
	}

	// Check prefix match (e.g., "claude-3-opus-20240229" matches "claude-3-opus")
	for prefix, rate := range modelRates {
		if strings.HasPrefix(modelLower, prefix) {
			return rate
		}
	}

	// Check if it's a known family
	for family, rate := range modelRates {
		if strings.Contains(modelLower, family) {
			return rate
		}
	}

	return modelRates["default"]
}

// isClaudeModel returns true if the model is a Claude family model
func isClaudeModel(modelName string) bool {
	return strings.Contains(strings.ToLower(modelName), "claude")
}

// ModelAwareTokenEstimate estimates tokens using model-specific rates
func ModelAwareTokenEstimate(text string, modelName string) int {
	if text == "" {
		return 0
	}

	chars := len(text)
	if chars <= 4 {
		return 1
	}

	rate := GetTokenRate(modelName)

	// Count whitespace
	whitespace := float64(len(whitespaceRE.ReplaceAllString(text, " ")))

	// Count code-like content (code compresses more efficiently)
	code := float64(len(codeBlockRE.ReplaceAllString(text, "")))

	// Count JSON-like structures (denser than plain text)
	json := float64(len(jsonRE.ReplaceAllString(text, "")))

	// Plain text
	plain := float64(chars) - code - json

	// Code tokens: code block rate
	codeTokens := int(code / (rate + 0.5))

	// JSON tokens: slightly denser than plain text
	jsonRate := rate - 0.5
	if jsonRate < 2.0 {
		jsonRate = 2.0
	}
	jsonTokens := int(json / jsonRate)

	// Plain tokens: standard rate
	plainTokens := int((whitespace + plain) / rate)

	// Total
	total := codeTokens + jsonTokens + plainTokens + 1

	// Normalize to reasonable range
	if total < 1 {
		total = 1
	}

	return total
}

func AdvancedTokenEstimate(text string) int {
	if text == "" {
		return 0
	}

	chars := len(text)
	if chars <= 4 {
		return 1
	}

	// Count whitespace
	whitespace := float64(len(whitespaceRE.ReplaceAllString(text, " ")))

	// Count code-like content
	code := float64(len(codeBlockRE.ReplaceAllString(text, "")))

	// Count JSON-like structures
	json := float64(len(jsonRE.ReplaceAllString(text, "")))

	// Plain text
	plain := float64(chars) - code - json

	// Code tokens (roughly 4 chars per token)
	codeTokens := int(code / 4)

	// JSON tokens (roughly 5 chars per token)
	jsonTokens := int(json / 5)

	// Plain tokens (roughly 4.5 chars per token for English)
	plainTokens := int((whitespace + plain*2) / 6)

	// JSON is denser
	jsonDenseTokens := int(float64(jsonTokens) * 1.5)

	return codeTokens + plainTokens + jsonDenseTokens + 1
}

// FastTokenEstimate is a quick approximation
func FastTokenEstimate(text string) int {
	return len(text) / 4
}
