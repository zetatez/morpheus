package app

import (
	"sync"
	"time"
)

type PermissionGrant struct {
	Permission string
	Pattern    string
	GrantedAt  time.Time
	ExpiresAt  time.Time
	SessionID  string
	AgentName  string
}

type PermissionTracker struct {
	mu     sync.RWMutex
	grants map[string]PermissionGrant
}

func NewPermissionTracker() *PermissionTracker {
	return &PermissionTracker{
		grants: make(map[string]PermissionGrant),
	}
}

func (t *PermissionTracker) Grant(permission, pattern, sessionID, agentName string, durationSeconds int) PermissionGrant {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.grantKey(permission, pattern, sessionID)
	now := time.Now()
	grant := PermissionGrant{
		Permission: permission,
		Pattern:    pattern,
		GrantedAt:  now,
		ExpiresAt:  now.Add(time.Duration(durationSeconds) * time.Second),
		SessionID:  sessionID,
		AgentName:  agentName,
	}
	t.grants[key] = grant
	return grant
}

func (t *PermissionTracker) Check(permission, pattern, sessionID string) (bool, PermissionGrant) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	key := t.grantKey(permission, pattern, sessionID)
	grant, ok := t.grants[key]
	if !ok {
		return false, PermissionGrant{}
	}

	if time.Now().After(grant.ExpiresAt) {
		return false, PermissionGrant{}
	}

	return true, grant
}

func (t *PermissionTracker) Revoke(permission, pattern, sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	key := t.grantKey(permission, pattern, sessionID)
	delete(t.grants, key)
}

func (t *PermissionTracker) RevokeAll(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	for key, grant := range t.grants {
		if grant.SessionID == sessionID {
			delete(t.grants, key)
		}
	}
}

func (t *PermissionTracker) CleanupExpired() {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	for key, grant := range t.grants {
		if now.After(grant.ExpiresAt) {
			delete(t.grants, key)
		}
	}
}

func (t *PermissionTracker) grantKey(permission, pattern, sessionID string) string {
	return permission + "|" + pattern + "|" + sessionID
}

type RateLimitEntry struct {
	Count       int
	WindowStart time.Time
}

type PermissionRateLimiter struct {
	mu       sync.RWMutex
	requests map[string]*RateLimitEntry
}

func NewPermissionRateLimiter() *PermissionRateLimiter {
	return &PermissionRateLimiter{
		requests: make(map[string]*RateLimitEntry),
	}
}

func (r *PermissionRateLimiter) CheckAndIncrement(permission, pattern string, maxRequests int, windowSeconds int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := permission + "|" + pattern
	entry, ok := r.requests[key]

	now := time.Now()
	if !ok || now.Sub(entry.WindowStart) > time.Duration(windowSeconds)*time.Second {
		r.requests[key] = &RateLimitEntry{
			Count:       1,
			WindowStart: now,
		}
		return true
	}

	if entry.Count >= maxRequests {
		return false
	}

	entry.Count++
	return true
}

func (r *PermissionRateLimiter) Cleanup() {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for key, entry := range r.requests {
		if now.Sub(entry.WindowStart) > time.Hour {
			delete(r.requests, key)
		}
	}
}
