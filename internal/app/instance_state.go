package app

import (
	"context"
	"sync"
	"time"
)

type InstanceState struct {
	mu           sync.RWMutex
	projectRoot  string
	createdAt    time.Time
	lastActiveAt time.Time
	data         map[string]any
	metrics      *InstanceMetrics
	sessionRefs  map[string]int
}

type InstanceMetrics struct {
	mu               sync.RWMutex
	TotalSessions    int
	TotalTokens      int64
	TotalCost        float64
	ToolExecutions   map[string]int
	PermissionGrants map[string]time.Time
	LastError        string
	LastErrorAt      time.Time
}

type InstanceManager struct {
	mu              sync.RWMutex
	instances       map[string]*InstanceState
	cleanupInterval time.Duration
	maxIdleTime     time.Duration
}

var globalInstanceManager *InstanceManager

func init() {
	globalInstanceManager = NewInstanceManager()
}

func GetInstanceManager() *InstanceManager {
	return globalInstanceManager
}

func NewInstanceManager() *InstanceManager {
	return &InstanceManager{
		instances:       make(map[string]*InstanceState),
		cleanupInterval: 5 * time.Minute,
		maxIdleTime:     30 * time.Minute,
	}
}

func (m *InstanceManager) GetOrCreate(projectRoot string) *InstanceState {
	m.mu.Lock()
	defer m.mu.Unlock()

	if instance, ok := m.instances[projectRoot]; ok {
		instance.mu.Lock()
		instance.lastActiveAt = time.Now()
		instance.sessionRefs[time.Now().String()]++
		instance.mu.Unlock()
		return instance
	}

	instance := &InstanceState{
		projectRoot:  projectRoot,
		createdAt:    time.Now(),
		lastActiveAt: time.Now(),
		data:         make(map[string]any),
		metrics:      &InstanceMetrics{},
		sessionRefs:  make(map[string]int),
	}
	instance.metrics.ToolExecutions = make(map[string]int)
	instance.metrics.PermissionGrants = make(map[string]time.Time)
	m.instances[projectRoot] = instance
	return instance
}

func (m *InstanceManager) Get(projectRoot string) (*InstanceState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instance, ok := m.instances[projectRoot]
	if ok {
		instance.mu.Lock()
		instance.lastActiveAt = time.Now()
		instance.mu.Unlock()
	}
	return instance, ok
}

func (m *InstanceManager) Delete(projectRoot string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.instances, projectRoot)
}

func (m *InstanceManager) List() []*InstanceState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	instances := make([]*InstanceState, 0, len(m.instances))
	for _, instance := range m.instances {
		instances = append(instances, instance)
	}
	return instances
}

func (m *InstanceManager) Cleanup(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for projectRoot, instance := range m.instances {
		instance.mu.RLock()
		idle := now.Sub(instance.lastActiveAt)
		sessionCount := len(instance.sessionRefs)
		instance.mu.RUnlock()

		if idle > m.maxIdleTime && sessionCount == 0 {
			delete(m.instances, projectRoot)
		}
	}
}

func (m *InstanceManager) StartCleanupWorker(ctx context.Context) {
	ticker := time.NewTicker(m.cleanupInterval)
	go func() {
		for {
			select {
			case <-ctx.Done():
				ticker.Stop()
				return
			case <-ticker.C:
				m.Cleanup(ctx)
			}
		}
	}()
}

func (s *InstanceState) ProjectRoot() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.projectRoot
}

func (s *InstanceState) CreatedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.createdAt
}

func (s *InstanceState) LastActiveAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastActiveAt
}

func (s *InstanceState) Get(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	return val, ok
}

func (s *InstanceState) Set(key string, value any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

func (s *InstanceState) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, key)
}

func (s *InstanceState) GetString(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	if !ok {
		return "", false
	}
	str, ok := val.(string)
	return str, ok
}

func (s *InstanceState) SetString(key string, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

func (s *InstanceState) GetInt(key string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.data[key]
	if !ok {
		return 0, false
	}
	i, ok := val.(int)
	return i, ok
}

func (s *InstanceState) SetInt(key string, value int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
}

func (s *InstanceState) Metrics() *InstanceMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.metrics
}

func (m *InstanceMetrics) RecordToolExecution(tool string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ToolExecutions[tool]++
}

func (m *InstanceMetrics) RecordTokens(count int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalTokens += count
}

func (m *InstanceMetrics) RecordCost(cost float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalCost += cost
}

func (m *InstanceMetrics) RecordPermissionGrant(permission string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PermissionGrants[permission] = time.Now()
}

func (m *InstanceMetrics) RecordError(err string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastError = err
	m.LastErrorAt = time.Now()
}

func (m *InstanceMetrics) IncrementSessions() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalSessions++
}

type ScopedCacheEntry struct {
	Value      any
	CreatedAt  time.Time
	ExpiresAt  time.Time
	InstanceID string
}

type ScopedCache struct {
	mu       sync.RWMutex
	items    map[string]*ScopedCacheEntry
	maxSize  int
	eviction func(key string, value any)
}

func NewScopedCache(maxSize int) *ScopedCache {
	return &ScopedCache{
		items:   make(map[string]*ScopedCacheEntry),
		maxSize: maxSize,
	}
}

func (c *ScopedCache) Set(key string, value any, instanceID string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.items) >= c.maxSize && c.maxSize > 0 {
		c.evictOldest()
	}

	now := time.Now()
	c.items[key] = &ScopedCacheEntry{
		Value:      value,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
		InstanceID: instanceID,
	}
}

func (c *ScopedCache) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, ok := c.items[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(entry.ExpiresAt) {
		return nil, false
	}

	return entry.Value, true
}

func (c *ScopedCache) GetByInstance(instanceID string) map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]any)
	now := time.Now()

	for key, entry := range c.items {
		if entry.InstanceID == instanceID && now.Before(entry.ExpiresAt) {
			result[key] = entry.Value
		}
	}

	return result
}

func (c *ScopedCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *ScopedCache) InvalidateInstance(instanceID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.eviction != nil {
		for key, entry := range c.items {
			if entry.InstanceID == instanceID {
				c.eviction(key, entry.Value)
				delete(c.items, key)
			}
		}
	} else {
		for key, entry := range c.items {
			if entry.InstanceID == instanceID {
				delete(c.items, key)
			}
		}
	}
}

func (c *ScopedCache) Invalidate(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *ScopedCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*ScopedCacheEntry)
}

func (c *ScopedCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *ScopedCache) WithEviction(fn func(key string, value any)) *ScopedCache {
	c.eviction = fn
	return c
}

func (c *ScopedCache) evictOldest() {
	var oldestKey string
	var oldestTime time.Time

	for key, entry := range c.items {
		if oldestTime.IsZero() || entry.CreatedAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.CreatedAt
		}
	}

	if oldestKey != "" {
		if c.eviction != nil {
			c.eviction(oldestKey, c.items[oldestKey].Value)
		}
		delete(c.items, oldestKey)
	}
}

func (c *ScopedCache) CleanupExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, entry := range c.items {
		if now.After(entry.ExpiresAt) {
			if c.eviction != nil {
				c.eviction(key, entry.Value)
			}
			delete(c.items, key)
		}
	}
}

type InstanceScopedCache struct {
	mu      sync.RWMutex
	caches  map[string]*ScopedCache
	onEvict func(instanceID, key string, value any)
}

func NewInstanceScopedCache() *InstanceScopedCache {
	return &InstanceScopedCache{
		caches: make(map[string]*ScopedCache),
	}
}

func (i *InstanceScopedCache) GetOrCreateCache(instanceID string, maxSize int) *ScopedCache {
	i.mu.Lock()
	defer i.mu.Unlock()

	if cache, ok := i.caches[instanceID]; ok {
		return cache
	}

	cache := NewScopedCache(maxSize)
	if i.onEvict != nil {
		cache = cache.WithEviction(func(key string, value any) {
			i.onEvict(instanceID, key, value)
		})
	}
	i.caches[instanceID] = cache
	return cache
}

func (i *InstanceScopedCache) InvalidateInstance(instanceID string) {
	i.mu.Lock()
	defer i.mu.Unlock()

	if cache, ok := i.caches[instanceID]; ok {
		cache.Clear()
	}
	delete(i.caches, instanceID)
}

func (i *InstanceScopedCache) WithEviction(fn func(instanceID, key string, value any)) *InstanceScopedCache {
	i.onEvict = fn
	return i
}

func (i *InstanceScopedCache) GetCache(instanceID string) (*ScopedCache, bool) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	cache, ok := i.caches[instanceID]
	return cache, ok
}

func (i *InstanceScopedCache) ListInstances() []string {
	i.mu.RLock()
	defer i.mu.RUnlock()
	instances := make([]string, 0, len(i.caches))
	for id := range i.caches {
		instances = append(instances, id)
	}
	return instances
}
