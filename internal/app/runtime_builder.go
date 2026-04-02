package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/configstore"
	"github.com/zetatez/morpheus/internal/convo"
	execpkg "github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/internal/session"
	"github.com/zetatez/morpheus/internal/skill"
	"github.com/zetatez/morpheus/internal/subagent"
	"github.com/zetatez/morpheus/internal/tools/agenttool"
	"github.com/zetatez/morpheus/internal/tools/lsp"
	"github.com/zetatez/morpheus/internal/tools/registry"
	"github.com/zetatez/morpheus/internal/tools/skilltool"
	"github.com/zetatez/morpheus/internal/tools/todotool"
	"github.com/zetatez/morpheus/pkg/sdk"
	"go.uber.org/zap"
)

type RuntimeBuilder struct {
	cfg          config.Config
	logger       *zap.Logger
	planner      *Lazy[sdk.Planner]
	orchestrator *Lazy[*execpkg.Orchestrator]
	registry     *Lazy[*registry.Registry]
	plugins      *Lazy[*plugin.Registry]
	skills       *Lazy[*skill.Loader]
	subagents    *Lazy[*subagent.Loader]
	session      *Lazy[*session.Writer]
	sessionStore *Lazy[*session.Store]
	audit        *Lazy[*auditWriter]
	metrics      *Lazy[*serverMetrics]

	initMu      sync.Mutex
	initialized bool
}

func NewRuntimeBuilder(cfg config.Config) *RuntimeBuilder {
	return &RuntimeBuilder{
		cfg:          cfg,
		planner:      NewLazy(func() sdk.Planner { return nil }),
		orchestrator: NewLazy(func() *execpkg.Orchestrator { return nil }),
		registry:     NewLazy(func() *registry.Registry { return nil }),
		plugins:      NewLazy(func() *plugin.Registry { return nil }),
		skills:       NewLazy(func() *skill.Loader { return nil }),
		subagents:    NewLazy(func() *subagent.Loader { return nil }),
		session:      NewLazy(func() *session.Writer { return nil }),
		sessionStore: NewLazy(func() *session.Store { return nil }),
		audit:        NewLazy(func() *auditWriter { return nil }),
		metrics:      NewLazy(func() *serverMetrics { return nil }),
	}
}

func (b *RuntimeBuilder) WithLogger(log *zap.Logger) *RuntimeBuilder {
	b.logger = log
	return b
}

func (b *RuntimeBuilder) WithPlannerProvider(p sdk.Planner) *RuntimeBuilder {
	b.planner = NewLazy(func() sdk.Planner { return p })
	return b
}

func (b *RuntimeBuilder) WithPlannerLazy(p *Lazy[sdk.Planner]) *RuntimeBuilder {
	b.planner = p
	return b
}

func (b *RuntimeBuilder) WithOrchestratorLazy(o *Lazy[*execpkg.Orchestrator]) *RuntimeBuilder {
	b.orchestrator = o
	return b
}

func (b *RuntimeBuilder) WithRegistryLazy(r *Lazy[*registry.Registry]) *RuntimeBuilder {
	b.registry = r
	return b
}

func (b *RuntimeBuilder) WithPluginsLazy(p *Lazy[*plugin.Registry]) *RuntimeBuilder {
	b.plugins = p
	return b
}

func (b *RuntimeBuilder) WithSkillsLazy(s *Lazy[*skill.Loader]) *RuntimeBuilder {
	b.skills = s
	return b
}

func (b *RuntimeBuilder) WithSubagentsLazy(s *Lazy[*subagent.Loader]) *RuntimeBuilder {
	b.subagents = s
	return b
}

func (b *RuntimeBuilder) WithSessionWriterLazy(s *Lazy[*session.Writer]) *RuntimeBuilder {
	b.session = s
	return b
}

func (b *RuntimeBuilder) WithSessionStoreLazy(s *Lazy[*session.Store]) *RuntimeBuilder {
	b.sessionStore = s
	return b
}

func (b *RuntimeBuilder) WithAuditLazy(a *Lazy[*auditWriter]) *RuntimeBuilder {
	b.audit = a
	return b
}

func (b *RuntimeBuilder) WithMetricsLazy(m *Lazy[*serverMetrics]) *RuntimeBuilder {
	b.metrics = m
	return b
}

func (b *RuntimeBuilder) Build(ctx context.Context) (*Runtime, error) {
	b.initMu.Lock()
	defer b.initMu.Unlock()

	if b.initialized {
		return nil, ErrRuntimeAlreadyBuilt
	}
	b.initialized = true

	var logger *zap.Logger
	if b.logger != nil {
		logger = b.logger
	} else {
		var err error
		logger, err = newLogger(b.cfg)
		if err != nil {
			return nil, err
		}
	}

	conv := convo.NewManager()
	pluginsVal := b.plugins.Get()
	if pluginsVal == nil {
		pluginsVal = plugin.NewRegistry()
		b.plugins = NewLazy(func() *plugin.Registry { return pluginsVal })
	}
	if soulPrompt, err := loadSoulPrompt(); err != nil {
		logger.Warn("failed to load SOUL.md", zap.Error(err))
	} else if soulPrompt != "" {
		fullPrompt := soulPrompt
		if contextFiles, err := loadContextFiles(b.cfg.WorkspaceRoot); err == nil && contextFiles != "" {
			fullPrompt = soulPrompt + "\n\n" + contextFiles
			logger.Info("context files loaded", zap.Int("chars", len(contextFiles)))
		}
		conv.SetSystemPrompt(pluginsVal.ApplySystem(plugin.SystemContext{SessionID: ""}, fullPrompt))
		logger.Info("SOUL.md loaded", zap.Int("chars", len(soulPrompt)))
	}

	regVal := b.registry.Get()
	if regVal == nil {
		regVal = registry.NewRegistry()
		b.registry = NewLazy(func() *registry.Registry { return regVal })
	}
	allSkillPaths := skill.DiscoverOpenCodePaths(b.cfg.WorkspaceRoot)
	logger.Info("discovered skill paths", zap.Strings("paths", allSkillPaths))
	skillsVal := b.skills.Get()
	if skillsVal == nil {
		skillsVal = skill.NewLoaderWithPaths(allSkillPaths)
		b.skills = NewLazy(func() *skill.Loader { return skillsVal })
	}
	for _, path := range allSkillPaths {
		_ = os.MkdirAll(path, 0o755)
	}
	subagentPath := filepath.Join(configstore.DefaultConfigDir(), "subagents")
	_ = os.MkdirAll(subagentPath, 0o755)
	subagentsVal := b.subagents.Get()
	if subagentsVal == nil {
		subagentsVal = subagent.NewLoader(subagentPath)
		b.subagents = NewLazy(func() *subagent.Loader { return subagentsVal })
	}

	plannerVal := b.planner.Get()
	if plannerVal == nil {
		var err error
		plannerVal, err = buildPlanner(b.cfg.Planner)
		if err != nil {
			return nil, err
		}
		b.planner = NewLazy(func() sdk.Planner { return plannerVal })
	}

	auditVal := b.audit.Get()
	if auditVal == nil {
		var err error
		auditVal, err = newAuditWriter(b.cfg.Logging.File)
		if err != nil {
			return nil, err
		}
		b.audit = NewLazy(func() *auditWriter { return auditVal })
	}

	sessionVal := b.session.Get()
	if sessionVal == nil {
		sessionVal = session.NewWriter(b.cfg.Session.Path, b.cfg.Session.Retention)
		b.session = NewLazy(func() *session.Writer { return sessionVal })
	}

	sqlitePath := b.cfg.Session.SQLitePath
	if strings.TrimSpace(sqlitePath) == "" && strings.TrimSpace(b.cfg.Session.Path) != "" {
		sqlitePath = filepath.Join(b.cfg.Session.Path, "sessions.db")
	}
	sessionStoreVal := b.sessionStore.Get()
	if sessionStoreVal == nil {
		var err error
		sessionStoreVal, err = session.NewStore(sqlitePath)
		if err != nil {
			logger.Warn("failed to open session sqlite store", zap.Error(err))
		} else if sessionStoreVal != nil {
			if err := sessionStoreVal.EnsureRunSchema(ctx); err != nil {
				logger.Warn("failed to ensure run sqlite schema", zap.Error(err))
			}
		}
		b.sessionStore = NewLazy(func() *session.Store { return sessionStoreVal })
	}

	metricsVal := b.metrics.Get()
	if metricsVal == nil {
		metricsVal = newServerMetrics()
		b.metrics = NewLazy(func() *serverMetrics { return metricsVal })
	}

	rt := &Runtime{
		cfg:           b.cfg,
		logger:        logger,
		conversation:  conv,
		planner:       plannerVal,
		orchestrator:  nil,
		registry:      regVal,
		audit:         auditVal,
		session:       sessionVal,
		sessionStore:  sessionStoreVal,
		plugins:       pluginsVal,
		skills:        skillsVal,
		agentRegistry: NewAgentRegistry(b.cfg.Agent),
		subagents:     subagentsVal,
		metrics:       metricsVal,
		runs:          newRunStore(),
	}

	if reg, ok := rt.registry.Get("agent.run"); ok {
		if agent, ok := reg.(*agenttool.Tool); ok {
			*agent = *agenttool.New(rt)
		}
	}
	if reg, ok := rt.registry.Get("agent.coordinate"); ok {
		if coordinator, ok := reg.(*agenttool.CoordinatorTool); ok {
			*coordinator = *agenttool.NewCoordinator(rt)
		}
		if messageTool, ok := reg.(*agenttool.MessageTool); ok {
			*messageTool = *agenttool.NewMessage(rt)
		}
	}
	if reg, ok := rt.registry.Get("skill.invoke"); ok {
		if skillInvoke, ok := reg.(*skilltool.Tool); ok {
			*skillInvoke = *skilltool.New(skillsVal, rt.ensureSkillAllowed)
		}
	}
	if reg, ok := rt.registry.Get("todo.write"); ok {
		if todoWrite, ok := reg.(*todotool.Tool); ok {
			*todoWrite = *todotool.New(rt)
		}
	}
	if reg, ok := rt.registry.Get("lsp.query"); ok {
		if lspTool, ok := reg.(*lsp.Tool); ok {
			lsp.RegisterHooks(pluginsVal, lspTool)
		}
	}

	rt.recoverRunsOnStartup(ctx)
	return rt, nil
}

var ErrRuntimeAlreadyBuilt = &RuntimeBuilderError{"runtime already built"}

type RuntimeBuilderError struct {
	msg string
}

func (e *RuntimeBuilderError) Error() string {
	return e.msg
}
