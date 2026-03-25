package config

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

type HotReloader interface {
	Reload(cfg Config) error
}

type ConfigWatcher struct {
	viper       *viper.Viper
	path        string
	reloader    HotReloader
	mu          sync.RWMutex
	stopCh      chan struct{}
	lastModTime time.Time
}

func NewConfigWatcher(path string, reloader HotReloader) (*ConfigWatcher, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	v := viper.New()
	v.SetConfigFile(absPath)
	v.SetConfigType("yaml")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	w := &ConfigWatcher{
		viper:    v,
		path:     absPath,
		reloader: reloader,
		stopCh:   make(chan struct{}),
	}

	if info, err := os.Stat(absPath); err == nil {
		w.lastModTime = info.ModTime()
	}

	return w, nil
}

func (w *ConfigWatcher) Start(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	dir := filepath.Dir(w.path)
	if err := watcher.Add(dir); err != nil {
		watcher.Close()
		return err
	}

	go w.run(ctx, watcher)
	return nil
}

func (w *ConfigWatcher) run(ctx context.Context, watcher *fsnotify.Watcher) {
	defer watcher.Close()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Name == w.path && (event.Op&fsnotify.Write == fsnotify.Write || event.Op&fsnotify.Create == fsnotify.Create) {
				w.checkAndReload()
			}
		case _, ok := <-watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *ConfigWatcher) checkAndReload() {
	info, err := os.Stat(w.path)
	if err != nil {
		return
	}

	w.mu.RLock()
	shouldReload := info.ModTime().After(w.lastModTime)
	w.mu.RUnlock()

	if !shouldReload {
		return
	}

	w.mu.Lock()
	w.lastModTime = info.ModTime()
	w.mu.Unlock()

	if err := w.viper.ReadInConfig(); err != nil {
		return
	}

	var cfg Config
	if err := w.viper.Unmarshal(&cfg); err != nil {
		return
	}

	if err := cfg.Validate(); err != nil {
		return
	}

	if w.reloader != nil {
		_ = w.reloader.Reload(cfg)
	}
}

func (w *ConfigWatcher) Stop() {
	close(w.stopCh)
}

func (w *ConfigWatcher) GetConfig() (Config, error) {
	var cfg Config
	if err := w.viper.Unmarshal(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func WatchConfig(ctx context.Context, path string, reloader HotReloader, pollInterval time.Duration) error {
	watcher, err := NewConfigWatcher(path, reloader)
	if err != nil {
		return err
	}

	if err := watcher.Start(ctx); err != nil {
		return err
	}

	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			watcher.Stop()
			return nil
		case <-ticker.C:
			watcher.checkAndReload()
		}
	}
}

type ConfigLayer struct {
	Priority   int
	Source     string
	Config     Config
	Watcher    *ConfigWatcher
	Valid      bool
	LastReload time.Time
}

type LayeredConfigManager struct {
	mu        sync.RWMutex
	layers    []*ConfigLayer
	listeners []func(Config)
}

func NewLayeredConfigManager() *LayeredConfigManager {
	return &LayeredConfigManager{
		layers: make([]*ConfigLayer, 0),
	}
}

func (m *LayeredConfigManager) AddLayer(layer *ConfigLayer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.layers = append(m.layers, layer)
	m.sortLayers()
}

func (m *LayeredConfigManager) sortLayers() {
	for i := 0; i < len(m.layers)-1; i++ {
		for j := i + 1; j < len(m.layers); j++ {
			if m.layers[i].Priority > m.layers[j].Priority {
				m.layers[i], m.layers[j] = m.layers[j], m.layers[i]
			}
		}
	}
}

func (m *LayeredConfigManager) GetMerged() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.layers) == 0 {
		return Config{}
	}

	merged := m.layers[0].Config
	for i := 1; i < len(m.layers); i++ {
		merged = m.mergeConfigs(merged, m.layers[i].Config)
	}

	return merged
}

func (m *LayeredConfigManager) mergeConfigs(base, override Config) Config {
	if override.WorkspaceRoot != "" {
		base.WorkspaceRoot = override.WorkspaceRoot
	}
	if override.Morpheus.ContextFile != "" {
		base.Morpheus.ContextFile = override.Morpheus.ContextFile
	}
	if override.Logging.Level != "" {
		base.Logging.Level = override.Logging.Level
	}
	if override.Logging.File != "" {
		base.Logging.File = override.Logging.File
	}
	if override.Planner.Provider != "" {
		base.Planner.Provider = override.Planner.Provider
	}
	if override.Planner.Model != "" {
		base.Planner.Model = override.Planner.Model
	}
	if override.Planner.APIKey != "" {
		base.Planner.APIKey = override.Planner.APIKey
	}
	if override.Planner.Endpoint != "" {
		base.Planner.Endpoint = override.Planner.Endpoint
	}
	if override.Server.Listen != "" {
		base.Server.Listen = override.Server.Listen
	}

	return base
}

func (m *LayeredConfigManager) ReloadAll() error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, layer := range m.layers {
		if layer.Watcher != nil {
			cfg, err := layer.Watcher.GetConfig()
			if err != nil {
				continue
			}
			layer.Config = cfg
			layer.LastReload = time.Now()
		}
	}

	return nil
}

func (m *LayeredConfigManager) Watch(ctx context.Context) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, layer := range m.layers {
		if layer.Watcher != nil {
			if err := layer.Watcher.Start(ctx); err != nil {
				return err
			}
		}
	}

	return nil
}

func (m *LayeredConfigManager) Stop() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, layer := range m.layers {
		if layer.Watcher != nil {
			layer.Watcher.Stop()
		}
	}
}

func (m *LayeredConfigManager) Subscribe(listener func(Config)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.listeners = append(m.listeners, listener)
}

func (m *LayeredConfigManager) Notify() {
	m.mu.RLock()
	defer m.mu.RUnlock()

	merged := m.GetMerged()
	for _, listener := range m.listeners {
		listener(merged)
	}
}

func (m *LayeredConfigManager) GetLayer(source string) *ConfigLayer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, layer := range m.layers {
		if layer.Source == source {
			return layer
		}
	}
	return nil
}

func (m *LayeredConfigManager) RemoveLayer(source string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for i, layer := range m.layers {
		if layer.Source == source {
			if layer.Watcher != nil {
				layer.Watcher.Stop()
			}
			m.layers = append(m.layers[:i], m.layers[i+1:]...)
			return
		}
	}
}

func (m *LayeredConfigManager) ListLayers() []ConfigLayer {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]ConfigLayer, len(m.layers))
	for i, layer := range m.layers {
		result[i] = *layer
	}
	return result
}

func (m *LayeredConfigManager) UpdateLayer(source string, cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, layer := range m.layers {
		if layer.Source == source {
			layer.Config = cfg
			layer.LastReload = time.Now()
			return
		}
	}
}

type ConfigSource int

const (
	ConfigSourceSystem ConfigSource = iota
	ConfigSourceGlobal
	ConfigSourceProject
	ConfigSourceEnvironment
	ConfigSourceCommandLine
)

func (s ConfigSource) Priority() int {
	return int(s)
}

func (s ConfigSource) String() string {
	switch s {
	case ConfigSourceSystem:
		return "system"
	case ConfigSourceGlobal:
		return "global"
	case ConfigSourceProject:
		return "project"
	case ConfigSourceEnvironment:
		return "environment"
	case ConfigSourceCommandLine:
		return "command-line"
	default:
		return "unknown"
	}
}

func GetSystemConfigPath() string {
	return "/etc/morpheus/config.yaml"
}

func GetGlobalConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "morpheus", "config.yaml")
}

func GetProjectConfigPath() string {
	return "./morpheus.yaml"
}

func LoadLayeredConfig(ctx context.Context) (*LayeredConfigManager, error) {
	manager := NewLayeredConfigManager()

	systemPath := GetSystemConfigPath()
	if _, err := os.Stat(systemPath); err == nil {
		watcher, err := NewConfigWatcher(systemPath, nil)
		if err == nil {
			cfg, _ := watcher.GetConfig()
			manager.AddLayer(&ConfigLayer{
				Priority: ConfigSourceSystem.Priority(),
				Source:   systemPath,
				Config:   cfg,
				Watcher:  watcher,
				Valid:    true,
			})
		}
	}

	globalPath := GetGlobalConfigPath()
	if _, err := os.Stat(globalPath); err == nil {
		watcher, err := NewConfigWatcher(globalPath, nil)
		if err == nil {
			cfg, _ := watcher.GetConfig()
			manager.AddLayer(&ConfigLayer{
				Priority: ConfigSourceGlobal.Priority(),
				Source:   globalPath,
				Config:   cfg,
				Watcher:  watcher,
				Valid:    true,
			})
		}
	}

	projectPath := GetProjectConfigPath()
	if _, err := os.Stat(projectPath); err == nil {
		watcher, err := NewConfigWatcher(projectPath, nil)
		if err == nil {
			cfg, _ := watcher.GetConfig()
			manager.AddLayer(&ConfigLayer{
				Priority: ConfigSourceProject.Priority(),
				Source:   projectPath,
				Config:   cfg,
				Watcher:  watcher,
				Valid:    true,
			})
		}
	}

	go func() {
		<-ctx.Done()
		manager.Stop()
	}()

	return manager, nil
}
