package app

import (
	"strings"

	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/tools/agenttool"
)

type AgentRegistry struct {
	profiles map[string]agenttool.AgentProfile
	tools    map[string][]string
}

func NewAgentRegistry(cfg config.AgentConfig) *AgentRegistry {
	registry := &AgentRegistry{
		profiles: make(map[string]agenttool.AgentProfile),
		tools:    make(map[string][]string),
	}
	registry.mergeDefaults()
	return registry
}

func (r *AgentRegistry) mergeDefaults() {
	defaults := agenttoolDefaultProfiles()
	for name, profile := range defaults {
		r.profiles[name] = profile
	}
	r.tools["implementer"] = []string{
		"fs.read", "fs.write", "fs.edit", "fs.glob", "fs.grep",
		"cmd.exec", "git.*", "conversation.echo", "mcp.*",
	}
	r.tools["explorer"] = []string{
		"fs.read", "fs.glob", "fs.grep", "lsp.query", "conversation.echo",
	}
	r.tools["reviewer"] = []string{
		"fs.read", "fs.glob", "fs.grep", "git.*", "conversation.echo",
	}
	r.tools["architect"] = []string{
		"fs.read", "fs.glob", "fs.grep", "lsp.query", "conversation.echo",
	}
	r.tools["tester"] = []string{
		"fs.read", "fs.glob", "fs.grep", "cmd.exec", "git.*", "conversation.echo",
	}
	r.tools["devops"] = []string{
		"fs.read", "fs.write", "fs.glob", "fs.grep", "cmd.exec", "git.*", "conversation.echo",
	}
	r.tools["data"] = []string{
		"fs.read", "fs.write", "fs.glob", "fs.grep", "cmd.exec", "conversation.echo",
	}
	r.tools["security"] = []string{
		"fs.read", "fs.glob", "fs.grep", "cmd.exec", "git.*", "conversation.echo",
	}
	r.tools["docs"] = []string{
		"fs.read", "fs.write", "fs.glob", "fs.grep", "conversation.echo",
	}
	r.tools["shell-python-operator"] = []string{
		"fs.read", "fs.write", "fs.edit", "fs.glob", "fs.grep",
		"cmd.exec", "git.*", "conversation.echo", "mcp.*",
	}
}

func (r *AgentRegistry) loadCustom(agents []config.AgentDefinition) {
	for _, agent := range agents {
		if !agent.Enabled {
			delete(r.profiles, agent.Name)
			delete(r.tools, agent.Name)
			continue
		}
		if agent.Name == "" {
			continue
		}
		key := strings.ToLower(agent.Name)
		r.profiles[key] = agenttool.AgentProfile{
			Name:         agent.Name,
			Description:  agent.Description,
			Instructions: agent.Instructions,
		}
		if len(agent.Tools) > 0 {
			r.tools[key] = agent.Tools
		}
	}
}

func (r *AgentRegistry) GetProfile(name string) (agenttool.AgentProfile, bool) {
	profile, ok := r.profiles[strings.ToLower(name)]
	return profile, ok
}

func (r *AgentRegistry) GetTools(name string) []string {
	return r.tools[strings.ToLower(name)]
}

func (r *AgentRegistry) AllProfiles() map[string]agenttool.AgentProfile {
	return r.profiles
}

func (r *AgentRegistry) AllTools() map[string][]string {
	return r.tools
}

func (r *AgentRegistry) AddProfile(profile agenttool.AgentProfile, tools []string) {
	name := strings.ToLower(strings.TrimSpace(profile.Name))
	if name == "" {
		return
	}
	r.profiles[name] = profile
	if len(tools) > 0 {
		r.tools[name] = tools
	}
}
