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
