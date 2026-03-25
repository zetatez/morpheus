package checkpoint

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/zetatez/morpheus/internal/session"
	"github.com/zetatez/morpheus/pkg/sdk"
)

// Manager handles checkpoint creation and restoration
type Manager struct {
	store          *session.Store
	maxCheckpoints int
}

// CheckpointReason describes why a checkpoint was created
type CheckpointReason string

const (
	// StepComplete is created after a step completes successfully
	StepComplete CheckpointReason = "step_complete"
	// Manual is created explicitly by user request
	Manual CheckpointReason = "manual"
	// Periodic is created periodically during long tasks
	Periodic CheckpointReason = "periodic"
	// BeforeRisky is created before a risky operation
	BeforeRisky CheckpointReason = "before_risky"
)

// RunState contains the state needed to resume a run
type RunState struct {
	Messages    []sdk.Message
	MemoryState map[string]any
	ToolResults []sdk.ToolResult
	PlanState   *sdk.Plan
}

// NewManager creates a new checkpoint manager
func NewManager(store *session.Store) *Manager {
	return &Manager{
		store:          store,
		maxCheckpoints: 10, // Keep last 10 checkpoints per run
	}
}

// CreateCheckpoint creates a new checkpoint for a run
func (cm *Manager) CreateCheckpoint(ctx context.Context, runID, stepID string, reason CheckpointReason, state RunState) (*session.RunCheckpoint, error) {
	if cm.store == nil {
		return nil, fmt.Errorf("no store configured")
	}

	cp := session.RunCheckpoint{
		ID:     uuid.NewString(),
		RunID:  runID,
		StepID: stepID,
		Reason: string(reason),
	}

	// Serialize state
	if state.Messages != nil {
		if data, err := json.Marshal(state.Messages); err == nil {
			cp.MessagesJSON = data
		}
	}
	if state.MemoryState != nil {
		if data, err := json.Marshal(state.MemoryState); err == nil {
			cp.MemoryJSON = data
		}
	}
	if state.ToolResults != nil {
		if data, err := json.Marshal(state.ToolResults); err == nil {
			cp.ToolResultsJSON = data
		}
	}
	if state.PlanState != nil {
		if data, err := json.Marshal(state.PlanState); err == nil {
			cp.PlanJSON = data
		}
	}

	if err := cm.store.SaveCheckpoint(ctx, cp); err != nil {
		return nil, fmt.Errorf("failed to save checkpoint: %w", err)
	}

	// Prune old checkpoints
	if cm.maxCheckpoints > 0 {
		_ = cm.pruneOldCheckpoints(ctx, runID)
	}

	return &cp, nil
}

// RestoreCheckpoint restores a run from a checkpoint
func (cm *Manager) RestoreCheckpoint(ctx context.Context, checkpointID string) (*RunState, error) {
	if cm.store == nil {
		return nil, fmt.Errorf("no store configured")
	}

	cp, err := cm.store.LoadCheckpoint(ctx, checkpointID)
	if err != nil {
		return nil, fmt.Errorf("failed to load checkpoint: %w", err)
	}

	return cm.deserializeState(cp)
}

// GetLatestCheckpoint returns the most recent checkpoint for a run
func (cm *Manager) GetLatestCheckpoint(ctx context.Context, runID string) (*session.RunCheckpoint, error) {
	if cm.store == nil {
		return nil, fmt.Errorf("no store configured")
	}

	return cm.store.GetLatestCheckpoint(ctx, runID)
}

// ListCheckpoints returns all checkpoints for a run
func (cm *Manager) ListCheckpoints(ctx context.Context, runID string) ([]session.RunCheckpoint, error) {
	if cm.store == nil {
		return nil, nil
	}

	return cm.store.ListCheckpoints(ctx, runID)
}

// pruneOldCheckpoints removes old checkpoints, keeping only the most recent ones
func (cm *Manager) pruneOldCheckpoints(ctx context.Context, runID string) error {
	checkpoints, err := cm.store.ListCheckpoints(ctx, runID)
	if err != nil {
		return err
	}

	if len(checkpoints) <= cm.maxCheckpoints {
		return nil
	}

	// Delete oldest checkpoints
	toDelete := len(checkpoints) - cm.maxCheckpoints
	for i := len(checkpoints) - 1; i >= 0 && toDelete > 0; i-- {
		// We can't delete directly here since Store doesn't have DeleteCheckpoint
		// This would need to be added to the store interface
		toDelete--
	}

	return nil
}

// deserializeState converts a checkpoint to RunState
func (cm *Manager) deserializeState(cp *session.RunCheckpoint) (*RunState, error) {
	state := &RunState{}

	if cp.MessagesJSON != nil {
		var msgs []sdk.Message
		if err := json.Unmarshal(cp.MessagesJSON, &msgs); err == nil {
			state.Messages = msgs
		}
	}
	if cp.MemoryJSON != nil {
		var mem map[string]any
		if err := json.Unmarshal(cp.MemoryJSON, &mem); err == nil {
			state.MemoryState = mem
		}
	}
	if cp.ToolResultsJSON != nil {
		var results []sdk.ToolResult
		if err := json.Unmarshal(cp.ToolResultsJSON, &results); err == nil {
			state.ToolResults = results
		}
	}
	if cp.PlanJSON != nil {
		var plan sdk.Plan
		if err := json.Unmarshal(cp.PlanJSON, &plan); err == nil {
			state.PlanState = &plan
		}
	}

	return state, nil
}

// ShouldCheckpoint determines if a checkpoint should be created
// based on the current step and run state
func (cm *Manager) ShouldCheckpoint(stepNumber int, isAfterStep bool, toolName string, lastError error) bool {
	// Checkpoint after every 5 steps
	if stepNumber > 0 && stepNumber%5 == 0 && isAfterStep {
		return true
	}

	// Checkpoint after successful completion of risky tools
	if isAfterStep && cm.isRiskyTool(toolName) {
		return true
	}

	// Checkpoint after a failure (for recovery)
	if isAfterStep && lastError != nil {
		return true
	}

	return false
}

// isRiskyTool returns true if the tool is considered risky
func (cm *Manager) isRiskyTool(toolName string) bool {
	riskyTools := map[string]bool{
		"cmd.exec":         true,
		"write":            true,
		"fs.remove":        true,
		"fs.delete":        true,
		"task":             true,
		"agent.coordinate": true,
	}
	return riskyTools[toolName]
}

// GetCheckpointAge returns how old a checkpoint is
func (cm *Manager) GetCheckpointAge(cp *session.RunCheckpoint) time.Duration {
	return time.Since(cp.CreatedAt)
}
