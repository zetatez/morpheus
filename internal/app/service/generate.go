package service

import (
	"fmt"

	"github.com/zetatez/morpheus/internal/app"
	"github.com/zetatez/morpheus/internal/config"
	"github.com/zetatez/morpheus/internal/effect"
)

type AgentGenerateInput struct {
	Description string
	Model       *config.ModelOverride
}

type AgentGenerateOutput struct {
	Identifier   string
	WhenToUse    string
	SystemPrompt string
}

type AgentGenerator struct {
	registry *app.AgentRegistry
}

func NewAgentGenerator(registry *app.AgentRegistry) *AgentGenerator {
	return &AgentGenerator{registry: registry}
}

func (g *AgentGenerator) Generate(input AgentGenerateInput) (*AgentGenerateOutput, error) {
	existing := g.registry.List()
	existingNames := make([]string, len(existing))
	for i, a := range existing {
		existingNames[i] = a.Name
	}

	return &AgentGenerateOutput{
		Identifier:   "generated_agent",
		WhenToUse:    "Use when: " + input.Description,
		SystemPrompt: fmt.Sprintf("You are a specialized agent for: %s\n\nExisting agents: %v", input.Description, existingNames),
	}, nil
}

func (g *AgentGenerator) CreateAgentFromOutput(output *AgentGenerateOutput) *app.Agent {
	return &app.Agent{
		Name:        output.Identifier,
		Description: output.WhenToUse,
		Mode:        app.AgentKindPrimary,
		Native:      false,
		Prompt:      output.SystemPrompt,
		Options:     make(map[string]any),
	}
}

func AgentGeneratorLayer(registry *app.AgentRegistry) *effect.Layer[*AgentGenerator] {
	return effect.LayerFunc(func(ctx *effect.Context) (*AgentGenerator, error) {
		return NewAgentGenerator(registry), nil
	})
}

type GenerateAgentEffect struct {
	generator *AgentGenerator
	input     AgentGenerateInput
}

func GenerateAgent(
	ctx *effect.Context,
	generator *AgentGenerator,
	input AgentGenerateInput,
) effect.Effect[*AgentGenerateOutput] {
	return func(ctx *effect.Context) (*AgentGenerateOutput, error) {
		return generator.Generate(input)
	}
}

func RegisterGeneratedAgent(
	ctx *effect.Context,
	registry *app.AgentRegistry,
	output *AgentGenerateOutput,
) effect.Effect[*app.Agent] {
	return func(ctx *effect.Context) (*app.Agent, error) {
		agent := &app.Agent{
			Name:        output.Identifier,
			Description: output.WhenToUse,
			Mode:        app.AgentKindPrimary,
			Native:      false,
			Prompt:      output.SystemPrompt,
			Options:     make(map[string]any),
		}
		registry.Register(agent)
		return agent, nil
	}
}

func GenerateAndRegisterAgent(
	ctx *effect.Context,
	registry *app.AgentRegistry,
	generator *AgentGenerator,
	input AgentGenerateInput,
) effect.Effect[*app.Agent] {
	return effect.FlatMap(
		GenerateAgent(ctx, generator, input),
		func(output *AgentGenerateOutput) effect.Effect[*app.Agent] {
			return RegisterGeneratedAgent(ctx, registry, output)
		},
	)
}
