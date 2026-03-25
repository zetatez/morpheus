package skilltool

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/zetatez/morpheus/internal/skill"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type Tool struct {
	loader          *skill.Loader
	allow           func(context.Context, string, string) error
	once            sync.Once
	err             error
	availableSkills string
}

func New(loader *skill.Loader, allow func(context.Context, string, string) error) *Tool {
	return &Tool{loader: loader, allow: allow}
}

func (t *Tool) Name() string { return "skill.invoke" }

func (t *Tool) Describe() string {
	skills := t.listSkillsForDescription()
	return fmt.Sprintf("Invoke a configured local skill by name. Available skills:\n%s", skills)
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":  map[string]any{"type": "string"},
			"query": map[string]any{"type": "string"},
			"input": map[string]any{"type": "object"},
		},
	}
}

func (t *Tool) listSkillsForDescription() string {
	if t.loader == nil {
		return "<no skills available>"
	}
	_ = t.loader.Load(context.Background())
	skills := t.loader.List()
	if len(skills) == 0 {
		return "<no skills available>"
	}
	var lines []string
	for _, s := range skills {
		lines = append(lines, fmt.Sprintf("- %s: %s", s.Name, s.Description))
	}
	return strings.Join(lines, "\n")
}

func formatSkillsForLLM(skills []sdk.SkillMetadata) string {
	if len(skills) == 0 {
		return "<available_skills></available_skills>"
	}
	var lines []string
	lines = append(lines, "<available_skills>")
	for _, s := range skills {
		lines = append(lines, fmt.Sprintf(`<skill><name>%s</name><description>%s</description></skill>`,
			s.Name, s.Description))
	}
	lines = append(lines, "</available_skills>")
	return strings.Join(lines, "\n")
}

func (t *Tool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	if t.loader == nil {
		return sdk.ToolResult{Success: false}, fmt.Errorf("skill loader not configured")
	}
	sessionID, _ := input["session_id"].(string)
	t.once.Do(func() {
		t.err = t.loader.Load(ctx)
	})
	if t.err != nil {
		return sdk.ToolResult{Success: false}, t.err
	}
	name, _ := input["name"].(string)
	query, _ := input["query"].(string)
	name = strings.TrimSpace(name)
	query = strings.TrimSpace(query)
	if name != "" && t.allow != nil {
		if err := t.allow(ctx, sessionID, name); err != nil {
			return sdk.ToolResult{Success: false}, err
		}
	}
	if name == "" {
		var skills []sdk.SkillMetadata
		if query == "" {
			skills = t.loader.List()
		} else {
			skills = t.loader.Search(query)
		}
		return sdk.ToolResult{
			Success: true,
			Data: map[string]any{
				"skills":         skills,
				"skills_for_llm": formatSkillsForLLM(skills),
				"count":          len(skills),
				"query":          query,
				"mode":           "list",
			},
		}, nil
	}
	payload, _ := input["input"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	out, err := t.loader.Invoke(ctx, name, payload)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"name":   name,
			"output": out,
			"mode":   "invoke",
		},
	}, nil
}
