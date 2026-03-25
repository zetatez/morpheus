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
	root string
}

type WriteTool struct {
	root         string
	maxWriteSize int
}

type FindTool struct {
	root string
}

type MkdirTool struct {
	root string
}

type CreateFileTool struct {
	root string
}

type ChmodTool struct {
	root string
}

type EditTool struct {
	root         string
	maxWriteSize int
}

type ListTool struct {
	root string
}

type GlobTool struct {
	root string
}

type GrepTool struct {
	root string
}

type RmTool struct {
	root string
}

type CpTool struct {
	root string
}

type MvTool struct {
	root string
}

type TreeTool struct {
	root string
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

func NewReadTool(root string) *ReadTool {
	return &ReadTool{root: root}
}

func NewWriteTool(root string, maxWriteSizeKB int) *WriteTool {
	return &WriteTool{root: root, maxWriteSize: maxWriteSizeKB}
}

func NewFindTool(root string) *FindTool {
	return &FindTool{root: root}
}

func NewMkdirTool(root string) *MkdirTool {
	return &MkdirTool{root: root}
}

func NewCreateFileTool(root string) *CreateFileTool {
	return &CreateFileTool{root: root}
}

func NewChmodTool(root string) *ChmodTool {
	return &ChmodTool{root: root}
}

func NewEditTool(root string, maxWriteSizeKB int) *EditTool {
	return &EditTool{root: root, maxWriteSize: maxWriteSizeKB}
}

func NewListTool(root string) *ListTool {
	return &ListTool{root: root}
}

func NewGlobTool(root string) *GlobTool {
	return &GlobTool{root: root}
}

func NewGrepTool(root string) *GrepTool {
	return &GrepTool{root: root}
}

func NewRmTool(root string) *RmTool {
	return &RmTool{root: root}
}

func NewCpTool(root string) *CpTool {
	return &CpTool{root: root}
}

func NewMvTool(root string) *MvTool {
	return &MvTool{root: root}
}

func NewTreeTool(root string) *TreeTool {
	return &TreeTool{root: root}
}

func (t *ReadTool) Name() string       { return "fs.read" }
func (t *WriteTool) Name() string      { return "fs.write" }
func (t *FindTool) Name() string       { return "fs.find" }
func (t *MkdirTool) Name() string      { return "fs.mkdir" }
func (t *CreateFileTool) Name() string { return "fs.create" }
func (t *ChmodTool) Name() string      { return "fs.chmod" }
func (t *EditTool) Name() string       { return "fs.edit" }
func (t *ListTool) Name() string       { return "fs.ls" }
func (t *GlobTool) Name() string       { return "fs.glob" }
func (t *GrepTool) Name() string       { return "fs.grep" }
func (t *RmTool) Name() string         { return "fs.rm" }
func (t *CpTool) Name() string         { return "fs.cp" }
func (t *MvTool) Name() string         { return "fs.mv" }
func (t *TreeTool) Name() string       { return "fs.tree" }

func (t *ReadTool) Describe() string       { return "Read a file from the workspace." }
func (t *WriteTool) Describe() string      { return "Write content to a file in the workspace." }
func (t *FindTool) Describe() string       { return "Find files by name pattern." }
func (t *MkdirTool) Describe() string      { return "Create directories in the workspace." }
func (t *CreateFileTool) Describe() string { return "Create a file with content." }
func (t *ChmodTool) Describe() string      { return "Change file permissions (octal string)." }
func (t *EditTool) Describe() string       { return "Make a targeted text replacement in a file." }
func (t *ListTool) Describe() string       { return "List directory contents." }
func (t *GlobTool) Describe() string       { return "Match files by glob pattern." }
func (t *GrepTool) Describe() string       { return "Search for a query string in files." }
func (t *RmTool) Describe() string         { return "Remove a file or directory." }
func (t *CpTool) Describe() string         { return "Copy a file." }
func (t *MvTool) Describe() string         { return "Move or rename a file or directory." }
func (t *TreeTool) Describe() string       { return "Show a tree view of a directory." }

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

func (t *FindTool) Schema() map[string]any {
	return schemaObject(map[string]any{"pattern": map[string]any{"type": "string"}}, "pattern")
}

func (t *MkdirTool) Schema() map[string]any {
	return schemaObject(map[string]any{"path": map[string]any{"type": "string"}}, "path")
}

func (t *CreateFileTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"path":    map[string]any{"type": "string"},
		"content": map[string]any{"type": "string"},
	}, "path")
}

func (t *ChmodTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"path": map[string]any{"type": "string"},
		"mode": map[string]any{"type": "string"},
	}, "path", "mode")
}

func (t *EditTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"path":        map[string]any{"type": "string"},
		"old_string":  map[string]any{"type": "string"},
		"new_string":  map[string]any{"type": "string"},
		"replace_all": map[string]any{"type": "boolean"},
	}, "path", "old_string", "new_string")
}

func (t *ListTool) Schema() map[string]any {
	return schemaObject(map[string]any{"path": map[string]any{"type": "string"}})
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

func (t *RmTool) Schema() map[string]any {
	return schemaObject(map[string]any{"path": map[string]any{"type": "string"}}, "path")
}

func (t *CpTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"src": map[string]any{"type": "string"},
		"dst": map[string]any{"type": "string"},
	}, "src", "dst")
}

func (t *MvTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"src": map[string]any{"type": "string"},
		"dst": map[string]any{"type": "string"},
	}, "src", "dst")
}

func (t *TreeTool) Schema() map[string]any {
	return schemaObject(map[string]any{
		"path":  map[string]any{"type": "string"},
		"depth": map[string]any{"type": "integer"},
	})
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

func (t *FindTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("pattern is required")
	}
	matches := []string{}
	err := filepath.Walk(t.root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(t.root, path)
		if err != nil {
			return nil
		}
		matched, err := filepath.Match(pattern, rel)
		if err != nil {
			return nil
		}
		if matched {
			matches = append(matches, rel)
		}
		return nil
	})
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"matches": matches,
			"count":   len(matches),
		},
	}, nil
}

func (t *MkdirTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("path is required")
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.MkdirAll(resolved, 0o755); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"path": path,
		},
	}, nil
}

func (t *CreateFileTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	content, _ := input["content"].(string)
	if path == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("path is required")
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

func (t *ChmodTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	modeStr, _ := input["mode"].(string)
	if path == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("path is required")
	}
	if modeStr == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("mode is required")
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	mode, err := parseMode(modeStr)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.Chmod(resolved, mode); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"path": path,
			"mode": modeStr,
		},
	}, nil
}

func (t *ListTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	if path == "" {
		path = "."
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	var files []map[string]any
	for _, e := range entries {
		info, _ := e.Info()
		files = append(files, map[string]any{
			"name":   e.Name(),
			"is_dir": e.IsDir(),
			"size":   info.Size(),
		})
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"path":  path,
			"files": files,
			"count": len(files),
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

func (t *RmTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	if path == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("path is required")
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if info.IsDir() {
		err = os.RemoveAll(resolved)
	} else {
		err = os.Remove(resolved)
	}
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"path":    path,
			"removed": true,
		},
	}, nil
}

func (t *CpTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	src, _ := input["src"].(string)
	dst, _ := input["dst"].(string)
	if src == "" || dst == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("src and dst are required")
	}
	srcResolved, err := t.resolve(src)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	dstResolved, err := t.resolve(dst)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	info, err := os.Stat(srcResolved)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if info.IsDir() {
		return sdk.ToolResult{Success: false}, fmt.Errorf("use fs.mv for directories")
	}
	data, err := os.ReadFile(srcResolved)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.MkdirAll(filepath.Dir(dstResolved), 0o755); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.WriteFile(dstResolved, data, info.Mode()); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"src":    src,
			"dst":    dst,
			"copied": true,
		},
	}, nil
}

func (t *MvTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	src, _ := input["src"].(string)
	dst, _ := input["dst"].(string)
	if src == "" || dst == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("src and dst are required")
	}
	srcResolved, err := t.resolve(src)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	dstResolved, err := t.resolve(dst)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.MkdirAll(filepath.Dir(dstResolved), 0o755); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if err := os.Rename(srcResolved, dstResolved); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"src":   src,
			"dst":   dst,
			"moved": true,
		},
	}, nil
}

func (t *TreeTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	path, _ := input["path"].(string)
	depth, _ := input["depth"].(int)
	if path == "" {
		path = "."
	}
	if depth == 0 {
		depth = 3
	}
	resolved, err := t.resolve(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	var result strings.Builder
	err = walkDir(resolved, "", depth, 0, &result)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"tree": result.String(),
		},
	}, nil
}

func walkDir(root, prefix string, maxDepth, depth int, sb *strings.Builder) error {
	if depth > maxDepth {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for i, e := range entries {
		name := e.Name()
		isLast := i == len(entries)-1
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		sb.WriteString(prefix + connector + name + "\n")
		if e.IsDir() && depth < maxDepth {
			newPrefix := prefix
			if isLast {
				newPrefix += "    "
			} else {
				newPrefix += "│   "
			}
			walkDir(filepath.Join(root, name), newPrefix, maxDepth, depth+1, sb)
		}
	}
	return nil
}

func parseMode(modeStr string) (os.FileMode, error) {
	var mode os.FileMode
	_, err := fmt.Sscanf(modeStr, "%o", &mode)
	if err != nil {
		return 0, err
	}
	return mode, nil
}

func (t *ReadTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }
func (t *WriteTool) resolve(rel string) (string, error)      { return resolvePath(t.root, rel) }
func (t *FindTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }
func (t *MkdirTool) resolve(rel string) (string, error)      { return resolvePath(t.root, rel) }
func (t *CreateFileTool) resolve(rel string) (string, error) { return resolvePath(t.root, rel) }
func (t *ChmodTool) resolve(rel string) (string, error)      { return resolvePath(t.root, rel) }
func (t *EditTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }
func (t *ListTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }
func (t *GlobTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }
func (t *GrepTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }
func (t *RmTool) resolve(rel string) (string, error)         { return resolvePath(t.root, rel) }
func (t *CpTool) resolve(rel string) (string, error)         { return resolvePath(t.root, rel) }
func (t *MvTool) resolve(rel string) (string, error)         { return resolvePath(t.root, rel) }
func (t *TreeTool) resolve(rel string) (string, error)       { return resolvePath(t.root, rel) }

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
