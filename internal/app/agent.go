package app

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/pkg/sdk"
	"gopkg.in/yaml.v3"
)

type AgentKind string

const (
	AgentKindPrimary  AgentKind = "primary"
	AgentKindSubAgent AgentKind = "subagent"
	AgentKindAll      AgentKind = "all"
)

type Agent struct {
	Name        string
	Description string
	Mode        AgentKind
	Native      bool
	Hidden      bool
	TopP        float64
	Temperature float64
	Color       string
	Variant     string
	Prompt      string
	Steps       int
	Options     map[string]any
	Permission  PermissionRuleset
	Model       *ModelOverride
}

type ModelOverride struct {
	ProviderID string
	ModelID    string
}

type AgentRegistry struct {
	mu       sync.RWMutex
	agents   map[string]*Agent
	defaults PermissionRuleset
}

var globalAgentRegistry *AgentRegistry

func init() {
	globalAgentRegistry = NewAgentRegistry()
	registerBuiltinAgents(globalAgentRegistry)
}

func GetAgentRegistry() *AgentRegistry {
	return globalAgentRegistry
}

func NewAgentRegistry() *AgentRegistry {
	return &AgentRegistry{
		agents:   make(map[string]*Agent),
		defaults: defaultPermissionRules(),
	}
}

func (r *AgentRegistry) Register(agent *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if agent.Permission == nil {
		agent.Permission = r.defaults
	} else {
		agent.Permission = MergePermissionRulesets(r.defaults, agent.Permission)
	}
	if agent.Options == nil {
		agent.Options = make(map[string]any)
	}
	r.agents[agent.Name] = agent
}

func (r *AgentRegistry) Get(name string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[name]
	return agent, ok
}

func (r *AgentRegistry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agents := make([]*Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		agents = append(agents, agent)
	}
	return agents
}

func (r *AgentRegistry) ListVisible() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agents := make([]*Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		if agent.Mode == AgentKindSubAgent && agent.Hidden {
			continue
		}
		agents = append(agents, agent)
	}
	return agents
}

func (r *AgentRegistry) ListSubagents() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agents := make([]*Agent, 0, len(r.agents))
	for _, agent := range r.agents {
		if agent.Mode == AgentKindSubAgent {
			agents = append(agents, agent)
		}
	}
	return agents
}

// agentFrontmatter is the YAML frontmatter for markdown-defined agents.
type agentFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Mode        string `yaml:"mode"`
	Hidden      bool   `yaml:"hidden"`
	Model       string `yaml:"model"`
}

var agentFrontmatterRE = regexp.MustCompile(`(?s)^---\n(.+?)\n---`)

// DiscoverAgentsFromMarkdown scans standard directories for agent markdown files
// and registers any found agents. Scans: .morpheus/agent/, .opencode/agent/,
// .morpheus/agents/, .opencode/agents/.
func DiscoverAgentsFromMarkdown(workspaceRoot string) []*Agent {
	var agents []*Agent

	searchDirs := []string{
		filepath.Join(workspaceRoot, ".morpheus", "agent"),
		filepath.Join(workspaceRoot, ".morpheus", "agents"),
		filepath.Join(workspaceRoot, ".opencode", "agent"),
		filepath.Join(workspaceRoot, ".opencode", "agents"),
	}

	for _, dir := range searchDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				continue
			}

			agent, err := parseAgentMarkdown(string(data), entry.Name())
			if err == nil && agent != nil {
				agents = append(agents, agent)
			}
		}
	}

	return agents
}

func parseAgentMarkdown(content, filename string) (*Agent, error) {
	matches := agentFrontmatterRE.FindStringSubmatch(content)
	var fm agentFrontmatter
	var promptContent string

	if matches != nil {
		if err := yaml.Unmarshal([]byte(matches[1]), &fm); err != nil {
			return nil, fmt.Errorf("failed to parse agent frontmatter: %w", err)
		}
		promptContent = strings.TrimSpace(content[len(matches[0]):])
	} else {
		promptContent = strings.TrimSpace(content)
	}

	name := fm.Name
	if name == "" {
		name = strings.TrimSuffix(filename, ".md")
	}

	if name == "" {
		return nil, fmt.Errorf("agent name is empty")
	}

	mode := AgentKindAll
	switch fm.Mode {
	case "primary":
		mode = AgentKindPrimary
	case "subagent":
		mode = AgentKindSubAgent
	}

	modelOverride := &ModelOverride{}
	if fm.Model != "" {
		parts := strings.SplitN(fm.Model, "/", 2)
		if len(parts) == 2 {
			modelOverride.ProviderID = parts[0]
			modelOverride.ModelID = parts[1]
		}
	}

	return &Agent{
		Name:        name,
		Description: fm.Description,
		Mode:        mode,
		Hidden:      fm.Hidden,
		Prompt:      promptContent,
		Model:       modelOverride,
		Permission:  defaultPermissionRules(),
		Options:     make(map[string]any),
	}, nil
}

func (r *AgentRegistry) ApplyConfig(cfg []config.AgentDefinition, workspaceRoot string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Discover agents from markdown files
	mdAgents := DiscoverAgentsFromMarkdown(workspaceRoot)
	for _, agent := range mdAgents {
		if _, exists := r.agents[agent.Name]; !exists {
			r.agents[agent.Name] = agent
		}
	}

	skillDirs := []string{}
	for _, dir := range skillDirs {
		_ = dir
	}

	for _, agentDef := range cfg {
		if agentDef.Name == "" {
			continue
		}

		agent, exists := r.agents[agentDef.Name]

		if !exists {
			agent = &Agent{
				Name:       agentDef.Name,
				Mode:       AgentKindAll,
				Permission: r.defaults,
				Options:    make(map[string]any),
			}
		}

		if agentDef.Description != "" {
			agent.Description = agentDef.Description
		}
		if agentDef.Mode != "" {
			agent.Mode = AgentKind(agentDef.Mode)
		}
		if agentDef.Native {
			agent.Native = agentDef.Native
		}
		if agentDef.Hidden {
			agent.Hidden = agentDef.Hidden
		}
		if agentDef.TopP > 0 {
			agent.TopP = agentDef.TopP
		}
		if agentDef.Temperature > 0 {
			agent.Temperature = agentDef.Temperature
		}
		if agentDef.Color != "" {
			agent.Color = agentDef.Color
		}
		if agentDef.Variant != "" {
			agent.Variant = agentDef.Variant
		}
		if agentDef.Prompt != "" {
			agent.Prompt = agentDef.Prompt
		}
		if agentDef.Steps > 0 {
			agent.Steps = agentDef.Steps
		}
		if agentDef.Model != nil {
			agent.Model = &ModelOverride{
				ProviderID: agentDef.Model.ProviderID,
				ModelID:    agentDef.Model.ModelID,
			}
		}

		for k, v := range agentDef.Options {
			agent.Options[k] = v
		}

		if agentDef.Permission != nil {
			configPerms := parsePermissionConfig(agentDef.Permission, workspaceRoot)
			agent.Permission = MergePermissionRulesets(r.defaults, configPerms)
		}

		if agentDef.Enabled {
			r.agents[agentDef.Name] = agent
		} else if !agentDef.Enabled && exists {
			delete(r.agents, agentDef.Name)
		}
	}
}

func parsePermissionConfig(perms map[string]any, workspaceRoot string) PermissionRuleset {
	var rules PermissionRuleset

	for perm, value := range perms {
		switch v := value.(type) {
		case string:
			rules = append(rules, PermissionRule{
				Permission: expandPermissionKey(perm, workspaceRoot),
				Pattern:    "*",
				Action:     PermissionAction(v),
			})
		case map[string]any:
			for pattern, action := range v {
				pattern = expandPermissionPattern(pattern, workspaceRoot)
				rules = append(rules, PermissionRule{
					Permission: expandPermissionKey(perm, workspaceRoot),
					Pattern:    pattern,
					Action:     PermissionAction(action.(string)),
				})
			}
		}
	}

	return rules
}

func expandPermissionKey(perm, workspaceRoot string) string {
	if perm == "external_directory" && workspaceRoot != "" {
		return filepath.Join(workspaceRoot, "*")
	}
	return perm
}

func expandPermissionPattern(pattern, workspaceRoot string) string {
	if strings.HasPrefix(pattern, ".") && workspaceRoot != "" {
		return filepath.Join(workspaceRoot, pattern)
	}
	return pattern
}

type PermissionRule struct {
	Permission         string
	Pattern            string
	Action             PermissionAction
	GracePeriodSeconds int
	MaxRequests        int
	WindowSeconds      int
}

type PermissionAction string

const (
	PermissionAllow PermissionAction = "allow"
	PermissionDeny  PermissionAction = "deny"
	PermissionAsk   PermissionAction = "ask"
)

type PermissionRuleset []PermissionRule

func defaultPermissionRules() PermissionRuleset {
	return PermissionRuleset{
		{Permission: "*", Pattern: "*", Action: PermissionAllow},
		{Permission: "doom_loop", Pattern: "*", Action: PermissionAsk},
		{Permission: "question", Pattern: "*", Action: PermissionDeny},
		{Permission: "plan_enter", Pattern: "*", Action: PermissionDeny},
		{Permission: "plan_exit", Pattern: "*", Action: PermissionDeny},
		{Permission: "read", Pattern: "*.env", Action: PermissionAsk},
		{Permission: "read", Pattern: "*.env.*", Action: PermissionAsk},
		{Permission: "read", Pattern: "*.env.example", Action: PermissionAllow},
	}
}

func MergePermissionRulesets(base ...PermissionRuleset) PermissionRuleset {
	var result PermissionRuleset
	for _, ruleset := range base {
		result = append(result, ruleset...)
	}
	return result
}

func EvaluatePermission(permission, pattern string, rulesets ...PermissionRuleset) PermissionRule {
	for _, ruleset := range rulesets {
		for _, rule := range ruleset {
			if matchPermission(permission, rule.Permission) && matchPattern(pattern, rule.Pattern) {
				return rule
			}
		}
	}
	return PermissionRule{Permission: permission, Pattern: pattern, Action: PermissionDeny}
}

func EvaluateToolPermission(tool, pattern string, rulesets ...PermissionRuleset) PermissionRule {
	perm := ToolPermission(tool)
	return EvaluatePermission(perm, pattern, rulesets...)
}

func matchPermission(perm, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(perm, prefix)
	}
	return perm == pattern
}

func matchPattern(path, pattern string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		if len(prefix) == 0 {
			return true
		}
		if len(path) >= len(prefix) && path[:len(prefix)] == prefix {
			return true
		}
		if strings.HasPrefix(path, prefix) {
			return true
		}
		return false
	}
	return path == pattern
}

type AgentToolSpec struct {
	agent *Agent
}

func (s *AgentToolSpec) Name() string { return "agent." + s.agent.Name }

func (s *AgentToolSpec) Describe() string {
	return s.agent.Description
}

func (s *AgentToolSpec) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt": map[string]any{
				"type":        "string",
				"description": "Task to delegate to this agent",
			},
		},
		"required": []string{"prompt"},
	}
}

func (s *AgentToolSpec) provider() string { return "" }
func (s *AgentToolSpec) Auth() string     { return "" }

var _ sdk.ToolSpec = (*AgentToolSpec)(nil)
