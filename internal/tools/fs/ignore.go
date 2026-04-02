package fs

import (
	"os"
	"path/filepath"
	"strings"
)

type IgnoreChecker struct {
	root     string
	patterns []ignorePattern
}

type ignorePattern struct {
	dir     string
	pattern string
	negate  bool
}

func LoadIgnoreChecker(root string) (*IgnoreChecker, error) {
	ic := &IgnoreChecker{
		root:     root,
		patterns: make([]ignorePattern, 0),
	}

	ignorePath := filepath.Join(root, ".morpheusignore")
	data, err := os.ReadFile(ignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ic, nil
		}
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		negate := false
		if strings.HasPrefix(line, "!") {
			negate = true
			line = strings.TrimPrefix(line, "!")
		}

		dir := ""
		if strings.Contains(line, "/") {
			parts := strings.Split(line, "/")
			dir = strings.Join(parts[:len(parts)-1], "/")
			line = parts[len(parts)-1]
		}

		ic.patterns = append(ic.patterns, ignorePattern{
			dir:     dir,
			pattern: line,
			negate:  negate,
		})
	}

	return ic, nil
}

func (ic *IgnoreChecker) ShouldIgnore(path string) bool {
	if ic == nil {
		return false
	}

	rel, err := filepath.Rel(ic.root, path)
	if err != nil {
		return false
	}

	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")

	for i := range parts {
		subPath := strings.Join(parts[:i+1], "/")
		for _, p := range ic.patterns {
			if p.dir != "" && !strings.HasPrefix(subPath, p.dir+"/") {
				continue
			}
			matched, _ := filepath.Match(p.pattern, filepath.Base(subPath))
			if matched {
				return !p.negate
			}
		}
	}

	return false
}

func (ic *IgnoreChecker) FilterPaths(paths []string) []string {
	if ic == nil {
		return paths
	}

	filtered := make([]string, 0, len(paths))
	for _, p := range paths {
		if !ic.ShouldIgnore(p) {
			filtered = append(filtered, p)
		}
	}
	return filtered
}
