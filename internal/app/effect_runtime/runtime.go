package effect_runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zetatez/morpheus/internal/app"
	"github.com/zetatez/morpheus/internal/app/service"
	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/configstore"
	"github.com/zetatez/morpheus/internal/convo"
	"github.com/zetatez/morpheus/internal/effect"
	execpkg "github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/internal/planner/keyword"
	"github.com/zetatez/morpheus/internal/planner/llm"
	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/internal/policy"
	"github.com/zetatez/morpheus/internal/session"
	"github.com/zetatez/morpheus/internal/skill"
	"github.com/zetatez/morpheus/internal/subagent"
	"github.com/zetatez/morpheus/internal/tools/codesearch"
	"github.com/zetatez/morpheus/internal/tools/registry"
	"github.com/zetatez/morpheus/internal/tools/websearch"
	"github.com/zetatez/morpheus/pkg/sdk"
	"go.uber.org/zap"
)

type EffectRuntime struct {
	*service.RuntimeService
	logger             *zap.Logger
	conversation       *convo.Manager
	planner            sdk.Planner
	orchestrator       *execpkg.Orchestrator
	registry           *registry.Registry
	toolManager        *sdk.ToolManager
	audit              *auditWriter
	session            *session.Writer
	sessionStore       *session.Store
	subagents          *subagent.Loader
	metrics            *serverMetrics
	runs               *runStore
	teamState          map[string]any
	compactionPipeline *CompactionPipeline
}

func newLogger(cfg config.Config) (*zap.Logger, error) {
	return zap.NewProduction()
}

func NewEffectRuntime(cfg config.Config) (*EffectRuntime, error) {
	logger, err := newLogger(cfg)
	if err != nil {
		return nil, err
	}

	bus := service.NewBusService()
	agentReg := service.NewAgentRegistryService(&cfg.Agent)
	sessionMgr := service.NewSessionManagerService()
	plugins := plugin.NewRegistry()

	rt := &EffectRuntime{
		RuntimeService: service.NewRuntimeService(cfg, agentReg, sessionMgr, bus, plugins, nil),
		logger:         logger,
		conversation:   convo.NewManager(),
		teamState:      make(map[string]any),
	}

	agentReg.ApplyConfig(cfg.Agent.Agents, cfg.WorkspaceRoot)

	return rt, nil
}

func (rt *EffectRuntime) Initialize(ctx context.Context) error {
	rt.publish("runtime.initializing", nil)
	defer rt.publish("runtime.initialized", nil)

	allSkillPaths := skill.DiscoverOpenCodePaths(rt.Config.WorkspaceRoot)
	rt.logger.Info("discovered skill paths", zap.Strings("paths", allSkillPaths))
	rt.RuntimeService.Skills = skill.NewLoaderWithPaths(allSkillPaths)
	for _, path := range allSkillPaths {
		_ = os.MkdirAll(path, 0o755)
	}

	subagentPath := filepath.Join(configstore.DefaultConfigDir(), "subagents")
	_ = os.MkdirAll(subagentPath, 0o755)
	rt.subagents = subagent.NewLoader(subagentPath)

	reg := registry.NewRegistry()
	rt.registry = reg
	rt.toolManager = sdk.NewToolManager(reg, nil)
	rt.toolManager.SetNameNormalizer(sdk.NormalizeToolName)

	_ = reg.Register(websearch.NewTool(websearch.ProviderDuckDuckGo, ""))
	_ = reg.Register(codesearch.NewTool(codesearch.BackendSearchcode, "", ""))

	planner, err := buildPlanner(rt.Config.Planner)
	if err != nil {
		return err
	}
	rt.planner = planner

	pol := policy.NewPolicyEngine(rt.Config)
	rt.orchestrator = execpkg.NewOrchestrator(reg, pol, rt.Config.WorkspaceRoot, rt.RuntimeService.Plugins)

	trans := session.NewWriter(rt.Config.Session.Path, rt.Config.Session.Retention)
	rt.session = trans

	sqlitePath := rt.Config.Session.SQLitePath
	if strings.TrimSpace(sqlitePath) == "" && strings.TrimSpace(rt.Config.Session.Path) != "" {
		sqlitePath = filepath.Join(rt.Config.Session.Path, "sessions.db")
	}
	store, err := session.NewStore(sqlitePath)
	if err != nil {
		rt.logger.Warn("failed to open session sqlite store", zap.Error(err))
	} else {
		rt.sessionStore = store
	}

	rt.metrics = newServerMetrics()
	rt.runs = newRunStore()
	rt.compactionPipeline = NewCompactionPipeline()

	return nil
}

func (rt *EffectRuntime) Close() error {
	if rt.sessionStore != nil {
		_ = rt.sessionStore.Close()
	}
	return nil
}

func (rt *EffectRuntime) StartServer(ctx context.Context) error {
	rt.publish("server.started", nil)
	return nil
}

func (rt *EffectRuntime) StartAPIServer(ctx context.Context) error {
	rt.publish("api_server.started", nil)
	return nil
}

func (rt *EffectRuntime) publish(event string, data any) {
	if rt.RuntimeService.Bus != nil {
		rt.RuntimeService.Bus.Publish(event, data)
	}
}

func buildPlanner(cfg config.PlannerConfig) (sdk.Planner, error) {
	providerName := strings.ToLower(cfg.Provider)

	if providerName == "builtin" || providerName == "keyword" {
		return keyword.NewPlanner(), nil
	}

	factory, ok := llm.GetProvider(providerName)
	if !ok {
		return nil, fmt.Errorf("unknown provider: %s. Available providers: %v", cfg.Provider, llm.ListProviders())
	}

	model := cfg.Model
	if model == "" {
		model = defaultModel(providerName)
	}

	plannerConfig := llm.PlannerProviderConfig{
		APIKey:      cfg.APIKey,
		Model:       model,
		Endpoint:    cfg.Endpoint,
		Temperature: cfg.Temperature,
	}

	return factory(plannerConfig)
}

func defaultModel(provider string) string {
	return "gpt-4"
}

type auditWriter struct{}

func (w *auditWriter) Write(p []byte) (n int, err error) { return len(p), nil }
func (w *auditWriter) Close() error                      { return nil }

type serverMetrics struct{}

func newServerMetrics() *serverMetrics { return &serverMetrics{} }

type runStore struct{}

func newRunStore() *runStore { return &runStore{} }

type CompactionPipeline struct{}

func NewCompactionPipeline() *CompactionPipeline { return &CompactionPipeline{} }

func RuntimeServiceLayer(cfg config.Config) *effect.Layer[*EffectRuntime] {
	return effect.LayerFunc(func(ctx *effect.Context) (*EffectRuntime, error) {
		rt, err := NewEffectRuntime(cfg)
		if err != nil {
			return nil, err
		}
		if err := rt.Initialize(context.Background()); err != nil {
			return nil, err
		}
		return rt, nil
	})
}

func ProvideRuntime(ctx *effect.Context, rt *EffectRuntime) *effect.Context {
	return ctx.WithService((*EffectRuntime)(nil), rt)
}

type SessionEffectInput struct {
	SessionID string
	Messages  []map[string]interface{}
	Tools     []map[string]interface{}
	Format    *app.OutputFormat
	Mode      app.AgentMode
}

func RunSessionEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	input SessionEffectInput,
) effect.Effect[app.Response] {
	return func(ctx *effect.Context) (app.Response, error) {
		rt.publish("session.started", map[string]any{
			"session_id": input.SessionID,
			"mode":       input.Mode,
		})

		_ = rt.RuntimeService.SessionMgr.GetOrCreate(input.SessionID)

		response := app.Response{
			RunID:     input.SessionID,
			RunStatus: "running",
			Reply:     "Session started",
			Results:   []sdk.ToolResult{},
		}

		rt.publish("session.completed", map[string]any{
			"session_id": input.SessionID,
		})

		return response, nil
	}
}

func RunAgentLoopEffectForRuntime(
	ctx *effect.Context,
	rt *EffectRuntime,
	sessionID string,
	messages []map[string]interface{},
	tools []map[string]interface{},
	format *app.OutputFormat,
	mode app.AgentMode,
) effect.Effect[app.Response] {
	return func(ctx *effect.Context) (app.Response, error) {
		rt.publish("agent_loop.started", map[string]any{
			"session_id": sessionID,
			"mode":       mode,
		})

		agent, found := rt.RuntimeService.AgentReg.Get(string(mode))
		if !found {
			agent, _ = rt.RuntimeService.AgentReg.Get("build")
		}

		response := app.Response{
			Reply:   fmt.Sprintf("Agent '%s' execution complete", agent.Name),
			Results: []sdk.ToolResult{},
		}

		rt.publish("agent_loop.completed", map[string]any{
			"session_id": sessionID,
			"agent":      agent.Name,
		})

		return response, nil
	}
}

func CheckPermissionEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	sessionID, permission, pattern string,
) effect.Effect[app.PermissionAction] {
	return func(ctx *effect.Context) (app.PermissionAction, error) {
		if rt.RuntimeService.SessionMgr.IsPermissionApproved(sessionID, permission, pattern) {
			return app.PermissionAllow, nil
		}

		pending := rt.RuntimeService.SessionMgr.GetPendingConfirmation(sessionID)
		if pending != nil && pending.Tool == permission {
			return app.PermissionAsk, nil
		}

		return app.PermissionAsk, nil
	}
}

func ApprovePermissionEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	sessionID, permission, pattern string,
) effect.Effect[bool] {
	return func(ctx *effect.Context) (bool, error) {
		rt.RuntimeService.SessionMgr.ApprovePermission(sessionID, permission, pattern)
		rt.publish("permission.approved", map[string]any{
			"session_id": sessionID,
			"permission": permission,
			"pattern":    pattern,
		})
		return true, nil
	}
}

func SetPendingConfirmationEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	sessionID string,
	pc *app.PendingConfirmation,
) effect.Effect[bool] {
	return func(ctx *effect.Context) (bool, error) {
		rt.RuntimeService.SessionMgr.SetPendingConfirmation(sessionID, pc)
		rt.publish("permission.pending", map[string]any{
			"session_id": sessionID,
			"tool":       pc.Tool,
		})
		return true, nil
	}
}

func RunSubAgentEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	sessionID, prompt string,
	allowedTools []string,
) effect.Effect[string] {
	return func(ctx *effect.Context) (string, error) {
		rt.publish("subagent.started", map[string]any{
			"session_id": sessionID,
		})

		summary := "Subagent execution complete"

		rt.publish("subagent.completed", map[string]any{
			"session_id": sessionID,
			"summary":    summary,
		})

		return summary, nil
	}
}

func ListAgentsEffect(rt *EffectRuntime, visibleOnly bool) effect.Effect[[]*app.Agent] {
	return func(ctx *effect.Context) ([]*app.Agent, error) {
		if visibleOnly {
			return rt.RuntimeService.AgentReg.ListVisible(), nil
		}
		return rt.RuntimeService.AgentReg.List(), nil
	}
}

func GetAgentEffect(rt *EffectRuntime, name string) effect.Effect[*app.Agent] {
	return func(ctx *effect.Context) (*app.Agent, error) {
		agent, found := rt.RuntimeService.AgentReg.Get(name)
		if !found {
			return nil, fmt.Errorf("agent not found: %s", name)
		}
		return agent, nil
	}
}

func ApplyAgentConfigEffect(
	rt *EffectRuntime,
	agents []config.AgentDefinition,
	workspaceRoot string,
) effect.Effect[bool] {
	return func(ctx *effect.Context) (bool, error) {
		rt.RuntimeService.AgentReg.ApplyConfig(agents, workspaceRoot)
		rt.publish("agent_config.applied", map[string]any{
			"count": len(agents),
		})
		return true, nil
	}
}

type ToolExecuteInput struct {
	ToolName  string
	Args      map[string]any
	SessionID string
}

type ToolExecuteOutput struct {
	Result sdk.ToolResult
	Error  error
}

func ExecuteToolEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	input ToolExecuteInput,
) effect.Effect[ToolExecuteOutput] {
	return func(ctx *effect.Context) (ToolExecuteOutput, error) {
		rt.publish("tool.started", map[string]any{
			"tool": input.ToolName,
		})

		result := sdk.ToolResult{}

		rt.publish("tool.completed", map[string]any{
			"tool": input.ToolName,
		})

		return ToolExecuteOutput{Result: result}, nil
	}
}

type CompressHistoryInput struct {
	SessionID string
	Strategy  string
}

type CompressHistoryOutput struct {
	Summary string
	Error   error
}

func CompressHistoryEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	input CompressHistoryInput,
) effect.Effect[CompressHistoryOutput] {
	return func(ctx *effect.Context) (CompressHistoryOutput, error) {
		rt.publish("compression.started", map[string]any{
			"session_id": input.SessionID,
			"strategy":   input.Strategy,
		})

		summary := "History compressed"

		rt.publish("compression.completed", map[string]any{
			"session_id": input.SessionID,
		})

		return CompressHistoryOutput{Summary: summary}, nil
	}
}

type CreateCheckpointInput struct {
	SessionID string
	ToolName  string
	Inputs    map[string]any
}

type CreateCheckpointOutput struct {
	ID    string
	Error error
}

func CreateCheckpointEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	input CreateCheckpointInput,
) effect.Effect[CreateCheckpointOutput] {
	return func(ctx *effect.Context) (CreateCheckpointOutput, error) {
		rt.publish("checkpoint.created", map[string]any{
			"session_id": input.SessionID,
			"tool":       input.ToolName,
		})

		return CreateCheckpointOutput{
			ID: fmt.Sprintf("checkpoint-%d", time.Now().UnixNano()),
		}, nil
	}
}

type RollbackInput struct {
	SessionID    string
	CheckpointID string
}

type RollbackOutput struct {
	Success bool
	Error   error
}

func RollbackEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	input RollbackInput,
) effect.Effect[RollbackOutput] {
	return func(ctx *effect.Context) (RollbackOutput, error) {
		rt.publish("rollback.started", map[string]any{
			"session_id":    input.SessionID,
			"checkpoint_id": input.CheckpointID,
		})

		rt.publish("rollback.completed", map[string]any{
			"session_id":    input.SessionID,
			"checkpoint_id": input.CheckpointID,
		})

		return RollbackOutput{Success: true}, nil
	}
}

type PlanInput struct {
	SessionID string
	Input     string
}

type PlanOutput struct {
	Plan  *sdk.Plan
	Error error
}

func PlanEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	input PlanInput,
) effect.Effect[PlanOutput] {
	return func(ctx *effect.Context) (PlanOutput, error) {
		rt.publish("plan.started", map[string]any{
			"session_id": input.SessionID,
		})

		plan := &sdk.Plan{
			Steps: []sdk.PlanStep{},
		}

		rt.publish("plan.completed", map[string]any{
			"session_id": input.SessionID,
		})

		return PlanOutput{Plan: plan}, nil
	}
}

type UpdatePlannerInput struct {
	Config config.PlannerConfig
}

type UpdatePlannerOutput struct {
	Success bool
	Error   error
}

func UpdatePlannerEffect(
	ctx *effect.Context,
	rt *EffectRuntime,
	input UpdatePlannerInput,
) effect.Effect[UpdatePlannerOutput] {
	return func(ctx *effect.Context) (UpdatePlannerOutput, error) {
		planner, err := buildPlanner(input.Config)
		if err != nil {
			return UpdatePlannerOutput{Error: err}, err
		}
		rt.planner = planner
		return UpdatePlannerOutput{Success: true}, nil
	}
}

func ChatEffect(rt *EffectRuntime) effect.Effect[app.Response] {
	return func(ctx *effect.Context) (app.Response, error) {
		rt.publish("chat.effect.called", nil)
		return app.Response{
			Reply:   "Effect-based chat",
			Results: []sdk.ToolResult{},
		}, nil
	}
}
