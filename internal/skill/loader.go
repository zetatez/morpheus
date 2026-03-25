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
	for name, builtin := range builtinSkills() {
		l.skills[name] = builtin
	}

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

func (l *Loader) LoadBuiltinOnly() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.skills = make(map[string]sdk.Skill)
	for name, builtin := range builtinSkills() {
		l.skills[name] = builtin
	}
	l.loaded = true
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

func (l *Loader) ListBuiltin() []sdk.SkillMetadata {
	l.mu.RLock()
	defer l.mu.RUnlock()
	var list []sdk.SkillMetadata
	for name, skill := range l.skills {
		if _, ok := builtinSkills()[name]; ok {
			list = append(list, skill.Describe())
		}
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

type staticSkill struct {
	meta   sdk.SkillMetadata
	prompt string
}

func (s *staticSkill) Describe() sdk.SkillMetadata { return s.meta }

func (s *staticSkill) Warmup(ctx context.Context) error { return nil }

func (s *staticSkill) Invoke(ctx context.Context, input map[string]any) (map[string]any, error) {
	inputText := extractSkillInput(input)
	prompt := strings.ReplaceAll(s.prompt, "{{input}}", inputText)
	return map[string]any{
		"name":   s.meta.Name,
		"prompt": prompt,
		"input":  inputText,
	}, nil
}

func builtinSkills() map[string]sdk.Skill {
	return map[string]sdk.Skill{
		"review": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "review",
			Description:  "Review code changes and highlight risks, edge cases, and test gaps.",
			Capabilities: []string{"review", "code-review"},
		}, prompt: "Review the relevant changes. Focus on correctness, edge cases, and tests. Summarize issues and recommendations. Context: {{input}}"},

		"test": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "test",
			Description:  "Recommend and run the most relevant tests for the change.",
			Capabilities: []string{"test", "testing"},
		}, prompt: "Identify the best test commands for this project and explain why. If unsure, ask for the preferred test command. Context: {{input}}"},

		"docs": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "docs",
			Description:  "Draft or update documentation for the change.",
			Capabilities: []string{"docs", "documentation"},
		}, prompt: "Draft documentation updates based on the change. Call out files and sections to edit. Context: {{input}}"},

		"refactor": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "refactor",
			Description:  "Analyze code and suggest refactoring opportunities for better maintainability.",
			Capabilities: []string{"refactor", "improvement"},
		}, prompt: "Analyze the code for refactoring opportunities. Focus on: readability, performance, DRY, SOLID principles. Provide specific suggestions with file:line references. Context: {{input}}"},

		"debug": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "debug",
			Description:  "Help diagnose bugs and provide troubleshooting guidance.",
			Capabilities: []string{"debug", "troubleshooting"},
		}, prompt: "Analyze the issue and provide debugging guidance. Consider: error messages, logs, stack traces, common pitfalls. Ask clarifying questions if needed. Context: {{input}}"},

		"security": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "security",
			Description:  "Review code for security vulnerabilities and best practices.",
			Capabilities: []string{"security", "vulnerability"},
		}, prompt: "Review the code for security issues. Check for: injection risks, auth issues, data exposure, dependency vulnerabilities. Provide severity levels. Context: {{input}}"},

		"git": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "git",
			Description:  "Provide git workflow guidance and commands.",
			Capabilities: []string{"git", "version-control"},
		}, prompt: "Provide git commands and workflow guidance. Consider: branching strategy, commit messages, rebasing vs merging. Context: {{input}}"},

		"explain": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "explain",
			Description:  "Explain code, concepts, or architecture in simple terms.",
			Capabilities: []string{"explain", "education"},
		}, prompt: "Explain the code or concept clearly. Use analogies where helpful. Break down complex ideas into simpler parts. Context: {{input}}"},

		"optimize": &staticSkill{meta: sdk.SkillMetadata{
			Name:         "optimize",
			Description:  "Analyze performance and suggest optimization opportunities.",
			Capabilities: []string{"optimize", "performance"},
		}, prompt: "Analyze code for performance bottlenecks. Consider: algorithmic complexity, memory usage, I/O operations, database queries. Provide specific improvements. Context: {{input}}"},
	}
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
