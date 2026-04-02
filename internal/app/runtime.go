package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/configstore"
	"github.com/zetatez/morpheus/internal/convo"
	execpkg "github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/internal/planner/keyword"
	"github.com/zetatez/morpheus/internal/planner/llm"
	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/internal/policy"
	"github.com/zetatez/morpheus/internal/session"
	"github.com/zetatez/morpheus/internal/skill"
	"github.com/zetatez/morpheus/internal/subagent"
	"github.com/zetatez/morpheus/internal/tools/agenttool"
	"github.com/zetatez/morpheus/internal/tools/ask"
	cmdtool "github.com/zetatez/morpheus/internal/tools/cmd"
	fstool "github.com/zetatez/morpheus/internal/tools/fs"
	"github.com/zetatez/morpheus/internal/tools/lsp"
	"github.com/zetatez/morpheus/internal/tools/mcp"
	"github.com/zetatez/morpheus/internal/tools/registry"
	respondtool "github.com/zetatez/morpheus/internal/tools/respond"
	"github.com/zetatez/morpheus/internal/tools/skilltool"
	"github.com/zetatez/morpheus/internal/tools/todotool"
	"github.com/zetatez/morpheus/internal/tools/webfetch"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type Runtime struct {
	cfg                  config.Config
	logger               *zap.Logger
	conversation         *convo.Manager
	planner              sdk.Planner
	orchestrator         *execpkg.Orchestrator
	registry             *registry.Registry
	audit                *auditWriter
	session              *session.Writer
	sessionStore         *session.Store
	plugins              *plugin.Registry
	skills               *skill.Loader
	allowedSkills        sync.Map
	allowedSubagents     sync.Map
	taskState            sync.Map
	mcpSessions          sync.Map
	pendingConfirmations sync.Map
	agentRegistry        *AgentRegistry
	sessionMemory        sync.Map
	intentCache          sync.Map
	teamState            sync.Map
	subagents            *subagent.Loader
	metrics              *serverMetrics
	runs                 *runStore
	checkpoints          sync.Map
}

type sessionTaskState struct {
	mu           sync.RWMutex
	lastTaskNote string
	isCodeTask   bool
}

type sessionMemoryState struct {
	mu        sync.RWMutex
	shortTerm string
	longTerm  string
}

type sessionIntentState struct {
	mu      sync.RWMutex
	entries map[string]intentClassification
}

type sessionCheckpointState struct {
	mu               sync.RWMutex
	entries          []session.CheckpointMetadata
	lastCheckpointAt time.Time
	seq              int64
}

type pendingConfirmation struct {
	Tool     string
	Inputs   map[string]any
	Decision sdk.PolicyDecision
	Kind     string
}

type ConfirmationDecision struct {
	Reason       string   `json:"reason,omitempty"`
	RuleName     string   `json:"rule_name,omitempty"`
	RiskLevel    string   `json:"risk_level,omitempty"`
	RiskScore    int      `json:"risk_score,omitempty"`
	Alternatives []string `json:"alternatives,omitempty"`
	Suggestions  []string `json:"suggestions,omitempty"`
}

type ConfirmationPayload struct {
	Tool     string               `json:"tool"`
	Inputs   map[string]any       `json:"inputs,omitempty"`
	Decision ConfirmationDecision `json:"decision,omitempty"`
}

type AgentMode string

const (
	AgentModeBuild AgentMode = "build"
	AgentModePlan  AgentMode = "plan"
)

// Response wraps plan execution output for callers.
type Response struct {
	RunID        string               `json:"run_id,omitempty"`
	RunStatus    string               `json:"run_status,omitempty"`
	Plan         sdk.Plan             `json:"plan"`
	Results      []sdk.ToolResult     `json:"results"`
	Reply        string               `json:"reply"`
	Confirmation *ConfirmationPayload `json:"confirmation,omitempty"`
	Todos        []map[string]any     `json:"todos,omitempty"`
}

// NewRuntime statically wires the baseline BruteCode stack.
func NewRuntime(ctx context.Context, cfg config.Config) (*Runtime, error) {
	logger, err := newLogger(cfg)
	if err != nil {
		return nil, err
	}
	conv := convo.NewManager()
	plugins := plugin.NewRegistry()
	if soulPrompt, err := loadSoulPrompt(); err != nil {
		logger.Warn("failed to load SOUL.md", zap.Error(err))
	} else if soulPrompt != "" {
		fullPrompt := soulPrompt
		if contextFiles, err := loadContextFiles(cfg.WorkspaceRoot); err == nil && contextFiles != "" {
			fullPrompt = soulPrompt + "\n\n" + contextFiles
			logger.Info("context files loaded", zap.Int("chars", len(contextFiles)))
		}
		conv.SetSystemPrompt(plugins.ApplySystem(plugin.SystemContext{SessionID: ""}, fullPrompt))
		logger.Info("SOUL.md loaded", zap.Int("chars", len(soulPrompt)))
	}
	reg := registry.NewRegistry()
	allSkillPaths := skill.DiscoverOpenCodePaths(cfg.WorkspaceRoot)
	logger.Info("discovered skill paths", zap.Strings("paths", allSkillPaths))
	skills := skill.NewLoaderWithPaths(allSkillPaths)
	for _, path := range allSkillPaths {
		_ = os.MkdirAll(path, 0o755)
	}
	subagentPath := filepath.Join(configstore.DefaultConfigDir(), "subagents")
	_ = os.MkdirAll(subagentPath, 0o755)
	subagents := subagent.NewLoader(subagentPath)
	mcpManager := mcp.NewManager(filepath.Join(cfg.Session.Path, "mcp-cache.json"))
	mcpManager.SetOnChange(func() error {
		for _, tool := range mcpManager.AllTools() {
			_ = reg.Register(tool)
		}
		return nil
	})
	mcpManager.SetOnResourceUpdate(func(server, uri string, payload map[string]any) {
		sessionID, _ := payload["session_id"].(string)
		if strings.TrimSpace(sessionID) == "" {
			return
		}
		text := formatMCPResourceUpdate(server, uri, payload)
		part := mcpResourceUpdatePart(server, uri, payload)
		if strings.TrimSpace(text) != "" {
			_, _ = conv.AppendWithParts(context.Background(), sessionID, "system", text, []sdk.MessagePart{part})
		}
	})
	tools := buildAvailableTools(cfg, skills, mcpManager)
	if mcpTools, err := mcpManager.Bootstrap(ctx, toMCPServerConfigs(cfg.MCP.Servers)); err == nil {
		tools = append(tools, mcpTools...)
	}
	for _, tool := range tools {
		if err := reg.Register(tool); err != nil {
			return nil, err
		}
	}
	pol := policy.NewPolicyEngine(cfg)
	orch := execpkg.NewOrchestrator(reg, pol, cfg.WorkspaceRoot, plugins)

	planner, err := buildPlanner(cfg.Planner)
	if err != nil {
		return nil, err
	}
	audit, err := newAuditWriter(cfg.Logging.File)
	if err != nil {
		return nil, err
	}
	trans := session.NewWriter(cfg.Session.Path, cfg.Session.Retention)
	sqlitePath := cfg.Session.SQLitePath
	if strings.TrimSpace(sqlitePath) == "" && strings.TrimSpace(cfg.Session.Path) != "" {
		sqlitePath = filepath.Join(cfg.Session.Path, "sessions.db")
	}
	store, err := session.NewStore(sqlitePath)
	if err != nil {
		logger.Warn("failed to open session sqlite store", zap.Error(err))
	} else if store != nil {
		if err := store.EnsureRunSchema(ctx); err != nil {
			logger.Warn("failed to ensure run sqlite schema", zap.Error(err))
		}
	}
	agentRegistry := NewAgentRegistry(cfg.Agent)
	metrics := newServerMetrics()
	rt := &Runtime{
		cfg:           cfg,
		logger:        logger,
		conversation:  conv,
		planner:       planner,
		orchestrator:  orch,
		registry:      reg,
		audit:         audit,
		session:       trans,
		sessionStore:  store,
		plugins:       plugins,
		skills:        skills,
		agentRegistry: agentRegistry,
		subagents:     subagents,
		metrics:       metrics,
		runs:          newRunStore(),
	}
	rt.recoverRunsOnStartup(ctx)
	if tool, ok := reg.Get("agent.run"); ok {
		if agent, ok := tool.(*agenttool.Tool); ok {
			*agent = *agenttool.New(rt)
		}
	}
	if tool, ok := reg.Get("agent.coordinate"); ok {
		if coordinator, ok := tool.(*agenttool.CoordinatorTool); ok {
			*coordinator = *agenttool.NewCoordinator(rt)
		}
		if messageTool, ok := tool.(*agenttool.MessageTool); ok {
			*messageTool = *agenttool.NewMessage(rt)
		}
	}
	if tool, ok := reg.Get("skill.invoke"); ok {
		if skillInvoke, ok := tool.(*skilltool.Tool); ok {
			*skillInvoke = *skilltool.New(skills, rt.ensureSkillAllowed)
		}
	}
	if tool, ok := reg.Get("todo.write"); ok {
		if todoWrite, ok := tool.(*todotool.Tool); ok {
			*todoWrite = *todotool.New(rt)
		}
	}
	if tool, ok := reg.Get("lsp.query"); ok {
		if lspTool, ok := tool.(*lsp.Tool); ok {
			lsp.RegisterHooks(plugins, lspTool)
		}
	}
	return rt, nil
}

func buildAvailableTools(cfg config.Config, skills *skill.Loader, mcpManager *mcp.Manager) []sdk.Tool {
	rtAgent := agenttool.New(nil)

	ignoreChecker, _ := fstool.LoadIgnoreChecker(cfg.WorkspaceRoot)

	return []sdk.Tool{
		rtAgent,
		agenttool.NewCoordinator(nil),
		agenttool.NewMessage(nil),
		ask.NewQuestionTool(),
		todotool.New(nil),
		fstool.NewReadTool(cfg.WorkspaceRoot),
		fstool.NewWriteTool(cfg.WorkspaceRoot, cfg.Permissions.FileSystem.MaxWriteSizeKB),
		fstool.NewEditTool(cfg.WorkspaceRoot, cfg.Permissions.FileSystem.MaxWriteSizeKB),
		fstool.NewGlobToolWithIgnore(cfg.WorkspaceRoot, ignoreChecker),
		fstool.NewGrepToolWithIgnore(cfg.WorkspaceRoot, ignoreChecker),
		lsp.New(cfg.WorkspaceRoot),
		mcp.NewControlTool(mcpManager),
		skilltool.New(skills, nil),
		webfetch.NewFetchTool(),
		cmdtool.NewExecTool(cfg.WorkspaceRoot, 0),
		respondtool.NewEcho(),
	}
}

func toMCPServerConfigs(servers []config.MCPServerConfig) []mcp.ServerConfig {
	out := make([]mcp.ServerConfig, 0, len(servers))
	for _, server := range servers {
		out = append(out, mcp.ServerConfig{Name: server.Name, Command: server.Command, Transport: server.Transport, URL: server.URL, SSEURL: server.SSEURL, AuthToken: server.AuthToken, AuthHeader: server.AuthHeader})
	}
	return out
}

func (rt *Runtime) RunSubAgent(ctx context.Context, prompt string, allowedTools []string) (string, error) {
	subID := fmt.Sprintf("subagent-%d", time.Now().UnixNano())
	ctx = withTeamSession(ctx, subID)
	if len(allowedTools) > 0 {
		ctx = execpkg.WithAllowedTools(ctx, allowedTools)
	}
	if teamID := agentTeamIDFromContext(ctx); teamID != "" {
		ctx = withAgentTeam(ctx, teamID)
	}
	resp, err := rt.AgentLoop(ctx, subID, UserInput{Text: prompt}, nil, AgentModeBuild)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Reply), nil
}

func (rt *Runtime) RunSubAgentWithProfile(ctx context.Context, profile agenttool.AgentProfile, prompt string) (string, error) {
	role := strings.TrimSpace(profile.Name)
	if role == "" {
		role = "Agent"
	}
	rolePrompt := fmt.Sprintf("Role: %s\n\n%s\n\nTask:\n%s", role, strings.TrimSpace(profile.Instructions), strings.TrimSpace(prompt))

	// Get allowed tools from profile
	var allowedTools []string
	if rt.agentRegistry != nil {
		allowedTools = rt.agentRegistry.GetTools(strings.ToLower(profile.Name))
	}

	return rt.RunSubAgent(ctx, rolePrompt, allowedTools)
}

func (rt *Runtime) SendTeamMessage(ctx context.Context, from, to, kind, content, replyTo, threadID string, broadcast bool) (map[string]any, error) {
	sessionID := teamSessionIDFromContext(ctx)
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	return rt.sendTeamMessage(ctx, sessionID, from, to, kind, content, replyTo, threadID, broadcast)
}

type sessionSkillSet struct {
	mu    sync.RWMutex
	names map[string]struct{}
}

type sessionSubagentSet struct {
	mu    sync.RWMutex
	names map[string]struct{}
}

func (rt *Runtime) allowMentionedSkills(sessionID, input string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	set := rt.sessionSkillSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	for _, name := range extractRequestedSkills(input) {
		set.names[strings.ToLower(name)] = struct{}{}
	}
}

func (rt *Runtime) allowMentionedSubagents(sessionID, input string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	set := rt.sessionSubagentSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	for _, name := range extractRequestedSubagents(input) {
		set.names[strings.ToLower(name)] = struct{}{}
	}
}

func (rt *Runtime) allowSkill(sessionID, name string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	set := rt.sessionSkillSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	set.names[name] = struct{}{}
}

func (rt *Runtime) allowSubagent(sessionID, name string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	set := rt.sessionSubagentSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	set.names[name] = struct{}{}
}

func (rt *Runtime) setPendingConfirmation(sessionID string, pending pendingConfirmation) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	rt.pendingConfirmations.Store(sessionID, pending)
}

func (rt *Runtime) getPendingConfirmation(sessionID string) (pendingConfirmation, bool) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	if val, ok := rt.pendingConfirmations.Load(sessionID); ok {
		return val.(pendingConfirmation), true
	}
	return pendingConfirmation{}, false
}

func (rt *Runtime) clearPendingConfirmation(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	rt.pendingConfirmations.Delete(sessionID)
}

func (rt *Runtime) ensureSkillAllowed(ctx context.Context, sessionID, name string) error {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}
	set := rt.sessionSkillSet(sessionID)
	set.mu.RLock()
	_, ok := set.names[name]
	set.mu.RUnlock()
	if ok {
		return nil
	}
	return fmt.Errorf("skill.invoke is only allowed after the user explicitly requests that skill by name in the conversation")
}

func (rt *Runtime) sessionSkillSet(sessionID string) *sessionSkillSet {
	if current, ok := rt.allowedSkills.Load(sessionID); ok {
		return current.(*sessionSkillSet)
	}
	set := &sessionSkillSet{names: make(map[string]struct{})}
	actual, _ := rt.allowedSkills.LoadOrStore(sessionID, set)
	return actual.(*sessionSkillSet)
}

func (rt *Runtime) sessionSubagentSet(sessionID string) *sessionSubagentSet {
	if current, ok := rt.allowedSubagents.Load(sessionID); ok {
		return current.(*sessionSubagentSet)
	}
	set := &sessionSubagentSet{names: make(map[string]struct{})}
	actual, _ := rt.allowedSubagents.LoadOrStore(sessionID, set)
	return actual.(*sessionSubagentSet)
}

func (rt *Runtime) allowedSubagentNames(sessionID string) []string {
	set := rt.sessionSubagentSet(sessionID)
	set.mu.RLock()
	defer set.mu.RUnlock()
	allowed := make([]string, 0, len(set.names))
	for name := range set.names {
		allowed = append(allowed, name)
	}
	sort.Strings(allowed)
	return allowed
}

func (rt *Runtime) restoreAllowedSubagents(sessionID string, names []string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	set := rt.sessionSubagentSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	set.names = make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		set.names[name] = struct{}{}
	}
}

func (rt *Runtime) isSubagentAllowed(sessionID, name string) bool {
	set := rt.sessionSubagentSet(sessionID)
	set.mu.RLock()
	defer set.mu.RUnlock()
	_, ok := set.names[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (rt *Runtime) clearSessionState(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	rt.conversation.Clear(sessionID)
	rt.allowedSkills.Delete(sessionID)
	rt.allowedSubagents.Delete(sessionID)
	rt.taskState.Delete(sessionID)
	rt.pendingConfirmations.Delete(sessionID)
	rt.sessionMemory.Delete(sessionID)
	rt.teamState.Delete(sessionID)
	rt.checkpoints.Delete(sessionID)
	compressionState.Delete(sessionID)
}

func (rt *Runtime) sessionMetadata(sessionID, summary string) session.Metadata {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	set := rt.sessionSkillSet(sessionID)
	set.mu.RLock()
	allowedSkills := make([]string, 0, len(set.names))
	for name := range set.names {
		allowedSkills = append(allowedSkills, name)
	}
	set.mu.RUnlock()
	sort.Strings(allowedSkills)
	allowedSubagents := rt.allowedSubagentNames(sessionID)
	return session.Metadata{
		SessionID:        sessionID,
		Summary:          summary,
		ShortTerm:        rt.shortTermMemory(sessionID),
		LongTerm:         rt.longTermMemory(sessionID),
		AllowedSkills:    allowedSkills,
		AllowedSubagents: allowedSubagents,
		LastTaskNote:     rt.lastTaskNote(sessionID),
		CompressedAt:     compressionSessionState(sessionID).lastCompressedAt,
		IsCodeTask:       rt.isCodeTask(sessionID),
		Checkpoints:      rt.checkpointEntries(sessionID),
		CheckpointedAt:   rt.lastCheckpointAt(sessionID),
	}
}

func (rt *Runtime) restoreSessionMetadata(sessionID string, meta session.Metadata) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	set := rt.sessionSkillSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	set.names = make(map[string]struct{}, len(meta.AllowedSkills))
	for _, name := range meta.AllowedSkills {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		set.names[name] = struct{}{}
	}
	rt.restoreAllowedSubagents(sessionID, meta.AllowedSubagents)
	rt.setLastTaskNote(sessionID, meta.LastTaskNote)
	rt.setIsCodeTask(sessionID, meta.IsCodeTask)
	rt.restoreCheckpoints(sessionID, meta.Checkpoints, meta.CheckpointedAt)
	if !meta.CompressedAt.IsZero() {
		state := compressionSessionState(sessionID)
		state.mu.Lock()
		state.lastCompressedAt = meta.CompressedAt
		state.mu.Unlock()
	}
}

func (rt *Runtime) sessionCheckpointState(sessionID string) *sessionCheckpointState {
	if current, ok := rt.checkpoints.Load(sessionID); ok {
		return current.(*sessionCheckpointState)
	}
	state := &sessionCheckpointState{}
	actual, _ := rt.checkpoints.LoadOrStore(sessionID, state)
	return actual.(*sessionCheckpointState)
}

func (rt *Runtime) checkpointEntries(sessionID string) []session.CheckpointMetadata {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	if len(state.entries) == 0 {
		return nil
	}
	out := make([]session.CheckpointMetadata, len(state.entries))
	copy(out, state.entries)
	return out
}

func (rt *Runtime) lastCheckpointAt(sessionID string) time.Time {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.lastCheckpointAt
}

func (rt *Runtime) restoreCheckpoints(sessionID string, entries []session.CheckpointMetadata, checkpointedAt time.Time) {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.entries = append([]session.CheckpointMetadata(nil), entries...)
	state.lastCheckpointAt = checkpointedAt
	state.seq = int64(len(entries))
}

const (
	checkpointCooldown       = 30 * time.Second
	maxCheckpointsPerSession = 20
)

func (rt *Runtime) maybeCreateCheckpoint(ctx context.Context, sessionID, toolName string, inputs map[string]any) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	if !rt.isGitWorkspace() {
		return
	}
	state := rt.sessionCheckpointState(sessionID)
	state.mu.RLock()
	last := state.lastCheckpointAt
	state.mu.RUnlock()
	if !last.IsZero() && time.Since(last) < checkpointCooldown {
		return
	}
	checkpoint, err := rt.createCheckpoint(sessionID, toolName, inputs)
	if err != nil {
		rt.logger.Debug("checkpoint skipped", zap.String("sessionID", sessionID), zap.String("tool", toolName), zap.Error(err))
		return
	}
	state.mu.Lock()
	stale := append([]session.CheckpointMetadata(nil), state.entries[max(0, maxCheckpointsPerSession-1):]...)
	state.entries = append([]session.CheckpointMetadata{checkpoint}, state.entries...)
	if len(state.entries) > maxCheckpointsPerSession {
		state.entries = state.entries[:maxCheckpointsPerSession]
	}
	state.lastCheckpointAt = checkpoint.CreatedAt
	state.seq++
	state.mu.Unlock()
	rt.pruneCheckpointRefs(stale)
	_ = rt.persistSession(ctx, sessionID)
}

func (rt *Runtime) createCheckpoint(sessionID, toolName string, inputs map[string]any) (session.CheckpointMetadata, error) {
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	id := fmt.Sprintf("cp-%d", time.Now().Unix())
	summary := checkpointSummary(toolName, inputs)
	message := checkpointMessage(id, summary)
	cmd := exec.Command("git", "stash", "push", "--all", "--message", message)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		return session.CheckpointMetadata{}, fmt.Errorf("create checkpoint: %w: %s", err, strings.TrimSpace(string(output)))
	}
	ref, err := rt.resolveCheckpointRef(workspace, message)
	if err != nil {
		return session.CheckpointMetadata{}, err
	}
	return session.CheckpointMetadata{ID: id, Ref: ref, Tool: toolName, CreatedAt: time.Now().UTC(), Summary: summary}, nil
}

func checkpointMessage(id, summary string) string {
	return fmt.Sprintf("morpheus checkpoint %s [%s]", id, summary)
}

func (rt *Runtime) resolveCheckpointRef(workspace, message string) (string, error) {
	cmd := exec.Command("git", "stash", "list", "--format=%gd:%s")
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve checkpoint ref: %w", err)
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, ":") || !strings.Contains(line, message) {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		return strings.TrimSpace(parts[0]), nil
	}
	return "", fmt.Errorf("checkpoint ref not found")
}

func (rt *Runtime) rollbackCheckpoint(sessionID, id string, drop bool) (string, error) {
	entry, err := rt.findCheckpoint(sessionID, id)
	if err != nil {
		return "", err
	}
	if err := rt.validateCheckpointRef(entry); err != nil {
		return "", err
	}
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	cmd := exec.Command("git", "stash", "apply", entry.Ref)
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("rollback checkpoint %s: %w: %s", id, err, strings.TrimSpace(string(output)))
	}
	if drop {
		rt.removeCheckpoint(sessionID, id)
		rt.pruneCheckpointRefs([]session.CheckpointMetadata{entry})
	}
	return strings.TrimSpace(string(output)), nil
}

func (rt *Runtime) pruneCheckpointRefs(entries []session.CheckpointMetadata) {
	if len(entries) == 0 {
		return
	}
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	for _, entry := range entries {
		if strings.TrimSpace(entry.Ref) == "" {
			continue
		}
		if err := rt.validateCheckpointRef(entry); err != nil {
			rt.logger.Debug("skip dropping non-morpheus stash", zap.String("ref", entry.Ref), zap.Error(err))
			continue
		}
		cmd := exec.Command("git", "stash", "drop", entry.Ref)
		cmd.Dir = workspace
		if output, err := cmd.CombinedOutput(); err != nil {
			rt.logger.Debug("failed to drop checkpoint stash", zap.String("ref", entry.Ref), zap.Error(err), zap.String("output", strings.TrimSpace(string(output))))
		}
	}
}

func (rt *Runtime) pruneCheckpoints(sessionID string, keep int) int {
	if keep < 0 {
		keep = 0
	}
	state := rt.sessionCheckpointState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.entries) <= keep {
		return 0
	}
	removed := append([]session.CheckpointMetadata(nil), state.entries[keep:]...)
	state.entries = append([]session.CheckpointMetadata(nil), state.entries[:keep]...)
	go rt.pruneCheckpointRefs(removed)
	return len(removed)
}

func (rt *Runtime) validateCheckpointRef(entry session.CheckpointMetadata) error {
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	cmd := exec.Command("git", "stash", "list", "--format=%gd:%s")
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("validate checkpoint ref: %w", err)
	}
	expectedStart := fmt.Sprintf("%s:morpheus checkpoint %s ", strings.TrimSpace(entry.Ref), entry.ID)
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, expectedStart) {
			return nil
		}
	}
	return fmt.Errorf("stash ref %s is not a morpheus checkpoint for %s", entry.Ref, entry.ID)
}

func (rt *Runtime) gitWorkspaceDirty() (bool, string, error) {
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	cmd := exec.Command("git", "status", "--short")
	cmd.Dir = workspace
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, "", fmt.Errorf("git status: %w", err)
	}
	status := strings.TrimSpace(string(output))
	return status != "", status, nil
}

func (rt *Runtime) findCheckpoint(sessionID, id string) (session.CheckpointMetadata, error) {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	for _, entry := range state.entries {
		if entry.ID == id {
			return entry, nil
		}
	}
	return session.CheckpointMetadata{}, fmt.Errorf("checkpoint %s not found", id)
}

func (rt *Runtime) removeCheckpoint(sessionID, id string) {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	if len(state.entries) == 0 {
		return
	}
	filtered := state.entries[:0]
	for _, entry := range state.entries {
		if entry.ID == id {
			continue
		}
		filtered = append(filtered, entry)
	}
	state.entries = filtered
}

func (rt *Runtime) isGitWorkspace() bool {
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	info, err := os.Stat(filepath.Join(workspace, ".git"))
	return err == nil && !info.IsDir() || err == nil && info.IsDir()
}

func checkpointSummary(toolName string, inputs map[string]any) string {
	summary := strings.TrimSpace(toolName)
	switch toolName {
	case "cmd.exec":
		if command, _ := inputs["command"].(string); strings.TrimSpace(command) != "" {
			summary = truncate(strings.TrimSpace(command), 80)
		}
	case "fs.write", "fs.edit", "fs.read", "bash":
		if path, _ := inputs["path"].(string); strings.TrimSpace(path) != "" {
			summary = fmt.Sprintf("%s %s", toolName, strings.TrimSpace(path))
		}
	}
	if summary == "" {
		summary = "tool execution"
	}
	return summary
}

func (rt *Runtime) handleCheckpointCommand(ctx context.Context, sessionID, input string) (Response, bool, error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/checkpoint") {
		return Response{}, false, nil
	}
	args := strings.Fields(trimmed)
	dropAfterRollback := len(args) == 4 && args[3] == "--drop"
	if len(args) == 1 || (len(args) == 2 && args[1] == "list") {
		entries := rt.checkpointEntries(sessionID)
		if len(entries) == 0 {
			reply := "No checkpoints available."
			_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
			return Response{Reply: reply}, true, nil
		}
		var b strings.Builder
		b.WriteString("Available checkpoints:\n")
		for _, entry := range entries {
			b.WriteString(fmt.Sprintf("- %s | %s | ref=%s | tool=%s | %s\n", entry.ID, entry.CreatedAt.Format(time.RFC3339), valueOrDash(entry.Ref), valueOrDash(entry.Tool), entry.Summary))
		}
		reply := strings.TrimSpace(b.String())
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		return Response{Reply: reply}, true, nil
	}
	if len(args) == 3 && args[1] == "prune" {
		keep, err := strconv.Atoi(args[2])
		if err != nil {
			return Response{}, true, fmt.Errorf("usage: /checkpoint prune <keep>")
		}
		removed := rt.pruneCheckpoints(sessionID, keep)
		_ = rt.persistSession(ctx, sessionID)
		reply := fmt.Sprintf("Pruned %d checkpoint(s); kept %d.", removed, keep)
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		return Response{Reply: reply}, true, nil
	}
	if len(args) == 3 && args[1] == "show" {
		entry, err := rt.findCheckpoint(sessionID, args[2])
		if err != nil {
			return Response{}, true, err
		}
		reply := formatCheckpointDetail(entry)
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		return Response{Reply: reply}, true, nil
	}
	if (len(args) == 3 || len(args) == 4) && args[1] == "rollback" && (!dropAfterRollback && len(args) == 3 || dropAfterRollback) {
		dirty, status, err := rt.gitWorkspaceDirty()
		if err != nil {
			return Response{}, true, err
		}
		if dirty {
			pending := pendingConfirmation{
				Kind:   "checkpoint_rollback",
				Tool:   "checkpoint.rollback",
				Inputs: map[string]any{"id": args[2], "status": status, "drop": dropAfterRollback},
			}
			rt.setPendingConfirmation(sessionID, pending)
			question := formatConfirmationPrompt(pending)
			_, _ = rt.appendMessage(ctx, sessionID, "assistant", question, nil)
			payload := confirmationPayload(pending)
			return Response{Reply: question, Confirmation: payload}, true, nil
		}
		output, err := rt.rollbackCheckpoint(sessionID, args[2], dropAfterRollback)
		if err != nil {
			return Response{}, true, err
		}
		reply := fmt.Sprintf("Rolled back to checkpoint %s.", args[2])
		if dropAfterRollback {
			reply = fmt.Sprintf("Rolled back to checkpoint %s and dropped it.", args[2])
		}
		if output != "" {
			reply += "\n" + output
		}
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		_ = rt.persistSession(ctx, sessionID)
		return Response{Reply: reply}, true, nil
	}
	return Response{}, true, fmt.Errorf("usage: /checkpoint [list] | /checkpoint show <id> | /checkpoint rollback <id> [--drop] | /checkpoint prune <keep>")
}

func (rt *Runtime) handleTeamCommand(ctx context.Context, sessionID, input string) (Response, bool, error) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/team") {
		return Response{}, false, nil
	}
	args := strings.Fields(trimmed)
	if len(args) == 1 || (len(args) == 2 && args[1] == "status") {
		reply := rt.formatTeamStatus(sessionID)
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		return Response{Reply: reply}, true, nil
	}
	if len(args) == 2 && args[1] == "tasks" {
		reply := rt.formatTeamTasks(sessionID)
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		return Response{Reply: reply}, true, nil
	}
	if len(args) == 2 && args[1] == "messages" {
		reply := rt.formatTeamMessages(sessionID, "")
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		return Response{Reply: reply}, true, nil
	}
	if len(args) == 4 && args[1] == "messages" && args[2] == "--thread" {
		reply := rt.formatTeamMessages(sessionID, args[3])
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		return Response{Reply: reply}, true, nil
	}
	return Response{}, true, fmt.Errorf("usage: /team [status] | /team tasks | /team messages [--thread <id>]")
}

func valueOrDash(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "-"
	}
	return text
}

func formatCheckpointDetail(entry session.CheckpointMetadata) string {
	var b strings.Builder
	b.WriteString("Checkpoint details:\n")
	b.WriteString(fmt.Sprintf("- id: %s\n", valueOrDash(entry.ID)))
	b.WriteString(fmt.Sprintf("- created_at: %s\n", valueOrDash(entry.CreatedAt.Format(time.RFC3339))))
	b.WriteString(fmt.Sprintf("- ref: %s\n", valueOrDash(entry.Ref)))
	b.WriteString(fmt.Sprintf("- tool: %s\n", valueOrDash(entry.Tool)))
	b.WriteString(fmt.Sprintf("- summary: %s", valueOrDash(entry.Summary)))
	return b.String()
}

func (rt *Runtime) sessionTaskState(sessionID string) *sessionTaskState {
	if current, ok := rt.taskState.Load(sessionID); ok {
		return current.(*sessionTaskState)
	}
	state := &sessionTaskState{}
	actual, _ := rt.taskState.LoadOrStore(sessionID, state)
	return actual.(*sessionTaskState)
}

func (rt *Runtime) setLastTaskNote(sessionID, note string) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	state := rt.sessionTaskState(sessionID)
	state.mu.Lock()
	state.lastTaskNote = strings.TrimSpace(note)
	state.mu.Unlock()
}

func normalizeAgentMode(mode AgentMode) AgentMode {
	text := strings.ToLower(strings.TrimSpace(string(mode)))
	if text == string(AgentModePlan) {
		return AgentModePlan
	}
	return AgentModeBuild
}

func (rt *Runtime) defaultAgentMode() AgentMode {
	return normalizeAgentMode(AgentMode(rt.cfg.Agent.DefaultMode))
}

func (rt *Runtime) sessionMemoryState(sessionID string) *sessionMemoryState {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	if current, ok := rt.sessionMemory.Load(sessionID); ok {
		return current.(*sessionMemoryState)
	}
	state := &sessionMemoryState{}
	actual, _ := rt.sessionMemory.LoadOrStore(sessionID, state)
	return actual.(*sessionMemoryState)
}

func (rt *Runtime) setShortTermMemory(sessionID, content string) {
	state := rt.sessionMemoryState(sessionID)
	state.mu.Lock()
	state.shortTerm = strings.TrimSpace(content)
	state.mu.Unlock()
}

func (rt *Runtime) setLongTermMemory(sessionID, content string) {
	state := rt.sessionMemoryState(sessionID)
	state.mu.Lock()
	state.longTerm = strings.TrimSpace(content)
	state.mu.Unlock()
}

func (rt *Runtime) shortTermMemory(sessionID string) string {
	state := rt.sessionMemoryState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.shortTerm
}

func (rt *Runtime) longTermMemory(sessionID string) string {
	state := rt.sessionMemoryState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.longTerm
}

func (rt *Runtime) intentState(sessionID string) *sessionIntentState {
	state := &sessionIntentState{entries: map[string]intentClassification{}}
	actual, _ := rt.intentCache.LoadOrStore(sessionID, state)
	return actual.(*sessionIntentState)
}

func (rt *Runtime) getCachedIntent(sessionID, key string) (intentClassification, bool) {
	state := rt.intentState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	intent, ok := state.entries[key]
	return intent, ok
}

func (rt *Runtime) setCachedIntent(sessionID, key string, intent intentClassification) {
	state := rt.intentState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.entries[key] = intent
	if len(state.entries) <= 32 {
		return
	}
	for existing := range state.entries {
		delete(state.entries, existing)
		break
	}
}

func (rt *Runtime) ensureSkillsLoaded(ctx context.Context) {
	if rt.skills == nil {
		return
	}
	_ = rt.skills.EnsureLoaded(ctx)
}

func (rt *Runtime) preprocessSkillCommand(ctx context.Context, input string) (string, string, bool) {
	trimmed := strings.TrimSpace(input)
	if !strings.HasPrefix(trimmed, "/") {
		return input, "", false
	}
	name, rest := splitSlashCommand(trimmed)
	if name == "" || isReservedCommand(name) {
		return input, "", false
	}
	rt.ensureSkillsLoaded(ctx)
	if rt.skills == nil {
		return input, "", false
	}
	if rt.skills.Get(name) == nil {
		return input, "", false
	}
	if rest != "" {
		return fmt.Sprintf("Use skill %s. Input: %s", name, rest), name, true
	}
	return fmt.Sprintf("Use skill %s.", name), name, true
}

func splitSlashCommand(input string) (string, string) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(input, "/"))
	if trimmed == "" {
		return "", ""
	}
	parts := strings.Fields(trimmed)
	if len(parts) == 0 {
		return "", ""
	}
	name := parts[0]
	if len(parts) == 1 {
		return name, ""
	}
	return name, strings.TrimSpace(trimmed[len(name):])
}

func isConfirmationApproval(input string) bool {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return false
	}
	approved := []string{"yes", "y", "approve", "approved", "allow", "ok", "confirm", "proceed", "continue"}
	for _, token := range approved {
		if text == token {
			return true
		}
	}
	return false
}

func isConfirmationDenial(input string) bool {
	text := strings.ToLower(strings.TrimSpace(input))
	if text == "" {
		return false
	}
	denied := []string{"no", "n", "deny", "denied", "cancel", "stop"}
	for _, token := range denied {
		if text == token {
			return true
		}
	}
	return false
}

func isReservedCommand(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "new", "sessions", "skills", "models", "monitor", "resume", "plan", "vim", "ssh", "connect", "help", "exit", "checkpoint":
		return true
	case "team":
		return true
	default:
		return false
	}
}

func formatConfirmationPrompt(pending pendingConfirmation) string {
	parts := []string{"# Confirmation Required"}

	meta := []string{}
	if level := pending.Decision.RiskLevel.String(); level != "" && level != "unknown" {
		meta = append(meta, "Risk: "+level)
	}
	if pending.Decision.RuleName != "" {
		meta = append(meta, "Rule: "+pending.Decision.RuleName)
	}
	if len(meta) > 0 {
		parts = append(parts, strings.Join(meta, " | "))
	}

	if pending.Decision.Reason != "" {
		parts = append(parts, "", "## Reason", pending.Decision.Reason)
	}

	switch pending.Tool {
	case "cmd.exec":
		if cmd, ok := pending.Inputs["command"].(string); ok && strings.TrimSpace(cmd) != "" {
			parts = append(parts, "", "## Command", "```bash", strings.TrimSpace(cmd), "```")
		}
	case "checkpoint.rollback":
		if id, ok := pending.Inputs["id"].(string); ok && strings.TrimSpace(id) != "" {
			parts = append(parts, "", "## Checkpoint", strings.TrimSpace(id))
		}
		if drop, _ := pending.Inputs["drop"].(bool); drop {
			parts = append(parts, "", "## Post Action", "Drop checkpoint stash after successful rollback")
		}
		if status, ok := pending.Inputs["status"].(string); ok && strings.TrimSpace(status) != "" {
			parts = append(parts, "", "## Working Tree Changes", "```", truncate(strings.TrimSpace(status), 2000), "```")
		}
	case "fs.write", "fs.edit", "fs.read":
		if path, ok := pending.Inputs["path"].(string); ok && strings.TrimSpace(path) != "" {
			parts = append(parts, "", "## Path", strings.TrimSpace(path))
		}
	}

	if len(pending.Decision.Alternatives) > 0 {
		parts = append(parts, "", "## Alternatives")
		for _, alt := range pending.Decision.Alternatives {
			if strings.TrimSpace(alt) != "" {
				parts = append(parts, "- "+strings.TrimSpace(alt))
			}
		}
	}

	if len(pending.Decision.Suggestions) > 0 {
		parts = append(parts, "", "## Suggestions")
		for _, suggestion := range pending.Decision.Suggestions {
			if strings.TrimSpace(suggestion) != "" {
				parts = append(parts, "- "+strings.TrimSpace(suggestion))
			}
		}
	}

	parts = append(parts, "", "## Action", "- Type `approve` to proceed", "- Type `deny` to cancel")
	return strings.Join(parts, "\n")
}

func confirmationPayload(pending pendingConfirmation) *ConfirmationPayload {
	if pending.Tool == "" && len(pending.Inputs) == 0 && pending.Decision.Action == "" {
		return nil
	}
	return &ConfirmationPayload{
		Tool:   pending.Tool,
		Inputs: pending.Inputs,
		Decision: ConfirmationDecision{
			Reason:       pending.Decision.Reason,
			RuleName:     pending.Decision.RuleName,
			RiskLevel:    pending.Decision.RiskLevel.String(),
			RiskScore:    pending.Decision.RiskScore,
			Alternatives: pending.Decision.Alternatives,
			Suggestions:  pending.Decision.Suggestions,
		},
	}
}

func (rt *Runtime) setIsCodeTask(sessionID string, isCode bool) {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	state := rt.sessionTaskState(sessionID)
	state.mu.Lock()
	state.isCodeTask = isCode
	state.mu.Unlock()
}

func (rt *Runtime) isCodeTask(sessionID string) bool {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	state := rt.sessionTaskState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.isCodeTask
}

func (rt *Runtime) lastTaskNote(sessionID string) string {
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	state := rt.sessionTaskState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.lastTaskNote
}

func (rt *Runtime) updateLastTaskNote(sessionID string, plan *sdk.Plan, results []sdk.ToolResult) {
	rt.setLastTaskNote(sessionID, buildTaskNote(plan, results))
	_ = rt.SaveSession(context.Background(), sessionID)
}

func buildTaskNote(plan *sdk.Plan, results []sdk.ToolResult) string {
	if plan == nil {
		return ""
	}
	performed := summarizeCompletedActions(plan, results)
	next := suggestNextActions(plan, results)
	var parts []string
	if performed != "" {
		parts = append(parts, "Did: "+performed)
	}
	if next != "" {
		parts = append(parts, "Next: "+next)
	}
	return strings.Join(parts, " ")
}

func summarizeCompletedActions(plan *sdk.Plan, results []sdk.ToolResult) string {
	if plan == nil || len(plan.Steps) == 0 {
		return ""
	}
	var done []string
	for _, step := range plan.Steps {
		if step.Status == sdk.StepStatusSucceeded {
			text := strings.TrimSpace(step.Description)
			if text == "" {
				text = step.Tool
			}
			done = append(done, text)
		}
		if len(done) >= 3 {
			break
		}
	}
	if len(done) == 0 {
		return "no completed actions recorded"
	}
	return strings.Join(done, "; ")
}

func suggestNextActions(plan *sdk.Plan, results []sdk.ToolResult) string {
	if plan != nil {
		for _, step := range plan.Steps {
			if step.Status == sdk.StepStatusFailed {
				return "fix the failed step: " + strings.TrimSpace(step.Description)
			}
		}
	}
	if len(results) == 0 {
		return "review the session and continue with the next task"
	}
	lastTool := planStepTool(plan, results[len(results)-1].StepID)
	switch lastTool {
	case "fs.edit", "fs.write":
		return "review the edited files and run relevant tests or checks"
	case "cmd.exec":
		return "review command output and decide whether any follow-up changes are needed"
	case "skill.invoke":
		return "review the skill output and apply or refine the result if needed"
	default:
		return "review the latest result and continue with the next useful improvement"
	}
}

func extractRequestedSkills(input string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bskill\s*[:：]\s*([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\buse\s+skill\s+([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\busing\s+skill\s+([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\b调用\s*skill\s*([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\b使用\s*skill\s*([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)^/([a-zA-Z0-9_-]+)`),
	}
	seen := map[string]struct{}{}
	var out []string
	for _, re := range patterns {
		for _, match := range re.FindAllStringSubmatch(input, -1) {
			if len(match) < 2 {
				continue
			}
			name := strings.TrimSpace(match[1])
			if name == "" {
				continue
			}
			if isReservedCommand(name) {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

func extractRequestedSubagents(input string) []string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsubagent\s*[:：]\s*([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\buse\s+subagent\s+([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\bsubagent\s+([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\b使用\s*subagent\s*([a-zA-Z0-9_-]+)`),
		regexp.MustCompile(`(?i)\b调用\s*subagent\s*([a-zA-Z0-9_-]+)`),
	}
	seen := map[string]struct{}{}
	var out []string
	for _, re := range patterns {
		for _, match := range re.FindAllStringSubmatch(input, -1) {
			if len(match) < 2 {
				continue
			}
			name := strings.TrimSpace(match[1])
			if name == "" {
				continue
			}
			key := strings.ToLower(name)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, name)
		}
	}
	return out
}

func formatMCPResourceUpdate(server, uri string, payload map[string]any) string {
	delete(payload, "session_id")
	structured := summarizeMCPResourcePayload(server, uri, payload)
	body, _ := json.MarshalIndent(structured, "", "  ")
	return fmt.Sprintf("MCP resource updated\n%s", string(body))
}

func mcpResourceUpdatePart(server, uri string, payload map[string]any) sdk.MessagePart {
	delete(payload, "session_id")
	return sdk.MessagePart{
		Type:   "tool",
		Tool:   "mcp.resource_update",
		Status: "updated",
		Input: map[string]any{
			"server": server,
			"uri":    uri,
		},
		Output: summarizeMCPResourcePayload(server, uri, payload),
	}
}

func summarizeMCPResourcePayload(server, uri string, payload map[string]any) map[string]any {
	result := map[string]any{
		"server":      server,
		"uri":         uri,
		"change_type": detectMCPChangeType(payload),
	}

	contents, _ := payload["contents"].([]any)
	if len(contents) > 0 {
		result["summary"] = summarizeMCPContents(contents)
		result["highlights"] = extractMCPHighlights(contents)
		result["truncated"] = isMCPContentTruncated(contents)
		return result
	}

	result["summary"] = summarizeGenericMCPPayload(payload)
	result["highlights"] = topLevelMCPKeys(payload)
	result["truncated"] = len(fmt.Sprint(payload)) > 800
	return result
}

func detectMCPChangeType(payload map[string]any) string {
	if _, ok := payload["contents"]; ok {
		return "resource_contents"
	}
	if _, ok := payload["resources"]; ok {
		return "resource_list"
	}
	return "resource_update"
}

func summarizeMCPContents(contents []any) string {
	if len(contents) == 0 {
		return "empty resource payload"
	}
	first, _ := contents[0].(map[string]any)
	if text, _ := first["text"].(string); strings.TrimSpace(text) != "" {
		if mime, _ := first["mimeType"].(string); mime != "" {
			return summarizeMIMEText(mime, text)
		}
		lines := strings.Split(strings.TrimSpace(text), "\n")
		if len(lines) > 3 {
			lines = lines[:3]
		}
		return truncate(strings.Join(lines, " | "), 240)
	}
	if blob, _ := first["blob"].(string); blob != "" {
		return fmt.Sprintf("binary content (%d bytes base64)", len(blob))
	}
	if mime, _ := first["mimeType"].(string); mime != "" {
		return "resource content with mime type " + mime
	}
	body, _ := json.Marshal(first)
	return truncate(string(body), 240)
}

func extractMCPHighlights(contents []any) []string {
	if len(contents) == 0 {
		return nil
	}
	first, _ := contents[0].(map[string]any)
	highlights := []string{}
	if uri, _ := first["uri"].(string); uri != "" {
		highlights = append(highlights, "uri="+uri)
	}
	if mime, _ := first["mimeType"].(string); mime != "" {
		highlights = append(highlights, "mime="+mime)
	}
	if text, _ := first["text"].(string); text != "" {
		highlights = append(highlights, fmt.Sprintf("chars=%d", len(text)))
		lines := 1 + strings.Count(text, "\n")
		highlights = append(highlights, fmt.Sprintf("lines=%d", lines))
		if maybeJSONKeys := extractTopJSONKeys(text); len(maybeJSONKeys) > 0 {
			highlights = append(highlights, "keys="+strings.Join(maybeJSONKeys, ","))
		}
		if mime, _ := first["mimeType"].(string); mime != "" {
			highlights = append(highlights, mimeSpecificHighlight(mime, text)...)
		}
	}
	return highlights
}

func summarizeMIMEText(mime, text string) string {
	switch {
	case strings.Contains(mime, "json"):
		keys := extractTopJSONKeys(text)
		if len(keys) > 0 {
			return fmt.Sprintf("JSON content with keys: %s", strings.Join(keys, ", "))
		}
	case strings.Contains(mime, "markdown"):
		lines := strings.Split(strings.TrimSpace(text), "\n")
		headings := []string{}
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				headings = append(headings, strings.TrimSpace(line))
			}
			if len(headings) >= 3 {
				break
			}
		}
		if len(headings) > 0 {
			return "Markdown headings: " + strings.Join(headings, " | ")
		}
	case strings.Contains(mime, "html"):
		return "HTML content updated"
	case strings.Contains(mime, "yaml"), strings.Contains(mime, "yml"):
		return "YAML content updated"
	case strings.Contains(mime, "xml"):
		return "XML content updated"
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return truncate(strings.Join(lines, " | "), 240)
}

func mimeSpecificHighlight(mime, text string) []string {
	out := []string{}
	switch {
	case strings.Contains(mime, "json"):
		if keys := extractTopJSONKeys(text); len(keys) > 0 {
			out = append(out, "json_keys="+strings.Join(keys, ","))
		}
	case strings.Contains(mime, "markdown"):
		out = append(out, fmt.Sprintf("headings=%d", strings.Count(text, "\n#")+btoi(strings.HasPrefix(strings.TrimSpace(text), "#"))))
	case strings.Contains(mime, "html"):
		out = append(out, fmt.Sprintf("tags~=%d", strings.Count(text, "<")))
	}
	return out
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

func isMCPContentTruncated(contents []any) bool {
	if len(contents) == 0 {
		return false
	}
	first, _ := contents[0].(map[string]any)
	if text, _ := first["text"].(string); len(text) > 240 {
		return true
	}
	if blob, _ := first["blob"].(string); len(blob) > 120 {
		return true
	}
	return false
}

func summarizeGenericMCPPayload(payload map[string]any) string {
	body, _ := json.Marshal(payload)
	return truncate(string(body), 240)
}

func topLevelMCPKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 8 {
		keys = keys[:8]
	}
	return keys
}

func extractTopJSONKeys(text string) []string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(text), &obj); err != nil {
		return nil
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) > 6 {
		keys = keys[:6]
	}
	return keys
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

func (rt *Runtime) UpdatePlanner(cfg config.PlannerConfig) error {
	planner, err := buildPlanner(cfg)
	if err != nil {
		return err
	}
	rt.cfg.Planner = cfg
	rt.planner = planner
	return nil
}

// Close flushes resources.
func (rt *Runtime) Close() error {
	if rt.logger != nil {
		_ = rt.logger.Sync()
	}
	if rt.audit != nil {
		return rt.audit.Close()
	}
	return nil
}

// SaveSession persists the session to disk.
func (rt *Runtime) SaveSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		sessionID = "default"
	}
	return rt.persistSession(ctx, sessionID)
}

func (rt *Runtime) persistSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		sessionID = "default"
	}
	messages := rt.conversation.History(ctx, sessionID)
	if err := rt.session.Write(ctx, sessionID, messages); err != nil {
		return err
	}
	summary := rt.conversation.Summary(sessionID)
	if summary != "" {
		_ = rt.session.SaveSummary(ctx, sessionID, summary)
	}
	shortTerm := rt.shortTermMemory(sessionID)
	if strings.TrimSpace(shortTerm) != "" {
		_ = rt.session.SaveShortTerm(ctx, sessionID, shortTerm)
	}
	longTerm := rt.longTermMemory(sessionID)
	if strings.TrimSpace(longTerm) != "" {
		_ = rt.session.SaveLongTerm(ctx, sessionID, longTerm)
	}
	meta := rt.sessionMetadata(sessionID, summary)
	_ = rt.session.SaveMetadata(ctx, sessionID, meta)
	if rt.sessionStore != nil {
		_ = rt.sessionStore.SaveSession(ctx, sessionID, messages, summary, meta)
	}
	return nil
}

// LoadSession loads session history from disk into memory.
func (rt *Runtime) LoadSession(ctx context.Context, sessionID string) error {
	if sessionID == "" {
		sessionID = "default"
	}
	if rt.sessionStore != nil && rt.sessionStore.HasSession(ctx, sessionID) {
		stored, err := rt.sessionStore.LoadSession(ctx, sessionID)
		if err == nil {
			if len(stored.Messages) > 0 {
				rt.conversation.ReplaceMessages(sessionID, stored.Messages)
			}
			if strings.TrimSpace(stored.Summary) != "" {
				rt.conversation.SetSummary(sessionID, stored.Summary)
			}
			if strings.TrimSpace(stored.Metadata.ShortTerm) != "" {
				rt.setShortTermMemory(sessionID, stored.Metadata.ShortTerm)
			}
			if strings.TrimSpace(stored.Metadata.LongTerm) != "" {
				rt.setLongTermMemory(sessionID, stored.Metadata.LongTerm)
			}
			rt.restoreSessionMetadata(sessionID, stored.Metadata)
			return nil
		}
	}
	sess, err := rt.session.LoadSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess.Conversation == "" {
		if strings.TrimSpace(sess.Summary) != "" {
			rt.conversation.SetSummary(sessionID, sess.Summary)
		}
		return nil
	}
	lines := strings.Split(sess.Conversation, "\n")
	var currentRole string
	var currentContent strings.Builder
	var inContent bool
	var entries []struct {
		role    string
		content string
	}

	flush := func() {
		if currentRole == "" || currentContent.Len() == 0 {
			return
		}
		entries = append(entries, struct {
			role    string
			content string
		}{role: currentRole, content: currentContent.String()})
		currentRole = ""
		currentContent.Reset()
		inContent = false
	}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			flush()
			parts := strings.SplitN(line, "|", 2)
			if len(parts) >= 2 {
				currentRole = strings.TrimSpace(parts[0][3:])
			}
			continue
		}
		if line == "---" {
			flush()
			continue
		}
		if line == "" {
			continue
		}
		if !inContent {
			inContent = true
		} else {
			currentContent.WriteString("\n")
		}
		currentContent.WriteString(line)
	}
	flush()

	if strings.TrimSpace(sess.Summary) == "" {
		for i, entry := range entries {
			content := strings.TrimSpace(entry.content)
			if entry.role != "system" || content == "" {
				continue
			}
			if strings.HasPrefix(content, "Previous conversation summary:") || strings.Contains(content, "## Goal") {
				sess.Summary = content
				entries = append(entries[:i], entries[i+1:]...)
				break
			}
		}
	}

	for _, entry := range entries {
		rt.conversation.Append(ctx, sessionID, entry.role, entry.content)
	}
	if strings.TrimSpace(sess.Summary) != "" {
		rt.conversation.SetSummary(sessionID, sess.Summary)
	}
	shortTerm := strings.TrimSpace(sess.ShortTermMemory)
	if shortTerm == "" {
		shortTerm = strings.TrimSpace(sess.Metadata.ShortTerm)
	}
	if shortTerm != "" {
		rt.setShortTermMemory(sessionID, shortTerm)
	}
	longTerm := strings.TrimSpace(sess.LongTermMemory)
	if longTerm == "" {
		longTerm = strings.TrimSpace(sess.Metadata.LongTerm)
	}
	if longTerm != "" {
		rt.setLongTermMemory(sessionID, longTerm)
	}
	rt.restoreSessionMetadata(sessionID, sess.Metadata)
	return nil
}

// Plan produces a plan for the provided input without executing it.
func (rt *Runtime) Plan(ctx context.Context, sessionID string, input UserInput) (*sdk.Plan, sdk.PlanRequest, error) {
	if sessionID == "" {
		sessionID = "default"
	}

	rt.logger.Info("Plan called", zap.String("sessionID", sessionID), zap.String("input", input.Text))

	normalized, err := rt.normalizeUserInput(ctx, input)
	if err != nil {
		return nil, sdk.PlanRequest{}, err
	}
	if _, err := rt.appendMessage(ctx, sessionID, "user", normalized.Text, normalized.Parts); err != nil {
		return nil, sdk.PlanRequest{}, err
	}

	messages := rt.conversation.Messages(sessionID)
	if len(messages) > 0 {
		last := messages[len(messages)-1]
		if last.Role == "user" && last.Content == normalized.Text {
			messages = messages[:len(messages)-1]
		}
	}

	systemPrompt := rt.conversation.SystemPrompt()
	if rt.plugins != nil {
		systemPrompt = rt.plugins.ApplySystem(plugin.SystemContext{SessionID: sessionID}, systemPrompt)
	}
	historyContext := rt.buildHistoryContext(sessionID, systemPrompt, rt.conversation.Summary(sessionID), messages)
	if historyContext != "" {
		rt.logger.Info("History context built", zap.String("contextLength", fmt.Sprintf("%d", len(historyContext))), zap.String("context", historyContext))
	}

	fullPrompt := "Current task: " + normalized.Text
	if historyContext != "" {
		fullPrompt = historyContext + "\n\n" + fullPrompt
	}

	planReq := sdk.PlanRequest{
		ConversationID: sessionID,
		Prompt:         fullPrompt,
		Context:        rt.conversation.Summaries(sessionID),
		Intent:         classifyIntent(normalized.Text),
	}

	rt.checkAndCompress(ctx, sessionID)

	plan, err := rt.planner.Plan(ctx, planReq)
	if err != nil {
		return nil, sdk.PlanRequest{}, err
	}
	plan.Status = sdk.PlanStatusDraft
	return &plan, planReq, nil
}

func (rt *Runtime) buildHistoryContext(sessionID, systemPrompt, summary string, messages []sdk.Message) string {
	const (
		maxSystemChars  = 1200
		maxSummaryChars = 2000
		maxMsgChars     = 800
		recentLimit     = 8
		maxSummaryLines = 200
	)

	var b strings.Builder
	if strings.TrimSpace(systemPrompt) != "" {
		b.WriteString("System instructions:\n")
		b.WriteString(truncate(systemPrompt, maxSystemChars))
		b.WriteString("\n\n")
	}
	for _, doc := range rt.contextDocuments() {
		b.WriteString(doc.Label)
		b.WriteString(":\n")
		b.WriteString(truncateLines(doc.Content, maxSummaryLines))
		b.WriteString("\n\n")
	}
	if longTerm := rt.longTermMemory(sessionID); longTerm != "" {
		b.WriteString("Long-term memory:\n")
		b.WriteString(truncateLines(longTerm, maxSummaryLines))
		b.WriteString("\n\n")
	}
	if shortTerm := rt.shortTermMemory(sessionID); shortTerm != "" && shortTerm != summary {
		b.WriteString("Short-term memory:\n")
		b.WriteString(truncateLines(shortTerm, maxSummaryLines))
		b.WriteString("\n\n")
	}
	if strings.TrimSpace(summary) != "" {
		b.WriteString("Conversation summary:\n")
		b.WriteString(truncateLines(truncate(summary, maxSummaryChars), maxSummaryLines))
		b.WriteString("\n\n")
	}
	if len(messages) > 0 {
		b.WriteString("Recent messages:\n")
		start := 0
		if len(messages) > recentLimit {
			start = len(messages) - recentLimit
		}
		for _, msg := range messages[start:] {
			role := msg.Role
			if role == "assistant" {
				role = "ai"
			}
			content := truncate(msg.Content, maxMsgChars)
			b.WriteString(fmt.Sprintf("%s: %s\n", role, content))
		}
	}
	return strings.TrimSpace(b.String())
}

type contextDocument struct {
	Label   string
	Content string
}

func (rt *Runtime) contextDocuments() []contextDocument {
	var docs []contextDocument
	for _, entry := range rt.contextDocumentSources() {
		content := readContextFile(entry.Path)
		if strings.TrimSpace(content) == "" {
			continue
		}
		docs = append(docs, contextDocument{Label: entry.Label, Content: content})
	}
	return docs
}

type contextSource struct {
	Label string
	Path  string
}

func (rt *Runtime) contextDocumentSources() []contextSource {
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	projectPath := rt.resolveProjectContextPath(workspace)
	globalPath := filepath.Join(userHomeDir(), ".morpheus.md")
	return []contextSource{
		{Label: "Global context (~/.morpheus.md)", Path: globalPath},
		{Label: "Project context (" + filepath.Base(projectPath) + ")", Path: projectPath},
	}
}

func (rt *Runtime) resolveProjectContextPath(workspace string) string {
	path := strings.TrimSpace(rt.cfg.Morpheus.ContextFile)
	if path == "" {
		path = ".morpheus.md"
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(workspace, path)
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "."
	}
	return home
}

func readContextFile(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return ""
	}
	if len(data) > ContextMaxBytes {
		data = data[:ContextMaxBytes]
	}
	return strings.TrimSpace(string(data))
}

func truncateLines(text string, maxLines int) string {
	if maxLines <= 0 || strings.TrimSpace(text) == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if len(lines) <= maxLines {
		return strings.TrimSpace(text)
	}
	return strings.TrimSpace(strings.Join(lines[:maxLines], "\n"))
}

func trimMemory(text string, maxBytes int) string {
	text = strings.TrimSpace(text)
	if maxBytes <= 0 || text == "" {
		return ""
	}
	if len(text) <= maxBytes {
		return text
	}
	return strings.TrimSpace(text[len(text)-maxBytes:])
}

const (
	MaxHistoryTokens        = 60000
	CompactionReserveTokens = 20000
	PruneMinimumTokens      = 20000
	PruneProtectTokens      = 40000
	CompressionCooldown     = 2 * time.Minute
	CompactionTriggerRatio  = 0.95
	ContextMaxBytes         = 12000
	ShortTermMaxBytes       = 4000
	LongTermMaxBytes        = 12000
)

var compressionState sync.Map

type sessionCompressionState struct {
	mu               sync.Mutex
	lastCompressedAt time.Time
}

func (rt *Runtime) checkAndCompress(ctx context.Context, sessionID string) {
	rt.pruneHistory(sessionID)

	messages := rt.conversation.Messages(sessionID)
	var totalTokens int
	for _, msg := range messages {
		totalTokens += estimateMessageTokens(msg)
	}
	threshold := int(float64(MaxHistoryTokens) * CompactionTriggerRatio)
	if threshold > MaxHistoryTokens-CompactionReserveTokens {
		threshold = MaxHistoryTokens - CompactionReserveTokens
	}
	if totalTokens >= threshold {
		rt.compressHistory(ctx, sessionID)
	}
}

func (rt *Runtime) pruneHistory(sessionID string) {
	msgs := rt.conversation.Messages(sessionID)
	if len(msgs) == 0 {
		return
	}

	var total, pruned int
	var toPrune []int
	turns := 0

	for i := len(msgs) - 1; i >= 0; i-- {
		msg := msgs[i]
		if msg.Role == "user" {
			turns++
		}
		if turns < 2 {
			continue
		}
		if msg.Role == "assistant" && strings.Contains(msg.Content, "## Goal") {
			break
		}
		if msg.Role != "assistant" || !(isToolLikeContent(msg.Content) || hasToolParts(msg.Parts)) {
			continue
		}
		est := estimateMessageTokens(msg)
		total += est
		if total > PruneProtectTokens {
			pruned += est
			toPrune = append(toPrune, i)
		}
	}

	if pruned < PruneMinimumTokens {
		return
	}

	for _, idx := range toPrune {
		msgs[idx] = compactToolMessage(msgs[idx])
	}

	rt.conversation.ReplaceMessages(sessionID, msgs)
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	// Rough heuristic: ~4 chars per token for English-ish text.
	return (len(text) + 3) / 4
}

func isToolLikeContent(content string) bool {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "```") {
		return true
	}
	if strings.Contains(trimmed, "Output:") || strings.Contains(trimmed, "stdout:") {
		return true
	}
	prefixes := []string{"Written ", "Created ", "Removed ", "Done ", "$ ", "Step: "}
	for _, p := range prefixes {
		if strings.Contains(trimmed, p) {
			return true
		}
	}
	return false
}

func compactToolOutput(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return trimmed
	}
	preview := trimmed
	if len(preview) > 200 {
		preview = preview[:200] + "..."
	}
	return fmt.Sprintf("%s\n\n[Compacted tool output: original length %d chars]", preview, len(trimmed))
}

func hasToolParts(parts []sdk.MessagePart) bool {
	for _, part := range parts {
		if part.Type == "tool" {
			return true
		}
	}
	return false
}

func compactToolMessage(msg sdk.Message) sdk.Message {
	msg.Content = compactToolOutput(msg.Content)
	if len(msg.Parts) == 0 {
		return msg
	}
	for i, part := range msg.Parts {
		if part.Type != "tool" || part.Output == nil {
			continue
		}
		part.Output = truncateOutputMap(part.Output)
		msg.Parts[i] = part
	}
	return msg
}

func truncateOutputMap(output map[string]any) map[string]any {
	const maxLen = 1000
	const previewLen = 200
	truncated := false
	for k, v := range output {
		str, ok := v.(string)
		if !ok || len(str) <= maxLen {
			continue
		}
		output[k] = truncate(str, previewLen)
		truncated = true
	}
	if truncated {
		if _, ok := output["truncated"]; !ok {
			output["truncated"] = true
		}
	}
	return output
}

func (rt *Runtime) compressHistory(ctx context.Context, sessionID string) {
	state := compressionSessionState(sessionID)
	state.mu.Lock()
	if !state.lastCompressedAt.IsZero() && time.Since(state.lastCompressedAt) < CompressionCooldown {
		state.mu.Unlock()
		return
	}
	state.lastCompressedAt = time.Now().UTC()
	state.mu.Unlock()

	history := rt.conversation.History(ctx, sessionID)
	if len(history) <= 4 {
		return
	}

	recentCount := 4
	recent := history[len(history)-recentCount:]
	old := history[:len(history)-recentCount]

	var oldContent strings.Builder
	for _, msg := range old {
		role := msg.Role
		if role == "assistant" {
			role = "ai"
		}
		oldContent.WriteString(fmt.Sprintf("%s: %s\n", role, msg.Content))
	}

	isCode := rt.isCodeTask(sessionID)
	if !isCode {
		isCode = looksLikeCodeTask(old)
		rt.setIsCodeTask(sessionID, isCode)
	}
	summaryPrompt := buildCompressionPrompt(oldContent.String(), isCode)

	summary, err := rt.generateSummary(ctx, summaryPrompt)
	if err != nil || summary == "" {
		return
	}

	cleanSummary := strings.TrimSpace(summary)
	if cleanSummary == "" {
		return
	}
	if existing := strings.TrimSpace(rt.conversation.Summary(sessionID)); existing != "" {
		cleanSummary = strings.TrimSpace(existing + "\n\n" + cleanSummary)
	}
	rt.conversation.SetSummary(sessionID, cleanSummary)
	rt.setShortTermMemory(sessionID, trimMemory(cleanSummary, ShortTermMaxBytes))
	longTerm := rt.longTermMemory(sessionID)
	if longTerm != "" {
		longTerm = strings.TrimSpace(longTerm + "\n\n---\n\n" + cleanSummary)
	} else {
		longTerm = cleanSummary
	}
	rt.setLongTermMemory(sessionID, trimMemory(longTerm, LongTermMaxBytes))
	rt.conversation.ReplaceMessages(sessionID, recent)
	_ = rt.session.SaveSummary(ctx, sessionID, cleanSummary)
	_ = rt.SaveSession(ctx, sessionID)
}

func compressionSessionState(sessionID string) *sessionCompressionState {
	if current, ok := compressionState.Load(sessionID); ok {
		return current.(*sessionCompressionState)
	}
	state := &sessionCompressionState{}
	actual, _ := compressionState.LoadOrStore(sessionID, state)
	return actual.(*sessionCompressionState)
}

func looksLikeCodeTask(messages []sdk.Message) bool {
	for _, msg := range messages {
		content := strings.ToLower(msg.Content)
		if strings.Contains(content, "go test") || strings.Contains(content, "npm") || strings.Contains(content, "python") {
			return true
		}
		if strings.Contains(content, ".go") || strings.Contains(content, ".ts") || strings.Contains(content, ".py") || strings.Contains(content, "function ") || strings.Contains(content, "class ") {
			return true
		}
		for _, part := range msg.Parts {
			if part.Type == "tool" {
				return true
			}
		}
	}
	return false
}

func buildCompressionPrompt(history string, codeTask bool) string {
	if codeTask {
		return fmt.Sprintf(`Provide a continuation summary for an engineering task.
Focus on implementation state, files, commands, decisions, remaining work, and anything needed so another coding agent can continue immediately.

Use this template:
---
## Goal
[User objective]

## Constraints
- [Important constraints, style rules, safety rules]

## Current state
- [What is already implemented]
- [What is in progress]
- [What still needs work]

## Decisions
- [Technical decisions and rationale]

## Relevant files
- [Exact paths and why they matter]

## Commands
- [Only important commands and high-signal outcomes]

## Next steps
- [Concrete next actions]
---

Conversation history:
%s

Keep it concise and specific, within 300 words.`, history)
	}

	return fmt.Sprintf(`Provide a continuation summary for a general task.
Focus on the user's goal, key facts, decisions, unresolved questions, and the most useful next steps.

Use this template:
---
## Goal
[User objective]

## Important context
- [Important facts and constraints]

## Decisions
- [Decisions made]

## Current state
- [What has been done]
- [What remains]

## Open questions
- [Unresolved questions, if any]

## Next steps
- [Best next actions]
---

Conversation history:
%s

Keep it concise and specific, within 220 words.`, history)
}

func (rt *Runtime) generateSummary(ctx context.Context, prompt string) (string, error) {
	plannerCfg := rt.cfg.Planner
	if plannerCfg.Provider == "builtin" || plannerCfg.APIKey == "" {
		return "", nil
	}

	model := plannerCfg.Model
	if model == "" {
		model = defaultModel(plannerCfg.Provider)
	}

	payload := map[string]any{
		"model":       model,
		"messages":    []map[string]string{{"role": "user", "content": prompt}},
		"temperature": 0.3,
		"max_tokens":  500,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	endpoint := plannerCfg.Endpoint
	if endpoint == "" {
		endpoint = chatEndpoint(plannerCfg.Provider)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	switch plannerCfg.Provider {
	case "openai", "glm", "deepseek", "anthropic", "openrouter", "groq", "mistral", "togetherai", "perplexity":
		httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
	case "minimax", "minmax":
		httpReq.Header.Set("x-api-key", plannerCfg.APIKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	if plannerCfg.Provider == "minmax" {
		var result struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			return "", err
		}
		for _, block := range result.Content {
			if block.Type == "text" && block.Text != "" {
				return strings.TrimSpace(block.Text), nil
			}
		}
		return "", nil
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}

	if len(result.Choices) == 0 {
		return "", nil
	}

	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// PlanWithError generates a new plan to recover from an error.
func (rt *Runtime) PlanWithError(ctx context.Context, sessionID, originalInput, failedStep, errorMsg string) (sdk.Plan, sdk.PlanRequest, error) {
	if sessionID == "" {
		sessionID = "default"
	}

	recoveryPrompt := fmt.Sprintf(`Task failed. Analyze the error and fix correctly.

Original task: %s
Failed step: %s
Error: %s

Guidelines (accuracy first):
1. Understand WHY it failed
2. Fix the root cause, not symptoms
3. Use correct tool and inputs:
   - agent.run: {"prompt": "delegated task"}
   - conversation.ask: {"question": "question", "options": ["option 1", "option 2"], "multiple": false}
   - fs.read: {"path": "file path", "offset": 1, "limit": 200}
   - fs.write: {"path": "file path", "content": "content"}
   - fs.edit: {"path": "file path", "old_string": "old", "new_string": "new", "replace_all": false}
   - fs.glob: {"pattern": "glob pattern"}
   - fs.grep: {"pattern": "text"}
   - lsp.query: {"action": "definition|typeDefinition|references|hover|implementations|symbols|documentSymbols|callHierarchy|diagnostics|rename|codeAction|capabilities|workspaceFolders|addWorkspaceRoot|removeWorkspaceRoot|status|restart|shutdown", "path": "file path", "line": 1, "column": 1, "newName": "optional rename target"}
   - mcp.query: {"action": "connect|disconnect|servers|tools|resources|readResource|subscribe", "name": "server name", "transport": "stdio|http|sse", "command": "launch command", "url": "remote endpoint", "uri": "resource uri"}
   - skill.invoke: {"name": "skill-name", "input": {}}
   - cmd.exec: {"command": "shell command"}
4. Combine with && or ; when appropriate
5. Do not require interactive input or selection; choose a safe default
6. Before fs.read, verify path exists using fs.glob
7. Prefer fs.grep first, then fs.read with offset/limit to inspect only the relevant lines
8. Keep fs.read limit small and never exceed 400 lines in one read
9. Only use conversation.ask when you are truly blocked and need a targeted clarification
10. Use skill.invoke only when the user explicitly asked for a skill
11. Prefer fs.edit for precise changes; use fs.write only for full-file creation or full replacement
12. Keep user-facing replies precise, brief, clear, and necessary; avoid filler and repetition
13. When writing code, prefer solutions that are short, elegant, efficient, and readable
14. Add comments only when they are necessary to explain non-obvious logic
15. Prefer lsp.query for definitions, type info, references, symbols, document symbols, implementations, hierarchy, diagnostics, and code actions before falling back to grep-only navigation
16. Output valid JSON: {"summary": "...", "steps": [...], "risks": []}`, originalInput, failedStep, errorMsg)

	planReq := sdk.PlanRequest{
		ConversationID: sessionID,
		Prompt:         recoveryPrompt,
		Context:        nil,
		Intent:         "recovery",
	}
	plan, err := rt.planner.Plan(ctx, planReq)
	if err != nil {
		return sdk.Plan{}, sdk.PlanRequest{}, err
	}
	plan.Status = sdk.PlanStatusDraft
	return plan, planReq, nil
}

// Chat does simple conversation without plan generation.
func (rt *Runtime) Chat(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode) (string, error) {
	resp, err := rt.AgentLoop(ctx, sessionID, input, format, mode)
	if err != nil {
		return "", err
	}
	return resp.Reply, nil
}

// Execute confirms and runs a plan, returning the response payload.
func (rt *Runtime) Execute(ctx context.Context, sessionID string, req sdk.PlanRequest, plan *sdk.Plan) (Response, error) {
	if plan == nil {
		return Response{}, fmt.Errorf("plan is required")
	}
	plan.Status = sdk.PlanStatusConfirmed
	maxRetries := 2
	startIndex := 0
	fatalFailure := false
	var fatalErr error
	results := make([]sdk.ToolResult, 0, len(plan.Steps))

	for retry := 0; retry <= maxRetries; retry++ {
		retryNeeded := false
		for i := startIndex; i < len(plan.Steps); i++ {
			step := plan.Steps[i]
			result, err := rt.ExecuteStep(ctx, sessionID, req, step)
			results = append(results, result)

			if err != nil {
				plan.Steps[i].Status = sdk.StepStatusFailed
				errDetails := buildErrorContext(err, result)
				if errDetails == "" {
					errDetails = err.Error()
				}
				if isNonFatalError(step.Tool, errDetails) {
					continue
				}
				if retry < maxRetries && isRetryableError(errDetails) {
					newPlan, newReq, recoveryErr := rt.PlanWithError(ctx, sessionID, req.Prompt, step.Description, errDetails)
					if recoveryErr != nil {
						fatalFailure = true
						fatalErr = recoveryErr
						break
					}
					if len(newPlan.Steps) > 0 {
						tail := append([]sdk.PlanStep{}, plan.Steps[i+1:]...)
						plan.Steps = append(plan.Steps[:i+1], append(newPlan.Steps, tail...)...)
					}
					req = newReq
					retryNeeded = true
					startIndex = i + 1
					break
				}
				fatalFailure = true
				fatalErr = err
				break
			}

			if result.Success {
				plan.Steps[i].Status = sdk.StepStatusSucceeded
			} else {
				plan.Steps[i].Status = sdk.StepStatusFailed
				plan.Status = sdk.PlanStatusBlocked
			}
		}
		if fatalFailure || !retryNeeded {
			break
		}
	}

	allDone := true
	for _, step := range plan.Steps {
		if step.Status != sdk.StepStatusSucceeded {
			allDone = false
			break
		}
	}
	if allDone {
		plan.Status = sdk.PlanStatusDone
	} else if plan.Status != sdk.PlanStatusBlocked {
		plan.Status = sdk.PlanStatusBlocked
	}

	execSummary := rt.buildExecutionSummary(plan, results)
	_, _ = rt.appendMessage(ctx, sessionID, "assistant", execSummary, nil)

	reply := rt.composeReply(plan, results)
	_ = rt.audit.Record(req, *plan, results)
	_ = rt.persistSession(ctx, sessionID)
	return Response{Plan: *plan, Results: results, Reply: reply}, fatalErr
}

func (rt *Runtime) buildExecutionSummary(plan *sdk.Plan, results []sdk.ToolResult) string {
	var b strings.Builder
	b.WriteString("Executed plan:\n")
	for i, step := range plan.Steps {
		var cmd string
		if c, ok := step.Inputs["command"].(string); ok {
			cmd = c
		} else if p, ok := step.Inputs["path"].(string); ok {
			cmd = step.Tool + " " + p
		} else {
			cmd = step.Description
		}

		status := "success"
		var output string
		for _, r := range results {
			if r.StepID == step.ID {
				if r.Success {
					if r.Data != nil {
						if stdout, ok := r.Data["stdout"].(string); ok {
							output = stdout
						} else if content, ok := r.Data["content"].(string); ok {
							output = content
						}
					}
				} else {
					status = "failed: " + r.Error
				}
				break
			}
		}

		if len(output) > 200 {
			output = output[:200] + "..."
		}
		b.WriteString(fmt.Sprintf("%d. %s -> %s\n", i+1, cmd, status))
		if output != "" && status == "success" {
			b.WriteString(fmt.Sprintf("   Output: %s\n", output))
		}
	}
	return b.String()
}

// ExecuteStep executes a single step of a plan.
func (rt *Runtime) ExecuteStep(ctx context.Context, sessionID string, req sdk.PlanRequest, step sdk.PlanStep) (sdk.ToolResult, error) {
	step.Status = sdk.StepStatusRunning
	result, err := rt.orchestrator.ExecuteStep(ctx, sessionID, step)
	result.Data = rt.truncateToolResult(ctx, sessionID, step.Tool, result.Data)

	if err == nil && !result.Success {
		if result.Error != "" {
			err = fmt.Errorf("%s", result.Error)
		} else {
			err = fmt.Errorf("tool reported failure")
		}
	}

	var execInfo string
	if err != nil {
		step.Status = sdk.StepStatusFailed
		execInfo = fmt.Sprintf("Step failed: %s -> Error: %s", step.Description, buildErrorContext(err, result))
	} else {
		step.Status = sdk.StepStatusSucceeded
		var output string
		if result.Success && result.Data != nil {
			if stdout, ok := result.Data["stdout"].(string); ok {
				output = stdout
			} else if content, ok := result.Data["content"].(string); ok {
				output = content
			}
			if len(output) > 300 {
				output = output[:300] + "..."
			}
		}
		if output != "" {
			execInfo = fmt.Sprintf("Step: %s\nOutput: %s", step.Description, output)
		} else {
			execInfo = fmt.Sprintf("Step: %s -> Success", step.Description)
		}
	}

	partStatus := "completed"
	if err != nil || !result.Success {
		partStatus = "error"
	}
	toolPart := sdk.MessagePart{
		Type:   "tool",
		Tool:   step.Tool,
		Input:  step.Inputs,
		Output: result.Data,
		Error:  result.Error,
		Status: partStatus,
	}

	rt.logger.Info("ExecuteStep appending to conversation", zap.String("sessionID", sessionID), zap.String("execInfo", execInfo[:min(100, len(execInfo))]))
	_, _ = rt.appendMessage(ctx, sessionID, "assistant", execInfo, []sdk.MessagePart{toolPart})

	history := rt.conversation.History(ctx, sessionID)
	rt.logger.Info("History after ExecuteStep", zap.Int("msgCount", len(history)))

	_ = rt.audit.Record(req, sdk.Plan{Steps: []sdk.PlanStep{step}}, []sdk.ToolResult{result})
	return result, err
}

func buildErrorContext(err error, result sdk.ToolResult) string {
	var parts []string
	if err != nil {
		parts = append(parts, strings.TrimSpace(err.Error()))
	}
	if result.Error != "" && (err == nil || !strings.Contains(err.Error(), result.Error)) {
		parts = append(parts, strings.TrimSpace(result.Error))
	}
	if result.Data != nil {
		if stderr, ok := result.Data["stderr"].(string); ok {
			stderr = strings.TrimSpace(stderr)
			if stderr != "" {
				parts = append(parts, "stderr: "+truncate(stderr, 400))
			}
		}
		if exitCode, ok := result.Data["exit_code"]; ok {
			parts = append(parts, fmt.Sprintf("exit_code: %v", exitCode))
		}
		if stdout, ok := result.Data["stdout"].(string); ok {
			stdout = strings.TrimSpace(stdout)
			if stdout != "" {
				parts = append(parts, "stdout: "+truncate(stdout, 400))
			}
		}
	}
	return strings.Join(parts, " | ")
}

func isRetryableError(details string) bool {
	lower := strings.ToLower(details)
	if strings.Contains(lower, "query is required") || strings.Contains(lower, "pattern is required") || strings.Contains(lower, "path is required") {
		return false
	}
	if strings.Contains(lower, "path escapes workspace") {
		return false
	}
	if strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "file does not exist") {
		return false
	}
	return true
}

func isNonFatalError(tool, details string) bool {
	lower := strings.ToLower(details)
	switch tool {
	case "fs.read":
		return strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "file does not exist")
	case "fs.grep", "fs.find", "fs.glob":
		return strings.Contains(lower, "query is required") || strings.Contains(lower, "pattern is required")
	default:
		return false
	}
}

// HandleInput processes a single conversational instruction (plan + execute).
func (rt *Runtime) HandleInput(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode) (Response, error) {
	return rt.AgentLoop(ctx, sessionID, input, format, mode)
}

// StartServer starts the BruteCode REST API server.
func (rt *Runtime) StartServer(ctx context.Context) error {
	return rt.StartAPIServer(ctx)
}

func (rt *Runtime) composeReply(plan *sdk.Plan, results []sdk.ToolResult) string {
	if len(results) == 0 {
		return ""
	}
	last := results[len(results)-1]
	switch planStepTool(plan, last.StepID) {
	case "conversation.echo":
		if text, ok := last.Data["text"].(string); ok {
			return text
		}
	case "conversation.ask":
		return formatAskQuestion(last.Data)
	case "fs.read":
		if content, ok := last.Data["content"].(string); ok {
			return truncate(content, 400)
		}
	case "cmd.exec":
		if out, ok := last.Data["stdout"].(string); ok {
			return truncate(out, 400)
		}
	}
	return ""
}

func formatAskQuestion(data map[string]any) string {
	question, _ := data["question"].(string)
	question = strings.TrimSpace(question)
	if question == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(question)
	options, _ := data["options"].([]string)
	if len(options) == 0 {
		if raw, ok := data["options"].([]any); ok {
			for _, item := range raw {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					options = append(options, strings.TrimSpace(text))
				}
			}
		}
	}
	for i, option := range options {
		b.WriteString(fmt.Sprintf("\n%d. %s", i+1, option))
	}
	if multiple, _ := data["multiple"].(bool); multiple {
		b.WriteString("\nYou may choose more than one option.")
	}
	return b.String()
}

func (rt *Runtime) appendMessage(ctx context.Context, sessionID, role, content string, parts []sdk.MessagePart) (sdk.Message, error) {
	payload := plugin.MessagePayload{
		Role:    role,
		Content: content,
		Parts:   parts,
	}
	if rt.plugins != nil {
		payload = rt.plugins.ApplyMessage(plugin.MessageContext{SessionID: sessionID}, payload)
	}
	return rt.conversation.AppendWithParts(ctx, sessionID, payload.Role, payload.Content, payload.Parts)
}

func planStepTool(plan *sdk.Plan, stepID string) string {
	for _, step := range plan.Steps {
		if step.ID == stepID {
			return step.Tool
		}
	}
	return ""
}

func truncate(text string, max int) string {
	if len(text) <= max {
		return text
	}
	return text[:max] + "..."
}

func estimateMessageTokens(msg sdk.Message) int {
	total := estimateTokens(msg.Content)
	for _, part := range msg.Parts {
		total += estimateTokens(part.Text)
		total += estimateTokens(part.Error)
		total += estimateTokens(renderMapForTokens(part.Input))
		total += estimateTokens(renderMapForTokens(part.Output))
	}
	return total
}

func renderMapForTokens(value map[string]any) string {
	if value == nil {
		return ""
	}
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func (rt *Runtime) truncateToolResult(ctx context.Context, sessionID, tool string, data map[string]any) map[string]any {
	if data == nil {
		return data
	}
	const maxLen = 4000
	const previewLen = 2000
	fields := []string{"stdout", "content", "body", "tree", "patch"}
	outputFiles := map[string]string{}
	truncated := false
	for _, field := range fields {
		value, ok := data[field].(string)
		if !ok || len(value) <= maxLen {
			continue
		}
		path, err := rt.session.SaveToolOutput(ctx, sessionID, tool, []byte(value))
		if err == nil && path != "" {
			outputFiles[field] = path
		}
		data[field] = truncate(value, previewLen)
		truncated = true
	}
	if truncated {
		data["truncated"] = true
		if len(outputFiles) > 0 {
			data["output_files"] = outputFiles
		}
	}
	return data
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func loadSoulPrompt() (string, error) {
	if soulPrompt, ok := loadExternalSoulFile(); ok {
		return strings.TrimSpace(soulPrompt), nil
	}
	return strings.TrimSpace(defaultSoulPromptText), nil
}

func defaultModel(provider string) string {
	switch provider {
	case "openai":
		return "gpt-4o-mini"
	case "minimax", "minmax":
		return "abab6.5s-chat"
	case "glm":
		return "glm-4-flash"
	case "gemini":
		return "gemini-2.0-flash"
	case "deepseek":
		return "deepseek-chat"
	case "anthropic":
		return "claude-sonnet-4-5"
	case "openrouter":
		return "openai/gpt-4o"
	case "azure":
		return "gpt-4o"
	case "ollama":
		return "llama3.2"
	case "lmstudio":
		return "local-model"
	case "groq":
		return "mixtral-8x7b-32768"
	case "mistral":
		return "mistral-large-latest"
	case "cohere":
		return "command-r-plus"
	case "togetherai":
		return "meta-llama/Llama-3.2-90B-Vision-Instruct-Turbo"
	case "perplexity":
		return "llama-3.1-sonar-large-128k-online"
	case "openai-compatible":
		return "default"
	default:
		return "gpt-4o-mini"
	}
}

func chatEndpoint(provider string) string {
	switch provider {
	case "minimax", "minmax":
		return "https://api.minimaxi.com/anthropic/v1/messages"
	case "glm":
		return "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/models"
	case "deepseek":
		return "https://api.deepseek.com/v1/chat/completions"
	case "anthropic":
		return "https://api.anthropic.com/v1/messages"
	case "openrouter":
		return "https://openrouter.ai/api/v1/chat/completions"
	case "groq":
		return "https://api.groq.com/openai/v1/chat/completions"
	case "mistral":
		return "https://api.mistral.ai/v1/chat/completions"
	case "cohere":
		return "https://api.cohere.ai/v2/chat"
	case "togetherai":
		return "https://api.together.ai/v1/chat/completions"
	case "perplexity":
		return "https://api.perplexity.ai/chat/completions"
	default:
		return "https://api.openai.com/v1/chat/completions"
	}
}

func classifyIntent(input string) string {
	lower := strings.ToLower(strings.TrimSpace(input))
	switch {
	case strings.HasPrefix(lower, "run "):
		return "command"
	case strings.HasPrefix(lower, "read "):
		return "read"
	case strings.HasPrefix(lower, "write "):
		return "write"
	default:
		return "conversation"
	}
}

func newLogger(cfg config.Config) (*zap.Logger, error) {
	zapCfg := zap.NewProductionConfig()
	if cfg.Logging.Level != "" {
		if err := zapCfg.Level.UnmarshalText([]byte(cfg.Logging.Level)); err != nil {
			return nil, err
		}
	}
	if cfg.Logging.File != "" {
		if err := os.MkdirAll(filepath.Dir(cfg.Logging.File), 0o755); err != nil {
			return nil, err
		}
		zapCfg.OutputPaths = []string{cfg.Logging.File}
	}
	return zapCfg.Build()
}

type auditWriter struct {
	mu   sync.Mutex
	file *os.File
}

func newAuditWriter(path string) (*auditWriter, error) {
	if path == "" {
		return &auditWriter{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &auditWriter{file: file}, nil
}

func (w *auditWriter) Record(req sdk.PlanRequest, plan sdk.Plan, results []sdk.ToolResult) error {
	if w == nil || w.file == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, step := range plan.Steps {
		var cmd string
		if c, ok := step.Inputs["command"].(string); ok {
			cmd = c
		} else if p, ok := step.Inputs["path"].(string); ok {
			cmd = step.Tool + " " + p
		}

		var output string
		for _, r := range results {
			if r.StepID == step.ID {
				if r.Success && r.Data != nil {
					if stdout, ok := r.Data["stdout"].(string); ok {
						output = stdout
					} else if content, ok := r.Data["content"].(string); ok {
						output = content
					}
				}
				if r.Error != "" {
					output = "Error: " + r.Error
				}
				break
			}
		}

		entry := map[string]any{
			"ts":      time.Now().Format("2006-01-02 15:04:05"),
			"tool":    step.Tool,
			"command": cmd,
			"output":  output,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := w.file.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func (w *auditWriter) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}

type Task struct {
	ID          string           `json:"id"`
	SessionID   string           `json:"session_id"`
	Input       string           `json:"input"`
	Plan        *sdk.Plan        `json:"plan,omitempty"`
	Results     []sdk.ToolResult `json:"results,omitempty"`
	Status      TaskStatus       `json:"status"`
	Reply       string           `json:"reply,omitempty"`
	Error       string           `json:"error,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
	runtime     *Runtime
}

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusPlanning  TaskStatus = "planning"
	TaskStatusExecuting TaskStatus = "executing"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type APIServer struct {
	runtime     *Runtime
	tasks       map[string]*Task
	tasksMu     sync.RWMutex
	mux         *http.ServeMux
	logger      *zap.Logger
	skills      *skill.Loader
	skillsPaths []string
	modelStore  *configstore.Store
	activeSkill string
	monitor     *resourceMonitor
	metrics     *serverMetrics
}

func NewAPIServer(rt *Runtime) *APIServer {
	globalSkillsPath := filepath.Join(configstore.DefaultConfigDir(), "skills")
	projectSkillsPath := filepath.Join(rt.cfg.WorkspaceRoot, ".morpheus", "skills")
	return &APIServer{
		runtime: rt,
		tasks:   make(map[string]*Task),
		mux:     http.NewServeMux(),
		logger:  rt.logger,
		skills:  skill.NewLoaderWithPaths([]string{globalSkillsPath, projectSkillsPath}),
		skillsPaths: []string{
			globalSkillsPath,
			projectSkillsPath,
		},
		modelStore: configstore.NewStore(""),
		monitor:    newResourceMonitor(time.Duration(rt.cfg.Server.Limits.SampleIntervalMs) * time.Millisecond),
		metrics:    rt.metrics,
	}
}

func (rt *Runtime) StartAPIServer(ctx context.Context) error {
	server := NewAPIServer(rt)
	server.registerRoutes()

	srv := &http.Server{
		Addr:    rt.cfg.Server.Listen,
		Handler: server.mux,
	}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	rt.logger.Info("serving REST API", zap.String("addr", rt.cfg.Server.Listen))
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (s *APIServer) registerRoutes() {
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/shell", s.handleShell)
	s.mux.HandleFunc("/api/v1/metrics", s.handleMetrics)
	s.mux.HandleFunc("/api/v1/tasks", s.handleTasks)
	s.mux.HandleFunc("/api/v1/tasks/", s.handleTaskByID)
	s.mux.HandleFunc("/api/v1/plan", s.wrapLimited("plan", s.handlePlan))
	s.mux.HandleFunc("/api/v1/execute", s.wrapLimited("execute", s.handleExecute))
	s.mux.HandleFunc("/api/v1/sessions", s.handleSessions)
	s.mux.HandleFunc("/api/v1/sessions/", s.handleSessionByID)
	s.mux.HandleFunc("/api/v1/skills", s.handleSkills)
	s.mux.HandleFunc("/api/v1/skills/", s.handleSkillByName)
	s.mux.HandleFunc("/api/v1/models", s.handleModels)
	s.mux.HandleFunc("/api/v1/models/select", s.handleModelSelect)
	s.mux.HandleFunc("/api/v1/runs", s.handleRuns)
	s.mux.HandleFunc("/api/v1/runs/", s.handleRunByID)
	s.mux.HandleFunc("/api/v1/remote-file", s.handleRemoteFile)
	s.mux.HandleFunc("/api/v1/ssh-info", s.handleSSHInfo)
	s.mux.HandleFunc("/api/v1/chat", s.wrapLimited("chat", s.handleChat))
	s.mux.HandleFunc("/api/v1/ws", s.handleRemoteWS)
	s.mux.HandleFunc("/repl", s.wrapLimited("repl", s.handleRepl))
	s.mux.HandleFunc("/repl/stream", s.wrapLimited("repl_stream", s.handleReplStream))
}

type remoteFileResponse struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Hash    string `json:"hash"`
	Size    int    `json:"size"`
}

type remoteFileWriteRequest struct {
	Path         string `json:"path"`
	Content      string `json:"content"`
	ExpectedHash string `json:"expected_hash"`
}

type sshInfoResponse struct {
	Host      string `json:"host"`
	User      string `json:"user"`
	Workspace string `json:"workspace"`
}

func (s *APIServer) handleRemoteFile(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if r.Method == http.MethodPost {
		var req remoteFileWriteRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		path = strings.TrimSpace(req.Path)
		if path == "" {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
			return
		}
		s.writeRemoteFile(w, path, req)
		return
	}
	if path == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "path is required"})
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.readRemoteFile(w, path)
}

func (s *APIServer) readRemoteFile(w http.ResponseWriter, rel string) {
	absPath, err := fstool.NewReadTool(s.runtime.cfg.WorkspaceRoot).ResolveForAPI(rel)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			w.WriteHeader(http.StatusNotFound)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(remoteFileResponse{
		Path:    rel,
		Content: string(content),
		Hash:    sha256Hex(content),
		Size:    len(content),
	})
}

func (s *APIServer) writeRemoteFile(w http.ResponseWriter, rel string, req remoteFileWriteRequest) {
	absPath, err := fstool.NewWriteTool(s.runtime.cfg.WorkspaceRoot, 0).ResolveForAPI(rel)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	var currentHash string
	exists := true
	current, err := os.ReadFile(absPath)
	if err != nil {
		if !os.IsNotExist(err) {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		exists = false
		currentHash = ""
	} else {
		currentHash = sha256Hex(current)
	}
	emptyHash := sha256Hex([]byte{})
	if strings.TrimSpace(req.ExpectedHash) == "" || req.ExpectedHash == emptyHash {
		if exists {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "expected_hash is required for existing file"})
			return
		}
	} else if exists && req.ExpectedHash != currentHash {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(remoteFileResponse{
			Path:    rel,
			Content: string(current),
			Hash:    currentHash,
			Size:    len(current),
		})
		return
	}
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	dirInfo, err := os.Stat(dir)
	var dirUID, dirGID int
	if err == nil {
		dirUID = int(dirInfo.Sys().(*syscall.Stat_t).Uid)
		dirGID = int(dirInfo.Sys().(*syscall.Stat_t).Gid)
	} else {
		dirUID = os.Getuid()
		dirGID = os.Getgid()
	}
	umask := syscall.Umask(0)
	syscall.Umask(umask)
	newPerm := os.FileMode(0o666 &^ umask)
	if exists {
		currentPerm := os.FileMode(0)
		if fi, err := os.Stat(absPath); err == nil {
			currentPerm = fi.Mode().Perm()
		}
		newPerm = currentPerm
	}
	if exists {
		if err := os.Rename(absPath, absPath+".bak"); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
	}
	if err := os.WriteFile(absPath, []byte(req.Content), newPerm); err != nil {
		if exists {
			os.Rename(absPath+".bak", absPath)
		}
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	os.Chown(absPath, dirUID, dirGID)
	if exists {
		os.Remove(absPath + ".bak")
	}
	updated := []byte(req.Content)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(remoteFileResponse{
		Path:    rel,
		Content: req.Content,
		Hash:    sha256Hex(updated),
		Size:    len(updated),
	})
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum[:])
}

func (s *APIServer) handleSSHInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	host, _, err := sshHostPortFromRequest(r, s.runtime.cfg.Server.Listen)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	currentUser, err := user.Current()
	if err != nil || strings.TrimSpace(currentUser.Username) == "" {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to determine SSH user"})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sshInfoResponse{
		Host:      host,
		User:      currentUser.Username,
		Workspace: s.runtime.cfg.WorkspaceRoot,
	})
}

func sshHostPortFromRequest(r *http.Request, listen string) (string, string, error) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	port := strings.TrimSpace(r.URL.Query().Get("port"))
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return "", "", fmt.Errorf("unable to determine SSH host")
	}
	parsedHost, parsedPort, err := net.SplitHostPort(host)
	if err == nil {
		if parsedHost != "" {
			host = parsedHost
		}
		if port == "" {
			port = parsedPort
		}
		if strings.Contains(host, ":") {
			host = strings.Trim(host, "[]")
		}
	}
	if port == "" {
		_, listenPort, listenErr := net.SplitHostPort(listen)
		if listenErr == nil && strings.TrimSpace(listenPort) != "" {
			port = listenPort
		} else {
			port = "22"
		}
	}
	return host, port, nil
}

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *APIServer) handleShell(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Command string `json:"command"`
		Workdir string `json:"workdir,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.Command == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "command is required"})
		return
	}

	workdir := req.Workdir
	if workdir == "" {
		workdir = s.runtime.cfg.WorkspaceRoot
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", req.Command)
	cmd.Dir = workdir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	w.Header().Set("Content-Type", "application/json")
	var exitCode int
	var errMsg string
	if err != nil {
		errMsg = err.Error()
	}
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"stdout":    stdout.String(),
		"stderr":    stderr.String(),
		"exit_code": exitCode,
		"success":   err == nil,
		"error":     errMsg,
	})
}

func (s *APIServer) wrapLimited(name string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		atomic.AddInt64(&s.metrics.active, 1)
		defer atomic.AddInt64(&s.metrics.active, -1)

		if s.monitor == nil || !s.runtime.cfg.Server.Limits.Enabled {
			handler(w, r)
			s.metrics.record(time.Since(start), false)
			return
		}
		snapshot := s.monitor.Snapshot()
		if s.runtime.cfg.Server.Limits.MaxCPUPercent > 0 && snapshot.CPUAvailable {
			if snapshot.CPUPercent >= s.runtime.cfg.Server.Limits.MaxCPUPercent {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("server CPU usage too high"))
				s.metrics.record(time.Since(start), true)
				return
			}
		}
		if s.runtime.cfg.Server.Limits.MaxMemoryPercent > 0 {
			if snapshot.MemPercent >= s.runtime.cfg.Server.Limits.MaxMemoryPercent {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte("server memory usage too high"))
				s.metrics.record(time.Since(start), true)
				return
			}
		}
		handler(w, r)
		s.metrics.record(time.Since(start), false)
	}
}

func (s *APIServer) handleRepl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Session     string            `json:"session"`
		Input       string            `json:"input"`
		Attachments []InputAttachment `json:"attachments,omitempty"`
		Format      *OutputFormat     `json:"format,omitempty"`
		Mode        string            `json:"mode,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	mode := s.runtime.defaultAgentMode()
	if strings.TrimSpace(body.Mode) != "" {
		mode = normalizeAgentMode(AgentMode(body.Mode))
	}
	resp, err := s.runtime.AgentLoop(r.Context(), body.Session, UserInput{Text: body.Input, Attachments: body.Attachments}, body.Format, mode)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

type replStreamEvent struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data,omitempty"`
}

func (s *APIServer) handleReplStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("streaming not supported"))
		return
	}

	var body struct {
		Session     string            `json:"session"`
		Input       string            `json:"input"`
		Attachments []InputAttachment `json:"attachments,omitempty"`
		Format      *OutputFormat     `json:"format,omitempty"`
		Mode        string            `json:"mode,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	emit := func(event string, data interface{}) error {
		payload, err := json.Marshal(replStreamEvent{Event: event, Data: data})
		if err != nil {
			return err
		}
		if _, err := w.Write([]byte("data: ")); err != nil {
			return err
		}
		if _, err := w.Write(payload); err != nil {
			return err
		}
		if _, err := w.Write([]byte("\n\n")); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// Keep-alive pings so proxies/terminals don't time out on long runs.
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-r.Context().Done():
				return
			case <-ticker.C:
				_, _ = w.Write([]byte(": ping\n\n"))
				flusher.Flush()
			}
		}
	}()

	mode := s.runtime.defaultAgentMode()
	if strings.TrimSpace(body.Mode) != "" {
		mode = normalizeAgentMode(AgentMode(body.Mode))
	}
	run := s.runtime.startRun(body.Session, UserInput{Text: body.Input, Attachments: body.Attachments}, body.Format, mode)
	_ = emit("run_event", map[string]any{
		"run_id": run.ID,
		"seq":    0,
		"type":   "run_created",
		"data": map[string]any{
			"run_id":     run.ID,
			"session_id": run.SessionID,
			"mode":       string(mode),
		},
	})
	resp, err := s.runtime.AgentLoopStream(r.Context(), body.Session, UserInput{Text: body.Input, Attachments: body.Attachments}, body.Format, mode, emit)
	if err != nil {
		_ = emit("error", map[string]any{"error": err.Error()})
		return
	}
	_ = emit("done", resp)
}

func (s *APIServer) handleTasks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		s.createTask(w, r)
	case http.MethodGet:
		s.listTasks(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) handleTaskByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/tasks/")
	if id == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.getTask(w, r, id)
	case http.MethodDelete:
		s.cancelTask(w, r, id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

type CreateTaskRequest struct {
	Session  string `json:"session"`
	Input    string `json:"input"`
	AutoExec bool   `json:"auto_exec"`
}

type TaskResponse struct {
	ID          string           `json:"id"`
	SessionID   string           `json:"session_id"`
	Input       string           `json:"input"`
	Plan        *sdk.Plan        `json:"plan,omitempty"`
	Results     []sdk.ToolResult `json:"results,omitempty"`
	Status      TaskStatus       `json:"status"`
	Reply       string           `json:"reply,omitempty"`
	Error       string           `json:"error,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	CompletedAt *time.Time       `json:"completed_at,omitempty"`
}

func (s *APIServer) createTask(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.Session == "" {
		req.Session = "api-session-" + time.Now().Format("20060102-150405")
	}

	task := &Task{
		ID:        "task-" + time.Now().Format("20060102-150405.000000"),
		SessionID: req.Session,
		Input:     req.Input,
		Status:    TaskStatusPlanning,
		CreatedAt: time.Now(),
		runtime:   s.runtime,
	}

	s.tasksMu.Lock()
	s.tasks[task.ID] = task
	s.tasksMu.Unlock()

	go s.executeTask(task, req.AutoExec)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(TaskResponse{
		ID:        task.ID,
		SessionID: task.SessionID,
		Input:     task.Input,
		Status:    task.Status,
		CreatedAt: task.CreatedAt,
	})
}

func (s *APIServer) executeTask(task *Task, autoExec bool) {
	ctx := context.Background()

	plan, _, err := task.runtime.Plan(ctx, task.SessionID, UserInput{Text: task.Input})
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
		now := time.Now()
		task.CompletedAt = &now
		s.logger.Error("plan generation failed", zap.Error(err))
		return
	}

	task.Plan = plan

	if !autoExec {
		task.Status = TaskStatusPending
		return
	}

	task.Status = TaskStatusExecuting
	req := sdk.PlanRequest{
		ConversationID: task.SessionID,
		Prompt:         plan.Summary,
		Intent:         "execute",
	}
	resp, err := task.runtime.Execute(ctx, task.SessionID, req, plan)
	task.Plan = &resp.Plan
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
		task.Results = resp.Results
		task.Reply = resp.Reply
	} else {
		task.Results = resp.Results
		task.Status = TaskStatusCompleted
		task.Reply = resp.Reply
	}

	now := time.Now()
	task.CompletedAt = &now
}

func (s *APIServer) listTasks(w http.ResponseWriter, r *http.Request) {
	s.tasksMu.RLock()
	defer s.tasksMu.RUnlock()

	var tasks []TaskResponse
	for _, t := range s.tasks {
		tasks = append(tasks, TaskResponse{
			ID:          t.ID,
			SessionID:   t.SessionID,
			Input:       t.Input,
			Status:      t.Status,
			CreatedAt:   t.CreatedAt,
			CompletedAt: t.CompletedAt,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
}

func (s *APIServer) getTask(w http.ResponseWriter, r *http.Request, id string) {
	s.tasksMu.RLock()
	task, ok := s.tasks[id]
	s.tasksMu.RUnlock()

	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TaskResponse{
		ID:          task.ID,
		SessionID:   task.SessionID,
		Input:       task.Input,
		Plan:        task.Plan,
		Results:     task.Results,
		Status:      task.Status,
		Reply:       task.Reply,
		Error:       task.Error,
		CreatedAt:   task.CreatedAt,
		CompletedAt: task.CompletedAt,
	})
}

func (s *APIServer) cancelTask(w http.ResponseWriter, r *http.Request, id string) {
	s.tasksMu.Lock()
	defer s.tasksMu.Unlock()

	task, ok := s.tasks[id]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	if task.Status == TaskStatusPlanning || task.Status == TaskStatusExecuting {
		task.Status = TaskStatusCancelled
		now := time.Now()
		task.CompletedAt = &now
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(TaskResponse{
		ID:        task.ID,
		SessionID: task.SessionID,
		Status:    task.Status,
	})
}

type PlanRequest struct {
	Session     string            `json:"session"`
	Input       string            `json:"input"`
	Attachments []InputAttachment `json:"attachments,omitempty"`
}

func (s *APIServer) handlePlan(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req PlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.Session == "" {
		req.Session = "api-session-" + time.Now().Format("20060102-150405")
	}

	ctx := context.Background()
	plan, _, err := s.runtime.Plan(ctx, req.Session, UserInput{Text: req.Input, Attachments: req.Attachments})
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(plan)
}

type ExecuteRequest struct {
	Session string   `json:"session"`
	Plan    sdk.Plan `json:"plan"`
}

func (s *APIServer) handleExecute(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req ExecuteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.Session == "" {
		req.Session = "api-session-" + time.Now().Format("20060102-150405")
	}

	ctx := context.Background()
	plan := req.Plan
	execReq := sdk.PlanRequest{
		ConversationID: req.Session,
		Prompt:         plan.Summary,
		Intent:         "execute",
	}
	resp, err := s.runtime.Execute(ctx, req.Session, execReq, &plan)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"plan":    resp.Plan,
		"results": resp.Results,
		"reply":   resp.Reply,
	})
}

type SessionInfo struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *APIServer) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if s.runtime.sessionStore != nil {
		metas, err := s.runtime.sessionStore.ListSessions(r.Context(), query)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		var sessions []SessionInfo
		for _, meta := range metas {
			createdAt := meta.UpdatedAt
			if createdAt.IsZero() {
				createdAt = time.Now().UTC()
			}
			sessions = append(sessions, SessionInfo{ID: meta.SessionID, CreatedAt: createdAt})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
		return
	}

	sessionsPath := s.runtime.cfg.Session.Path
	entries, err := os.ReadDir(sessionsPath)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	var sessions []SessionInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(entry.Name()), query) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		sessions = append(sessions, SessionInfo{
			ID:        entry.Name(),
			CreatedAt: info.ModTime(),
		})
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"sessions": sessions})
}

func (s *APIServer) handleSessionByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/sessions/")
	if path == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if strings.HasSuffix(path, "/load") {
		id := strings.TrimSuffix(path, "/load")
		if id == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		s.loadSession(w, r, id)
		return
	}

	id := path

	switch r.Method {
	case http.MethodGet:
		s.getSession(w, r, id)
	case http.MethodDelete:
		s.deleteSession(w, r, id)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *APIServer) getSession(w http.ResponseWriter, r *http.Request, id string) {
	if s.runtime.sessionStore != nil && s.runtime.sessionStore.HasSession(r.Context(), id) {
		stored, err := s.runtime.sessionStore.LoadSession(r.Context(), id)
		if err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		conversation := s.runtime.sessionStore.ConversationMarkdown(id, stored.Messages)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id":           id,
			"conversation": conversation,
		})
		return
	}
	sessionsPath := s.runtime.cfg.Session.Path
	sessionDir := filepath.Join(sessionsPath, id)

	// Session transcripts are written by internal/session as conversation.raw.md.
	conversationFile := filepath.Join(sessionDir, "conversation.raw.md")
	content, err := os.ReadFile(conversationFile)
	if err != nil {
		// Backward-compat fallback for older sessions.
		fallback := filepath.Join(sessionDir, "conversation.md")
		content, err = os.ReadFile(fallback)
	}
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"id":           id,
		"conversation": string(content),
	})
}

func (s *APIServer) deleteSession(w http.ResponseWriter, r *http.Request, id string) {
	if s.runtime.sessionStore != nil {
		_ = s.runtime.sessionStore.DeleteSession(r.Context(), id)
	}
	sessionsPath := s.runtime.cfg.Session.Path
	sessionDir := filepath.Join(sessionsPath, id)

	if err := os.RemoveAll(sessionDir); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	s.runtime.clearSessionState(id)

	w.WriteHeader(http.StatusNoContent)
}

func (s *APIServer) loadSession(w http.ResponseWriter, r *http.Request, id string) {
	if err := s.runtime.LoadSession(r.Context(), id); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

type SkillListResponse struct {
	Skills      []sdk.SkillMetadata `json:"skills"`
	ActiveSkill string              `json:"active"`
}

func (s *APIServer) ensureSkillsLoaded(ctx context.Context) {
	if len(s.skills.List()) > 0 {
		return
	}
	for _, path := range s.skillsPaths {
		_ = os.MkdirAll(path, 0o755)
	}
	_ = s.skills.Load(ctx)
}

func (s *APIServer) handleSkills(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	s.ensureSkillsLoaded(r.Context())
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	var skills []sdk.SkillMetadata
	if query == "" {
		skills = s.skills.List()
	} else {
		skills = s.skills.Search(query)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(SkillListResponse{Skills: skills, ActiveSkill: s.activeSkill})
}

func (s *APIServer) handleSkillByName(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/skills/")
	name = strings.TrimSuffix(name, "/load")
	name = strings.Trim(name, "/")
	if name == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	s.ensureSkillsLoaded(r.Context())
	skill := s.skills.Get(name)
	if skill == nil {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "skill not found"})
		return
	}
	s.activeSkill = name
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(skill.Describe())
}

type ModelProviderInfo struct {
	Name     string   `json:"name"`
	Models   []string `json:"models"`
	HasToken bool     `json:"has_token"`
}

type ModelsResponse struct {
	Providers []ModelProviderInfo `json:"providers"`
	Current   struct {
		Provider string `json:"provider"`
		Model    string `json:"model"`
	} `json:"current"`
}

func (s *APIServer) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, _ := s.modelStore.Load()
	providers := []string{"openai", "deepseek", "gemini", "glm", "minmax", "builtin"}
	var out []ModelProviderInfo
	for _, provider := range providers {
		models := []string{}
		if provider == "builtin" {
			models = append(models, "keyword")
		} else {
			models = append(models, defaultModel(provider))
		}
		currentModel := s.runtime.cfg.Planner.Model
		if s.runtime.cfg.Planner.Provider == provider && currentModel != "" && !contains(models, currentModel) {
			models = append(models, currentModel)
		}
		if data.Providers != nil {
			if stored, ok := data.Providers[provider]; ok && stored.Model != "" && !contains(models, stored.Model) {
				models = append(models, stored.Model)
			}
		}
		out = append(out, ModelProviderInfo{
			Name:     provider,
			Models:   models,
			HasToken: data.Providers != nil && data.Providers[provider].APIKey != "",
		})
	}
	resp := ModelsResponse{Providers: out}
	resp.Current.Provider = s.runtime.cfg.Planner.Provider
	resp.Current.Model = s.runtime.cfg.Planner.Model
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

type ModelSelectRequest struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"api_key,omitempty"`
	Endpoint string `json:"endpoint,omitempty"`
}

func (s *APIServer) handleModelSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req ModelSelectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	req.Provider = strings.TrimSpace(req.Provider)
	if req.Provider == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "provider is required"})
		return
	}
	if req.Model == "" {
		req.Model = defaultModel(req.Provider)
	}

	data, _ := s.modelStore.Load()
	if data.Providers != nil {
		if stored, ok := data.Providers[req.Provider]; ok {
			if req.APIKey == "" {
				req.APIKey = stored.APIKey
			}
			if req.Endpoint == "" {
				req.Endpoint = stored.Endpoint
			}
		}
	}
	if req.Provider != "builtin" && req.APIKey == "" {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "api_key is required"})
		return
	}

	update := config.PlannerConfig{
		Provider:    req.Provider,
		Model:       req.Model,
		Temperature: s.runtime.cfg.Planner.Temperature,
		APIKey:      req.APIKey,
		Endpoint:    req.Endpoint,
	}
	if err := s.runtime.UpdatePlanner(update); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.APIKey != "" {
		_, _ = s.modelStore.Upsert(configstore.ModelConfig{
			Provider: req.Provider,
			Model:    req.Model,
			APIKey:   req.APIKey,
			Endpoint: req.Endpoint,
		})
	} else {
		if data.Providers != nil {
			data.Current = req.Provider
			_ = s.modelStore.Save(data)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

type ChatRequest struct {
	Session     string            `json:"session"`
	Input       string            `json:"input"`
	Attachments []InputAttachment `json:"attachments,omitempty"`
	Format      *OutputFormat     `json:"format,omitempty"`
	Mode        string            `json:"mode,omitempty"`
}

func (s *APIServer) handleChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	var req ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	if req.Session == "" {
		req.Session = "api-session-" + time.Now().Format("20060102-150405")
	}

	ctx := context.Background()
	mode := s.runtime.defaultAgentMode()
	if strings.TrimSpace(req.Mode) != "" {
		mode = normalizeAgentMode(AgentMode(req.Mode))
	}
	reply, err := s.runtime.Chat(ctx, req.Session, UserInput{Text: req.Input, Attachments: req.Attachments}, req.Format, mode)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"reply": reply})
}
