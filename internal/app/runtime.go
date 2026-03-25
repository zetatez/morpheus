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

	"github.com/zetatez/morpheus/internal/app/prompts"
	"github.com/zetatez/morpheus/internal/attestation"
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
	"github.com/zetatez/morpheus/internal/tools/codesearch"
	fstool "github.com/zetatez/morpheus/internal/tools/fs"
	"github.com/zetatez/morpheus/internal/tools/lsp"
	"github.com/zetatez/morpheus/internal/tools/mcp"
	"github.com/zetatez/morpheus/internal/tools/registry"
	"github.com/zetatez/morpheus/internal/tools/skilltool"
	"github.com/zetatez/morpheus/internal/tools/todotool"
	"github.com/zetatez/morpheus/internal/tools/webfetch"
	"github.com/zetatez/morpheus/internal/tools/websearch"
	"github.com/zetatez/morpheus/pkg/sdk"
)

var summaryHTTPClient = &http.Client{
	Timeout: 30 * time.Second,
}

type Runtime struct {
	cfg                config.Config
	logger             *zap.Logger
	conversation       *convo.Manager
	planner            sdk.Planner
	orchestrator       *execpkg.Orchestrator
	registry           *registry.Registry
	toolManager        *sdk.ToolManager
	audit              *auditWriter
	session            *session.Writer
	sessionStore       *session.Store
	plugins            *plugin.Registry
	skills             *skill.Loader
	agentRegistry      *AgentRegistry
	subagents          *subagent.Loader
	metrics            *serverMetrics
	runs               *runStore
	sessionManager     *SessionManager
	teamState          sync.Map
	compactionPipeline *CompactionPipeline
}

type AgentMode string

const (
	AgentModeBuild AgentMode = "build"
	AgentModePlan  AgentMode = "plan"
)

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

	if err := runAttestationCheck(logger); err != nil {
		return nil, err
	}

	if err := prompts.Load(); err != nil {
		logger.Warn("failed to load prompts", zap.Error(err))
	} else {
		logger.Info("prompts loaded",
			zap.Int("system", len(prompts.System)),
			zap.Int("coding", len(prompts.Coding)),
			zap.Int("debug", len(prompts.Debug)),
			zap.Int("testing", len(prompts.Testing)),
			zap.Int("refactor", len(prompts.Refactor)))
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
		if isEmpty(sessionID) {
			return
		}
		text := formatMCPResourceUpdate(server, uri, payload)
		part := mcpResourceUpdatePart(server, uri, payload)
		if isNotEmpty(text) {
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
	toolMgr := sdk.NewToolManager(reg, nil)
	toolMgr.SetNameNormalizer(sdk.NormalizeToolName)

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
	agentRegistry := NewAgentRegistry()
	agentRegistry.ApplyConfig(cfg.Agent.Agents, cfg.WorkspaceRoot)
	metrics := newServerMetrics()
	rt := &Runtime{
		cfg:            cfg,
		logger:         logger,
		conversation:   conv,
		planner:        planner,
		orchestrator:   orch,
		registry:       reg,
		toolManager:    toolMgr,
		audit:          audit,
		session:        trans,
		sessionStore:   store,
		plugins:        plugins,
		skills:         skills,
		agentRegistry:  agentRegistry,
		subagents:      subagents,
		metrics:        metrics,
		runs:           newRunStore(),
		sessionManager: NewSessionManager(),
	}
	rt.recoverRunsOnStartup(ctx)
	if tool, ok := reg.Get("task"); ok {
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
	if tool, ok := reg.Get("skill"); ok {
		if skillInvoke, ok := tool.(*skilltool.Tool); ok {
			*skillInvoke = *skilltool.New(skills, rt.ensureSkillAllowed)
		}
	}
	if tool, ok := reg.Get("todowrite"); ok {
		if todoWrite, ok := tool.(*todotool.Tool); ok {
			*todoWrite = *todotool.New(rt)
		}
	}
	if tool, ok := reg.Get("lsp"); ok {
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
		websearch.NewTool(websearch.ProviderDuckDuckGo, ""),
		codesearch.NewTool(codesearch.BackendSearchcode, "", ""),
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
	return rt.RunSubAgentWithBackground(ctx, prompt, allowedTools, false)
}

func (rt *Runtime) RunSubAgentWithBackground(ctx context.Context, prompt string, allowedTools []string, background bool) (string, error) {
	return rt.runSubAgentImpl(ctx, prompt, allowedTools, 0, background, false)
}

func (rt *Runtime) RunSubAgentFork(ctx context.Context, prompt string, allowedTools []string) (string, error) {
	return rt.runSubAgentImpl(ctx, prompt, allowedTools, 0, false, true)
}

func (rt *Runtime) runSubAgentImpl(ctx context.Context, prompt string, allowedTools []string, depth int, isBackground bool, forkIsolated bool) (string, error) {
	const maxSubagentDepth = 3

	if depth >= maxSubagentDepth {
		return "", fmt.Errorf("max subagent nesting depth (%d) exceeded", maxSubagentDepth)
	}

	subID := fmt.Sprintf("subagent-%d-%d", time.Now().UnixNano(), depth)
	ctx = withTeamSession(ctx, subID)
	ctx = withSubagentDepth(ctx, depth+1)
	if forkIsolated {
		ctx = withForkIsolation(ctx, true)
	}

	if len(allowedTools) > 0 {
		ctx = execpkg.WithAllowedTools(ctx, allowedTools)
	}
	if teamID := agentTeamIDFromContext(ctx); teamID != "" && !forkIsolated {
		ctx = withAgentTeam(ctx, teamID)
	}

	if isBackground {
		go func() {
			_, _ = rt.AgentLoop(ctx, subID, UserInput{Text: prompt}, nil, AgentModeBuild)
		}()
		return "background agent started: " + subID, nil
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
	if rt.subagents != nil {
		if def, ok, _ := rt.subagents.LoadByName(strings.ToLower(profile.Name)); ok {
			allowedTools = def.Tools
		}
	}

	return rt.RunSubAgent(ctx, rolePrompt, allowedTools)
}

func (rt *Runtime) SendTeamMessage(ctx context.Context, from, to, kind, content, replyTo, threadID string, broadcast bool) (map[string]any, error) {
	sessionID := teamSessionIDFromContext(ctx)
	sessionID = normalizeSessionID(sessionID)
	return rt.sendTeamMessage(ctx, sessionID, from, to, kind, content, replyTo, threadID, broadcast)
}

func (rt *Runtime) allowMentionedSkills(sessionID, input string) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, name := range extractRequestedSkills(input) {
		state.AllowedSkills[strings.ToLower(name)] = struct{}{}
	}
}

func (rt *Runtime) allowMentionedSubagents(sessionID, input string) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	for _, name := range extractRequestedSubagents(input) {
		state.AllowedSubagents[strings.ToLower(name)] = struct{}{}
	}
}

func (rt *Runtime) allowSkill(sessionID, name string) {
	sessionID = normalizeSessionID(sessionID)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.AllowedSkills[name] = struct{}{}
}

func (rt *Runtime) setPendingConfirmation(sessionID string, pending PendingConfirmation) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.PendingConfirm = &pending
}

func (rt *Runtime) getPendingConfirmation(sessionID string) (PendingConfirmation, bool) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.PendingConfirm != nil {
		return *state.PendingConfirm, true
	}
	return PendingConfirmation{}, false
}

func (rt *Runtime) clearPendingConfirmation(sessionID string) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.PendingConfirm = nil
}

func (rt *Runtime) isPermissionApproved(sessionID, tool, pattern string) bool {
	sessionID = normalizeSessionID(sessionID)
	return rt.sessionManager.IsPermissionApproved(sessionID, tool, pattern)
}

func (rt *Runtime) approvePermission(sessionID, tool, pattern string) {
	sessionID = normalizeSessionID(sessionID)
	rt.sessionManager.ApprovePermission(sessionID, tool, pattern)
}

func (rt *Runtime) ensureSkillAllowed(ctx context.Context, sessionID, name string) error {
	sessionID = normalizeSessionID(sessionID)
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return nil
	}
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.RLock()
	_, ok := state.AllowedSkills[name]
	state.mu.RUnlock()
	if ok {
		return nil
	}
	return fmt.Errorf("skill.invoke is only allowed after the user explicitly requests that skill by name in the conversation")
}

func (rt *Runtime) allowedSubagentNames(sessionID string) []string {
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	allowed := make([]string, 0, len(state.AllowedSubagents))
	for name := range state.AllowedSubagents {
		allowed = append(allowed, name)
	}
	sort.Strings(allowed)
	return allowed
}

func (rt *Runtime) restoreAllowedSubagents(sessionID string, names []string) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.AllowedSubagents = make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		state.AllowedSubagents[name] = struct{}{}
	}
}

func (rt *Runtime) isSubagentAllowed(sessionID, name string) bool {
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	_, ok := state.AllowedSubagents[strings.ToLower(strings.TrimSpace(name))]
	return ok
}

func (rt *Runtime) clearSessionState(sessionID string) {
	sessionID = normalizeSessionID(sessionID)
	rt.conversation.Clear(sessionID)
	rt.sessionManager.Delete(sessionID)
	rt.syncMemoryToSession(sessionID)
}

func (rt *Runtime) sessionMetadata(sessionID, summary string) session.Metadata {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.RLock()
	allowedSkills := make([]string, 0, len(state.AllowedSkills))
	for name := range state.AllowedSkills {
		allowedSkills = append(allowedSkills, name)
	}
	state.mu.RUnlock()
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
		CompressedAt:     rt.compressionLastCompressedAt(sessionID),
		IsCodeTask:       rt.isCodeTask(sessionID),
		Checkpoints:      rt.checkpointEntries(sessionID),
		CheckpointedAt:   rt.lastCheckpointAt(sessionID),
	}
}

func (rt *Runtime) restoreSessionMetadata(sessionID string, meta session.Metadata) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.AllowedSkills = make(map[string]struct{}, len(meta.AllowedSkills))
	for _, name := range meta.AllowedSkills {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		state.AllowedSkills[name] = struct{}{}
	}
	rt.restoreAllowedSubagents(sessionID, meta.AllowedSubagents)
	rt.setLastTaskNote(sessionID, meta.LastTaskNote)
	rt.setIsCodeTask(sessionID, meta.IsCodeTask)
	rt.restoreCheckpoints(sessionID, meta.Checkpoints, meta.CheckpointedAt)
	if !meta.CompressedAt.IsZero() {
		state.CompressionState.mu.Lock()
		state.CompressionState.LastCompressedAt = meta.CompressedAt
		state.CompressionState.mu.Unlock()
	}
}

func (rt *Runtime) sessionCheckpointState(sessionID string) *SessionCheckpointState {
	state := rt.sessionManager.GetOrCreate(sessionID)
	return state.Checkpoints
}

func (rt *Runtime) checkpointEntries(sessionID string) []session.CheckpointMetadata {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	if len(state.Entries) == 0 {
		return nil
	}
	out := make([]session.CheckpointMetadata, len(state.Entries))
	copy(out, state.Entries)
	return out
}

func (rt *Runtime) lastCheckpointAt(sessionID string) time.Time {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.LastCheckpointAt
}

func (rt *Runtime) restoreCheckpoints(sessionID string, entries []session.CheckpointMetadata, checkpointedAt time.Time) {
	state := rt.sessionCheckpointState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Entries = append([]session.CheckpointMetadata(nil), entries...)
	state.LastCheckpointAt = checkpointedAt
	state.Seq = int64(len(entries))
}

const (
	checkpointCooldown       = 30 * time.Second
	maxCheckpointsPerSession = 20
)

func (rt *Runtime) maybeCreateCheckpoint(ctx context.Context, sessionID, toolName string, inputs map[string]any) {
	sessionID = normalizeSessionID(sessionID)
	if !rt.isGitWorkspace() {
		return
	}
	state := rt.sessionCheckpointState(sessionID)
	state.mu.RLock()
	last := state.LastCheckpointAt
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
	var stale []session.CheckpointMetadata
	if len(state.Entries) >= maxCheckpointsPerSession {
		stale = append(stale, state.Entries[maxCheckpointsPerSession-1:]...)
	}
	state.Entries = append([]session.CheckpointMetadata{checkpoint}, state.Entries...)
	if len(state.Entries) > maxCheckpointsPerSession {
		state.Entries = state.Entries[:maxCheckpointsPerSession]
	}
	state.LastCheckpointAt = checkpoint.CreatedAt
	state.Seq++
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
	if len(state.Entries) <= keep {
		return 0
	}
	removed := append([]session.CheckpointMetadata(nil), state.Entries[keep:]...)
	state.Entries = append([]session.CheckpointMetadata(nil), state.Entries[:keep]...)
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
	for _, entry := range state.Entries {
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
	if len(state.Entries) == 0 {
		return
	}
	filtered := state.Entries[:0]
	for _, entry := range state.Entries {
		if entry.ID == id {
			continue
		}
		filtered = append(filtered, entry)
	}
	state.Entries = filtered
}

func (rt *Runtime) isGitWorkspace() bool {
	workspace := strings.TrimSpace(rt.cfg.WorkspaceRoot)
	if workspace == "" {
		workspace = "."
	}
	info, err := os.Stat(filepath.Join(workspace, ".git"))
	return err == nil && !info.IsDir() || err == nil && info.IsDir()
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
			pending := PendingConfirmation{
				Kind:      "checkpoint_rollback",
				Tool:      "checkpoint.rollback",
				Inputs:    map[string]any{"id": args[2], "status": status, "drop": dropAfterRollback},
				CreatedAt: time.Now(),
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
	return Response{}, true, fmt.Errorf("usage: /team [status]")
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

func (rt *Runtime) sessionTaskState(sessionID string) *SessionTaskState {
	state := rt.sessionManager.GetOrCreate(sessionID)
	return state.TaskState
}

func (rt *Runtime) setLastTaskNote(sessionID, note string) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionTaskState(sessionID)
	state.mu.Lock()
	state.LastTaskNote = strings.TrimSpace(note)
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

func (rt *Runtime) sessionMemoryState(sessionID string) *SessionMemoryState {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	return state.SessionMemory
}

func (rt *Runtime) setShortTermMemory(sessionID, content string) {
	state := rt.sessionMemoryState(sessionID)
	state.mu.Lock()
	state.ShortTerm = strings.TrimSpace(content)
	state.mu.Unlock()
}

func (rt *Runtime) setLongTermMemory(sessionID, content string) {
	state := rt.sessionMemoryState(sessionID)
	state.mu.Lock()
	state.LongTerm = strings.TrimSpace(content)
	state.mu.Unlock()
}

func (rt *Runtime) shortTermMemory(sessionID string) string {
	state := rt.sessionMemoryState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.ShortTerm
}

func (rt *Runtime) longTermMemory(sessionID string) string {
	state := rt.sessionMemoryState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.LongTerm
}

func (rt *Runtime) memorySystem(sessionID string) *MemorySystem {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionManager.GetOrCreate(sessionID)
	if state.MemorySystem == nil {
		state.MemorySystem = NewMemorySystem(sessionID)
	}
	return state.MemorySystem
}

func (rt *Runtime) syncMemoryToSession(sessionID string) {
	ms := rt.memorySystem(sessionID)
	ms.SetWorkingMemory(rt.shortTermMemory(sessionID))

	semantic := ms.GetSemantic("", 10)
	var semanticLines []string
	for _, entry := range semantic {
		semanticLines = append(semanticLines, entry.Content)
	}
	if len(semanticLines) > 0 {
		rt.setLongTermMemory(sessionID, strings.Join(semanticLines, "\n"))
	}
}

func (rt *Runtime) addEpisodicMemory(sessionID, content string, tags []string) {
	ms := rt.memorySystem(sessionID)
	ms.AddEpisodic(MemoryEntry{
		Content: content,
		Tags:    tags,
	})
}

func (rt *Runtime) getRecentEpisodicMemory(sessionID string, limit int) []MemoryEntry {
	return rt.memorySystem(sessionID).GetEpisodic("", limit)
}

func (rt *Runtime) intentState(sessionID string) *SessionIntentState {
	state := rt.sessionManager.GetOrCreate(sessionID)
	return state.IntentCache
}

func (rt *Runtime) getCachedIntent(sessionID, key string) (intentClassification, bool) {
	state := rt.intentState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	intent, ok := state.Entries[key]
	return intent, ok
}

func (rt *Runtime) setCachedIntent(sessionID, key string, intent intentClassification) {
	state := rt.intentState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Entries[key] = intent
	if len(state.Entries) <= 64 {
		return
	}
	for k := range state.Entries {
		delete(state.Entries, k)
		if len(state.Entries) <= 32 {
			break
		}
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

func formatConfirmationPrompt(pending PendingConfirmation) string {
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
	case "write", "edit", "read":
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

func confirmationPayload(pending PendingConfirmation) *ConfirmationPayload {
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
		ReplyOptions: []string{"once", "always", "reject"},
	}
}

func (rt *Runtime) setIsCodeTask(sessionID string, isCode bool) {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionTaskState(sessionID)
	state.mu.Lock()
	state.IsCodeTask = isCode
	state.mu.Unlock()
}

func (rt *Runtime) isCodeTask(sessionID string) bool {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionTaskState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.IsCodeTask
}

func (rt *Runtime) lastTaskNote(sessionID string) string {
	sessionID = normalizeSessionID(sessionID)
	state := rt.sessionTaskState(sessionID)
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.LastTaskNote
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
	case "edit", "write":
		return "review the edited files and run relevant tests or checks"
	case "bash":
		return "review command output and decide whether any follow-up changes are needed"
	case "skill":
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
	rt.syncMemoryToSession(sessionID)
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

// checkAndCompress runs the 4-layer compression pipeline
func (rt *Runtime) checkAndCompress(ctx context.Context, sessionID string) {
	pipeline := rt.compactionPipeline
	if pipeline == nil {
		pipeline = NewCompactionPipeline()
		rt.compactionPipeline = pipeline
	}

	ms := rt.memorySystem(sessionID)
	messages := rt.conversation.Messages(sessionID)
	if len(messages) == 0 {
		return
	}

	// Get turn count for constraint tracking
	turnCount := 0
	for _, msg := range messages {
		if msg.Role == "user" {
			turnCount++
		}
	}

	// Estimate total tokens using advanced estimator
	var totalTokens int64
	for _, msg := range messages {
		totalTokens += int64(AdvancedTokenEstimate(msg.Content))
		for _, part := range msg.Parts {
			totalTokens += int64(AdvancedTokenEstimate(part.Text))
		}
	}

	// Layer 1: Micro-compaction on tool outputs (in-place, always runs first)
	taskContext := rt.buildTaskContext(sessionID)
	messages = pipeline.micro.ApplyToMessages(messages, taskContext)

	// Check if further compression is needed
	modelMaxTokens := rt.getModelMaxTokens()
	if totalTokens < int64(float64(modelMaxTokens)*AutoCompactTriggerRatio) && !rt.isLongRunningTask(sessionID) {
		// No compression needed
		rt.conversation.ReplaceMessages(sessionID, messages)
		return
	}

	// Layer 2: Context folding
	foldedMessages, _ := pipeline.fold.FoldMessages(messages)
	if len(foldedMessages) < len(messages) {
		messages = foldedMessages
		rt.logger.Info("context folding applied", zap.Int("original", len(messages)), zap.Int("folded", len(foldedMessages)))
	}

	// Recalculate tokens after folding
	var foldedTokens int64
	for _, msg := range messages {
		foldedTokens += int64(AdvancedTokenEstimate(msg.Content))
	}

	// Layer 3: Auto-compaction (LLM summarization)
	if pipeline.auto.ShouldTrigger(foldedTokens, int64(modelMaxTokens)) {
		rt.logger.Info("auto-compaction triggered", zap.Int64("tokens", foldedTokens), zap.Int64("threshold", int64(float64(modelMaxTokens)*AutoCompactTriggerRatio)))

		// Extract and preserve critical constraints
		constraints := pipeline.memory.PreserveConstraints(messages, turnCount)
		constraints = pipeline.memory.FilterValidConstraints(constraints, turnCount)

		// Build memory context for compression prompt
		memoryCtx := ms.GetSemantic("", 5)
		var memoryLines []string
		for _, entry := range memoryCtx {
			memoryLines = append(memoryLines, entry.Content)
		}

		// Compact messages (keep recent)
		recentCount := 4
		if len(messages) > recentCount {
			recent := messages[len(messages)-recentCount:]
			old := messages[:len(messages)-recentCount]

			// Build compression prompt
			compactPrompt := pipeline.auto.BuildCompactPrompt(old, constraints, strings.Join(memoryLines, "\n"))
			summary, err := rt.generateSummary(ctx, compactPrompt)
			if err == nil && summary != "" {
				cleanSummary := strings.TrimSpace(summary)
				if existing := strings.TrimSpace(rt.conversation.Summary(sessionID)); existing != "" {
					cleanSummary = existing + "\n\n" + cleanSummary
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

				// Layer 4: Preserve constraints in memory for next compression cycle
				for _, c := range constraints {
					ms.AddSemantic(MemoryEntry{
						Layer:    MemoryLayerSemantic,
						Content:  fmt.Sprintf("[Constraint from turn %d] %s", c.TurnNumber, c.Content),
						Tags:     []string{"constraint", c.Source},
						Metadata: map[string]any{"priority": c.Priority, "turn": c.TurnNumber},
					})
				}

				messages = recent
				_ = rt.session.SaveSummary(ctx, sessionID, cleanSummary)
			}
		}
	}

	rt.conversation.ReplaceMessages(sessionID, messages)
}

func (rt *Runtime) buildTaskContext(sessionID string) string {
	var b strings.Builder
	if summary := rt.conversation.Summary(sessionID); summary != "" {
		b.WriteString(summary)
		b.WriteString(" ")
	}
	if shortTerm := rt.shortTermMemory(sessionID); shortTerm != "" {
		b.WriteString(shortTerm)
	}
	return b.String()
}

func (rt *Runtime) isLongRunningTask(sessionID string) bool {
	messages := rt.conversation.Messages(sessionID)
	userTurns := 0
	for _, msg := range messages {
		if msg.Role == "user" {
			userTurns++
		}
	}
	return userTurns > 10
}

func (rt *Runtime) getModelMaxTokens() int {
	// Default max tokens for most LLMs (Claude, GPT-4, etc.)
	// This can be extended when config supports MaxTokens
	return 128000
}

func (rt *Runtime) compressHistory(ctx context.Context, sessionID string) {
	state := rt.compressionSessionState(sessionID)
	state.mu.Lock()
	if !state.LastCompressedAt.IsZero() && time.Since(state.LastCompressedAt) < CompressionCooldown {
		state.mu.Unlock()
		return
	}
	state.LastCompressedAt = time.Now().UTC()
	state.mu.Unlock()

	history := rt.conversation.History(ctx, sessionID)
	if len(history) <= 4 {
		return
	}

	recentCount := 4
	recent := history[len(history)-recentCount:]
	old := history[:len(history)-recentCount]

	ms := rt.memorySystem(sessionID)
	episodic := ms.GetEpisodic("", MaxEpisodicMemoryItems)
	episodicFacts := extractEpisodicFacts(episodic)
	semantic := ms.GetSemantic("", 5)
	var semanticLines []string
	for _, entry := range semantic {
		semanticLines = append(semanticLines, entry.Content)
	}

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
	summaryPrompt := buildCompressionPromptWithMemory(oldContent.String(), semanticLines, episodicFacts, isCode)

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

func (rt *Runtime) compressionSessionState(sessionID string) *SessionCompressionState {
	state := rt.sessionManager.GetOrCreate(sessionID)
	return state.CompressionState
}

func (rt *Runtime) compressionLastCompressedAt(sessionID string) time.Time {
	state := rt.compressionSessionState(sessionID)
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.LastCompressedAt
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

func extractEpisodicFacts(episodic []MemoryEntry) string {
	if len(episodic) == 0 {
		return ""
	}
	var facts []string
	toolFreq := make(map[string]int)
	for _, entry := range episodic {
		if entry.Tags != nil {
			for _, tag := range entry.Tags {
				toolFreq[tag]++
			}
		}
	}
	for tool, freq := range toolFreq {
		if freq >= 2 {
			facts = append(facts, fmt.Sprintf("Used %s %d times", tool, freq))
		}
	}
	return strings.Join(facts, "; ")
}

func buildCompressionPromptWithMemory(history string, semanticFacts []string, episodicFacts string, codeTask bool) string {
	var memoryContext strings.Builder
	if len(semanticFacts) > 0 {
		memoryContext.WriteString("Known facts:\n")
		for _, fact := range semanticFacts {
			memoryContext.WriteString("- " + fact + "\n")
		}
	}
	if episodicFacts != "" {
		memoryContext.WriteString("Event patterns: " + episodicFacts + "\n")
	}
	memoryStr := memoryContext.String()
	if codeTask {
		prompt := fmt.Sprintf(`Provide a continuation summary for an engineering task.
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

%s
Conversation history:
%s

Keep it concise and specific, within 300 words.`, memoryStr, history)
		return prompt
	}
	summaryPrompt := fmt.Sprintf(`Summarize this conversation concisely, preserving key facts, decisions, and any pending tasks.

%s
Conversation:
%s

Provide a 3-4 sentence summary that captures the essence.`, memoryStr, history)
	return summaryPrompt
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

	isMinimax := plannerCfg.Provider == "minimax"

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

	if isMinimax {
		httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
	} else {
		switch plannerCfg.Provider {
		case "openai", "glm", "deepseek", "anthropic", "openrouter", "groq", "mistral", "togetherai", "perplexity":
			httpReq.Header.Set("Authorization", "Bearer "+plannerCfg.APIKey)
		}
	}

	resp, err := summaryHTTPClient.Do(httpReq)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", err
	}

	if result.Error.Message != "" {
		return "", fmt.Errorf("minimax error: %s", result.Error.Message)
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
	case "read":
		return strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "file does not exist")
	case "grep", "glob":
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
	case "minimax":
		return "MiniMax-M2.7"
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
	case "minimax":
		return "https://api.minimaxi.com/v1/chat/completions"
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

func runAttestationCheck(logger *zap.Logger) error {
	attestor := attestation.NewAttestor(attestation.AttestationConfig{
		TrustedChecksums:   map[string]string{},
		RequireAttestation: false,
	})
	result, err := attestor.Attest()
	if err != nil {
		logger.Warn("attestation check failed", zap.Error(err))
		return nil
	}
	if result.Valid {
		logger.Info("attestation passed",
			zap.String("type", string(result.AttestationType)),
			zap.String("version", result.Version),
		)
	} else {
		logger.Warn("attestation failed",
			zap.String("type", string(result.AttestationType)),
			zap.String("error", result.Error),
		)
	}
	return nil
}

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusPlanning  TaskStatus = "planning"
	TaskStatusExecuting TaskStatus = "executing"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCancelled TaskStatus = "cancelled"
)

type APIServer struct {
	runtime          *Runtime
	tasks            map[string]*Task
	tasksMu          sync.RWMutex
	mux              *http.ServeMux
	logger           *zap.Logger
	skills           *skill.Loader
	skillsPaths      []string
	modelStore       *configstore.Store
	activeSkill      string
	metrics          *serverMetrics
	eventBroadcaster *eventBroadcaster
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
		modelStore:       configstore.NewStore(""),
		metrics:          rt.metrics,
		eventBroadcaster: newEventBroadcaster(),
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
	s.mux.HandleFunc("/global/health", s.handleHealth)
	s.mux.HandleFunc("/global/event", s.handleGlobalEvent)
	s.mux.HandleFunc("/global/sync-event", s.handleGlobalSyncEvent)
	s.mux.HandleFunc("/global/config", s.handleConfig)
	s.mux.HandleFunc("/config/providers", s.handleConfigProviders)
	s.mux.HandleFunc("/shell", s.handleShell)
	s.mux.HandleFunc("/event", s.handleGlobalEvent)
	s.mux.HandleFunc("/doc", s.handleDoc)
	s.mux.HandleFunc("/permission/", s.handlePermission)
	s.mux.HandleFunc("/permission/{id}/reply", s.handlePermissionReply)
	s.mux.HandleFunc("/question/", s.handleQuestion)
	s.mux.HandleFunc("/question/{id}/reply", s.handleQuestionReply)
	s.mux.HandleFunc("/question/{id}/reject", s.handleQuestionReject)
	s.mux.HandleFunc("/mcp/", s.handleMCP)
	s.mux.HandleFunc("/mcp/{name}/connect", s.handleMCPConnect)
	s.mux.HandleFunc("/mcp/{name}/disconnect", s.handleMCPDisconnect)
	s.mux.HandleFunc("/mcp/{name}/auth", s.handleMCPAuth)
	s.mux.HandleFunc("/mcp/{name}/auth/callback", s.handleMCPAuthCallback)
	s.mux.HandleFunc("/mcp/{name}/auth/authenticate", s.handleMCPAuthenticate)
	s.mux.HandleFunc("/find", s.handleFind)
	s.mux.HandleFunc("/find/file", s.handleFindFile)
	s.mux.HandleFunc("/find/symbol", s.handleFindSymbol)
	s.mux.HandleFunc("/file", s.handleFileList)
	s.mux.HandleFunc("/file/content", s.handleFileContent)
	s.mux.HandleFunc("/file/status", s.handleFileStatus)
	s.mux.HandleFunc("/project/", s.handleProjectList)
	s.mux.HandleFunc("/project/{id}", s.handleProjectByID)
	s.mux.HandleFunc("/project/current", s.handleProjectCurrent)
	s.mux.HandleFunc("/project/git/init", s.handleProjectGitInit)
	s.mux.HandleFunc("/provider/", s.handleProvider)
	s.mux.HandleFunc("/provider/auth", s.handleProviderAuth)
	s.mux.HandleFunc("/provider/{id}/oauth/authorize", s.handleProviderOAuthAuthorize)
	s.mux.HandleFunc("/provider/{id}/oauth/callback", s.handleProviderOAuthCallback)
	s.mux.HandleFunc("/session/", s.handleSessionList)
	s.mux.HandleFunc("/session/status", s.handleSessionStatus)
	s.mux.HandleFunc("/session/{id}", s.handleSessionByID)
	s.mux.HandleFunc("/session/{id}/children", s.handleSessionChildren)
	s.mux.HandleFunc("/session/{id}/todo", s.handleSessionTodo)
	s.mux.HandleFunc("/session/{id}/init", s.handleSessionInit)
	s.mux.HandleFunc("/session/{id}/fork", s.handleSessionFork)
	s.mux.HandleFunc("/session/{id}/abort", s.handleSessionAbort)
	s.mux.HandleFunc("/session/{id}/share", s.handleSessionShare)
	s.mux.HandleFunc("/session/{id}/summarize", s.handleSessionSummarize)
	s.mux.HandleFunc("/session/{id}/diff", s.handleSessionDiff)
	s.mux.HandleFunc("/session/{id}/revert", s.handleSessionRevert)
	s.mux.HandleFunc("/session/{id}/unrevert", s.handleSessionUnrevert)
	s.mux.HandleFunc("/session/{id}/message", s.handleSessionMessage)
	s.mux.HandleFunc("/session/{id}/message/{messageID}", s.handleSessionMessageByID)
	s.mux.HandleFunc("/session/{id}/message/{messageID}/part/{partID}", s.handleSessionMessagePart)
	s.mux.HandleFunc("/session/{id}/command", s.handleSessionCommand)
	s.mux.HandleFunc("/session/{id}/shell", s.handleSessionShell)
	s.mux.HandleFunc("/metrics", s.handleMetrics)
	s.mux.HandleFunc("/tasks/", s.handleTasks)
	s.mux.HandleFunc("/tasks/{id}", s.handleTaskByID)
	s.mux.HandleFunc("/plan", s.wrapLimited("plan", s.handlePlan))
	s.mux.HandleFunc("/execute", s.wrapLimited("execute", s.handleExecute))
	s.mux.HandleFunc("/chat", s.wrapLimited("chat", s.handleChat))
	s.mux.HandleFunc("/skill", s.handleSkillList)
	s.mux.HandleFunc("/skill/{name}", s.handleSkillByName)
	s.mux.HandleFunc("/models/", s.handleModels)
	s.mux.HandleFunc("/models/select", s.handleModelSelect)
	s.mux.HandleFunc("/runs/", s.handleRuns)
	s.mux.HandleFunc("/runs/{id}", s.handleRunByID)
	s.mux.HandleFunc("/repl", s.wrapLimited("repl", s.handleRepl))
	s.mux.HandleFunc("/repl/stream", s.wrapLimited("repl_stream", s.handleReplStream))
	s.mux.HandleFunc("/vim", s.handleRemoteFile)
	s.mux.HandleFunc("/ssh", s.handleSSHInfo)
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
	id := strings.TrimPrefix(r.URL.Path, "/tasks/")
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

func (s *APIServer) handleSessionList(w http.ResponseWriter, r *http.Request) {
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
	path := strings.TrimPrefix(r.URL.Path, "/session/")
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

func (s *APIServer) handleSkillList(w http.ResponseWriter, r *http.Request) {
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
	name := strings.TrimPrefix(r.URL.Path, "/skill/")
	name = strings.TrimSuffix(name, "/load")
	name = strings.Trim(name, "/")

	if name == "" {
		if r.Method == http.MethodGet {
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
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
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
	providers := []string{"openai", "deepseek", "gemini", "glm", "minimax", "builtin"}
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
