package skill

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/zetatez/morpheus/pkg/sdk"
	"gopkg.in/yaml.v3"
)

type LazySkill struct {
	dir             string
	loaded          bool
	manifest        *skillManifest
	additionalFiles map[string]string
	mu              sync.RWMutex
}

func NewLazySkill(dir string, manifest skillManifest) *LazySkill {
	return &LazySkill{
		dir:      dir,
		manifest: &manifest,
	}
}

func (s *LazySkill) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}

	s.additionalFiles = make(map[string]string)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == SkillFileName || name == ManifestFileName || name == PromptFileName {
			continue
		}
		if path := filepath.Join(s.dir, name); path != "" {
			if data, err := os.ReadFile(path); err == nil {
				s.additionalFiles[name] = string(data)
			}
		}
	}
	s.loaded = true
	return nil
}

func (s *LazySkill) Describe() sdk.SkillMetadata {
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

func (s *LazySkill) Warmup(ctx context.Context) error {
	return nil
}

func (s *LazySkill) Invoke(ctx context.Context, input map[string]any) (map[string]any, error) {
	if err := s.load(); err != nil {
		return nil, err
	}

	prompt := strings.TrimSpace(s.manifest.Prompt)
	if prompt == "" {
		return nil, nil
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

type Tier int

const (
	TierBasic Tier = iota
	TierFull
	TierReferences
)

type ProgressiveSkillLoader struct {
	loader *Loader
	tiers  map[string]*LazySkill
	mu     sync.RWMutex
}

func NewProgressiveSkillLoader(loader *Loader) *ProgressiveSkillLoader {
	return &ProgressiveSkillLoader{
		loader: loader,
		tiers:  make(map[string]*LazySkill),
	}
}

func (p *ProgressiveSkillLoader) ListBasic() []sdk.SkillMetadata {
	p.mu.RLock()
	defer p.mu.RUnlock()

	var list []sdk.SkillMetadata
	for _, entry := range p.tiers {
		meta := entry.Describe()
		list = append(list, sdk.SkillMetadata{
			Name:        meta.Name,
			Description: meta.Description,
		})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}

func (p *ProgressiveSkillLoader) GetFull(name string) (sdk.Skill, bool) {
	p.mu.RLock()
	lazy, ok := p.tiers[strings.ToLower(name)]
	p.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if err := lazy.load(); err != nil {
		return nil, false
	}

	return lazy, true
}

func (p *ProgressiveSkillLoader) GetReferences(name string) (map[string]string, bool) {
	p.mu.RLock()
	lazy, ok := p.tiers[strings.ToLower(name)]
	p.mu.RUnlock()

	if !ok {
		return nil, false
	}

	if err := lazy.load(); err != nil {
		return nil, false
	}

	lazy.mu.RLock()
	defer lazy.mu.RUnlock()
	if lazy.additionalFiles == nil {
		return nil, false
	}

	refs := make(map[string]string)
	for k, v := range lazy.additionalFiles {
		refs[k] = v
	}
	return refs, true
}

func (p *ProgressiveSkillLoader) AddSkill(dir string, manifest skillManifest) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tiers[strings.ToLower(manifest.Name)] = NewLazySkill(dir, manifest)
}

func (p *ProgressiveSkillLoader) DiscoverSkills(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.tiers = make(map[string]*LazySkill)

	for _, path := range p.loader.skillsPaths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		entries, err := os.ReadDir(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillDir := filepath.Join(path, entry.Name())
			manifest, err := p.loadManifestOnly(skillDir)
			if err != nil {
				continue
			}
			if manifest.Name == "" {
				manifest.Name = entry.Name()
			}
			p.tiers[strings.ToLower(manifest.Name)] = NewLazySkill(skillDir, *manifest)
		}
	}
	return nil
}

func (p *ProgressiveSkillLoader) loadManifestOnly(dir string) (*skillManifest, error) {
	skillMDPath := filepath.Join(dir, SkillFileName)
	manifestPath := filepath.Join(dir, ManifestFileName)

	var manifest skillManifest

	if data, err := os.ReadFile(skillMDPath); err == nil {
		content := string(data)
		match := frontmatterRegex.FindStringSubmatch(content)
		if match != nil {
			frontmatter := match[1]
			if err := yaml.Unmarshal([]byte(frontmatter), &manifest); err != nil {
				return nil, err
			}
		}
	} else if data, err := os.ReadFile(manifestPath); err == nil {
		if err := yaml.Unmarshal(data, &manifest); err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("no SKILL.md or skill.yaml found in %s", dir)
	}

	return &manifest, nil
}

type ProgressiveSkillProvider struct {
	loader      *Loader
	progressive *ProgressiveSkillLoader
}

func NewProgressiveSkillProvider(loader *Loader) *ProgressiveSkillProvider {
	return &ProgressiveSkillProvider{
		loader:      loader,
		progressive: NewProgressiveSkillLoader(loader),
	}
}

func (p *ProgressiveSkillProvider) ListTier1() []sdk.SkillMetadata {
	return p.progressive.ListBasic()
}

func (p *ProgressiveSkillProvider) GetTier2(name string) (sdk.SkillMetadata, bool) {
	skill, ok := p.progressive.GetFull(name)
	if !ok {
		return sdk.SkillMetadata{}, false
	}
	return skill.Describe(), true
}

func (p *ProgressiveSkillProvider) GetTier3(name string, ref string) (string, bool) {
	refs, ok := p.progressive.GetReferences(name)
	if !ok {
		return "", false
	}
	content, ok := refs[ref]
	return content, ok
}

func (p *ProgressiveSkillProvider) Discover(ctx context.Context) error {
	return p.progressive.DiscoverSkills(ctx)
}
