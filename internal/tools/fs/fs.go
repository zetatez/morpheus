package fs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type ReadTool struct {
	root         string
	allowedRoots []string
}

type WriteTool struct {
	root         string
	maxWriteSize int
}

type EditTool struct {
	root         string
	maxWriteSize int
}

type GlobTool struct {
	root          string
	ignoreChecker *IgnoreChecker
}

type GrepTool struct {
	root          string
	ignoreChecker *IgnoreChecker
}

const defaultReadLimit = 200
const maxReadLimit = 400
const defaultGrepPreviewLimit = 100

func schemaObject(properties map[string]any, required ...string) map[string]any {
	if properties == nil {
		properties = map[string]any{}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": properties,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func NewReadTool(root string, allowedRoots ...string) *ReadTool {
	cleaned := make([]string, 0, len(allowedRoots))
	for _, r := range allowedRoots {
		if strings.TrimSpace(r) == "" {
			continue
		}
		cleaned = append(cleaned, r)
	}
	return &ReadTool{root: root, allowedRoots: cleaned}
}

func NewWriteTool(root string, maxWriteSizeKB int) *WriteTool {
	return &WriteTool{root: root, maxWriteSize: maxWriteSizeKB}
}

func NewEditTool(root string, maxWriteSizeKB int) *EditTool {
	return &EditTool{root: root, maxWriteSize: maxWriteSizeKB}
}

func NewGlobTool(root string) *GlobTool {
	return &GlobTool{root: root}
}

func NewGlobToolWithIgnore(root string, checker *IgnoreChecker) *GlobTool {
	return &GlobTool{root: root, ignoreChecker: checker}
}

func NewGrepTool(root string) *GrepTool {
	return &GrepTool{root: root}
}

func NewGrepToolWithIgnore(root string, checker *IgnoreChecker) *GrepTool {
	return &GrepTool{root: root, ignoreChecker: checker}
}

func (t *ReadTool) Name() string  { return "read" }
func (t *WriteTool) Name() string { return "write" }
func (t *EditTool) Name() string  { return "edit" }
func (t *GlobTool) Name() string  { return "glob" }
func (t *GrepTool) Name() string  { return "grep" }

func (t *ReadTool) Describe() string  { return "Read a file from the workspace." }
func (t *WriteTool) Describe() string { return "Write content to a file in the workspace." }
func (t *EditTool) Describe() string  { return "Make a targeted text replacement in a file." }
func (t *GlobTool) Describe() string  { return "Match files by glob pattern." }
func (t *GrepTool) Describe() string  { return "Search for a query string in files." }

func (t *ReadTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"path":   map[string]any{"type": "string"},
		"offset": map[string]any{"type": "integer", "minimum": 1},
		"limit":  map[string]any{"type": "integer", "minimum": 1},
	}, "path")
}

func (t *WriteTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"path":    map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
	}, "path", "content")
}

func (t *EditTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"path":        map[string]any{"type": "string"},
		"old_string":  map[string]any{"type": "string"},
		"new_string":  map[string]any{"type": "string"},
		"replace_all": map[string]any{"type": "boolean"},
	}, "path", "old_string", "new_string")
}

func (t *GlobTool) Schema() map[string]any {
	return schemaObject(map[string]any{"pattern": map[string]any{"type": "string"}}, "pattern")
}

func (t *GrepTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"query":   map[string]any{"type": "string"},
		"pattern": map[string]any{"type": "string"},
		"include": map[string]any{"type": "string"},
	}, "query")
}

func (t *ReadTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("path is required")
	}
	offset := intValue(input["offset"])
	limit := intValue(input["limit"])
	if offset < 0 {
		return sdk.ToolResult{Success: false}, fmt.Errorf("offset must be >= 0")
	}
	if limit < 0 {
		return sdk.ToolResult{Success: false}, fmt.Errorf("limit must be >= 0")
	}
	if limit > maxReadLimit {
		return sdk.ToolResult{Success: false}, fmt.Errorf("limit must be <= %d", maxReadLimit)
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	content, startLine, endLine, totalLines, truncated := paginateContent(string(data), offset, limit)
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"path":        path,
			"content":     content,
			"offset":      startLine,
			"limit":       endLine - startLine + 1,
			"end_line":    endLine,
			"total_lines": totalLines,
			"truncated":   truncated,
		},
	}, nil
}

func intValue(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		return 0
	}
}

func paginateContent(content string, offset, limit int) (string, int, int, int, bool) {
	if offset <= 0 {
		offset = 1
	}
	if limit <= 0 {
		limit = defaultReadLimit
	}

	lines := splitLinesPreserve(content)
	totalLines := len(lines)
	if totalLines == 0 {
		return "", 1, 0, 0, false
	}
	if offset > totalLines {
		return "", offset, offset - 1, totalLines, false
	}

	start := offset - 1
	end := start + limit
	if end > totalLines {
		end = totalLines
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		b.WriteString(fmt.Sprintf("%d: %s", i+1, lines[i]))
	}

	return b.String(), offset, end, totalLines, end < totalLines
}

func splitLinesPreserve(content string) []string {
	if content == "" {
		return nil
	}
	parts := strings.SplitAfter(content, "\n")
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 {
		return []string{content}
	}
	if !strings.HasSuffix(content, "\n") {
		last := parts[len(parts)-1]
		parts[len(parts)-1] = strings.TrimSuffix(last, "\n")
	}
	return parts
}

func (t *WriteTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	if path == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("path is required")
	}
	if t.maxWriteSize > 0 && len(content)/1024 > t.maxWriteSize {
		return sdk.ToolResult{Success: false}, fmt.Errorf("write exceeds size limit")
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"path": path,
			"size": len(content),
		},
	}, nil
}

func (t *EditTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	oldString, _ := input["old_string"].(string)
	newString, _ := input["new_string"].(string)
	replaceAll, _ := input["replace_all"].(bool)
	if path == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("path is required")
	}
	if oldString == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("old_string is required")
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	content, err := os.ReadFile(resolved)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	original := string(content)
	count := strings.Count(original, oldString)
	if count == 0 {
		return sdk.ToolResult{Success: false}, fmt.Errorf("old_string not found")
	}
	if !replaceAll && count != 1 {
		return sdk.ToolResult{Success: false}, fmt.Errorf("old_string matched %d times; set replace_all=true or provide a more specific match", count)
	}
	updated := original
	replaced := 1
	if replaceAll {
		updated = strings.ReplaceAll(original, oldString, newString)
		replaced = count
	} else {
		updated = strings.Replace(original, oldString, newString, 1)
	}
	if t.maxWriteSize > 0 && len(updated)/1024 > t.maxWriteSize {
		return sdk.ToolResult{Success: false}, fmt.Errorf("edited content exceeds size limit")
	}
	if err := os.WriteFile(resolved, []byte(updated), 0o644); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"path":           path,
			"replacements":   replaced,
			"replace_all":    replaceAll,
			"original_count": count,
		},
	}, nil
}

func (t *GlobTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("pattern is required")
	}
	matches, err := filepath.Glob(filepath.Join(t.root, pattern))
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	var results []string
	for _, m := range matches {
		rel, err := filepath.Rel(t.root, m)
		if err != nil {
			continue
		}
		if t.ignoreChecker != nil && t.ignoreChecker.ShouldIgnore(m) {
			continue
		}
		results = append(results, rel)
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"matches": results,
			"count":   len(results),
		},
	}, nil
}

func (t *GrepTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		query, _ = input["pattern"].(string)
	}
	if query == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("query is required")
	}
	include, _ := input["include"].(string)
	matches := []map[string]any{}
	files := map[string]int{}
	previewLines := []string{}
	err := filepath.WalkDir(t.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if t.ignoreChecker != nil && t.ignoreChecker.ShouldIgnore(path) {
			return nil
		}
		if include != "" {
			rel, _ := filepath.Rel(t.root, path)
			matched, _ := filepath.Match(include, rel)
			if !matched {
				return nil
			}
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if strings.Contains(line, query) {
				rel, _ := filepath.Rel(t.root, path)
				files[rel]++
				matches = append(matches, map[string]any{
					"file": rel,
					"line": i + 1,
					"text": strings.TrimSpace(line),
				})
				if len(previewLines) < defaultGrepPreviewLimit {
					previewLines = append(previewLines, fmt.Sprintf("%s:%d: %s", rel, i+1, strings.TrimSpace(line)))
				}
			}
		}
		return nil
	})
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"query":         query,
			"include":       include,
			"matches":       matches,
			"count":         len(matches),
			"files":         summarizeFileMatches(files),
			"preview":       strings.Join(previewLines, "\n"),
			"preview_count": len(previewLines),
			"truncated":     len(matches) > len(previewLines),
		},
	}, nil
}

func summarizeFileMatches(files map[string]int) []map[string]any {
	if len(files) == 0 {
		return nil
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	summary := make([]map[string]any, 0, len(paths))
	for _, path := range paths {
		summary = append(summary, map[string]any{
			"file":  path,
			"count": files[path],
		})
	}
	return summary
}

func (t *ReadTool) resolve(rel string) (string, error) {
	return resolveReadPath(t.root, rel, t.allowedRoots)
}
func (t *ReadTool) ResolveForAPI(rel string) (string, error)  { return t.resolve(rel) }
func (t *WriteTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }
func (t *WriteTool) ResolveForAPI(rel string) (string, error) { return t.resolve(rel) }
func (t *EditTool) resolve(rel string) (string, error)        { return resolvePath(t.root, rel) }

func resolvePath(root, rel string) (string, error) {
	cleaned := filepath.Clean(rel)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	absTarget, err := filepath.Abs(filepath.Join(root, cleaned))
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(absTarget, absRoot) {
		return "", fmt.Errorf("path escapes workspace")
	}
	return absTarget, nil
}

func resolveReadPath(root, rel string, allowedRoots []string) (string, error) {
	cleaned := filepath.Clean(rel)
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var absTarget string
	if filepath.IsAbs(cleaned) {
		absTarget, err = filepath.Abs(cleaned)
	} else {
		absTarget, err = filepath.Abs(filepath.Join(root, cleaned))
	}
	if err != nil {
		return "", err
	}
	if hasPathPrefix(absTarget, absRoot) {
		return absTarget, nil
	}
	for _, allowed := range allowedRoots {
		absAllowed, err := filepath.Abs(allowed)
		if err != nil {
			continue
		}
		if hasPathPrefix(absTarget, absAllowed) {
			return absTarget, nil
		}
	}
	return "", fmt.Errorf("path escapes workspace")
}

func hasPathPrefix(target, root string) bool {
	if target == root {
		return true
	}
	sep := string(os.PathSeparator)
	if strings.HasSuffix(root, sep) {
		return strings.HasPrefix(target, root)
	}
	return strings.HasPrefix(target, root+sep)
}
