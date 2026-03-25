package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	protocol "go.lsp.dev/protocol"

	"github.com/zetatez/morpheus/internal/plugin"
	"github.com/zetatez/morpheus/pkg/sdk"
)

type Tool struct {
	root  string
	mu    sync.Mutex
	pool  map[string]*client
	roots map[string]struct{}
}

type serverStrategy struct {
	SupportsRename      bool
	SupportsCodeAction  bool
	SupportsHierarchy   bool
	SupportsTypeDef     bool
	SupportsDiagnostics bool
	CodeActionResolve   bool
	RenamePrepare       bool
}

type client struct {
	cmd             *exec.Cmd
	root            string
	server          string
	in              io.WriteCloser
	reader          *bufio.Reader
	mu              sync.Mutex
	seq             int64
	closed          bool
	capabilities    map[string]any
	workspaceFolder []map[string]any
	versions        map[string]int
	diagnosticStore map[string][]map[string]any
	pending         map[string]responseEnvelope
	cond            *sync.Cond
	restartCount    int
	lastCrash       time.Time
	lastHealthOK    time.Time
	lastError       string
	consecutiveFail int
	lastUsedAt      time.Time
	state           string
}

func New(root string) *Tool {
	return &Tool{root: root, pool: make(map[string]*client), roots: map[string]struct{}{root: {}}}
}

func (t *Tool) Name() string { return "lsp.query" }

func (t *Tool) Describe() string {
	return "Query language server features like definition, typeDefinition, references, hover, implementations, symbols, call hierarchy, and diagnostics."
}

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":  map[string]any{"type": "string"},
			"path":    map[string]any{"type": "string"},
			"line":    map[string]any{"type": "integer", "minimum": 1},
			"column":  map[string]any{"type": "integer", "minimum": 1},
			"query":   map[string]any{"type": "string"},
			"newName": map[string]any{"type": "string"},
		},
		"required": []string{"action"},
	}
}

func (t *Tool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	action, _ := input["action"].(string)
	action = strings.TrimSpace(action)
	if action == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("action is required")
	}
	path, _ := input["path"].(string)
	query, _ := input["query"].(string)
	newName, _ := input["newName"].(string)
	line := intValue(input["line"])
	column := intValue(input["column"])
	resolved, rel, err := t.resolveOptional(path)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	lang, server, ok := detectServer(rel)
	if !ok {
		return sdk.ToolResult{Success: false}, fmt.Errorf("no supported language server for path: %s", path)
	}
	workspaceRoot := t.pickWorkspaceRoot(resolved)
	cli, err := t.getClient(ctx, lang, server, workspaceRoot)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if resolved != "" {
		if err := cli.syncDocument(ctx, resolved); err != nil {
			return sdk.ToolResult{Success: false}, err
		}
	}
	data, err := t.dispatch(ctx, cli, action, resolved, rel, line, column, query, newName)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{Success: true, Data: data}, nil
}

func (t *Tool) dispatch(ctx context.Context, cli *client, action, resolved, rel string, line, column int, query, newName string) (map[string]any, error) {
	if resolved != "" {
		_ = t.addResolvedRoot(resolved)
	}
	switch action {
	case "definition":
		return cli.locationQuery(ctx, action, "textDocument/definition", resolved, line, column)
	case "typeDefinition":
		if !inferStrategy(cli.capabilities, cli.server).SupportsTypeDef {
			return nil, fmt.Errorf("typeDefinition not supported by %s", cli.server)
		}
		return cli.locationQuery(ctx, action, "textDocument/typeDefinition", resolved, line, column)
	case "references":
		params := textDocPosition(resolved, line, column)
		params["context"] = map[string]any{"includeDeclaration": true}
		return cli.locationQueryWithParams(ctx, action, "textDocument/references", params)
	case "hover":
		resp, err := cli.call(ctx, "textDocument/hover", textDocPosition(resolved, line, column))
		if err != nil {
			return nil, err
		}
		return map[string]any{"action": action, "result": normalizeHover(resp)}, nil
	case "implementations":
		return cli.locationQuery(ctx, action, "textDocument/implementation", resolved, line, column)
	case "symbols":
		resp, err := cli.call(ctx, "workspace/symbol", map[string]any{"query": query})
		if err != nil {
			return nil, err
		}
		items := normalizeSymbols(resp)
		return map[string]any{"action": action, "query": query, "items": items, "count": len(items)}, nil
	case "diagnostics":
		if !inferStrategy(cli.capabilities, cli.server).SupportsDiagnostics {
			return nil, fmt.Errorf("diagnostics not supported by %s", cli.server)
		}
		return cli.diagnostics(ctx, resolved, rel)
	case "callHierarchy":
		if !inferStrategy(cli.capabilities, cli.server).SupportsHierarchy {
			return nil, fmt.Errorf("callHierarchy not supported by %s", cli.server)
		}
		return cli.callHierarchy(ctx, resolved, line, column)
	case "documentSymbols":
		return cli.documentSymbols(ctx, resolved, rel)
	case "rename":
		if !inferStrategy(cli.capabilities, cli.server).SupportsRename {
			return nil, fmt.Errorf("rename not supported by %s", cli.server)
		}
		return cli.rename(ctx, resolved, line, column, newName)
	case "codeAction":
		if !inferStrategy(cli.capabilities, cli.server).SupportsCodeAction {
			return nil, fmt.Errorf("codeAction not supported by %s", cli.server)
		}
		return cli.codeAction(ctx, resolved, rel, line, column)
	case "restart":
		return t.restartClient(ctx, rel)
	case "shutdown":
		return t.shutdownClient(rel)
	case "capabilities":
		return cli.capabilitySummary(), nil
	case "workspaceFolders":
		return cli.workspaceFolderSummary(), nil
	case "addWorkspaceRoot":
		return t.addWorkspaceRoot(rel)
	case "removeWorkspaceRoot":
		return t.removeWorkspaceRoot(rel)
	case "status":
		return cli.statusSummary(), nil
	default:
		return nil, fmt.Errorf("unsupported action: %s", action)
	}
}

func (t *Tool) NotifyFileChange(path string) {
	resolved, rel, err := t.resolveOptional(path)
	if err != nil || rel == "" {
		return
	}
	lang, server, ok := detectServer(rel)
	if !ok {
		return
	}
	cli, err := t.getClient(context.Background(), lang, server, t.pickWorkspaceRoot(resolved))
	if err != nil {
		return
	}
	_ = cli.syncDocument(context.Background(), resolved)
	_ = cli.didSave(resolved)
}

func (t *Tool) addWorkspaceRoot(rel string) (map[string]any, error) {
	if strings.TrimSpace(rel) == "" {
		return nil, fmt.Errorf("path is required")
	}
	resolved, _, err := t.resolveOptional(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	root := resolved
	if !info.IsDir() {
		root = filepath.Dir(resolved)
	}
	roots := t.addRootAndBroadcast(root)
	return map[string]any{"action": "addWorkspaceRoot", "workspace_folders": roots}, nil
}

func (t *Tool) removeWorkspaceRoot(rel string) (map[string]any, error) {
	if strings.TrimSpace(rel) == "" {
		return nil, fmt.Errorf("path is required")
	}
	resolved, _, err := t.resolveOptional(rel)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	root := resolved
	if !info.IsDir() {
		root = filepath.Dir(resolved)
	}
	t.mu.Lock()
	delete(t.roots, root)
	roots := t.workspaceFoldersLocked()
	for key, cli := range t.pool {
		if cli.root == root {
			cli.close()
			delete(t.pool, key)
			continue
		}
		_ = cli.setWorkspaceFolders(roots)
	}
	t.mu.Unlock()
	return map[string]any{"action": "removeWorkspaceRoot", "workspace_folders": roots}, nil
}

func (t *Tool) addResolvedRoot(resolved string) error {
	info, err := os.Stat(resolved)
	if err != nil {
		return err
	}
	root := resolved
	if !info.IsDir() {
		root = filepath.Dir(resolved)
	}
	t.addRootAndBroadcast(root)
	return nil
}

func (t *Tool) addRootAndBroadcast(root string) []map[string]any {
	t.mu.Lock()
	if _, exists := t.roots[root]; exists {
		roots := t.workspaceFoldersLocked()
		t.mu.Unlock()
		return roots
	}
	t.roots[root] = struct{}{}
	roots := t.workspaceFoldersLocked()
	for _, cli := range t.pool {
		_ = cli.setWorkspaceFolders(roots)
	}
	t.mu.Unlock()
	return roots
}

func RegisterHooks(reg *plugin.Registry, tool *Tool) {
	if reg == nil || tool == nil {
		return
	}
	reg.RegisterToolAfter(func(ctx plugin.ToolContext, result sdk.ToolResult) sdk.ToolResult {
		if !result.Success {
			return result
		}
		if ctx.Tool != "fs.write" && ctx.Tool != "fs.edit" {
			return result
		}
		path, _ := result.Data["path"].(string)
		if strings.TrimSpace(path) != "" {
			tool.NotifyFileChange(path)
		}
		return result
	})
}

func (t *Tool) getClient(ctx context.Context, lang, server, workspaceRoot string) (*client, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	key := poolKey(lang, workspaceRoot)
	if cli, ok := t.pool[key]; ok && !cli.closed {
		cli.touch()
		return cli, nil
	}
	cli, err := newClient(ctx, workspaceRoot, server, t.workspaceFolders())
	if err != nil {
		return nil, err
	}
	t.pool[key] = cli
	return cli, nil
}

func (t *Tool) pickWorkspaceRoot(resolved string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	best := t.root
	for root := range t.roots {
		if strings.HasPrefix(resolved, root) && len(root) > len(best) {
			best = root
		}
	}
	return best
}

func poolKey(lang, root string) string { return lang + "::" + root }

func (t *Tool) workspaceFolders() []map[string]any {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.workspaceFoldersLocked()
}

func (t *Tool) workspaceFoldersLocked() []map[string]any {
	roots := make([]string, 0, len(t.roots))
	for root := range t.roots {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	folders := make([]map[string]any, 0, len(roots))
	for _, root := range roots {
		folders = append(folders, map[string]any{"uri": pathToURI(root), "name": filepath.Base(root)})
	}
	return folders
}

func (t *Tool) restartClient(ctx context.Context, rel string) (map[string]any, error) {
	lang, server, ok := detectServer(rel)
	if !ok {
		return nil, fmt.Errorf("no supported language server for path: %s", rel)
	}
	t.mu.Lock()
	if existing, ok := t.pool[lang]; ok {
		existing.close()
		delete(t.pool, lang)
	}
	t.mu.Unlock()
	cli, err := t.getClient(ctx, lang, server, t.pickWorkspaceRoot(rel))
	if err != nil {
		return nil, err
	}
	return map[string]any{"action": "restart", "language": lang, "capabilities": cli.capabilitySummary()["capabilities"]}, nil
}

func (t *Tool) shutdownClient(rel string) (map[string]any, error) {
	lang, _, ok := detectServer(rel)
	if !ok {
		return nil, fmt.Errorf("no supported language server for path: %s", rel)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if cli, ok := t.pool[lang]; ok {
		cli.close()
		delete(t.pool, lang)
	}
	return map[string]any{"action": "shutdown", "language": lang}, nil
}

func newClient(ctx context.Context, root, server string, folders []map[string]any) (*client, error) {
	cmd := exec.CommandContext(ctx, server)
	cmd.Dir = root
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	cli := &client{cmd: cmd, root: root, server: server, in: in, reader: bufio.NewReader(out), versions: map[string]int{}, diagnosticStore: map[string][]map[string]any{}, pending: map[string]responseEnvelope{}}
	cli.cond = sync.NewCond(&cli.mu)
	cli.state = "starting"
	cli.lastUsedAt = time.Now().UTC()
	go cli.readLoop()
	go cli.supervise()
	if err := cli.initialize(root, folders); err != nil {
		_ = cmd.Process.Kill()
		return nil, err
	}
	return cli, nil
}

func (c *client) initialize(root string, folders []map[string]any) error {
	params := map[string]any{
		"processId":        os.Getpid(),
		"rootUri":          pathToURI(root),
		"workspaceFolders": folders,
		"capabilities": map[string]any{
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{},
				"callHierarchy":      map[string]any{},
			},
			"workspace": map[string]any{"symbol": map[string]any{}, "workspaceFolders": true},
		},
	}
	resp, err := c.call(context.Background(), "initialize", params)
	if err != nil {
		return err
	}
	result, _ := normalizeGeneric(resp).(map[string]any)
	if caps, ok := result["capabilities"].(map[string]any); ok {
		c.mu.Lock()
		c.capabilities = caps
		c.workspaceFolder = folders
		c.state = "ready"
		c.mu.Unlock()
	}
	return c.notify("initialized", map[string]any{})
}

func (c *client) setWorkspaceFolders(folders []map[string]any) error {
	c.mu.Lock()
	old := c.workspaceFolder
	c.workspaceFolder = folders
	added, removed := diffFolders(old, folders)
	c.mu.Unlock()
	return c.notify("workspace/didChangeWorkspaceFolders", map[string]any{
		"event": map[string]any{
			"added":   added,
			"removed": removed,
		},
	})
}

func (c *client) capabilitySummary() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"action":            "capabilities",
		"server":            c.server,
		"strategy":          inferStrategy(c.capabilities, c.server),
		"workspace_folders": c.workspaceFolder,
		"capabilities":      c.capabilities,
	}
}

func (c *client) workspaceFolderSummary() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"action":            "workspaceFolders",
		"workspace_folders": c.workspaceFolder,
	}
}

func (c *client) statusSummary() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return map[string]any{
		"action":           "status",
		"server":           c.server,
		"state":            c.state,
		"closed":           c.closed,
		"restart_count":    c.restartCount,
		"last_crash":       c.lastCrash,
		"last_health_ok":   c.lastHealthOK,
		"last_error":       c.lastError,
		"consecutive_fail": c.consecutiveFail,
		"last_used_at":     c.lastUsedAt,
	}
}

func (c *client) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_, _ = c.call(context.Background(), "shutdown", map[string]any{})
	_ = c.notify("exit", map[string]any{})
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

func (c *client) touch() {
	c.mu.Lock()
	c.lastUsedAt = time.Now().UTC()
	c.mu.Unlock()
}

func (c *client) locationQuery(ctx context.Context, action, method, path string, line, column int) (map[string]any, error) {
	return c.locationQueryWithParams(ctx, action, method, textDocPosition(path, line, column))
}

func (c *client) locationQueryWithParams(ctx context.Context, action, method string, params map[string]any) (map[string]any, error) {
	resp, err := c.call(ctx, method, params)
	if err != nil {
		return nil, err
	}
	items := normalizeLocations(resp)
	return map[string]any{"action": action, "items": items, "count": len(items)}, nil
}

func (c *client) diagnostics(ctx context.Context, resolved, rel string) (map[string]any, error) {
	if resolved == "" {
		return nil, fmt.Errorf("path is required for diagnostics")
	}
	if err := c.syncDocument(ctx, resolved); err != nil {
		return nil, err
	}
	_ = c.didSave(resolved)
	uri := pathToURI(resolved)
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		items := c.getDiagnostics(uri)
		if items != nil {
			return map[string]any{"action": "diagnostics", "path": rel, "items": items, "count": len(items)}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	items := c.getDiagnostics(uri)
	return map[string]any{"action": "diagnostics", "path": rel, "items": items, "count": len(items)}, nil
}

func (c *client) callHierarchy(ctx context.Context, resolved string, line, column int) (map[string]any, error) {
	prepare, err := c.call(ctx, "textDocument/prepareCallHierarchy", textDocPosition(resolved, line, column))
	if err != nil {
		return nil, err
	}
	items := normalizeCallHierarchyItems(prepare)
	if len(items) == 0 {
		return map[string]any{"action": "callHierarchy", "items": []any{}, "incoming": []any{}, "outgoing": []any{}}, nil
	}
	first := items[0]["raw"]
	incomingResp, _ := c.call(ctx, "callHierarchy/incomingCalls", map[string]any{"item": first})
	outgoingResp, _ := c.call(ctx, "callHierarchy/outgoingCalls", map[string]any{"item": first})
	return map[string]any{
		"action":   "callHierarchy",
		"items":    stripRaw(items),
		"incoming": normalizeCallHierarchyCalls(incomingResp, "from"),
		"outgoing": normalizeCallHierarchyCalls(outgoingResp, "to"),
	}, nil
}

func (c *client) documentSymbols(ctx context.Context, resolved, rel string) (map[string]any, error) {
	resp, err := c.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]any{"uri": pathToURI(resolved)}})
	if err != nil {
		return nil, err
	}
	items := normalizeDocumentSymbols(resp)
	return map[string]any{"action": "documentSymbols", "path": rel, "items": items, "count": len(items)}, nil
}

func (c *client) rename(ctx context.Context, resolved string, line, column int, newName string) (map[string]any, error) {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return nil, fmt.Errorf("newName is required for rename")
	}
	strategy := inferStrategy(c.capabilities, c.server)
	if strategy.RenamePrepare {
		_, _ = c.call(ctx, "textDocument/prepareRename", map[string]any{
			"textDocument": map[string]any{"uri": pathToURI(resolved)},
			"position":     map[string]any{"line": max(line-1, 0), "character": max(column-1, 0)},
		})
	}
	resp, err := c.call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(resolved)},
		"position":     map[string]any{"line": max(line-1, 0), "character": max(column-1, 0)},
		"newName":      newName,
	})
	if err != nil {
		return nil, err
	}
	edit := normalizeWorkspaceEdit(resp)
	applied, err := applyWorkspaceEdit(c.root, edit)
	if err != nil {
		return nil, err
	}
	for _, path := range applied {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			_ = c.syncDocument(ctx, path)
			_ = c.didSave(path)
		}
	}
	return map[string]any{"action": "rename", "newName": newName, "workspaceEdit": edit, "applied": applied}, nil
}

func (c *client) codeAction(ctx context.Context, resolved, rel string, line, column int) (map[string]any, error) {
	uri := pathToURI(resolved)
	strategy := inferStrategy(c.capabilities, c.server)
	diagnostics := c.getDiagnostics(uri)
	resp, err := c.call(ctx, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": max(line-1, 0), "character": max(column-1, 0)},
			"end":   map[string]any{"line": max(line-1, 0), "character": max(column-1, 0)},
		},
		"context": map[string]any{"diagnostics": diagnostics},
	})
	if err != nil {
		return nil, err
	}
	actions := normalizeCodeActions(resp)
	applied := []map[string]any{}
	for _, action := range actions {
		if strategy.CodeActionResolve {
			if data, ok := action["data"]; ok {
				if resolvedAction, err := c.call(ctx, "codeAction/resolve", map[string]any{"data": data}); err == nil {
					if resolvedMap, ok := normalizeGeneric(resolvedAction).(map[string]any); ok {
						for k, v := range resolvedMap {
							action[k] = v
						}
					}
				}
			}
		}
		if edit, ok := action["edit"].(map[string]any); ok {
			changed, err := applyWorkspaceEdit(c.root, edit)
			if err == nil && len(changed) > 0 {
				for _, path := range changed {
					if info, err := os.Stat(path); err == nil && !info.IsDir() {
						_ = c.syncDocument(ctx, path)
						_ = c.didSave(path)
					}
				}
				action["applied"] = changed
				applied = append(applied, map[string]any{"title": action["title"], "files": changed})
			}
		}
		if cmd, ok := action["command"].(map[string]any); ok {
			if result, err := c.call(ctx, "workspace/executeCommand", cmd); err == nil {
				action["command_executed"] = true
				action["command_result"] = normalizeGeneric(result)
				if err := c.syncDocument(ctx, resolved); err == nil {
					_ = c.didSave(resolved)
				}
				action["post_command_diagnostics"] = c.getDiagnostics(uri)
			}
		}
	}
	return map[string]any{"action": "codeAction", "path": rel, "items": actions, "applied": applied}, nil
}

func (c *client) syncDocument(ctx context.Context, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	uri := pathToURI(path)
	text := string(data)
	c.mu.Lock()
	version := c.versions[uri]
	opened := version > 0
	if opened {
		version++
	} else {
		version = 1
	}
	c.versions[uri] = version
	c.mu.Unlock()
	if !opened {
		return c.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri":        uri,
				"languageId": languageID(path),
				"version":    version,
				"text":       text,
			},
		})
	}
	return c.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

func (c *client) didSave(path string) error {
	return c.notify("textDocument/didSave", map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
	})
}

func (c *client) notify(method string, params any) error {
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *client) call(ctx context.Context, method string, params any) (any, error) {
	c.mu.Lock()
	c.seq++
	id := fmt.Sprintf("%d", c.seq)
	c.state = "busy"
	c.lastUsedAt = time.Now().UTC()
	c.mu.Unlock()
	if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		if restartErr := c.restart(); restartErr == nil {
			if err := c.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err == nil {
				goto wait
			}
		}
		return nil, err
	}
wait:
	deadline := time.Now().Add(20 * time.Second)
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		if env, ok := c.pending[id]; ok {
			delete(c.pending, id)
			c.state = "ready"
			c.lastError = ""
			if env.Error != nil {
				c.lastError = fmt.Sprint(env.Error)
				return nil, fmt.Errorf("lsp error: %v", env.Error)
			}
			return env.Result, nil
		}
		if time.Now().After(deadline) {
			c.state = "degraded"
			c.lastError = fmt.Sprintf("timeout: %s", method)
			c.consecutiveFail++
			if restartErr := c.restart(); restartErr == nil {
				return c.call(ctx, method, params)
			}
			return nil, fmt.Errorf("lsp call timed out: %s", method)
		}
		c.cond.Wait()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
}

func (c *client) restart() error {
	time.Sleep(time.Duration(max(c.restartCount, 1)) * 200 * time.Millisecond)
	c.close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.server)
	cmd.Dir = c.root
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	c.mu.Lock()
	c.cmd = cmd
	c.in = in
	c.reader = bufio.NewReader(out)
	c.closed = false
	c.pending = map[string]responseEnvelope{}
	c.restartCount++
	c.state = "restarting"
	c.cond = sync.NewCond(&c.mu)
	c.mu.Unlock()
	go c.readLoop()
	return c.initialize(c.root, c.workspaceFolder)
}

func (c *client) supervise() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		closed := c.closed
		state := c.state
		lastUsed := c.lastUsedAt
		c.mu.Unlock()
		if !lastUsed.IsZero() && time.Since(lastUsed) > 5*time.Minute {
			c.close()
			return
		}
		if closed {
			if state == "crashed" || state == "degraded" {
				_ = c.restart()
			}
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_, err := c.call(ctx, "workspace/symbol", map[string]any{"query": ""})
		cancel()
		if err != nil {
			c.mu.Lock()
			c.state = "degraded"
			c.lastError = err.Error()
			c.consecutiveFail++
			c.mu.Unlock()
			_ = c.restart()
			continue
		}
		c.mu.Lock()
		c.lastHealthOK = time.Now().UTC()
		c.lastError = ""
		c.consecutiveFail = 0
		c.mu.Unlock()
	}
}

func (c *client) write(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err = fmt.Fprintf(c.in, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func (c *client) readLoop() {
	for {
		env, err := readMessage(c.reader)
		if err != nil {
			c.mu.Lock()
			c.closed = true
			c.lastCrash = time.Now().UTC()
			c.state = "crashed"
			c.lastError = err.Error()
			c.consecutiveFail++
			c.cond.Broadcast()
			c.mu.Unlock()
			return
		}
		c.handleEnvelope(env)
	}
}

func (c *client) handleEnvelope(env responseEnvelope) {
	if env.Method == "textDocument/publishDiagnostics" {
		params := env.Params
		uri, _ := params["uri"].(string)
		items := normalizeDiagnostics(params["diagnostics"])
		c.mu.Lock()
		c.diagnosticStore[uri] = items
		c.cond.Broadcast()
		c.mu.Unlock()
		return
	}
	if env.ID == nil {
		return
	}
	id := fmt.Sprint(env.ID)
	c.mu.Lock()
	c.pending[id] = env
	c.cond.Broadcast()
	c.mu.Unlock()
}

func (c *client) getDiagnostics(uri string) []map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()
	items, ok := c.diagnosticStore[uri]
	if !ok {
		return nil
	}
	copyItems := make([]map[string]any, len(items))
	copy(copyItems, items)
	return copyItems
}

type responseEnvelope struct {
	ID     any            `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
	Result any            `json:"result"`
	Error  any            `json:"error"`
}

func readMessage(r *bufio.Reader) (responseEnvelope, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return responseEnvelope{}, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			_, _ = fmt.Sscanf(line, "Content-Length: %d", &contentLength)
		}
	}
	if contentLength <= 0 {
		return responseEnvelope{}, fmt.Errorf("invalid lsp content length")
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return responseEnvelope{}, err
	}
	var env responseEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return responseEnvelope{}, err
	}
	return env, nil
}

func detectServer(path string) (string, string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go", "gopls", true
	case ".ts", ".tsx", ".js", ".jsx":
		return "typescript", "typescript-language-server", true
	case ".py":
		return "python", "pylsp", true
	default:
		return "", "", false
	}
}

func strategyForServer(server string) serverStrategy {
	switch server {
	case "gopls":
		return serverStrategy{SupportsRename: true, SupportsCodeAction: true, SupportsHierarchy: true, SupportsTypeDef: true, SupportsDiagnostics: true}
	case "typescript-language-server":
		return serverStrategy{SupportsRename: true, SupportsCodeAction: true, SupportsHierarchy: true, SupportsTypeDef: true, SupportsDiagnostics: true}
	case "pylsp":
		return serverStrategy{SupportsRename: true, SupportsCodeAction: false, SupportsHierarchy: false, SupportsTypeDef: false, SupportsDiagnostics: true}
	default:
		return serverStrategy{}
	}
}

func inferStrategy(caps map[string]any, server string) serverStrategy {
	base := strategyForServer(server)
	if caps == nil {
		return base
	}
	if renameProvider, ok := caps["renameProvider"]; ok {
		base.SupportsRename = renameProvider != nil && renameProvider != false
	}
	if codeActionProvider, ok := caps["codeActionProvider"]; ok {
		base.SupportsCodeAction = codeActionProvider != nil && codeActionProvider != false
	}
	if typeDefProvider, ok := caps["typeDefinitionProvider"]; ok {
		base.SupportsTypeDef = typeDefProvider != nil && typeDefProvider != false
	}
	if callHierarchyProvider, ok := caps["callHierarchyProvider"]; ok {
		base.SupportsHierarchy = callHierarchyProvider != nil && callHierarchyProvider != false
	}
	if renameProvider, ok := caps["renameProvider"].(map[string]any); ok {
		if prepare, ok := renameProvider["prepareProvider"]; ok {
			base.RenamePrepare = prepare != nil && prepare != false
		}
	}
	if codeActionProvider, ok := caps["codeActionProvider"].(map[string]any); ok {
		if resolve, ok := codeActionProvider["resolveProvider"]; ok {
			base.CodeActionResolve = resolve != nil && resolve != false
		}
	}
	return base
}

func languageID(path string) string {
	lang, _, _ := detectServer(path)
	return lang
}

func textDocPosition(path string, line, column int) map[string]any {
	if line <= 0 {
		line = 1
	}
	if column <= 0 {
		column = 1
	}
	return map[string]any{
		"textDocument": map[string]any{"uri": pathToURI(path)},
		"position":     map[string]any{"line": line - 1, "character": column - 1},
	}
}

func normalizeLocations(value any) []map[string]any {
	data, _ := json.Marshal(value)
	var locations []protocol.Location
	if err := json.Unmarshal(data, &locations); err == nil {
		return locationItems(locations)
	}
	var single protocol.Location
	if err := json.Unmarshal(data, &single); err == nil && single.URI != "" {
		return locationItems([]protocol.Location{single})
	}
	return nil
}

func locationItems(locations []protocol.Location) []map[string]any {
	items := make([]map[string]any, 0, len(locations))
	for _, loc := range locations {
		items = append(items, map[string]any{
			"uri":        string(loc.URI),
			"start_line": int(loc.Range.Start.Line) + 1,
			"start_col":  int(loc.Range.Start.Character) + 1,
			"end_line":   int(loc.Range.End.Line) + 1,
			"end_col":    int(loc.Range.End.Character) + 1,
		})
	}
	return items
}

func normalizeHover(value any) string {
	data, _ := json.Marshal(value)
	var hover struct {
		Contents any `json:"contents"`
	}
	if err := json.Unmarshal(data, &hover); err != nil {
		return string(data)
	}
	pretty, _ := json.Marshal(hover.Contents)
	return string(pretty)
}

func normalizeSymbols(value any) []map[string]any {
	data, _ := json.Marshal(value)
	var symbols []map[string]any
	_ = json.Unmarshal(data, &symbols)
	items := make([]map[string]any, 0, len(symbols))
	for _, symbol := range symbols {
		item := map[string]any{
			"name":      symbol["name"],
			"kind":      symbol["kind"],
			"container": symbol["containerName"],
			"location":  symbol["location"],
		}
		items = append(items, item)
	}
	return items
}

func normalizeDocumentSymbols(value any) []map[string]any {
	data, _ := json.Marshal(value)
	var symbols []map[string]any
	_ = json.Unmarshal(data, &symbols)
	return flattenDocumentSymbols(symbols, "")
}

func flattenDocumentSymbols(symbols []map[string]any, parent string) []map[string]any {
	items := []map[string]any{}
	for _, symbol := range symbols {
		name, _ := symbol["name"].(string)
		item := map[string]any{
			"name":      name,
			"kind":      symbol["kind"],
			"parent":    parent,
			"range":     symbol["range"],
			"selection": symbol["selectionRange"],
		}
		items = append(items, item)
		if children, ok := symbol["children"].([]any); ok {
			childMaps := make([]map[string]any, 0, len(children))
			for _, child := range children {
				if m, ok := child.(map[string]any); ok {
					childMaps = append(childMaps, m)
				}
			}
			items = append(items, flattenDocumentSymbols(childMaps, name)...)
		}
	}
	return items
}

func normalizeDiagnostics(value any) []map[string]any {
	data, _ := json.Marshal(value)
	var items []map[string]any
	_ = json.Unmarshal(data, &items)
	return items
}

func normalizeGeneric(value any) any {
	data, _ := json.Marshal(value)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}

func normalizeWorkspaceEdit(value any) map[string]any {
	if edit, ok := normalizeGeneric(value).(map[string]any); ok {
		return edit
	}
	return map[string]any{}
}

func normalizeCodeActions(value any) []map[string]any {
	data, _ := json.Marshal(value)
	var items []map[string]any
	_ = json.Unmarshal(data, &items)
	return items
}

func diffFolders(oldFolders, newFolders []map[string]any) ([]map[string]any, []map[string]any) {
	oldSet := map[string]map[string]any{}
	newSet := map[string]map[string]any{}
	for _, folder := range oldFolders {
		if uri, _ := folder["uri"].(string); uri != "" {
			oldSet[uri] = folder
		}
	}
	for _, folder := range newFolders {
		if uri, _ := folder["uri"].(string); uri != "" {
			newSet[uri] = folder
		}
	}
	added := []map[string]any{}
	removed := []map[string]any{}
	for uri, folder := range newSet {
		if _, ok := oldSet[uri]; !ok {
			added = append(added, folder)
		}
	}
	for uri, folder := range oldSet {
		if _, ok := newSet[uri]; !ok {
			removed = append(removed, folder)
		}
	}
	sort.Slice(added, func(i, j int) bool { return fmt.Sprint(added[i]["uri"]) < fmt.Sprint(added[j]["uri"]) })
	sort.Slice(removed, func(i, j int) bool { return fmt.Sprint(removed[i]["uri"]) < fmt.Sprint(removed[j]["uri"]) })
	return added, removed
}

func normalizeCallHierarchyItems(value any) []map[string]any {
	data, _ := json.Marshal(value)
	var raw []map[string]any
	_ = json.Unmarshal(data, &raw)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		copyItem := map[string]any{}
		for k, v := range item {
			copyItem[k] = v
		}
		copyItem["raw"] = item
		items = append(items, copyItem)
	}
	return items
}

func normalizeCallHierarchyCalls(value any, key string) []map[string]any {
	data, _ := json.Marshal(value)
	var raw []map[string]any
	_ = json.Unmarshal(data, &raw)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		entry := map[string]any{}
		if target, ok := item[key].(map[string]any); ok {
			for k, v := range target {
				entry[k] = v
			}
		}
		if fromRanges, ok := item["fromRanges"]; ok {
			entry["ranges"] = fromRanges
		}
		if fromRanges, ok := item["toRanges"]; ok {
			entry["ranges"] = fromRanges
		}
		items = append(items, entry)
	}
	return items
}

func stripRaw(items []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		copyItem := map[string]any{}
		for k, v := range item {
			if k == "raw" {
				continue
			}
			copyItem[k] = v
		}
		out = append(out, copyItem)
	}
	return out
}

func (t *Tool) resolveOptional(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", nil
	}
	cleaned := filepath.Clean(path)
	absRoot, err := filepath.Abs(t.root)
	if err != nil {
		return "", "", err
	}
	absPath, err := filepath.Abs(filepath.Join(t.root, cleaned))
	if err != nil {
		return "", "", err
	}
	if !strings.HasPrefix(absPath, absRoot) {
		return "", "", fmt.Errorf("path escapes workspace")
	}
	rel, err := filepath.Rel(t.root, absPath)
	if err != nil {
		return "", "", err
	}
	return absPath, rel, nil
}

func pathToURI(path string) string {
	return "file://" + filepath.ToSlash(path)
}

func uriToPath(uri string) string {
	return filepath.FromSlash(strings.TrimPrefix(uri, "file://"))
}

func applyWorkspaceEdit(root string, edit map[string]any) ([]string, error) {
	changes, _ := edit["changes"].(map[string]any)
	documentChanges, _ := edit["documentChanges"].([]any)
	if len(changes) == 0 && len(documentChanges) == 0 {
		return nil, nil
	}
	applied := []string{}
	if len(documentChanges) > 0 {
		for _, change := range documentChanges {
			item, ok := change.(map[string]any)
			if !ok {
				continue
			}
			if kind, _ := item["kind"].(string); kind == "create" {
				uri, _ := item["uri"].(string)
				path := uriToPath(uri)
				if !strings.HasPrefix(path, root) {
					return nil, fmt.Errorf("workspace edit escapes root")
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return nil, err
				}
				if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
					return nil, err
				}
				applied = append(applied, path)
				continue
			}
			if kind, _ := item["kind"].(string); kind == "rename" {
				oldURI, _ := item["oldUri"].(string)
				newURI, _ := item["newUri"].(string)
				oldPath := uriToPath(oldURI)
				newPath := uriToPath(newURI)
				if !strings.HasPrefix(oldPath, root) || !strings.HasPrefix(newPath, root) {
					return nil, fmt.Errorf("workspace edit escapes root")
				}
				if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
					return nil, err
				}
				if err := os.Rename(oldPath, newPath); err != nil {
					return nil, err
				}
				applied = append(applied, newPath)
				continue
			}
			if kind, _ := item["kind"].(string); kind == "delete" {
				uri, _ := item["uri"].(string)
				path := uriToPath(uri)
				if !strings.HasPrefix(path, root) {
					return nil, fmt.Errorf("workspace edit escapes root")
				}
				if err := os.RemoveAll(path); err != nil {
					return nil, err
				}
				applied = append(applied, path)
				continue
			}
			textDoc, _ := item["textDocument"].(map[string]any)
			uri, _ := textDoc["uri"].(string)
			if uri == "" {
				continue
			}
			path := uriToPath(uri)
			if !strings.HasPrefix(path, root) {
				return nil, fmt.Errorf("workspace edit escapes root")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			updated, err := applyTextEdits(string(data), item["edits"])
			if err != nil {
				return nil, err
			}
			if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
				return nil, err
			}
			applied = append(applied, path)
		}
	}
	keys := make([]string, 0, len(changes))
	for uri := range changes {
		keys = append(keys, uri)
	}
	sort.Strings(keys)
	for _, uri := range keys {
		path := uriToPath(uri)
		if !strings.HasPrefix(path, root) {
			return nil, fmt.Errorf("workspace edit escapes root")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		updated, err := applyTextEdits(string(data), changes[uri])
		if err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
			return nil, err
		}
		applied = append(applied, path)
	}
	return applied, nil
}

func applyTextEdits(content string, raw any) (string, error) {
	data, _ := json.Marshal(raw)
	var edits []struct {
		Range struct {
			Start struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"start"`
			End struct {
				Line      int `json:"line"`
				Character int `json:"character"`
			} `json:"end"`
		} `json:"range"`
		NewText string `json:"newText"`
	}
	if err := json.Unmarshal(data, &edits); err != nil {
		return "", err
	}
	lines := strings.SplitAfter(content, "\n")
	if len(lines) == 0 {
		lines = []string{""}
	}
	full := strings.Join(lines, "")
	offsets := lineOffsets(full)
	for i := len(edits) - 1; i >= 0; i-- {
		start := positionOffset(offsets, edits[i].Range.Start.Line, edits[i].Range.Start.Character)
		end := positionOffset(offsets, edits[i].Range.End.Line, edits[i].Range.End.Character)
		if start < 0 || end < start || end > len(full) {
			return "", fmt.Errorf("invalid text edit range")
		}
		full = full[:start] + edits[i].NewText + full[end:]
		offsets = lineOffsets(full)
	}
	return full, nil
}

func lineOffsets(content string) []int {
	offsets := []int{0}
	for i, r := range content {
		if r == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	return offsets
}

func positionOffset(offsets []int, line, char int) int {
	if line < 0 {
		line = 0
	}
	if line >= len(offsets) {
		return -1
	}
	return offsets[line] + char
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func intValue(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
