package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/zetatez/morpheus/pkg/sdk"
	"gopkg.in/yaml.v3"
)

const (
	SkillFileName    = "SKILL.md"
	ManifestFileName = "skill.yaml"
	PromptFileName   = "prompt.md"
)

type Loader struct {
	skillsPaths []string
	skills      map[string]sdk.Skill
	loaded      bool
	lazyMode    bool
	mu          sync.RWMutex
}

func NewLoader(paths ...string) *Loader {
	return &Loader{skillsPaths: paths, skills: make(map[string]sdk.Skill), lazyMode: true}
}

func NewLoaderWithPaths(paths []string) *Loader {
	return &Loader{skillsPaths: paths, skills: make(map[string]sdk.Skill), lazyMode: true}
}

func (l *Loader) Load(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.skills = make(map[string]sdk.Skill)

	if l.lazyMode {
		l.loaded = true
		return nil
	}

	for _, path := range l.skillsPaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := l.loadFromPath(path); err != nil {
			return err
		}
	}
	l.loaded = true
	return nil
}

func (l *Loader) LoadCustom(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.loaded && !l.lazyMode {
		return nil
	}

	for _, path := range l.skillsPaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if err := l.loadFromPath(path); err != nil {
			return err
		}
	}
	l.loaded = true
	return nil
}

func (l *Loader) EnsureLoaded(ctx context.Context) error {
	l.mu.RLock()
	loaded := l.loaded
	l.mu.RUnlock()
	if loaded {
		return nil
	}
	return l.Load(ctx)
}

func (l *Loader) loadFromPath(path string) error {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to read skills directory: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		skillDir := filepath.Join(path, entry.Name())
		skill, err := l.loadSkill(skillDir)
		if err != nil {
			continue
		}
		meta := skill.Describe()
		l.skills[strings.ToLower(meta.Name)] = skill
	}
	return nil
}

func (l *Loader) loadSkill(dir string) (sdk.Skill, error) {
	skillName := filepath.Base(dir)

	skillMDPath := filepath.Join(dir, SkillFileName)
	manifestPath := filepath.Join(dir, ManifestFileName)
	promptPath := filepath.Join(dir, PromptFileName)

	var manifest skillManifest
	var promptContent string
	var additionalFiles map[string]string

	if data, err := os.ReadFile(skillMDPath); err == nil {
		content := string(data)
		match := frontmatterRegex.FindStringSubmatch(content)
		if match != nil {
			frontmatter := match[1]
			if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
				return nil, fmt.Errorf("failed to parse SKILL.md frontmatter: %w", err)
			}
			promptContent = strings.TrimSpace(content[len(match[0]):])
		} else {
			promptContent = strings.TrimSpace(content)
		}
	} else if data, err := os.ReadFile(manifestPath); err == nil {
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return nil, fmt.Errorf("failed to parse skill.yaml: %w", err)
		}
		if promptData, err := os.ReadFile(promptPath); err == nil {
			promptContent = string(promptData)
		}
	} else {
		return nil, fmt.Errorf("no SKILL.md or skill.yaml found in %s", dir)
	}

	if manifest.Name == "" {
		manifest.Name = skillName
	}
	if manifest.Description == "" {
		manifest.Description = fmt.Sprintf("Custom skill from %s", dir)
	}
	if manifest.Prompt == "" && promptContent != "" {
		manifest.Prompt = promptContent
	}

	if entries, err := os.ReadDir(dir); err == nil {
		additionalFiles = make(map[string]string)
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if name == SkillFileName || name == ManifestFileName || name == PromptFileName {
				continue
			}
			if path := filepath.Join(dir, name); path != "" {
				if data, err := os.ReadFile(path); err == nil {
					additionalFiles[name] = string(data)
				}
			}
		}
		if len(additionalFiles) == 0 {
			additionalFiles = nil
		}
	}

	return &fileSkill{
		dir:             dir,
		manifest:        manifest,
		additionalFiles: additionalFiles,
	}, nil
}

func (l *Loader) List() []sdk.SkillMetadata {
	l.mu.RLock()
	lazyMode := l.lazyMode
	l.mu.RUnlock()

	if lazyMode {
		_ = l.LoadCustom(context.Background())
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
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
	l.mu.RLock()
	skill := l.skills[strings.ToLower(name)]
	lazyMode := l.lazyMode
	l.mu.RUnlock()

	if skill != nil {
		return skill
	}

	if lazyMode {
		_ = l.LoadCustom(context.Background())
		l.mu.RLock()
		skill = l.skills[strings.ToLower(name)]
		l.mu.RUnlock()
	}

	return skill
}

func (l *Loader) Search(query string) []sdk.SkillMetadata {
	l.mu.RLock()
	lazyMode := l.lazyMode
	l.mu.RUnlock()

	if lazyMode {
		_ = l.LoadCustom(context.Background())
	}

	l.mu.RLock()
	defer l.mu.RUnlock()
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
	l.mu.RLock()
	skill := l.skills[strings.ToLower(name)]
	l.mu.RUnlock()
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
	dir             string
	manifest        skillManifest
	additionalFiles map[string]string
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\n(.+?)\n---`)

func (s *fileSkill) Describe() sdk.SkillMetadata {
	meta := sdk.SkillMetadata{
		Name:              s.manifest.Name,
		Description:       s.manifest.Description,
		Capabilities:      s.manifest.Capabilities,
		ExpectedTokenCost: s.manifest.ExpectedTokenCost,
		License:           s.manifest.License,
		Compatibility:     s.manifest.Compatibility,
		Metadata:          s.manifest.Metadata,
	}

	if s.manifest.UserInvocable != nil {
		meta.UserInvocable = *s.manifest.UserInvocable
	}
	if len(s.manifest.AllowedTools) > 0 {
		meta.AllowedTools = s.manifest.AllowedTools
	}

	return meta
}

func (s *fileSkill) Warmup(ctx context.Context) error {
	return nil
}

func (s *fileSkill) Invoke(ctx context.Context, input map[string]any) (map[string]any, error) {
	prompt := strings.TrimSpace(s.manifest.Prompt)
	if prompt == "" {
		return nil, fmt.Errorf("skill %s has no prompt configured", s.manifest.Name)
	}
	inputText := extractSkillInput(input)
	prompt = strings.ReplaceAll(prompt, "{{input}}", inputText)

	result := map[string]any{
		"name":         s.manifest.Name,
		"description":  s.manifest.Description,
		"prompt":       prompt,
		"input":        inputText,
		"capabilities": s.manifest.Capabilities,
	}

	if len(s.additionalFiles) > 0 {
		result["additional_files"] = s.additionalFiles
	}

	return result, nil
}

type skillManifest struct {
	Name              string         `yaml:"name"`
	Description       string         `yaml:"description"`
	Capabilities      []string       `yaml:"capabilities"`
	ExpectedTokenCost int            `yaml:"expected_token_cost"`
	Prompt            string         `yaml:"prompt"`
	License           string         `yaml:"license"`
	Compatibility     string         `yaml:"compatibility"`
	UserInvocable     *bool          `yaml:"user-invocable"`
	AllowedTools      []string       `yaml:"allowed-tools"`
	Metadata          map[string]any `yaml:"metadata"`
}

func extractSkillInput(input map[string]any) string {
	if input == nil {
		return ""
	}
	if text, ok := input["text"].(string); ok {
		return strings.TrimSpace(text)
	}
	if text, ok := input["input"].(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func DiscoverOpenCodePaths(workspaceRoot string) []string {
	var paths []string
	homeDir, _ := os.UserHomeDir()

	morpheusGlobal := filepath.Join(homeDir, ".config", "morpheus", "skills")
	opencodeGlobal := filepath.Join(homeDir, ".config", "opencode", "skills")
	claudeGlobal := filepath.Join(homeDir, ".claude", "skills")
	agentsGlobal := filepath.Join(homeDir, ".agents", "skills")

	morpheusProject := filepath.Join(workspaceRoot, ".morpheus", "skills")
	opencodeProject := filepath.Join(workspaceRoot, ".opencode", "skills")
	claudeProject := filepath.Join(workspaceRoot, ".claude", "skills")
	agentsProject := filepath.Join(workspaceRoot, ".agents", "skills")

	globalPaths := []string{morpheusGlobal, opencodeGlobal, claudeGlobal, agentsGlobal}
	projectPaths := []string{morpheusProject, opencodeProject, claudeProject, agentsProject}

	seen := make(map[string]bool)

	for _, path := range globalPaths {
		if _, err := os.Stat(path); err == nil && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}

	for _, path := range projectPaths {
		if _, err := os.Stat(path); err == nil && !seen[path] {
			paths = append(paths, path)
			seen[path] = true
		}
	}

	return paths
}

func (l *Loader) AddPaths(paths []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		found := false
		for _, existing := range l.skillsPaths {
			if existing == path {
				found = true
				break
			}
		}
		if !found {
			l.skillsPaths = append(l.skillsPaths, path)
		}
	}
}

func (l *Loader) GetPaths() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	paths := make([]string, len(l.skillsPaths))
	copy(paths, l.skillsPaths)
	return paths
}
