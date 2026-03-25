package subagent

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/zetatez/morpheus/internal/tools/agenttool"
	"gopkg.in/yaml.v3"
)

const SubagentFileName = "SUBAGENT.md"

var frontmatterRegex = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---\s*\n?`)

type manifest struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Tools       []string `yaml:"tools"`
}

type Definition struct {
	Profile agenttool.AgentProfile
	Tools   []string
}

type Loader struct {
	paths []string
	cache map[string]Definition
	mu    sync.RWMutex
}

func NewLoader(paths ...string) *Loader {
	return &Loader{paths: paths, cache: make(map[string]Definition)}
}

func (l *Loader) AddPaths(paths []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		found := false
		for _, existing := range l.paths {
			if existing == path {
				found = true
				break
			}
		}
		if !found {
			l.paths = append(l.paths, path)
		}
	}
}

func (l *Loader) LoadByName(name string) (Definition, bool, error) {
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return Definition{}, false, nil
	}

	l.mu.RLock()
	if def, ok := l.cache[key]; ok {
		l.mu.RUnlock()
		return def, true, nil
	}
	l.mu.RUnlock()

	paths := l.snapshotPaths()
	for _, base := range paths {
		if strings.TrimSpace(base) == "" {
			continue
		}
		if def, ok, err := l.loadFromPath(base, key); err != nil {
			return Definition{}, false, err
		} else if ok {
			l.mu.Lock()
			l.cache[key] = def
			l.mu.Unlock()
			return def, true, nil
		}
	}

	return Definition{}, false, nil
}

func (l *Loader) snapshotPaths() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	paths := make([]string, len(l.paths))
	copy(paths, l.paths)
	return paths
}

func (l *Loader) loadFromPath(base, name string) (Definition, bool, error) {
	filePath := filepath.Join(base, name+".md")
	if def, ok, err := l.loadFromFile(filePath, name); ok || err != nil {
		return def, ok, err
	}

	dirPath := filepath.Join(base, name, SubagentFileName)
	return l.loadFromFile(dirPath, name)
}

func (l *Loader) loadFromFile(path, name string) (Definition, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Definition{}, false, nil
		}
		return Definition{}, false, fmt.Errorf("failed to read subagent file %s: %w", path, err)
	}

	content := string(data)
	var meta manifest
	instructions := strings.TrimSpace(content)
	if match := frontmatterRegex.FindStringSubmatch(content); match != nil {
		if err := yaml.Unmarshal([]byte(match[1]), &meta); err != nil {
			return Definition{}, false, fmt.Errorf("failed to parse subagent frontmatter: %w", err)
		}
		instructions = strings.TrimSpace(content[len(match[0]):])
	}

	if meta.Name == "" {
		meta.Name = name
	}
	if meta.Description == "" {
		meta.Description = fmt.Sprintf("Custom subagent from %s", path)
	}

	profile := agenttool.AgentProfile{
		Name:         meta.Name,
		Description:  meta.Description,
		Instructions: instructions,
	}

	return Definition{Profile: profile, Tools: meta.Tools}, true, nil
}
