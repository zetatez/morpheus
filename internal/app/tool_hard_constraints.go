package app

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// HardConstraint defines a registration-level constraint on tool usage
type HardConstraint interface {
	// Name returns the constraint name
	Name() string
	// Check evaluates the constraint and returns error if violated
	Check(ctx context.Context, toolName string, args map[string]any) error
}

// DisallowedToolsConstraint blocks specific tools at registration level
type DisallowedToolsConstraint struct {
	mu            sync.RWMutex
	disallowedSet map[string]bool
	reason        map[string]string
}

// NewDisallowedToolsConstraint creates a new disallowed tools constraint
func NewDisallowedToolsConstraint() *DisallowedToolsConstraint {
	return &DisallowedToolsConstraint{
		disallowedSet: make(map[string]bool),
		reason:        make(map[string]string),
	}
}

// Add adds a tool to the disallowed list with a reason
func (d *DisallowedToolsConstraint) Add(toolName string, reason string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.disallowedSet[toolName] = true
	d.reason[toolName] = reason
}

// Remove removes a tool from the disallowed list
func (d *DisallowedToolsConstraint) Remove(toolName string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.disallowedSet, toolName)
	delete(d.reason, toolName)
}

// IsDisallowed checks if a tool is disallowed
func (d *DisallowedToolsConstraint) IsDisallowed(toolName string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.disallowedSet[toolName]
}

// GetReason returns the reason a tool is disallowed
func (d *DisallowedToolsConstraint) GetReason(toolName string) string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.reason[toolName]
}

// Name returns the constraint name
func (d *DisallowedToolsConstraint) Name() string {
	return "disallowed_tools"
}

// Check verifies if the tool is allowed
func (d *DisallowedToolsConstraint) Check(ctx context.Context, toolName string, args map[string]any) error {
	if d.IsDisallowed(toolName) {
		return fmt.Errorf("tool '%s' is disallowed: %s", toolName, d.GetReason(toolName))
	}
	return nil
}

// ToolConstraintsManager manages all hard constraints at the tool registry level
type ToolConstraintsManager struct {
	mu           sync.RWMutex
	constraints  []HardConstraint
	verification *VerificationAgent
}

// NewToolConstraintsManager creates a new constraints manager
func NewToolConstraintsManager() *ToolConstraintsManager {
	return &ToolConstraintsManager{
		constraints:  []HardConstraint{},
		verification: NewVerificationAgent(),
	}
}

// AddConstraint adds a hard constraint
func (tcm *ToolConstraintsManager) AddConstraint(c HardConstraint) {
	tcm.mu.Lock()
	defer tcm.mu.Unlock()
	tcm.constraints = append(tcm.constraints, c)
}

// CheckAll checks all constraints for a tool call
func (tcm *ToolConstraintsManager) CheckAll(ctx context.Context, toolName string, args map[string]any) error {
	tcm.mu.RLock()
	defer tcm.mu.RUnlock()

	for _, c := range tcm.constraints {
		if err := c.Check(ctx, toolName, args); err != nil {
			return err
		}
	}
	return nil
}

// GetVerificationAgent returns the verification agent
func (tcm *ToolConstraintsManager) GetVerificationAgent() *VerificationAgent {
	return tcm.verification
}

// FileModification tracks a file modification for verification
type FileModification struct {
	Path      string
	Operation string // "create", "modify", "delete"
	Timestamp int64
}

// VerificationAgent triggers verification on 3+ file modifications per round
type VerificationAgent struct {
	mu                sync.RWMutex
	modifications     []FileModification
	threshold         int
	roundCount        int
	lastVerification  int64
	criticalReminder  string
}

// NewVerificationAgent creates a new verification agent
func NewVerificationAgent() *VerificationAgent {
	return &VerificationAgent{
		modifications:     []FileModification{},
		threshold:          3,
		roundCount:         0,
		lastVerification:   0,
		criticalReminder:   getDefaultCriticalReminder(),
	}
}

const (
	// CriticalSystemReminder is reinjected every round
	CriticalSystemReminder = `You are working on a codebase. Follow these critical rules:

1. SAFETY: Never modify /etc, /usr, /boot, /sys, /proc, /dev, /root, /.ssh
2. SAFETY: Never run commands that modify system state (mkfs, fdisk, dd to /dev/*)
3. SAFETY: Always confirm destructive operations (>3 files or system files)
4. QUALITY: After 3+ file changes, verify the changes are correct and consistent
5. QUALITY: Check that related files are updated if needed (imports, configs, tests)
6. CONTEXT: Remember to read relevant files before modifying them`

	// VerificationPrompt is used when verification is triggered
	VerificationPrompt = `You just made %d file modifications. Before continuing, verify:
1. All modified files are correct and intentional
2. Related files (imports, configs, tests) are updated if needed
3. No unintended changes to other files
4. Changes are consistent with the overall goal

If you notice any issues, fix them now before proceeding.`
)

func getDefaultCriticalReminder() string {
	return CriticalSystemReminder
}

// SetThreshold sets the modification threshold for verification
func (va *VerificationAgent) SetThreshold(threshold int) {
	va.mu.Lock()
	defer va.mu.Unlock()
	va.threshold = threshold
}

// RecordModification records a file modification
func (va *VerificationAgent) RecordModification(path, operation string) {
	va.mu.Lock()
	defer va.mu.Unlock()
	va.modifications = append(va.modifications, FileModification{
		Path:      path,
		Operation: operation,
		Timestamp: nowUnix(),
	})
}

// StartRound resets state for a new round
func (va *VerificationAgent) StartRound() {
	va.mu.Lock()
	defer va.mu.Unlock()
	va.modifications = []FileModification{}
	va.roundCount++
}

// ShouldVerify returns true if verification should be triggered
func (va *VerificationAgent) ShouldVerify() bool {
	va.mu.RLock()
	defer va.mu.RUnlock()
	return len(va.modifications) >= va.threshold
}

// GetModificationCount returns the number of modifications in current round
func (va *VerificationAgent) GetModificationCount() int {
	va.mu.RLock()
	defer va.mu.RUnlock()
	return len(va.modifications)
}

// GetVerificationPrompt returns the verification prompt
func (va *VerificationAgent) GetVerificationPrompt() string {
	va.mu.RLock()
	defer va.mu.RUnlock()
	return fmt.Sprintf(VerificationPrompt, len(va.modifications))
}

// GetCriticalReminder returns the critical system reminder
func (va *VerificationAgent) GetCriticalReminder() string {
	va.mu.RLock()
	defer va.mu.RUnlock()
	return va.criticalReminder
}

// SetCriticalReminder sets a custom critical reminder
func (va *VerificationAgent) SetCriticalReminder(reminder string) {
	va.mu.Lock()
	defer va.mu.Unlock()
	va.criticalReminder = reminder
}

// MarkVerified marks that verification was completed
func (va *VerificationAgent) MarkVerified() {
	va.mu.Lock()
	defer va.mu.Unlock()
	va.lastVerification = nowUnix()
	va.modifications = []FileModification{}
}

// GetLastVerificationTime returns the timestamp of last verification
func (va *VerificationAgent) GetLastVerificationTime() int64 {
	va.mu.RLock()
	defer va.mu.RUnlock()
	return va.lastVerification
}

// nowUnix returns current Unix timestamp
func nowUnix() int64 {
	return time.Now().Unix()
}

// HardConstraintRegistry wraps a ToolRegistry with hard constraints
type HardConstraintRegistry struct {
	underlying  interface {
		Register(tool interface{}) error
		Get(name string) (interface{}, bool)
	}
	constraints *ToolConstraintsManager
}

// ToolCallGuard wraps tool execution with hard constraint checks
type ToolCallGuard struct {
	constraints *ToolConstraintsManager
}

// NewToolCallGuard creates a new tool call guard
func NewToolCallGuard(constraints *ToolConstraintsManager) *ToolCallGuard {
	return &ToolCallGuard{constraints: constraints}
}

// GuardExecute checks constraints before tool execution
func (g *ToolCallGuard) GuardExecute(ctx context.Context, toolName string, args map[string]any) error {
	return g.constraints.CheckAll(ctx, toolName, args)
}

// RegistryLevelConstraint defines constraints applied at tool registration time
type RegistryLevelConstraint struct {
	mu            sync.RWMutex
	blockedTools  map[string]bool
	blockReason   map[string]string
	allowedTools  map[string]bool // If set, only these tools are allowed
	requireReason bool
}

// NewRegistryLevelConstraint creates a new registry-level constraint
func NewRegistryLevelConstraint() *RegistryLevelConstraint {
	return &RegistryLevelConstraint{
		blockedTools:  make(map[string]bool),
		blockReason:   make(map[string]string),
		allowedTools:  make(map[string]bool),
		requireReason: true,
	}
}

// BlockTool blocks a tool at the registry level
func (r *RegistryLevelConstraint) BlockTool(toolName string, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blockedTools[toolName] = true
	r.blockReason[toolName] = reason
}

// UnblockTool removes a tool from the blocked list
func (r *RegistryLevelConstraint) UnblockTool(toolName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.blockedTools, toolName)
	delete(r.blockReason, toolName)
}

// IsBlocked checks if a tool is blocked
func (r *RegistryLevelConstraint) IsBlocked(toolName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.blockedTools[toolName]
}

// SetAllowedTools sets the allowed tools whitelist (if non-empty, only these are allowed)
func (r *RegistryLevelConstraint) SetAllowedTools(tools []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.allowedTools = make(map[string]bool)
	for _, t := range tools {
		r.allowedTools[t] = true
	}
}

// IsAllowed checks if a tool is allowed (not blocked and in allowed list if set)
func (r *RegistryLevelConstraint) IsAllowed(toolName string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Check blocked
	if r.blockedTools[toolName] {
		return false
	}

	// Check allowed whitelist
	if len(r.allowedTools) > 0 {
		return r.allowedTools[toolName]
	}

	return true
}

// GetBlockReason returns the reason a tool is blocked
func (r *RegistryLevelConstraint) GetBlockReason(toolName string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.blockReason[toolName]
}
