package service

import (
	"context"

	"github.com/zetatez/morpheus/internal/app"
	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/effect"
	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/internal/skill"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type RuntimeService struct {
	Config     config.Config
	Bus        *BusService
	AgentReg   *AgentRegistryService
	SessionMgr *SessionManagerService
	Plugins    *plugin.Registry
	Skills     *skill.Loader
}

func (s *RuntimeService) ServiceName() string {
	return "@morpheus/Runtime"
}

func NewRuntimeService(
	cfg config.Config,
	agentReg *AgentRegistryService,
	sessionMgr *SessionManagerService,
	bus *BusService,
	plugins *plugin.Registry,
	skills *skill.Loader,
) *RuntimeService {
	return &RuntimeService{
		Config:     cfg,
		AgentReg:   agentReg,
		SessionMgr: sessionMgr,
		Bus:        bus,
		Plugins:    plugins,
		Skills:     skills,
	}
}

func RuntimeServiceLayer(
	cfg config.Config,
	agentReg *AgentRegistryService,
	sessionMgr *SessionManagerService,
	bus *BusService,
	plugins *plugin.Registry,
	skills *skill.Loader,
) *effect.Layer[*RuntimeService] {
	return effect.LayerFunc(func(ctx *effect.Context) (*RuntimeService, error) {
		return NewRuntimeService(cfg, agentReg, sessionMgr, bus, plugins, skills), nil
	})
}

type AgentLoopInput struct {
	SessionID string
	Messages  []map[string]interface{}
	Tools     []map[string]interface{}
	Format    *app.OutputFormat
	Mode      app.AgentMode
}

type AgentLoopResult struct {
	Reply   string
	Results []sdk.ToolResult
	Todos   []map[string]any
}

type AgentLoopExecutor struct {
	cfg        config.Config
	agentReg   *AgentRegistryService
	sessionMgr *SessionManagerService
	bus        *BusService
}

func NewAgentLoopExecutor(
	cfg config.Config,
	agentReg *AgentRegistryService,
	sessionMgr *SessionManagerService,
	bus *BusService,
) *AgentLoopExecutor {
	return &AgentLoopExecutor{
		cfg:        cfg,
		agentReg:   agentReg,
		sessionMgr: sessionMgr,
		bus:        bus,
	}
}

func (e *AgentLoopExecutor) Execute(ctx context.Context, input AgentLoopInput) (*AgentLoopResult, error) {
	agent, ok := e.agentReg.Get(string(input.Mode))
	if !ok {
		agent, _ = e.agentReg.Get("build")
	}

	if e.bus != nil {
		e.bus.Publish("agent.started", map[string]any{
			"session_id": input.SessionID,
			"agent":      agent.Name,
		})
	}

	result := &AgentLoopResult{
		Reply:   "Agent execution complete",
		Results: []sdk.ToolResult{},
		Todos:   []map[string]any{},
	}

	if e.bus != nil {
		e.bus.Publish("agent.completed", map[string]any{
			"session_id": input.SessionID,
			"agent":      agent.Name,
			"result":     result,
		})
	}

	return result, nil
}

func RunAgentLoop(
	ctx *effect.Context,
	runtime *RuntimeService,
	input AgentLoopInput,
) effect.Effect[*AgentLoopResult] {
	return func(ctx *effect.Context) (*AgentLoopResult, error) {
		executor := NewAgentLoopExecutor(
			runtime.Config,
			runtime.AgentReg,
			runtime.SessionMgr,
			runtime.Bus,
		)
		return executor.Execute(context.Background(), input)
	}
}
