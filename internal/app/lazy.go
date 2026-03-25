package app

import (
	"sync"

	"github.com/zetatez/morpheus/internal/convo"
	execpkg "github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/internal/session"
	"github.com/zetatez/morpheus/internal/skill"
	"github.com/zetatez/morpheus/internal/subagent"
	"github.com/zetatez/morpheus/internal/tools/registry"
	"github.com/zetatez/morpheus/pkg/sdk"
	"go.uber.org/zap"
)

type Lazy[T any] struct {
	init func() T
	val  T
	once sync.Once
}

func NewLazy[T any](init func() T) *Lazy[T] {
	return &Lazy[T]{init: init}
}

func (l *Lazy[T]) Get() T {
	l.once.Do(func() { l.val = l.init() })
	return l.val
}

func (l *Lazy[T]) IsInitialized() bool {
	l.once.Do(func() {})
	return true
}

type RuntimeOption func(*Runtime)

func WithPlanner(p sdk.Planner) RuntimeOption {
	return func(r *Runtime) { r.planner = p }
}

func WithConversationManager(c *convo.Manager) RuntimeOption {
	return func(r *Runtime) { r.conversation = c }
}

func WithOrchestrator(o *execpkg.Orchestrator) RuntimeOption {
	return func(r *Runtime) { r.orchestrator = o }
}

func WithToolRegistry(reg *registry.Registry) RuntimeOption {
	return func(r *Runtime) { r.registry = reg }
}

func WithToolManager(tm *sdk.ToolManager) RuntimeOption {
	return func(r *Runtime) { r.toolManager = tm }
}

func WithSessionWriter(w *session.Writer) RuntimeOption {
	return func(r *Runtime) { r.session = w }
}

func WithSessionStore(s *session.Store) RuntimeOption {
	return func(r *Runtime) { r.sessionStore = s }
}

func WithPluginRegistry(p *plugin.Registry) RuntimeOption {
	return func(r *Runtime) { r.plugins = p }
}

func WithSkillLoader(s *skill.Loader) RuntimeOption {
	return func(r *Runtime) { r.skills = s }
}

func WithSubagentLoader(s *subagent.Loader) RuntimeOption {
	return func(r *Runtime) { r.subagents = s }
}

func WithLogger(log *zap.Logger) RuntimeOption {
	return func(r *Runtime) { r.logger = log }
}

func WithMetrics(m *serverMetrics) RuntimeOption {
	return func(r *Runtime) { r.metrics = m }
}
