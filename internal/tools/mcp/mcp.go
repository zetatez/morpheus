package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type Manager struct {
	mu               sync.Mutex
	clients          map[string]*client
	tools            map[string]*ProxyTool
	onChange         func() error
	cachePath        string
	onResourceUpdate func(server, uri string, payload map[string]any)
	namer            *ToolNamer
	authStore        *AuthStore
	oauthProviders   map[string]*McpOAuthProvider
	onOAuthRedirect  func(mcpName string, authURL *url.URL) error
}

// ToolNamingStyle defines how MCP tool names are formatted
type ToolNamingStyle int

const (
	// FullQualified: mcp.filesystem.read (server.tool)
	FullQualified ToolNamingStyle = iota
	// Flat: filesystem_read (no prefix, underscores)
	Flat
	// Compact: read (abbreviated server, dot separator)
	Compact
	// DoubleColon: mcp::filesystem::read (double colon for clarity)
	DoubleColon
)

// ToolNamer provides configurable tool name formatting
type ToolNamer struct {
	style ToolNamingStyle
}

// Server abbreviation map for Compact style
var serverAbbreviations = map[string]string{
	"filesystem": "fs",
	"github":     "gh",
	"gitlab":     "gl",
	"jira":       "jira",
	"slack":      "slack",
	"docker":     "docker",
	"kubernetes": "k8s",
	"postgres":   "pg",
	"mysql":      "mysql",
	"redis":      "redis",
	"http":       "http",
	"sqlite":     "sql",
	"memory":     "mem",
}

// NewToolNamer creates a ToolNamer with the specified style
func NewToolNamer(style ToolNamingStyle) *ToolNamer {
	return &ToolNamer{style: style}
}

// FormatName formats a tool name according to the naming style
func (n *ToolNamer) FormatName(serverName, toolName string) string {
	switch n.style {
	case Flat:
		return sanitizeName(serverName) + "_" + sanitizeName(toolName)
	case Compact:
		abbr := n.abbreviate(serverName)
		return abbr + "." + sanitizeName(toolName)
	case DoubleColon:
		return "mcp::" + sanitizeName(serverName) + "::" + sanitizeName(toolName)
	case FullQualified:
		fallthrough
	default:
		return "mcp." + serverName + "." + toolName
	}
}

// abbreviate returns an abbreviated server name
func (n *ToolNamer) abbreviate(serverName string) string {
	if abbr, ok := serverAbbreviations[serverName]; ok {
		return abbr
	}
	// For unknown servers, use first 2-3 chars
	if len(serverName) <= 3 {
		return serverName
	}
	return serverName[:3]
}

// sanitizeName removes or replaces characters not suitable for tool names
func sanitizeName(name string) string {
	// Replace underscores with hyphens (common in MCP tool names)
	name = strings.ReplaceAll(name, "_", "-")
	// Remove any characters that aren't alphanumeric, hyphens, or dots
	reg := regexp.MustCompile(`[^a-zA-Z0-9.\-]`)
	return reg.ReplaceAllString(name, "")
}

// GetNamingStyles returns all available naming styles
func GetNamingStyles() map[string]ToolNamingStyle {
	return map[string]ToolNamingStyle{
		"full-qualified": FullQualified,
		"flat":           Flat,
		"compact":        Compact,
		"double-colon":   DoubleColon,
	}
}

// AutoDiscover searches for MCP server configurations in standard locations
func (m *Manager) AutoDiscover(ctx context.Context) ([]ServerConfig, error) {
	var configs []ServerConfig

	// Search paths for MCP configurations
	searchPaths := []string{
		filepath.Join(os.Getenv("HOME"), ".config", "morpheus", "mcp"),
		filepath.Join(os.Getenv("HOME"), ".config", "opencode", "mcp"),
		filepath.Join(os.Getenv("HOME"), ".claude", "mcp"),
		filepath.Join(os.Getenv("HOME"), ".agents", "mcp"),
	}

	// Config file names to look for
	configFiles := []string{
		"mcp.json",
		"mcp_config.json",
		".mcp.json",
		"servers.json",
	}

	for _, basePath := range searchPaths {
		for _, configFile := range configFiles {
			configPath := filepath.Join(basePath, configFile)
			if servers, err := m.loadServerConfigs(configPath); err == nil && len(servers) > 0 {
				configs = append(configs, servers...)
			}
		}

		// Also scan directory for individual server configs
		if entries, err := os.ReadDir(basePath); err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					// Check if directory contains a server config
					serverFile := filepath.Join(basePath, entry.Name(), "server.json")
					if servers, err := m.loadServerConfigs(serverFile); err == nil && len(servers) > 0 {
						configs = append(configs, servers...)
					}
				}
			}
		}
	}

	// Check environment variable for additional configs
	if envPath := os.Getenv("MCP_CONFIG_PATH"); envPath != "" {
		if servers, err := m.loadServerConfigs(envPath); err == nil && len(servers) > 0 {
			configs = append(configs, servers...)
		}
	}

	// Dedupe by name
	seen := make(map[string]bool)
	deduped := make([]ServerConfig, 0, len(configs))
	for _, c := range configs {
		if !seen[c.Name] {
			seen[c.Name] = true
			deduped = append(deduped, c)
		}
	}

	return deduped, nil
}

// loadServerConfigs loads server configs from a file
func (m *Manager) loadServerConfigs(path string) ([]ServerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	// Try parsing as array of configs
	var arrayConfigs []ServerConfig
	if err := json.Unmarshal(data, &arrayConfigs); err == nil && len(arrayConfigs) > 0 {
		return arrayConfigs, nil
	}

	// Try parsing as object with "servers" key
	var structConfig struct {
		Servers []ServerConfig `json:"servers"`
	}
	if err := json.Unmarshal(data, &structConfig); err == nil && len(structConfig.Servers) > 0 {
		return structConfig.Servers, nil
	}

	return nil, fmt.Errorf("invalid MCP config format: %s", path)
}

// GetServerCapabilities returns the capabilities of a connected server
func (m *Manager) GetServerCapabilities(name string) ServerCapabilities {
	m.mu.Lock()
	defer m.mu.Unlock()

	cli, ok := m.clients[name]
	if !ok {
		return ServerCapabilities{}
	}

	caps := ServerCapabilities{}

	// Check what lists we can query
	if _, err := cli.transport.Call(context.Background(), "tools/list", map[string]any{}); err == nil {
		caps.Tools = true
	}
	if _, err := cli.transport.Call(context.Background(), "resources/list", map[string]any{}); err == nil {
		caps.Resources = true
	}

	return caps
}

// ServerCapabilities describes what a server supports
type ServerCapabilities struct {
	Tools                bool
	Resources            bool
	ResourceSubscription bool
}

type ServerConfig struct {
	Name       string
	Command    string
	Transport  string
	URL        string
	SSEURL     string
	AuthToken  string
	AuthHeader string
	OAuth      OAuthConfig
}

type ProxyTool struct {
	manager    *Manager
	serverName string
	toolName   string
	desc       string
	schema     map[string]any
}

type ControlTool struct{ manager *Manager }

type transport interface {
	Call(ctx context.Context, method string, params map[string]any) (map[string]any, error)
	Notify(ctx context.Context, method string, params map[string]any) error
	Close() error
	Listen(func(map[string]any))
}

type client struct {
	name          string
	transport     transport
	mu            sync.Mutex
	resources     []map[string]any
	resourceCache map[string]map[string]any
	subscribed    map[string]map[string]struct{}
	config        ServerConfig
}

type stdioTransport struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
	mu  sync.Mutex
	seq int64
}

type httpTransport struct {
	url           string
	client        *http.Client
	mu            sync.Mutex
	seq           int64
	sseURL        string
	headers       map[string]string
	oauthProvider *McpOAuthProvider
}

func NewManager(cachePath string) *Manager {
	return &Manager{
		clients:        map[string]*client{},
		tools:          map[string]*ProxyTool{},
		cachePath:      cachePath,
		namer:          NewToolNamer(Compact),
		oauthProviders: map[string]*McpOAuthProvider{},
	}
}

func NewManagerWithAuthStore(cachePath, dataDir string) (*Manager, error) {
	store, err := NewAuthStore(dataDir)
	if err != nil {
		return nil, err
	}
	return &Manager{
		clients:        map[string]*client{},
		tools:          map[string]*ProxyTool{},
		cachePath:      cachePath,
		namer:          NewToolNamer(Compact),
		authStore:      store,
		oauthProviders: map[string]*McpOAuthProvider{},
	}, nil
}

func (m *Manager) SetOnChange(fn func() error) { m.onChange = fn }
func (m *Manager) SetOnResourceUpdate(fn func(server, uri string, payload map[string]any)) {
	m.onResourceUpdate = fn
}
func (m *Manager) SetOnOAuthRedirect(fn func(mcpName string, authURL *url.URL) error) {
	m.onOAuthRedirect = fn
}

// SetNamingStyle configures the tool naming style
func (m *Manager) SetNamingStyle(style ToolNamingStyle) {
	m.namer = NewToolNamer(style)
}

// GetNamingStyle returns the current naming style
func (m *Manager) GetNamingStyle() ToolNamingStyle {
	return m.namer.style
}

func NewControlTool(m *Manager) *ControlTool { return &ControlTool{manager: m} }

func (t *ControlTool) Name() string { return "mcp" }
func (t *ControlTool) Describe() string {
	return "Manage MCP servers, inspect tools/resources, and subscribe to MCP resources."
}
func (t *ControlTool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{
		"action":     map[string]any{"type": "string"},
		"name":       map[string]any{"type": "string"},
		"command":    map[string]any{"type": "string"},
		"transport":  map[string]any{"type": "string"},
		"url":        map[string]any{"type": "string"},
		"uri":        map[string]any{"type": "string"},
		"session_id": map[string]any{"type": "string"},
	}, "required": []string{"action"}}
}

func (t *ControlTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	action, _ := input["action"].(string)
	name, _ := input["name"].(string)
	command, _ := input["command"].(string)
	transportName, _ := input["transport"].(string)
	url, _ := input["url"].(string)
	uri, _ := input["uri"].(string)
	sessionID, _ := input["session_id"].(string)
	switch strings.TrimSpace(action) {
	case "connect":
		return t.manager.connect(ctx, ServerConfig{Name: name, Command: command, Transport: transportName, URL: url})
	case "disconnect":
		return t.manager.disconnect(name)
	case "servers":
		return t.manager.servers(), nil
	case "tools":
		return t.manager.listTools(ctx, name)
	case "resources":
		return t.manager.listResources(ctx, name)
	case "readResource":
		return t.manager.readResource(ctx, name, uri)
	case "subscribe":
		return t.manager.subscribe(ctx, name, uri, sessionID)
	default:
		return sdk.ToolResult{Success: false}, fmt.Errorf("unsupported action: %s", action)
	}
}

func (t *ProxyTool) Name() string           { return t.toolName }
func (t *ProxyTool) Describe() string       { return t.desc }
func (t *ProxyTool) Schema() map[string]any { return t.schema }
func (t *ProxyTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	return t.manager.callTool(ctx, t.serverName, t.toolName, input)
}

func (m *Manager) Bootstrap(ctx context.Context, servers []ServerConfig) ([]sdk.Tool, error) {
	_ = m.loadCache()
	tools := []sdk.Tool{NewControlTool(m)}
	for _, cfg := range servers {
		res, err := m.connect(ctx, cfg)
		if err != nil || !res.Success {
			continue
		}
	}
	return append(tools, m.proxyTools()...), nil
}

func (m *Manager) AllTools() []sdk.Tool {
	return append([]sdk.Tool{NewControlTool(m)}, m.proxyTools()...)
}

func (m *Manager) connect(ctx context.Context, cfg ServerConfig) (sdk.ToolResult, error) {
	if strings.TrimSpace(cfg.Name) == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("name is required")
	}
	if cfg.Transport == "" {
		cfg.Transport = "stdio"
	}

	var oauthProvider *McpOAuthProvider
	if m.authStore != nil && cfg.OAuth.AuthURL != "" && cfg.OAuth.TokenURL != "" {
		oauthProvider = NewMcpOAuthProvider(cfg.Name, cfg.URL, cfg.OAuth, m.authStore, func(authURL *url.URL) error {
			if m.onOAuthRedirect != nil {
				return m.onOAuthRedirect(cfg.Name, authURL)
			}
			return nil
		})
		m.mu.Lock()
		m.oauthProviders[cfg.Name] = oauthProvider
		m.mu.Unlock()
	}

	tr, err := newTransport(ctx, cfg, oauthProvider)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	cli := &client{name: cfg.Name, transport: tr, resourceCache: map[string]map[string]any{}, subscribed: map[string]map[string]struct{}{}, config: cfg}
	if err := cli.initialize(ctx); err != nil {
		_ = tr.Close()
		return sdk.ToolResult{Success: false}, err
	}
	m.restoreServerCache(cfg.Name, cli)
	tr.Listen(func(msg map[string]any) { m.handleNotification(cfg.Name, msg) })
	m.mu.Lock()
	m.clients[cfg.Name] = cli
	m.mu.Unlock()
	_, _ = m.refreshTools(ctx, cfg.Name)
	m.restoreSubscriptions(ctx, cli)
	_ = m.saveCache()
	return sdk.ToolResult{Success: true, Data: map[string]any{"name": cfg.Name, "status": "connected", "transport": cfg.Transport}}, nil
}

func (m *Manager) disconnect(name string) (sdk.ToolResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cli, ok := m.clients[name]
	if !ok {
		return sdk.ToolResult{Success: false}, fmt.Errorf("server not connected: %s", name)
	}
	_ = cli.transport.Close()
	delete(m.clients, name)
	for key, tool := range m.tools {
		if tool.serverName == name {
			delete(m.tools, key)
		}
	}
	_ = m.saveCache()
	return sdk.ToolResult{Success: true, Data: map[string]any{"name": name, "status": "disconnected"}}, nil
}

func (m *Manager) servers() sdk.ToolResult {
	m.mu.Lock()
	defer m.mu.Unlock()
	servers := make([]map[string]any, 0, len(m.clients))
	for name, cli := range m.clients {
		servers = append(servers, map[string]any{"name": name, "transport": cli.config.Transport, "command": cli.config.Command, "url": cli.config.URL})
	}
	sort.Slice(servers, func(i, j int) bool { return fmt.Sprint(servers[i]["name"]) < fmt.Sprint(servers[j]["name"]) })
	return sdk.ToolResult{Success: true, Data: map[string]any{"servers": servers, "count": len(servers)}}
}

func (m *Manager) listTools(ctx context.Context, name string) (sdk.ToolResult, error) {
	result, err := m.refreshTools(ctx, name)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{Success: true, Data: map[string]any{"server": name, "result": result}}, nil
}

func (m *Manager) callTool(ctx context.Context, name, toolName string, args map[string]any) (sdk.ToolResult, error) {
	cli, err := m.client(name)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	resp, err := cli.transport.Call(ctx, "tools/call", map[string]any{"name": toolName, "arguments": args})
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	return sdk.ToolResult{Success: true, Data: map[string]any{"server": name, "tool": toolName, "result": resp}}, nil
}

func (m *Manager) listResources(ctx context.Context, name string) (sdk.ToolResult, error) {
	cli, err := m.client(name)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	resp, err := cli.transport.Call(ctx, "resources/list", map[string]any{})
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	cli.mu.Lock()
	cli.resources = extractList(resp, "resources")
	cli.mu.Unlock()
	_ = m.saveCache()
	return sdk.ToolResult{Success: true, Data: map[string]any{"server": name, "result": resp}}, nil
}

func (m *Manager) readResource(ctx context.Context, name, uri string) (sdk.ToolResult, error) {
	cli, err := m.client(name)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	resp, err := cli.transport.Call(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	cli.mu.Lock()
	cli.resourceCache[uri] = resp
	cli.mu.Unlock()
	_ = m.saveCache()
	return sdk.ToolResult{Success: true, Data: map[string]any{"server": name, "uri": uri, "result": resp}}, nil
}

func (m *Manager) subscribe(ctx context.Context, name, uri, sessionID string) (sdk.ToolResult, error) {
	cli, err := m.client(name)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if _, err := cli.transport.Call(ctx, "resources/subscribe", map[string]any{"uri": uri}); err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	if strings.TrimSpace(sessionID) == "" {
		sessionID = "default"
	}
	cli.mu.Lock()
	if cli.subscribed[uri] == nil {
		cli.subscribed[uri] = map[string]struct{}{}
	}
	cli.subscribed[uri][sessionID] = struct{}{}
	cli.mu.Unlock()
	_ = m.saveCache()
	return sdk.ToolResult{Success: true, Data: map[string]any{"server": name, "uri": uri, "session_id": sessionID, "status": "subscribed"}}, nil
}

func (m *Manager) refreshTools(ctx context.Context, name string) (map[string]any, error) {
	cli, err := m.client(name)
	if err != nil {
		return nil, err
	}
	resp, err := cli.transport.Call(ctx, "tools/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	toolsRaw := extractList(resp, "tools")
	m.mu.Lock()
	for key, tool := range m.tools {
		if tool.serverName == name {
			delete(m.tools, key)
		}
	}
	for _, tool := range toolsRaw {
		toolName, _ := tool["name"].(string)
		if toolName == "" {
			continue
		}
		// Use configurable naming style
		proxyName := m.namer.FormatName(name, toolName)
		m.tools[proxyName] = &ProxyTool{manager: m, serverName: name, toolName: toolName, desc: fmt.Sprint(tool["description"]), schema: inputSchema(tool)}
	}
	m.mu.Unlock()
	if m.onChange != nil {
		_ = m.onChange()
	}
	return resp, nil
}

func (m *Manager) handleNotification(name string, msg map[string]any) {
	method, _ := msg["method"].(string)
	switch method {
	case "notifications/tools/list_changed":
		_, _ = m.refreshTools(context.Background(), name)
	case "notifications/resources/list_changed":
		_, _ = m.listResources(context.Background(), name)
	case "notifications/resources/updated":
		params, _ := msg["params"].(map[string]any)
		uri, _ := params["uri"].(string)
		if strings.TrimSpace(uri) != "" {
			_, _ = m.readResource(context.Background(), name, uri)
			if m.onResourceUpdate != nil {
				if cli, err := m.client(name); err == nil {
					cli.mu.Lock()
					payload := cli.resourceCache[uri]
					sessionIDs := nestedKeys(cli.subscribed[uri])
					cli.mu.Unlock()
					if payload != nil {
						for _, sessionID := range sessionIDs {
							payloadCopy := map[string]any{}
							for k, v := range payload {
								payloadCopy[k] = v
							}
							payloadCopy["session_id"] = sessionID
							m.onResourceUpdate(name, uri, payloadCopy)
						}
					}
				}
			}
		}
	}
}

func (m *Manager) proxyTools() []sdk.Tool {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.tools))
	for name := range m.tools {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]sdk.Tool, 0, len(names))
	for _, name := range names {
		out = append(out, m.tools[name])
	}
	return out
}

func (m *Manager) client(name string) (*client, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cli, ok := m.clients[name]
	if !ok {
		return nil, fmt.Errorf("server not connected: %s", name)
	}
	return cli, nil
}

func (m *Manager) restoreServerCache(name string, cli *client) {
	if strings.TrimSpace(m.cachePath) == "" {
		return
	}
	path := filepath.Join(filepath.Dir(m.cachePath), "mcp", name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var state struct {
		Resources     []map[string]any          `json:"resources"`
		ResourceCache map[string]map[string]any `json:"resource_cache"`
		Subscribed    map[string][]string       `json:"subscribed"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}
	cli.mu.Lock()
	cli.resources = state.Resources
	cli.resourceCache = state.ResourceCache
	cli.subscribed = makeNestedSet(state.Subscribed)
	cli.mu.Unlock()
}

func (m *Manager) restoreSubscriptions(ctx context.Context, cli *client) {
	cli.mu.Lock()
	uris := nestedMapKeys(cli.subscribed)
	cli.mu.Unlock()
	for _, uri := range uris {
		_, _ = cli.transport.Call(ctx, "resources/subscribe", map[string]any{"uri": uri})
	}
}

func inputSchema(tool map[string]any) map[string]any {
	if schema, ok := tool["inputSchema"].(map[string]any); ok {
		return schema
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func extractList(result map[string]any, key string) []map[string]any {
	raw, _ := result[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func (c *client) initialize(ctx context.Context) error {
	_, err := c.transport.Call(ctx, "initialize", map[string]any{
		"protocolVersion": "2025-11-25",
		"capabilities": map[string]any{
			"tools":     map[string]any{},
			"resources": map[string]any{"subscribe": true, "listChanged": true},
		},
		"clientInfo": map[string]any{"name": "morph", "version": "dev"},
	})
	if err != nil {
		return err
	}
	return c.transport.Notify(ctx, "notifications/initialized", map[string]any{})
}

func newTransport(ctx context.Context, cfg ServerConfig, oauthProvider *McpOAuthProvider) (transport, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Transport)) {
	case "", "stdio":
		return newStdioTransport(ctx, cfg.Command)
	case "http", "sse":
		tr := newHTTPTransport(cfg.URL, cfg.SSEURL, oauthProvider)
		header := strings.TrimSpace(cfg.AuthHeader)
		if header == "" {
			header = "Authorization"
		}
		if strings.TrimSpace(cfg.AuthToken) != "" {
			tr.headers[header] = cfg.AuthToken
		}
		return tr, nil
	default:
		return nil, fmt.Errorf("unsupported MCP transport: %s", cfg.Transport)
	}
}

func newStdioTransport(ctx context.Context, command string) (*stdioTransport, error) {
	if strings.TrimSpace(command) == "" {
		return nil, fmt.Errorf("command is required for stdio MCP")
	}
	cmd := exec.CommandContext(ctx, "sh", "-lc", command)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &stdioTransport{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}

func newHTTPTransport(url, sseURL string, oauthProvider *McpOAuthProvider) *httpTransport {
	return &httpTransport{
		url:           strings.TrimSpace(url),
		sseURL:        strings.TrimSpace(sseURL),
		client:        &http.Client{Timeout: 30 * time.Second},
		headers:       map[string]string{},
		oauthProvider: oauthProvider,
	}
}

func (t *stdioTransport) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seq++
	id := t.seq
	if err := t.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		msg, err := readRPC(t.out)
		if err != nil {
			return nil, err
		}
		if fmt.Sprint(msg["id"]) != fmt.Sprint(id) {
			continue
		}
		if errObj, ok := msg["error"]; ok && errObj != nil {
			return nil, fmt.Errorf("mcp error: %v", errObj)
		}
		result, _ := msg["result"].(map[string]any)
		return result, nil
	}
}

func (t *stdioTransport) Notify(ctx context.Context, method string, params map[string]any) error {
	return t.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (t *stdioTransport) Listen(fn func(map[string]any)) {
	go func() {
		for {
			msg, err := readRPC(t.out)
			if err != nil {
				return
			}
			if msg["id"] == nil && fn != nil {
				fn(msg)
			}
		}
	}()
}
func (t *stdioTransport) Close() error {
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Kill()
	}
	return nil
}
func (t *stdioTransport) write(msg map[string]any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(t.in, "Content-Length: %d\r\n\r\n%s", len(data), data)
	return err
}

func (t *httpTransport) Call(ctx context.Context, method string, params map[string]any) (map[string]any, error) {
	if t.oauthProvider != nil {
		if err := t.ensureValidToken(ctx); err != nil {
			return nil, err
		}
	}

	t.mu.Lock()
	t.seq++
	id := t.seq
	t.mu.Unlock()
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	if t.oauthProvider != nil {
		token, _ := t.oauthProvider.AccessToken(ctx)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var msg map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&msg); err != nil {
		return nil, err
	}
	if errObj, ok := msg["error"]; ok && errObj != nil {
		return nil, fmt.Errorf("mcp error: %v", errObj)
	}
	result, _ := msg["result"].(map[string]any)
	return result, nil
}

func (t *httpTransport) ensureValidToken(ctx context.Context) error {
	expired, err := t.oauthProvider.store.IsTokenExpired(t.oauthProvider.mcpName)
	if err != nil {
		return err
	}
	if !expired {
		return nil
	}

	tokens, err := t.oauthProvider.Tokens(ctx)
	if err != nil {
		return err
	}
	if tokens == nil || tokens.RefreshToken == "" {
		return fmt.Errorf("no refresh token available for MCP server: %s", t.oauthProvider.mcpName)
	}

	newTokens, err := t.oauthProvider.RefreshAccessToken(ctx, tokens.RefreshToken)
	if err != nil {
		return err
	}
	return t.oauthProvider.SaveTokens(ctx, newTokens)
}

func (t *httpTransport) Notify(ctx context.Context, method string, params map[string]any) error {
	_, err := t.Call(ctx, method, params)
	return err
}
func (t *httpTransport) Listen(fn func(map[string]any)) {
	if strings.TrimSpace(t.sseURL) == "" || fn == nil {
		return
	}
	go func() {
		for {
			req, err := http.NewRequest(http.MethodGet, t.sseURL, nil)
			if err != nil {
				return
			}
			for k, v := range t.headers {
				req.Header.Set(k, v)
			}
			resp, err := t.client.Do(req)
			if err != nil {
				time.Sleep(2 * time.Second)
				continue
			}
			scanner := bufio.NewScanner(resp.Body)
			var data strings.Builder
			for scanner.Scan() {
				line := scanner.Text()
				if strings.HasPrefix(line, "data:") {
					data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				}
				if line == "" && data.Len() > 0 {
					var msg map[string]any
					if err := json.Unmarshal([]byte(data.String()), &msg); err == nil {
						fn(msg)
					}
					data.Reset()
				}
			}
			resp.Body.Close()
			time.Sleep(2 * time.Second)
		}
	}()
}
func (t *httpTransport) Close() error { return nil }

func (m *Manager) saveCache() error {
	if strings.TrimSpace(m.cachePath) == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	baseDir := filepath.Join(filepath.Dir(m.cachePath), "mcp")
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return err
	}
	index := map[string]any{"servers": map[string]any{}}
	servers := index["servers"].(map[string]any)
	for name, cli := range m.clients {
		cli.mu.Lock()
		state := map[string]any{"resources": cli.resources, "resource_cache": cli.resourceCache, "subscribed": nestedSetToLists(cli.subscribed), "config": cli.config}
		cli.mu.Unlock()
		data, err := json.MarshalIndent(state, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(baseDir, name+".json"), data, 0o644); err != nil {
			return err
		}
		servers[name] = map[string]any{"path": filepath.Join("mcp", name+".json")}
	}
	if err := os.MkdirAll(filepath.Dir(m.cachePath), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.cachePath, data, 0o644)
}

func (m *Manager) loadCache() error {
	if strings.TrimSpace(m.cachePath) == "" {
		return nil
	}
	data, err := os.ReadFile(m.cachePath)
	if err != nil {
		return nil
	}
	var state struct {
		Servers map[string]map[string]any `json:"servers"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	return nil
}

func keys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func makeSet(items []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}

func nestedKeys(set map[string]struct{}) []string { return keys(set) }

func nestedMapKeys(set map[string]map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func makeNestedSet(items map[string][]string) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for uri, sessions := range items {
		out[uri] = makeSet(sessions)
	}
	return out
}

func nestedSetToLists(set map[string]map[string]struct{}) map[string][]string {
	out := map[string][]string{}
	for uri, sessions := range set {
		out[uri] = keys(sessions)
	}
	return out
}

func readRPC(r *bufio.Reader) (map[string]any, error) {
	var contentLength int
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			_, _ = fmt.Sscanf(line, "Content-Length: %d", &contentLength)
		}
	}
	body := make([]byte, contentLength)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}
