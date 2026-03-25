package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	PruneMinimum   = 20000
	PruneProtect   = 40000
	CompactReserve = 20000
)

type CompToolOutputInfo struct {
	ToolName     string
	OutputLen    int
	Timestamp    time.Time
	IsProtected  bool
	WasCompacted bool
}

type CompactionResult struct {
	Messages      []interface{}
	Summary       string
	TokensRemoved int
	NeedsFurther  bool
	SummaryParts  []CompSummarySection
}

type CompSummarySection struct {
	Title   string
	Content string
}

const (
	CompactionModeAuto     = "auto"
	CompactionModeManual   = "manual"
	CompactionModeOverflow = "overflow"
)

type CompactionService struct {
	mu              sync.RWMutex
	sessionID       string
	lastCompact     time.Time
	protectTools    map[string]bool
	modelMaxTokens  int64
	compactionCount int
}

func NewCompactionService(sessionID string) *CompactionService {
	return &CompactionService{
		sessionID:   sessionID,
		lastCompact: time.Time{},
		protectTools: map[string]bool{
			"skill":     true,
			"agent_run": true,
			"task":      true,
			"todowrite": true,
		},
		modelMaxTokens: 200000,
	}
}

func (s *CompactionService) SetModelMaxTokens(tokens int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.modelMaxTokens = tokens
}

func (s *CompactionService) MarkProtected(toolName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.protectTools[toolName] = true
}

func (s *CompactionService) IsProtected(toolName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.protectTools[toolName]
}

func (s *CompactionService) IsOverflow(currentTokens int64) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	available := s.modelMaxTokens - CompactReserve
	threshold := float64(available) * 0.85
	return int64(threshold) < currentTokens
}

func (s *CompactionService) NeedsCompaction(currentTokens int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastCompact.IsZero() {
		return currentTokens > PruneProtect
	}
	if time.Since(s.lastCompact) < 30*time.Second {
		return false
	}
	return currentTokens > PruneProtect
}

func (s *CompactionService) MarkCompacted() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCompact = time.Now()
	s.compactionCount++
}

func (s *CompactionService) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastCompact = time.Time{}
	s.compactionCount = 0
}

func (s *CompactionService) CompactionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.compactionCount
}

func (s *CompactionService) ShouldTrigger(currentTokens, modelMaxTokens int64) bool {
	available := modelMaxTokens - CompactReserve
	threshold := float64(available) * 0.85
	return currentTokens >= int64(threshold)
}

type CompPruneCandidate struct {
	Index            int
	PartIndex        int
	ToolName         string
	OutputLen        int
	Timestamp        time.Time
	IsProtected      bool
	IsCompacted      bool
	CumulativeTokens int
}

func (s *CompactionService) FindPruneCandidates(toolOutputs []CompToolOutputInfo, protectRecent int) []CompPruneCandidate {
	if len(toolOutputs) <= protectRecent*2 {
		return nil
	}

	var candidates []CompPruneCandidate
	var totalTokens int

	for i := 0; i < len(toolOutputs)-protectRecent; i++ {
		output := toolOutputs[i]
		if s.IsProtected(output.ToolName) {
			continue
		}
		if output.WasCompacted {
			continue
		}

		totalTokens += output.OutputLen
		if totalTokens > PruneProtect {
			candidates = append(candidates, CompPruneCandidate{
				Index:            i,
				ToolName:         output.ToolName,
				OutputLen:        output.OutputLen,
				Timestamp:        output.Timestamp,
				IsProtected:      output.IsProtected,
				IsCompacted:      output.WasCompacted,
				CumulativeTokens: totalTokens,
			})
		}
	}

	if totalTokens < PruneMinimum {
		return nil
	}

	return candidates
}

func (s *CompactionService) CompactToolOutput(content string, maxChars int) string {
	if content == "" {
		return content
	}

	trimmed := strings.TrimSpace(content)
	if len(trimmed) <= maxChars {
		return trimmed
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) <= 30 {
		return trimmed
	}

	keepFirst := 15
	keepLast := 10

	if len(lines) <= keepFirst+keepLast {
		return trimmed
	}

	skipped := len(lines) - keepFirst - keepLast
	result := append(lines[:keepFirst], fmt.Sprintf("... [%d lines skipped]", skipped))
	result = append(result, lines[len(lines)-keepLast:]...)

	return strings.Join(result, "\n")
}

func (s *CompactionService) BuildDefaultCompactionPrompt(contextStrings []string) string {
	var b strings.Builder

	b.WriteString("Provide a detailed prompt for continuing our conversation above.\n\n")
	b.WriteString("Focus on information that would be helpful for continuing the conversation, including:\n")
	b.WriteString("- What we did\n")
	b.WriteString("- What we're doing\n")
	b.WriteString("- Which files we're working on\n")
	b.WriteString("- What we're going to do next\n\n")
	b.WriteString("The summary that you construct will be used so that another agent can read it and continue the work.\n")
	b.WriteString("Do not call any tools. Respond only with the summary text.\n")
	b.WriteString("Respond in the same language as the user's messages in the conversation.\n\n")

	if len(contextStrings) > 0 {
		b.WriteString("## Additional Context\n")
		for _, ctx := range contextStrings {
			b.WriteString(ctx)
			b.WriteString("\n\n")
		}
	}

	b.WriteString("When constructing the summary, try to stick to this template:\n")
	b.WriteString("---\n")
	b.WriteString("## Goal\n\n")
	b.WriteString("[What goal(s) is the user trying to accomplish?]\n\n")
	b.WriteString("## Instructions\n\n")
	b.WriteString("- [What important instructions did the user give you that are relevant]\n")
	b.WriteString("- [If there is a plan or spec, include information about it]\n\n")
	b.WriteString("## Discoveries\n\n")
	b.WriteString("[What notable things were learned during this conversation]\n\n")
	b.WriteString("## Accomplished\n\n")
	b.WriteString("[What work has been completed, what work is in progress, what is left]\n\n")
	b.WriteString("## Relevant files / directories\n\n")
	b.WriteString("[Construct a structured list of relevant files]\n")
	b.WriteString("---\n")

	return b.String()
}

func (s *CompactionService) ParseCompactionResponse(response string) []CompSummarySection {
	var sections []CompSummarySection

	lines := strings.Split(response, "\n")
	currentTitle := ""
	currentContent := new(strings.Builder)
	inCodeBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "```") {
			inCodeBlock = !inCodeBlock
			continue
		}

		if !inCodeBlock && strings.HasPrefix(trimmed, "## ") {
			if currentTitle != "" {
				sections = append(sections, CompSummarySection{
					Title:   currentTitle,
					Content: strings.TrimSpace(currentContent.String()),
				})
				currentContent.Reset()
			}
			currentTitle = strings.TrimPrefix(trimmed, "## ")
			continue
		}

		if currentTitle != "" {
			currentContent.WriteString(line)
			currentContent.WriteString("\n")
		}
	}

	if currentTitle != "" {
		sections = append(sections, CompSummarySection{
			Title:   currentTitle,
			Content: strings.TrimSpace(currentContent.String()),
		})
	}

	if len(sections) == 0 && strings.TrimSpace(response) != "" {
		sections = append(sections, CompSummarySection{
			Title:   "Summary",
			Content: strings.TrimSpace(response),
		})
	}

	return sections
}

func (s *CompactionService) BuildCompactResult(summary string, originalMessages []interface{}, prunedCount int) *CompactionResult {
	parts := s.ParseCompactionResponse(summary)

	summaryText := s.formatSummary(parts)

	return &CompactionResult{
		Summary:       summaryText,
		TokensRemoved: prunedCount * 4,
		NeedsFurther:  false,
		SummaryParts:  parts,
	}
}

func (s *CompactionService) formatSummary(sections []CompSummarySection) string {
	if len(sections) == 0 {
		return ""
	}

	var b strings.Builder
	for _, section := range sections {
		b.WriteString("## ")
		b.WriteString(section.Title)
		b.WriteString("\n\n")
		b.WriteString(section.Content)
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

func (s *CompactionService) ExtractConstraints(messages []interface{}, currentTurn int) []CompConstraint {
	var constraints []CompConstraint

	constraintPatterns := []struct {
		Pattern  string
		Priority int
	}{
		{"must not", 5},
		{"never", 5},
		{"critical", 5},
		{"security", 5},
		{"secret", 5},
		{"password", 5},
		{"api key", 5},
		{"should not", 2},
		{"do not", 2},
		{"don't", 2},
		{"important", 2},
		{"must", 2},
		{"require", 2},
	}

	for i, msg := range messages {
		content := s.extractContent(msg)
		if content == "" {
			continue
		}

		lower := strings.ToLower(content)
		for _, cp := range constraintPatterns {
			if strings.Contains(lower, cp.Pattern) {
				constraint := CompConstraint{
					Content:    s.truncateConstraint(content),
					TurnNumber: currentTurn - (len(messages) - i),
					Source:     s.extractRole(msg),
					Priority:   cp.Priority,
				}
				if constraint.Priority > 0 {
					constraints = append(constraints, constraint)
				}
				break
			}
		}
	}

	for i := range constraints {
		for j := i + 1; j < len(constraints); j++ {
			if constraints[i].Priority < constraints[j].Priority {
				constraints[i], constraints[j] = constraints[j], constraints[i]
			}
		}
	}

	if len(constraints) > 10 {
		constraints = constraints[:10]
	}

	return constraints
}

func (s *CompactionService) extractContent(msg interface{}) string {
	switch v := msg.(type) {
	case string:
		return v
	case map[string]interface{}:
		if content, ok := v["content"].(string); ok {
			return content
		}
	}
	return ""
}

func (s *CompactionService) extractRole(msg interface{}) string {
	switch v := msg.(type) {
	case map[string]interface{}:
		if role, ok := v["role"].(string); ok {
			return role
		}
	}
	return "unknown"
}

func (s *CompactionService) truncateConstraint(content string) string {
	if len(content) > 4000 {
		return content[:4000] + "..."
	}
	return content
}

func (s *CompactionService) FilterValidConstraints(constraints []CompConstraint, currentTurn int, maxAge int) []CompConstraint {
	var valid []CompConstraint
	for _, c := range constraints {
		if currentTurn-c.TurnNumber <= maxAge {
			valid = append(valid, c)
		}
	}
	return valid
}

type CompactionContext struct {
	SessionID      string
	CurrentTurn    int
	Messages       []interface{}
	ModelMaxTokens int64
	Mode           string
}

type CompactionHandler struct {
	service *CompactionService
}

func NewCompactionHandler(sessionID string) *CompactionHandler {
	return &CompactionHandler{
		service: NewCompactionService(sessionID),
	}
}

func (h *CompactionHandler) SetModelMaxTokens(tokens int64) {
	h.service.SetModelMaxTokens(tokens)
}

func (h *CompactionHandler) MarkProtected(toolName string) {
	h.service.MarkProtected(toolName)
}

func (h *CompactionHandler) IsProtected(toolName string) bool {
	return h.service.IsProtected(toolName)
}

func (h *CompactionHandler) NeedsCompaction(currentTokens int64) bool {
	return h.service.NeedsCompaction(currentTokens)
}

func (h *CompactionHandler) MarkCompacted() {
	h.service.MarkCompacted()
}

func (h *CompactionHandler) Reset() {
	h.service.Reset()
}

func (h *CompactionHandler) IsOverflow(currentTokens int64) bool {
	return h.service.IsOverflow(currentTokens)
}

func (h *CompactionHandler) EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

func (h *CompactionHandler) FindCompPruneCandidates(toolOutputs []CompToolOutputInfo, protectRecent int) []int {
	candidates := h.service.FindPruneCandidates(toolOutputs, protectRecent)
	var indices []int
	for _, c := range candidates {
		indices = append(indices, c.Index)
	}
	return indices
}

func (h *CompactionHandler) CompactToolOutput(content string, maxChars int) string {
	return h.service.CompactToolOutput(content, maxChars)
}

func (h *CompactionHandler) BuildCompactPrompt(messages []interface{}, constraints []string, memoryContext string) string {
	contextStrings := constraints
	if memoryContext != "" {
		contextStrings = append(contextStrings, memoryContext)
	}
	return h.service.BuildDefaultCompactionPrompt(contextStrings)
}

func (h *CompactionHandler) ExtractCompConstraints(messages []interface{}, currentTurn int) []CompConstraint {
	return h.service.ExtractConstraints(messages, currentTurn)
}

func (h *CompactionHandler) FilterValidCompConstraints(constraints []CompConstraint, currentTurn int, maxAge int) []CompConstraint {
	return h.service.FilterValidConstraints(constraints, currentTurn, maxAge)
}

type CompConstraint struct {
	Content    string
	TurnNumber int
	Source     string
	Priority   int
}

type CompactionExecutor struct {
	service *CompactionService
}

func NewCompactionExecutor(sessionID string) *CompactionExecutor {
	return &CompactionExecutor{
		service: NewCompactionService(sessionID),
	}
}

func (e *CompactionExecutor) Execute(ctx context.Context, input *CompactionInput) (*CompactionResult, error) {
	if !e.service.NeedsCompaction(input.CurrentTokens) && input.Mode != CompactionModeOverflow {
		return &CompactionResult{
			NeedsFurther: false,
		}, nil
	}

	constraints := e.service.ExtractConstraints(input.Messages, input.CurrentTurn)
	constraintStrings := make([]string, len(constraints))
	for i, c := range constraints {
		constraintStrings[i] = c.Content
	}

	prompt := e.service.BuildDefaultCompactionPrompt(constraintStrings)

	result := &CompactionResult{
		Summary:       prompt,
		TokensRemoved: 0,
		NeedsFurther:  true,
	}

	e.service.MarkCompacted()

	return result, nil
}

type CompactionInput struct {
	SessionID     string
	Messages      []interface{}
	CurrentTokens int64
	CurrentTurn   int
	Mode          string
	Constraints   []CompConstraint
}

type ToolOutputPruner struct {
	protectTools map[string]bool
}

func NewToolOutputPruner() *ToolOutputPruner {
	return &ToolOutputPruner{
		protectTools: map[string]bool{
			"skill":     true,
			"agent_run": true,
			"task":      true,
			"todowrite": true,
		},
	}
}

func (p *ToolOutputPruner) Prune(outputs []ToolOutputEntry, targetTokens int) []ToolOutputEntry {
	if len(outputs) == 0 {
		return outputs
	}

	var result []ToolOutputEntry
	var totalTokens int

	for i := len(outputs) - 1; i >= 0; i-- {
		output := outputs[i]
		if p.IsProtected(output.ToolName) {
			result = append([]ToolOutputEntry{output}, result...)
			continue
		}

		tokens := output.OutputTokens
		if totalTokens+tokens <= targetTokens {
			result = append([]ToolOutputEntry{output}, result...)
			totalTokens += tokens
		} else if totalTokens < PruneMinimum {
			pruned := output
			pruned.IsCompacted = true
			pruned.OutputSummary = p.SummarizeOutput(output.Content)
			result = append([]ToolOutputEntry{pruned}, result...)
			totalTokens += output.OutputTokens
		}
	}

	return result
}

func (p *ToolOutputPruner) IsProtected(toolName string) bool {
	return p.protectTools[toolName]
}

func (p *ToolOutputPruner) SummarizeOutput(content string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	if len(lines) <= 5 {
		return content
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("[%d lines, truncated]\n", len(lines)))
	if len(lines) > 0 {
		b.WriteString(lines[0])
		if len(lines) > 1 {
			b.WriteString("\n...")
		}
	}
	return b.String()
}

type ToolOutputEntry struct {
	ID            string
	ToolName      string
	Content       string
	OutputTokens  int
	Timestamp     time.Time
	IsCompacted   bool
	OutputSummary string
}

func NewToolOutputEntry(toolName, content string) *ToolOutputEntry {
	return &ToolOutputEntry{
		ID:            uuid.NewString(),
		ToolName:      toolName,
		Content:       content,
		OutputTokens:  len(content) / 4,
		Timestamp:     time.Now(),
		IsCompacted:   false,
		OutputSummary: "",
	}
}
