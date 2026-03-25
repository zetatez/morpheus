package layer

import (
	"context"
	"fmt"
)

type LayerType string

const (
	LayerCommand LayerType = "command"
	LayerTool    LayerType = "tool"
	LayerSkill   LayerType = "skill"
	LayerAgent   LayerType = "agent"
)

type Layer interface {
	Type() LayerType
	Name() string
	Execute(ctx context.Context, input any) (any, error)
	Describe() string
}

type LayerRegistry struct {
	commands map[string]Layer
	tools    map[string]Layer
	skills   map[string]Layer
	agents   map[string]Layer
}

func NewLayerRegistry() *LayerRegistry {
	return &LayerRegistry{
		commands: make(map[string]Layer),
		tools:    make(map[string]Layer),
		skills:   make(map[string]Layer),
		agents:   make(map[string]Layer),
	}
}

func (r *LayerRegistry) Register(layer Layer) error {
	switch layer.Type() {
	case LayerCommand:
		r.commands[layer.Name()] = layer
	case LayerTool:
		r.tools[layer.Name()] = layer
	case LayerSkill:
		r.skills[layer.Name()] = layer
	case LayerAgent:
		r.agents[layer.Name()] = layer
	default:
		return fmt.Errorf("unknown layer type: %s", layer.Type())
	}
	return nil
}

func (r *LayerRegistry) Get(layerType LayerType, name string) (Layer, bool) {
	switch layerType {
	case LayerCommand:
		l, ok := r.commands[name]
		return l, ok
	case LayerTool:
		l, ok := r.tools[name]
		return l, ok
	case LayerSkill:
		l, ok := r.skills[name]
		return l, ok
	case LayerAgent:
		l, ok := r.agents[name]
		return l, ok
	}
	return nil, false
}

func (r *LayerRegistry) List(layerType LayerType) []Layer {
	switch layerType {
	case LayerCommand:
		return mapToSlice(r.commands)
	case LayerTool:
		return mapToSlice(r.tools)
	case LayerSkill:
		return mapToSlice(r.skills)
	case LayerAgent:
		return mapToSlice(r.agents)
	}
	return nil
}

func mapToSlice(m map[string]Layer) []Layer {
	result := make([]Layer, 0, len(m))
	for _, l := range m {
		result = append(result, l)
	}
	return result
}

type CommandLayer struct {
	name        string
	description string
	execute     func(ctx context.Context, input CommandInput) (CommandOutput, error)
}

type CommandInput struct {
	Command   string
	Args      map[string]string
	Context   map[string]any
	Workspace string
}

type CommandOutput struct {
	Steps     []StepOutput
	Result    string
	Artifacts []Artifact
	Error     error
}

type StepOutput struct {
	Tool     string
	Input    map[string]any
	Output   string
	Approved bool
}

type Artifact struct {
	Type string
	Path string
	Data any
}

func NewCommandLayer(name, description string, execute func(ctx context.Context, input CommandInput) (CommandOutput, error)) *CommandLayer {
	return &CommandLayer{
		name:        name,
		description: description,
		execute:     execute,
	}
}

func (l *CommandLayer) Type() LayerType  { return LayerCommand }
func (l *CommandLayer) Name() string     { return l.name }
func (l *CommandLayer) Describe() string { return l.description }

func (l *CommandLayer) Execute(ctx context.Context, input any) (any, error) {
	cmdInput, ok := input.(CommandInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type: %T", input)
	}
	return l.execute(ctx, cmdInput)
}

type ToolLayer struct {
	name        string
	description string
	tools       []string
	execute     func(ctx context.Context, input ToolInput) (ToolOutput, error)
}

type ToolInput struct {
	Tool     string
	Args     map[string]any
	Session  string
	Approved bool
}

type ToolOutput struct {
	Success bool
	Result  map[string]any
	Error   string
}

func NewToolLayer(name, description string, tools []string, execute func(ctx context.Context, input ToolInput) (ToolOutput, error)) *ToolLayer {
	return &ToolLayer{
		name:        name,
		description: description,
		tools:       tools,
		execute:     execute,
	}
}

func (l *ToolLayer) Type() LayerType  { return LayerTool }
func (l *ToolLayer) Name() string     { return l.name }
func (l *ToolLayer) Describe() string { return l.description }
func (l *ToolLayer) Tools() []string  { return l.tools }

func (l *ToolLayer) Execute(ctx context.Context, input any) (any, error) {
	toolInput, ok := input.(ToolInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type: %T", input)
	}
	return l.execute(ctx, toolInput)
}

type SkillLayer struct {
	name         string
	description  string
	capabilities []string
	allowedTools []string
	loadPrompt   func(level DisclosureLevel) (string, map[string]string, error)
}

type DisclosureLevel int

const (
	LevelMetadata DisclosureLevel = 1 << iota
	LevelBody
	LevelResources
)

func NewSkillLayer(name, description string, capabilities, allowedTools []string, loadPrompt func(level DisclosureLevel) (string, map[string]string, error)) *SkillLayer {
	return &SkillLayer{
		name:         name,
		description:  description,
		capabilities: capabilities,
		allowedTools: allowedTools,
		loadPrompt:   loadPrompt,
	}
}

func (l *SkillLayer) Type() LayerType        { return LayerSkill }
func (l *SkillLayer) Name() string           { return l.name }
func (l *SkillLayer) Describe() string       { return l.description }
func (l *SkillLayer) Capabilities() []string { return l.capabilities }
func (l *SkillLayer) AllowedTools() []string { return l.allowedTools }

func (l *SkillLayer) Execute(ctx context.Context, input any) (any, error) {
	skillInput, ok := input.(SkillInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type: %T", input)
	}

	prompt, resources, err := l.loadPrompt(skillInput.Level)
	if err != nil {
		return nil, err
	}

	return SkillOutput{
		Prompt:    prompt,
		Resources: resources,
		Input:     skillInput.Input,
	}, nil
}

type SkillInput struct {
	Level   DisclosureLevel
	Input   string
	Context map[string]any
}

type SkillOutput struct {
	Prompt     string
	Resources  map[string]string
	Input      string
	Calculated bool
}

type AgentLayer struct {
	name        string
	description string
	mode        AgentMode
	tools       []string
	prompt      string
	execute     func(ctx context.Context, input AgentInput) (AgentOutput, error)
}

type AgentMode string

const (
	ModeBuild   AgentMode = "build"
	ModePlan    AgentMode = "plan"
	ModeExplore AgentMode = "explore"
	ModeCustom  AgentMode = "custom"
)

type AgentInput struct {
	Task       string
	Mode       AgentMode
	Tools      []string
	Context    []Message
	Checkpoint string
}

type AgentOutput struct {
	Response   string
	ToolCalls  []ToolCall
	Summary    string
	Checkpoint string
	Tokens     int
}

type Message struct {
	Role    string
	Content string
}

type ToolCall struct {
	Name      string
	Arguments map[string]any
	Result    any
	Approved  bool
}

func NewAgentLayer(name, description string, mode AgentMode, tools []string, prompt string, execute func(ctx context.Context, input AgentInput) (AgentOutput, error)) *AgentLayer {
	return &AgentLayer{
		name:        name,
		description: description,
		mode:        mode,
		tools:       tools,
		prompt:      prompt,
		execute:     execute,
	}
}

func (l *AgentLayer) Type() LayerType  { return LayerAgent }
func (l *AgentLayer) Name() string     { return l.name }
func (l *AgentLayer) Describe() string { return l.description }
func (l *AgentLayer) Mode() AgentMode  { return l.mode }
func (l *AgentLayer) Tools() []string  { return l.tools }

func (l *AgentLayer) Execute(ctx context.Context, input any) (any, error) {
	agentInput, ok := input.(AgentInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type: %T", input)
	}
	return l.execute(ctx, agentInput)
}

type Orchestrator struct {
	layers *LayerRegistry
}

func NewOrchestrator() *Orchestrator {
	return &Orchestrator{
		layers: NewLayerRegistry(),
	}
}

func (o *Orchestrator) Register(layer Layer) error {
	return o.layers.Register(layer)
}

func (o *Orchestrator) ExecuteCommand(ctx context.Context, name string, input CommandInput) (CommandOutput, error) {
	layer, ok := o.layers.Get(LayerCommand, name)
	if !ok {
		return CommandOutput{}, fmt.Errorf("command not found: %s", name)
	}
	output, err := layer.Execute(ctx, input)
	if err != nil {
		return CommandOutput{}, err
	}
	return output.(CommandOutput), nil
}

func (o *Orchestrator) ExecuteTool(ctx context.Context, name string, input ToolInput) (ToolOutput, error) {
	layer, ok := o.layers.Get(LayerTool, name)
	if !ok {
		return ToolOutput{}, fmt.Errorf("tool not found: %s", name)
	}
	output, err := layer.Execute(ctx, input)
	if err != nil {
		return ToolOutput{}, err
	}
	return output.(ToolOutput), nil
}

func (o *Orchestrator) ExecuteSkill(ctx context.Context, name string, input SkillInput) (SkillOutput, error) {
	layer, ok := o.layers.Get(LayerSkill, name)
	if !ok {
		return SkillOutput{}, fmt.Errorf("skill not found: %s", name)
	}
	output, err := layer.Execute(ctx, input)
	if err != nil {
		return SkillOutput{}, err
	}
	return output.(SkillOutput), nil
}

func (o *Orchestrator) ExecuteAgent(ctx context.Context, name string, input AgentInput) (AgentOutput, error) {
	layer, ok := o.layers.Get(LayerAgent, name)
	if !ok {
		return AgentOutput{}, fmt.Errorf("agent not found: %s", name)
	}
	output, err := layer.Execute(ctx, input)
	if err != nil {
		return AgentOutput{}, err
	}
	return output.(AgentOutput), nil
}
