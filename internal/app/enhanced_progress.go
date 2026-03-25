package app

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/zetatez/morpheus/internal/config"
)

const (
	SimilarityThreshold    = 0.8
	DoomLoopMaxAttempts    = 3
	ProgressMilestoneCount = 5
)

const (
	defaultMaxConsecutiveFailures  = 10
	defaultMaxStepsWithoutProgress = 50
	defaultMaxSimilarResults       = 5
)

func bitsOnCount(x uint64) int {
	count := 0
	for x != 0 {
		count++
		x &= x - 1
	}
	return count
}

type ProgressMilestone struct {
	Description string
	AchievedAt  int
}

type EnhancedProgressTracker struct {
	cfg                  config.AgentConfig
	consecutiveFailures  int
	stepsWithoutProgress int
	recentResultHashes   []uint64
	similarStreak        int
	actionFingerprints   []string
	milestones           []ProgressMilestone
	doomLoopDetector     *DoomLoopDetector
	mu                   sync.RWMutex
}

func NewEnhancedProgressTracker(cfg config.AgentConfig) *EnhancedProgressTracker {
	maxSim := cfg.MaxSimilarResults
	if maxSim <= 0 {
		maxSim = defaultMaxSimilarResults
	}
	return &EnhancedProgressTracker{
		cfg:                cfg,
		recentResultHashes: make([]uint64, 0, maxSim),
		doomLoopDetector:   NewDoomLoopDetector(),
	}
}

func (pt *EnhancedProgressTracker) RecordFailure() {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.consecutiveFailures++
}

func (pt *EnhancedProgressTracker) RecordSuccess(isProgress bool) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.consecutiveFailures = 0
	if isProgress {
		pt.stepsWithoutProgress = 0
		pt.recentResultHashes = pt.recentResultHashes[:0]
		pt.similarStreak = 0
		pt.milestones = pt.milestones[:0]
	} else {
		pt.stepsWithoutProgress++
	}
}

func (pt *EnhancedProgressTracker) RecordResultHash(hash uint64) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	if len(pt.recentResultHashes) >= pt.effectiveMaxSimilarResults() {
		pt.recentResultHashes = pt.recentResultHashes[1:]
	}
	pt.recentResultHashes = append(pt.recentResultHashes, hash)
}

func (pt *EnhancedProgressTracker) RecordActionFingerprint(fp string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.actionFingerprints = append(pt.actionFingerprints, fp)
	if len(pt.actionFingerprints) > DoomLoopMaxAttempts*2 {
		pt.actionFingerprints = pt.actionFingerprints[len(pt.actionFingerprints)-DoomLoopMaxAttempts*2:]
	}
}

func (pt *EnhancedProgressTracker) DetectDoomLoop(toolName string, args map[string]any) (isDoomLoop bool, needsConfirmation bool) {
	return pt.doomLoopDetector.RecordToolCall("", toolName, args)
}

func (pt *EnhancedProgressTracker) DetectActionLoop() bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if len(pt.actionFingerprints) < DoomLoopMaxAttempts*2 {
		return false
	}

	recent := pt.actionFingerprints[len(pt.actionFingerprints)-DoomLoopMaxAttempts*2:]
	firstHalf := recent[:DoomLoopMaxAttempts]
	secondHalf := recent[DoomLoopMaxAttempts:]

	for i := 0; i < DoomLoopMaxAttempts; i++ {
		if firstHalf[i] != secondHalf[i] {
			return false
		}
	}
	return true
}

func (pt *EnhancedProgressTracker) RecordMilestone(description string) {
	pt.mu.Lock()
	defer pt.mu.Unlock()
	pt.milestones = append(pt.milestones, ProgressMilestone{
		Description: description,
		AchievedAt:  len(pt.milestones),
	})
	if len(pt.milestones) > ProgressMilestoneCount {
		pt.milestones = pt.milestones[len(pt.milestones)-ProgressMilestoneCount:]
	}
}

func (pt *EnhancedProgressTracker) GetMilestones() []ProgressMilestone {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	result := make([]ProgressMilestone, len(pt.milestones))
	copy(result, pt.milestones)
	return result
}

func (pt *EnhancedProgressTracker) IsSimilarToRecent(hash uint64) bool {
	pt.mu.RLock()
	defer pt.mu.RUnlock()
	if len(pt.recentResultHashes) < 2 {
		return false
	}
	similarCount := 0
	for _, h := range pt.recentResultHashes {
		if computeSimilarity(hash, h) >= SimilarityThreshold {
			similarCount++
		}
	}
	return similarCount >= len(pt.recentResultHashes)/2
}

func computeSimilarity(a, b uint64) float64 {
	x := a ^ b
	return 1.0 - float64(bitsOnCount(x))/64.0
}

func (pt *EnhancedProgressTracker) ShouldStop() (string, bool) {
	pt.mu.RLock()
	defer pt.mu.RUnlock()

	if pt.consecutiveFailures >= pt.effectiveMaxConsecutiveFailures() {
		return fmt.Sprintf("stopped after %d consecutive tool failures", pt.consecutiveFailures), true
	}
	if pt.stepsWithoutProgress >= pt.effectiveMaxStepsWithoutProgress() {
		return fmt.Sprintf("stopped after %d steps without progress", pt.stepsWithoutProgress), true
	}
	if pt.similarStreak >= pt.effectiveMaxSimilarResults() {
		return fmt.Sprintf("stopped after %d similar results (possible loop)", pt.similarStreak), true
	}
	if pt.detectActionLoopUnsafe() {
		return "stopped after detecting a repeated action pattern (doom loop)", true
	}
	return "", false
}

func (pt *EnhancedProgressTracker) detectActionLoopUnsafe() bool {
	if len(pt.actionFingerprints) < DoomLoopMaxAttempts*2 {
		return false
	}

	recent := pt.actionFingerprints[len(pt.actionFingerprints)-DoomLoopMaxAttempts*2:]
	firstHalf := recent[:DoomLoopMaxAttempts]
	secondHalf := recent[DoomLoopMaxAttempts:]

	for i := 0; i < DoomLoopMaxAttempts; i++ {
		if firstHalf[i] != secondHalf[i] {
			return false
		}
	}
	return true
}

func (pt *EnhancedProgressTracker) effectiveMaxConsecutiveFailures() int {
	if pt.cfg.MaxConsecutiveFailures <= 0 {
		return defaultMaxConsecutiveFailures
	}
	return pt.cfg.MaxConsecutiveFailures
}

func (pt *EnhancedProgressTracker) effectiveMaxStepsWithoutProgress() int {
	if pt.cfg.MaxStepsWithoutProgress <= 0 {
		return defaultMaxStepsWithoutProgress
	}
	return pt.cfg.MaxStepsWithoutProgress
}

func (pt *EnhancedProgressTracker) effectiveMaxSimilarResults() int {
	if pt.cfg.MaxSimilarResults <= 0 {
		return defaultMaxSimilarResults
	}
	return pt.cfg.MaxSimilarResults
}

func HashResult(data map[string]any) uint64 {
	bytes, _ := json.Marshal(data)
	hash := sha256.Sum256(bytes)
	return binary.BigEndian.Uint64(hash[:8])
}

func Fingerprint(toolName string, args map[string]any) string {
	data, _ := json.Marshal(args)
	sum := sha256.Sum256(append([]byte(toolName+":"), data...))
	return fmt.Sprintf("%x", sum[:])
}

type SemanticHasher struct{}

func NewSemanticHasher() *SemanticHasher {
	return &SemanticHasher{}
}

func (sh *SemanticHasher) HashResult(data map[string]any) uint64 {
	return HashResult(data)
}

func (sh *SemanticHasher) NormalizeForComparison(data map[string]any) string {
	normalized := make(map[string]any)
	for k, v := range data {
		normalized[k] = sh.normalizeValue(v)
	}
	bytes, _ := json.Marshal(normalized)
	return string(bytes)
}

func (sh *SemanticHasher) normalizeValue(v any) any {
	switch val := v.(type) {
	case string:
		val = strings.TrimSpace(val)
		if len(val) > 1000 {
			val = val[:1000] + "..."
		}
		return val
	case map[string]any:
		return sh.NormalizeForComparison(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			result[i] = sh.normalizeValue(item)
		}
		return result
	default:
		return val
	}
}
