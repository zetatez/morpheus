package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

type PluginManifest struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Description  string            `json:"description"`
	Author       string            `json:"author"`
	Homepage     string            `json:"homepage"`
	Tags         []string          `json:"tags"`
	Hooks        []string          `json:"hooks"`
	Entrypoint   string            `json:"entrypoint"`
	Checksum     string            `json:"checksum"`
	Dependencies map[string]string `json:"dependencies"`
}

type MarketplacePlugin struct {
	Manifest    PluginManifest
	Installed   bool
	InstallPath string
	UpdatedAt   time.Time
}

type Marketplace struct {
	mu        sync.RWMutex
	registry  *Registry
	logger    *zap.Logger
	installed map[string]*MarketplacePlugin
	remoteURL string
	cacheDir  string
}

func NewMarketplace(registry *Registry, logger *zap.Logger, cacheDir string) *Marketplace {
	return &Marketplace{
		registry:  registry,
		logger:    logger,
		installed: make(map[string]*MarketplacePlugin),
		cacheDir:  cacheDir,
	}
}

func (m *Marketplace) SetRemoteURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.remoteURL = strings.TrimSuffix(url, "/")
}

func (m *Marketplace) Install(ctx context.Context, manifest PluginManifest) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.installed[manifest.Name]; exists {
		return fmt.Errorf("plugin %s is already installed", manifest.Name)
	}

	installPath := filepath.Join(m.cacheDir, "plugins", manifest.Name, manifest.Version)
	if err := os.MkdirAll(installPath, 0o755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	plugin := &MarketplacePlugin{
		Manifest:    manifest,
		Installed:   true,
		InstallPath: installPath,
		UpdatedAt:   time.Now(),
	}
	m.installed[manifest.Name] = plugin

	m.logger.Info("plugin installed",
		zap.String("name", manifest.Name),
		zap.String("version", manifest.Version),
		zap.String("path", installPath))

	return nil
}

func (m *Marketplace) Uninstall(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	plugin, exists := m.installed[name]
	if !exists {
		return fmt.Errorf("plugin %s is not installed", name)
	}

	if err := os.RemoveAll(plugin.InstallPath); err != nil {
		m.logger.Warn("failed to remove plugin files", zap.String("path", plugin.InstallPath), zap.Error(err))
	}

	delete(m.installed, name)
	m.logger.Info("plugin uninstalled", zap.String("name", name))

	return nil
}

func (m *Marketplace) ListInstalled() []MarketplacePlugin {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]MarketplacePlugin, 0, len(m.installed))
	for _, p := range m.installed {
		result = append(result, *p)
	}
	return result
}

func (m *Marketplace) GetPlugin(name string) (*MarketplacePlugin, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.installed[name]
	if !ok {
		return nil, false
	}
	return p, true
}

func (m *Marketplace) Browse(ctx context.Context, query string, tags []string, limit int) ([]PluginManifest, error) {
	if m.remoteURL == "" {
		return nil, fmt.Errorf("marketplace remote URL is not configured")
	}

	url := fmt.Sprintf("%s/api/plugins?q=%s&tags=%s&limit=%d",
		m.remoteURL,
		query,
		strings.Join(tags, ","),
		limit)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch from marketplace: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("marketplace returned status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Plugins []PluginManifest `json:"plugins"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode marketplace response: %w", err)
	}

	return result.Plugins, nil
}

func (m *Marketplace) DownloadAndInstall(ctx context.Context, name, version string) error {
	if m.remoteURL == "" {
		return fmt.Errorf("marketplace remote URL is not configured")
	}

	url := fmt.Sprintf("%s/api/plugins/%s/%s/download", m.remoteURL, name, version)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download plugin: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to download plugin (status %d): %s", resp.StatusCode, string(body))
	}

	pluginPath := filepath.Join(m.cacheDir, "plugins", name, version)
	if err := os.MkdirAll(pluginPath, 0o755); err != nil {
		return fmt.Errorf("failed to create plugin directory: %w", err)
	}

	pluginFile := filepath.Join(pluginPath, "plugin.json")
	if err := os.WriteFile(pluginFile, []byte(fmt.Sprintf(`{"name": "%s", "version": "%s"}`, name, version)), 0o644); err != nil {
		return fmt.Errorf("failed to write plugin metadata: %w", err)
	}

	manifest := PluginManifest{
		Name:    name,
		Version: version,
	}

	return m.Install(ctx, manifest)
}

func CalculateChecksum(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

type PluginLoader struct {
	mu      sync.RWMutex
	loaders map[string]func([]byte) error
}

func NewPluginLoader() *PluginLoader {
	return &PluginLoader{
		loaders: make(map[string]func([]byte) error),
	}
}

func (pl *PluginLoader) RegisterLoader(pluginType string, loader func([]byte) error) {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	pl.loaders[pluginType] = loader
}

func (pl *PluginLoader) Load(data []byte, pluginType string) error {
	pl.mu.RLock()
	defer pl.mu.RUnlock()

	loader, ok := pl.loaders[pluginType]
	if !ok {
		return fmt.Errorf("no loader registered for plugin type: %s", pluginType)
	}

	return loader(data)
}
