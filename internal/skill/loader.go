package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type Loader struct {
	skillsPath string
	skills     map[string]sdk.Skill
}

func NewLoader(skillsPath string) *Loader {
	return &Loader{
		skillsPath: skillsPath,
		skills:     make(map[string]sdk.Skill),
	}
}

func (l *Loader) Load(ctx context.Context) error {
	if l.skillsPath == "" {
		return nil
	}
	l.skills = make(map[string]sdk.Skill)

	entries, err := os.ReadDir(l.skillsPath)
	if err != nil {
		return fmt.Errorf("failed to read skills directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		skillDir := filepath.Join(l.skillsPath, entry.Name())
		skill, err := l.loadSkill(skillDir)
		if err != nil {
			continue
		}

		meta := skill.Describe()
		l.skills[meta.Name] = skill
	}

	return nil
}

func (l *Loader) loadSkill(dir string) (sdk.Skill, error) {
	manifestPath := filepath.Join(dir, "skill.yaml")

	if _, err := os.Stat(manifestPath); err != nil {
		return nil, fmt.Errorf("no manifest found: %w", err)
	}

	return &fileSkill{
		dir: dir,
	}, nil
}

func (l *Loader) List() []sdk.SkillMetadata {
	var list []sdk.SkillMetadata
	for _, skill := range l.skills {
		list = append(list, skill.Describe())
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

func (l *Loader) Get(name string) sdk.Skill {
	return l.skills[name]
}

func (l *Loader) Search(query string) []sdk.SkillMetadata {
	var results []sdk.SkillMetadata
	query = strings.ToLower(query)

	for _, skill := range l.skills {
		meta := skill.Describe()
		name := strings.ToLower(meta.Name)
		desc := strings.ToLower(meta.Description)

		if strings.Contains(name, query) || strings.Contains(desc, query) {
			results = append(results, meta)
		}
	}

	return results
}

func (l *Loader) Invoke(ctx context.Context, name string, input map[string]any) (map[string]any, error) {
	skill := l.skills[name]
	if skill == nil {
		available := l.List()
		names := make([]string, 0, len(available))
		for _, meta := range available {
			names = append(names, meta.Name)
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("skill not found: %s (no local skills are available)", name)
		}
		return nil, fmt.Errorf("skill not found: %s (available: %s)", name, strings.Join(names, ", "))
	}

	if err := skill.Warmup(ctx); err != nil {
		return nil, fmt.Errorf("skill warmup failed: %w", err)
	}

	return skill.Invoke(ctx, input)
}

type fileSkill struct {
	dir string
}

func (s *fileSkill) Describe() sdk.SkillMetadata {
	name := filepath.Base(s.dir)
	return sdk.SkillMetadata{
		Name:         name,
		Description:  fmt.Sprintf("Custom skill from %s", s.dir),
		Capabilities: []string{"custom"},
	}
}

func (s *fileSkill) Warmup(ctx context.Context) error {
	return nil
}

func (s *fileSkill) Invoke(ctx context.Context, input map[string]any) (map[string]any, error) {
	return map[string]any{
		"status": "not implemented",
		"skill":  s.dir,
	}, nil
}
