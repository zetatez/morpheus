package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

type Registry struct {
	mu       sync.RWMutex
	agents   map[string]*Agent
	policies *DecisionMaker
}

type Agent struct {
	Name        string
	Description string
	Mode        AgentMode
	Native      bool
	Hidden      bool
	Prompt      string
	Variant     string
	Model       *ModelOverride
	Permission  PermissionRuleset
}

type ModelOverride struct {
	ProviderID string `yaml:"provider_id"`
	ModelID    string `yaml:"model_id"`
}

type PermissionRuleset []PermissionRule

type PermissionRule struct {
	Permission string `yaml:"permission"`
	Pattern    string `yaml:"pattern"`
	Action     string `yaml:"action"`
}

type PermissionAction string

const (
	PermissionAllow PermissionAction = "allow"
	PermissionDeny  PermissionAction = "deny"
	PermissionAsk   PermissionAction = "ask"
)

func NewRegistry() *Registry {
	return &Registry{
		agents:   make(map[string]*Agent),
		policies: NewDecisionMaker(),
	}
}

func (r *Registry) Register(agent *Agent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agents[strings.ToLower(agent.Name)] = agent
}

func (r *Registry) Get(name string) (*Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[strings.ToLower(name)]
	return agent, ok
}

func (r *Registry) List() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []*Agent
	for _, agent := range r.agents {
		if !agent.Hidden {
			list = append(list, agent)
		}
	}
	return list
}

func (r *Registry) ListAll() []*Agent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var list []*Agent
	for _, agent := range r.agents {
		list = append(list, agent)
	}
	return list
}

func (r *Registry) GetPolicy(name string) (*Policy, bool) {
	return r.policies.Get(name)
}

func (r *Registry) ListPolicies() []*Policy {
	return r.policies.List()
}

func (r *Registry) Decide(ctx context.Context, input DecisionInput) Decision {
	return r.policies.Decide(ctx, input)
}

func (r *Registry) RegisterPolicy(policy *Policy) {
	r.policies.Register(policy)
}

type Executor struct {
	registry *Registry
}

func NewExecutor(registry *Registry) *Executor {
	return &Executor{registry: registry}
}

func (e *Executor) Execute(ctx context.Context, input ExecutionInput) (*ExecutionOutput, error) {
	decision := e.registry.Decide(ctx, DecisionInput{
		Task:           input.Task,
		ContextSize:    input.ContextSize,
		AvailableTools: input.AvailableTools,
		Mode:           input.Mode,
	})

	agent, ok := e.registry.Get(decision.SelectedAgent)
	if !ok {
		return nil, fmt.Errorf("agent not found: %s", decision.SelectedAgent)
	}

	return &ExecutionOutput{
		Agent:         agent,
		Decision:      decision,
		SelectedTools: decision.SelectedTools,
		DeniedTools:   decision.DeniedTools,
	}, nil
}

type ExecutionInput struct {
	Task           string
	ContextSize    int
	AvailableTools []string
	UserTools      []string
	Mode           AgentMode
}

type ExecutionOutput struct {
	Agent         *Agent
	Decision      Decision
	SelectedTools []string
	DeniedTools   []string
}

func (r *Registry) ApplyConfig(agents []AgentConfig, workspaceRoot string) {
	for _, config := range agents {
		agent := &Agent{
			Name:        config.Name,
			Description: config.Description,
			Mode:        AgentMode(config.Mode),
			Native:      false,
			Hidden:      config.Hidden,
			Prompt:      config.Prompt,
			Variant:     config.Variant,
			Model:       config.Model,
			Permission:  parsePermissionRuleset(config.Permission),
		}
		r.Register(agent)

		if len(config.Permission) > 0 {
			policy := &Policy{
				Name:        config.Name,
				Description: config.Description,
				Mode:        AgentMode(config.Mode),
				Tools:       extractAllowedTools(config.Permission),
				DeniedTools: extractDeniedTools(config.Permission),
			}
			r.policies.Register(policy)
		}
	}
}

type AgentConfig struct {
	Name        string         `yaml:"name"`
	Description string         `yaml:"description"`
	Mode        string         `yaml:"mode"`
	Hidden      bool           `yaml:"hidden"`
	Prompt      string         `yaml:"prompt"`
	Variant     string         `yaml:"variant"`
	Model       *ModelOverride `yaml:"model"`
	Permission  map[string]any `yaml:"permission"`
}

func parsePermissionRuleset(perms map[string]any) PermissionRuleset {
	if perms == nil {
		return nil
	}
	var ruleset PermissionRuleset
	for perm, action := range perms {
		actionStr, ok := action.(string)
		if !ok {
			continue
		}
		ruleset = append(ruleset, PermissionRule{
			Permission: perm,
			Pattern:    "*",
			Action:     actionStr,
		})
	}
	return ruleset
}

func extractAllowedTools(perms map[string]any) []string {
	var tools []string
	for perm, action := range perms {
		actionStr, ok := action.(string)
		if !ok {
			continue
		}
		if actionStr == "allow" && perm != "*" {
			tools = append(tools, perm)
		}
	}
	return tools
}

func extractDeniedTools(perms map[string]any) []string {
	var tools []string
	for perm, action := range perms {
		actionStr, ok := action.(string)
		if !ok {
			continue
		}
		if actionStr == "deny" {
			tools = append(tools, perm)
		}
	}
	return tools
}
