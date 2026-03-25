package app

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/zetatez/morpheus/internal/session"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type SessionTaskState struct {
	mu           sync.RWMutex
	LastTaskNote string
	IsCodeTask   bool
}

type MCPSessionInfo struct{}

type SessionMemoryState struct {
	mu        sync.RWMutex
	ShortTerm string
	LongTerm  string
}

type SessionIntentState struct {
	mu      sync.RWMutex
	Entries map[string]intentClassification
}

type TeamStateData struct {
	SharedContext map[string]string
}

type SessionCheckpointState struct {
	mu               sync.RWMutex
	Entries          []session.CheckpointMetadata
	LastCheckpointAt time.Time
	Seq              int64
}

type SessionCompressionState struct {
	mu               sync.Mutex
	LastCompressedAt time.Time
}

type PendingConfirmation struct {
	Tool      string
	Inputs    map[string]any
	Decision  sdk.PolicyDecision
	Kind      string
	Patterns  []string
	CreatedAt time.Time
}

type PermissionReply string

const (
	PermissionReplyOnce   PermissionReply = "once"
	PermissionReplyAlways PermissionReply = "always"
	PermissionReplyReject PermissionReply = "reject"
)

type ApprovedPermission struct {
	Permission string
	Pattern    string
	CreatedAt  time.Time
}

type SessionMeta struct {
	ID          string
	ParentID    string
	ForkID      string
	Shared      bool
	SharedURL   string
	SharedBy    string
	Archived    bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
	Title       string
	Description string
	Tags        []string
	ProjectRoot string
}

type SessionState struct {
	mu                  sync.RWMutex
	Meta                *SessionMeta
	AllowedSkills       map[string]struct{}
	AllowedSubagents    map[string]struct{}
	TaskState           *SessionTaskState
	MCPSessions         map[string]*MCPSessionInfo
	PendingConfirm      *PendingConfirmation
	SessionMemory       *SessionMemoryState
	MemorySystem        *MemorySystem
	IntentCache         *SessionIntentState
	TeamState           *TeamStateData
	Checkpoints         *SessionCheckpointState
	CompressionState    *SessionCompressionState
	ApprovedPermissions []ApprovedPermission
}

type RiskLevel int

const (
	RiskLevelUnknown RiskLevel = iota
	RiskLevelLow
	RiskLevelMedium
	RiskLevelHigh
	RiskLevelCritical
)

func (r RiskLevel) String() string {
	switch r {
	case RiskLevelLow:
		return "low"
	case RiskLevelMedium:
		return "medium"
	case RiskLevelHigh:
		return "high"
	case RiskLevelCritical:
		return "critical"
	default:
		return "unknown"
	}
}

func NewSessionState() *SessionState {
	return &SessionState{
		AllowedSkills:       make(map[string]struct{}),
		AllowedSubagents:    make(map[string]struct{}),
		TaskState:           &SessionTaskState{},
		MCPSessions:         make(map[string]*MCPSessionInfo),
		SessionMemory:       &SessionMemoryState{},
		IntentCache:         &SessionIntentState{Entries: make(map[string]intentClassification)},
		TeamState:           &TeamStateData{SharedContext: make(map[string]string)},
		Checkpoints:         &SessionCheckpointState{},
		CompressionState:    &SessionCompressionState{},
		ApprovedPermissions: []ApprovedPermission{},
	}
}

type SessionManager struct {
	mu         sync.RWMutex
	states     map[string]*SessionState
	forkIndex  map[string][]string
	shareIndex map[string]string
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		states:     make(map[string]*SessionState),
		forkIndex:  make(map[string][]string),
		shareIndex: make(map[string]string),
	}
}

func (sm *SessionManager) GetOrCreate(sessionID string) *SessionState {
	sm.mu.RLock()
	if state, ok := sm.states[sessionID]; ok {
		sm.mu.RUnlock()
		return state
	}
	sm.mu.RUnlock()

	sm.mu.Lock()
	defer sm.mu.Unlock()
	if state, ok := sm.states[sessionID]; ok {
		return state
	}
	state := NewSessionState()
	state.Meta = &SessionMeta{
		ID:        sessionID,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	sm.states[sessionID] = state
	return state
}

func (sm *SessionManager) Get(sessionID string) (*SessionState, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	state, ok := sm.states[sessionID]
	return state, ok
}

func (sm *SessionManager) Fork(sessionID string, newSessionID string) (*SessionState, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	parent, ok := sm.states[sessionID]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", sessionID)
	}

	if parent.Meta != nil && parent.Meta.Shared {
		return nil, fmt.Errorf("cannot fork a shared session, unshare first")
	}

	child := &SessionState{
		AllowedSkills:       make(map[string]struct{}),
		AllowedSubagents:    make(map[string]struct{}),
		TaskState:           &SessionTaskState{},
		MCPSessions:         make(map[string]*MCPSessionInfo),
		SessionMemory:       &SessionMemoryState{},
		IntentCache:         &SessionIntentState{Entries: make(map[string]intentClassification)},
		TeamState:           &TeamStateData{SharedContext: make(map[string]string)},
		Checkpoints:         &SessionCheckpointState{},
		CompressionState:    &SessionCompressionState{},
		ApprovedPermissions: make([]ApprovedPermission, len(parent.ApprovedPermissions)),
	}

	copy(child.ApprovedPermissions, parent.ApprovedPermissions)

	for k, v := range parent.AllowedSkills {
		child.AllowedSkills[k] = v
	}
	for k, v := range parent.AllowedSubagents {
		child.AllowedSubagents[k] = v
	}

	child.Meta = &SessionMeta{
		ID:          newSessionID,
		ParentID:    sessionID,
		ForkID:      generateID(8),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
		ProjectRoot: parent.Meta.ProjectRoot,
	}

	sm.states[newSessionID] = child
	sm.forkIndex[sessionID] = append(sm.forkIndex[sessionID], newSessionID)

	return child, nil
}

func (sm *SessionManager) Share(sessionID string, sharedBy string) (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.states[sessionID]
	if !ok {
		return "", fmt.Errorf("session not found: %s", sessionID)
	}

	if state.Meta == nil {
		state.Meta = &SessionMeta{}
	}

	shareID := generateID(12)
	state.Meta.Shared = true
	state.Meta.SharedURL = fmt.Sprintf("/share/%s", shareID)
	state.Meta.SharedBy = sharedBy
	state.Meta.UpdatedAt = time.Now()

	sm.shareIndex[shareID] = sessionID

	return shareID, nil
}

func (sm *SessionManager) GetByShare(shareID string) (*SessionState, string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	sessionID, ok := sm.shareIndex[shareID]
	if !ok {
		return nil, "", false
	}

	state, ok := sm.states[sessionID]
	return state, sessionID, ok
}

func (sm *SessionManager) Unshare(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.states[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if state.Meta == nil {
		return nil
	}

	shareID := ""
	for sid, sid2 := range sm.shareIndex {
		if sid2 == sessionID {
			shareID = sid
			break
		}
	}

	if shareID != "" {
		delete(sm.shareIndex, shareID)
	}

	state.Meta.Shared = false
	state.Meta.SharedURL = ""
	state.Meta.SharedBy = ""
	state.Meta.UpdatedAt = time.Now()

	return nil
}

func (sm *SessionManager) Archive(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.states[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if state.Meta == nil {
		state.Meta = &SessionMeta{}
	}

	state.Meta.Archived = true
	state.Meta.UpdatedAt = time.Now()
	return nil
}

func (sm *SessionManager) Unarchive(sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.states[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if state.Meta == nil {
		return nil
	}

	state.Meta.Archived = false
	state.Meta.UpdatedAt = time.Now()
	return nil
}

func (sm *SessionManager) GetForks(sessionID string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.forkIndex[sessionID]
}

func (sm *SessionManager) GetParent(sessionID string) (string, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	state, ok := sm.states[sessionID]
	if !ok || state.Meta == nil {
		return "", false
	}
	return state.Meta.ParentID, state.Meta.ParentID != ""
}

func (sm *SessionManager) List(projectRoot string, includeArchived bool) []*SessionMeta {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	var result []*SessionMeta
	for _, state := range sm.states {
		if state.Meta == nil {
			continue
		}
		if state.Meta.ProjectRoot != projectRoot {
			continue
		}
		if !includeArchived && state.Meta.Archived {
			continue
		}
		result = append(result, state.Meta)
	}
	return result
}

func (sm *SessionManager) UpdateMeta(sessionID string, meta *SessionMeta) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.states[sessionID]
	if !ok {
		return fmt.Errorf("session not found: %s", sessionID)
	}

	if state.Meta == nil {
		state.Meta = &SessionMeta{}
	}

	if meta.Title != "" {
		state.Meta.Title = meta.Title
	}
	if meta.Description != "" {
		state.Meta.Description = meta.Description
	}
	if meta.Tags != nil {
		state.Meta.Tags = meta.Tags
	}
	state.Meta.UpdatedAt = time.Now()

	return nil
}

func (sm *SessionManager) Delete(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	state, ok := sm.states[sessionID]
	if ok && state.Meta != nil {
		if state.Meta.ForkID != "" {
			delete(sm.shareIndex, state.Meta.ForkID)
		}
	}

	delete(sm.states, sessionID)
}

func (sm *SessionManager) Clear() {
	sm.mu.Lock()
	sm.states = make(map[string]*SessionState)
	sm.forkIndex = make(map[string][]string)
	sm.shareIndex = make(map[string]string)
	sm.mu.Unlock()
}

func (sm *SessionManager) GetAllPendingConfirmations() map[string]*PendingConfirmation {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make(map[string]*PendingConfirmation)
	for id, state := range sm.states {
		if state.PendingConfirm != nil {
			result[id] = state.PendingConfirm
		}
	}
	return result
}

func (sm *SessionManager) GetAndClearPendingConfirmation(sessionID string) (*PendingConfirmation, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	state, ok := sm.states[sessionID]
	if !ok || state.PendingConfirm == nil {
		return nil, false
	}
	pc := state.PendingConfirm
	state.PendingConfirm = nil
	return pc, true
}

func (sm *SessionManager) GetAllMCPSessions() map[string]*MCPSessionInfo {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	result := make(map[string]*MCPSessionInfo)
	for _, state := range sm.states {
		for name, info := range state.MCPSessions {
			result[name] = info
		}
	}
	return result
}

func (sm *SessionManager) IsPermissionApproved(sessionID, permission, pattern string) bool {
	state := sm.GetOrCreate(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	for _, approved := range state.ApprovedPermissions {
		if approved.Permission == permission && (approved.Pattern == "*" || approved.Pattern == pattern) {
			return true
		}
	}
	return false
}

func (sm *SessionManager) ApprovePermission(sessionID, permission, pattern string) {
	state := sm.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.ApprovedPermissions = append(state.ApprovedPermissions, ApprovedPermission{
		Permission: permission,
		Pattern:    pattern,
		CreatedAt:  time.Now(),
	})
}

func (sm *SessionManager) ClearApprovedPermissions(sessionID string) {
	state := sm.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.ApprovedPermissions = nil
}

func (sm *SessionManager) GetPendingConfirmation(sessionID string) *PendingConfirmation {
	state := sm.GetOrCreate(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.PendingConfirm
}

func (sm *SessionManager) SetPendingConfirmation(sessionID string, pc *PendingConfirmation) {
	state := sm.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.PendingConfirm = pc
}

func generateID(length int) string {
	bytes := make([]byte, length/2)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
