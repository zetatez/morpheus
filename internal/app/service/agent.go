package service

import (
	"github.com/zetatez/morpheus/internal/app"
	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/effect"
)

type AgentRegistryService struct {
	registry *app.AgentRegistry
}

func NewAgentRegistryService(cfg *config.AgentConfig) *AgentRegistryService {
	registry := app.NewAgentRegistry()
	return &AgentRegistryService{registry: registry}
}

func (s *AgentRegistryService) Get(name string) (*app.Agent, bool) {
	return s.registry.Get(name)
}

func (s *AgentRegistryService) List() []*app.Agent {
	return s.registry.List()
}

func (s *AgentRegistryService) ListVisible() []*app.Agent {
	return s.registry.ListVisible()
}

func (s *AgentRegistryService) ListSubagents() []*app.Agent {
	return s.registry.ListSubagents()
}

func (s *AgentRegistryService) ApplyConfig(agents []config.AgentDefinition, workspaceRoot string) {
	s.registry.ApplyConfig(agents, workspaceRoot)
}

func AgentRegistryLayer(cfg *config.AgentConfig) *effect.Layer[*AgentRegistryService] {
	return effect.LayerFunc(func(ctx *effect.Context) (*AgentRegistryService, error) {
		return NewAgentRegistryService(cfg), nil
	})
}

type SessionManagerService struct {
	manager *app.SessionManager
}

func NewSessionManagerService() *SessionManagerService {
	return &SessionManagerService{manager: app.NewSessionManager()}
}

func (s *SessionManagerService) Manager() *app.SessionManager {
	return s.manager
}

func (s *SessionManagerService) GetOrCreate(sessionID string) *app.SessionState {
	return s.manager.GetOrCreate(sessionID)
}

func (s *SessionManagerService) IsPermissionApproved(sessionID, permission, pattern string) bool {
	return s.manager.IsPermissionApproved(sessionID, permission, pattern)
}

func (s *SessionManagerService) ApprovePermission(sessionID, permission, pattern string) {
	s.manager.ApprovePermission(sessionID, permission, pattern)
}

func (s *SessionManagerService) GetPendingConfirmation(sessionID string) *app.PendingConfirmation {
	return s.manager.GetPendingConfirmation(sessionID)
}

func (s *SessionManagerService) SetPendingConfirmation(sessionID string, pc *app.PendingConfirmation) {
	s.manager.SetPendingConfirmation(sessionID, pc)
}

func (s *SessionManagerService) GetAndClearPendingConfirmation(sessionID string) (*app.PendingConfirmation, bool) {
	return s.manager.GetAndClearPendingConfirmation(sessionID)
}

func SessionManagerLayer() *effect.Layer[*SessionManagerService] {
	return effect.LayerFunc(func(ctx *effect.Context) (*SessionManagerService, error) {
		return NewSessionManagerService(), nil
	})
}
