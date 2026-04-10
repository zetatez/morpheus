package sync

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var (
	ErrLockNotAcquired = errors.New("lock not acquired")
	ErrLockExpired     = errors.New("lock expired or stale")
	ErrLockCompromised = errors.New("lock metadata compromised")
)

type FlockOptions struct {
	Dir         string
	Signal      context.Context
	StaleMs     int64
	TimeoutMs   int64
	BaseDelayMs int64
	MaxDelayMs  int64
	OnWait      func(WaitEvent)
}

type WaitEvent struct {
	Key     string
	Attempt int
	Delay   int64
	Error   error
}

type LockMeta struct {
	Token     string `json:"token"`
	PID       int    `json:"pid"`
	Hostname  string `json:"hostname"`
	CreatedAt int64  `json:"created_at"`
}

type Lease interface {
	Release() error
	Valid() bool
}

type OwnedLock struct {
	key      string
	token    string
	dir      string
	mu       sync.RWMutex
	released bool
}

type Flock struct {
	opts FlockOptions
}

func NewFlock(opts FlockOptions) *Flock {
	if opts.Dir == "" {
		opts.Dir = os.TempDir()
	}
	if opts.StaleMs == 0 {
		opts.StaleMs = 60000
	}
	if opts.TimeoutMs == 0 {
		opts.TimeoutMs = 300000
	}
	if opts.BaseDelayMs == 0 {
		opts.BaseDelayMs = 100
	}
	if opts.MaxDelayMs == 0 {
		opts.MaxDelayMs = 2000
	}
	return &Flock{opts: opts}
}

func (f *Flock) lockPath(key string) string {
	safeKey := strings.ReplaceAll(key, "/", "-")
	safeKey = strings.ReplaceAll(safeKey, ":", "-")
	return filepath.Join(f.opts.Dir, safeKey+".lock")
}

func (f *Flock) metaPath(lockPath string) string {
	return filepath.Join(lockPath, "meta.json")
}

func (f *Flock) heartbeatPath(lockPath string) string {
	return filepath.Join(lockPath, "heartbeat")
}

func (f *Flock) breakerPath(lockPath string) string {
	return lockPath + ".breaker"
}

func generateToken() (string, error) {
	b := make([]byte, 16)
	_, err := cryptorand.Read(b)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

func (f *Flock) withRetry(ctx context.Context, key string, fn func() error) error {
	attempt := 0
	baseDelay := f.opts.BaseDelayMs
	maxDelay := f.opts.MaxDelayMs
	timeout := time.After(time.Duration(f.opts.TimeoutMs) * time.Millisecond)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.opts.Signal.Done():
			return f.opts.Signal.Err()
		case <-timeout:
			return ErrLockNotAcquired
		default:
		}

		attempt++
		err := fn()
		if err == nil {
			return nil
		}

		if !errors.Is(err, ErrLockNotAcquired) {
			return err
		}

		delay := baseDelay * int64(math.Pow(1.5, float64(attempt-1)))
		if delay > maxDelay {
			delay = maxDelay
		}
		jitter := int64(rand.Float64() * float64(delay/2))
		delay = delay/2 + jitter

		if f.opts.OnWait != nil {
			f.opts.OnWait(WaitEvent{Key: key, Attempt: attempt, Delay: delay, Error: err})
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-f.opts.Signal.Done():
			return f.opts.Signal.Err()
		case <-time.After(time.Duration(delay) * time.Millisecond):
		}
	}
}

func (f *Flock) tryAcquire(key string) (string, error) {
	lockPath := f.lockPath(key)
	metaPath := f.metaPath(lockPath)
	heartbeatPath := f.heartbeatPath(lockPath)

	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		if os.IsExist(err) {
			if stale, err := f.isStale(lockPath); err == nil && stale {
				if err := f.breakLock(key, lockPath); err != nil {
					return "", ErrLockNotAcquired
				}
				if err := os.MkdirAll(lockPath, 0o700); err != nil {
					return "", ErrLockNotAcquired
				}
			} else {
				return "", ErrLockNotAcquired
			}
		} else {
			return "", err
		}
	}

	token, err := generateToken()
	if err != nil {
		os.RemoveAll(lockPath)
		return "", err
	}

	hostname, _ := os.Hostname()
	meta := LockMeta{
		Token:     token,
		PID:       os.Getpid(),
		Hostname:  hostname,
		CreatedAt: time.Now().UnixMilli(),
	}

	metaData, err := json.Marshal(meta)
	if err != nil {
		os.RemoveAll(lockPath)
		return "", err
	}

	if err := os.WriteFile(metaPath, metaData, 0o600); err != nil {
		os.RemoveAll(lockPath)
		return "", err
	}

	if err := os.WriteFile(heartbeatPath, []byte{}, 0o600); err != nil {
		os.RemoveAll(lockPath)
		return "", err
	}

	if err := os.Chtimes(lockPath, time.Now(), time.Now()); err != nil {
		os.RemoveAll(lockPath)
		return "", err
	}

	verifyMeta, err := os.ReadFile(metaPath)
	if err != nil || string(verifyMeta) != string(metaData) {
		os.RemoveAll(lockPath)
		return "", ErrLockCompromised
	}

	return token, nil
}

func (f *Flock) isStale(lockPath string) (bool, error) {
	stat, err := os.Stat(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	cutoff := time.Now().Add(-time.Duration(f.opts.StaleMs) * time.Millisecond)
	if stat.ModTime().Before(cutoff) {
		return true, nil
	}

	metaPath := f.metaPath(lockPath)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		return true, nil
	}

	var meta LockMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return true, nil
	}

	heartbeatPath := f.heartbeatPath(lockPath)
	heartbeatStat, err := os.Stat(heartbeatPath)
	if err != nil {
		return true, nil
	}

	heartbeatCutoff := time.Now().Add(-time.Duration(f.opts.StaleMs*2) * time.Millisecond)
	if heartbeatStat.ModTime().Before(heartbeatCutoff) {
		return true, nil
	}

	return false, nil
}

func (f *Flock) breakLock(key string, lockPath string) error {
	breakerPath := f.breakerPath(lockPath)
	if err := os.MkdirAll(breakerPath, 0o700); err != nil {
		return err
	}
	defer os.RemoveAll(breakerPath)

	metaPath := f.metaPath(lockPath)
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		os.RemoveAll(lockPath)
		return nil
	}

	var meta LockMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		os.RemoveAll(lockPath)
		return nil
	}

	if meta.PID > 0 {
		process, err := os.FindProcess(meta.PID)
		if err == nil && process.Signal(os.Signal(nil)) == nil {
		}
	}

	os.RemoveAll(lockPath)
	return nil
}

func (f *Flock) Acquire(ctx context.Context, key string) (*OwnedLock, error) {
	var token string
	err := f.withRetry(ctx, key, func() error {
		var err error
		token, err = f.tryAcquire(key)
		return err
	})
	if err != nil {
		return nil, err
	}

	return &OwnedLock{
		key:   key,
		token: token,
		dir:   f.lockPath(key),
	}, nil
}

func (f *Flock) WithLock(ctx context.Context, key string, fn func() error) error {
	lock, err := f.Acquire(ctx, key)
	if err != nil {
		return err
	}
	defer lock.Release()
	return fn()
}

func (f *Flock) StartHeartbeat(lock *OwnedLock, intervalMs int64) {
	if intervalMs == 0 {
		intervalMs = f.opts.StaleMs / 3
	}
	ticker := time.NewTicker(time.Duration(intervalMs) * time.Millisecond)
	go func() {
		for {
			select {
			case <-ticker.C:
				lock.mu.RLock()
				if lock.released {
					lock.mu.RUnlock()
					ticker.Stop()
					return
				}
				heartbeatPath := f.heartbeatPath(lock.dir)
				os.Chtimes(heartbeatPath, time.Now(), time.Now())
				lock.mu.RUnlock()
			}
		}
	}()
}

func (l *OwnedLock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.released {
		return nil
	}
	l.released = true

	lockPath := l.dir
	metaPath := filepath.Join(lockPath, "meta.json")

	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var meta LockMeta
	if err := json.Unmarshal(metaData, &meta); err != nil {
		return ErrLockCompromised
	}

	if meta.Token != l.token {
		return ErrLockCompromised
	}

	os.RemoveAll(lockPath)
	return nil
}

func (l *OwnedLock) Valid() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return !l.released
}

type SimpleLease struct {
	mu        sync.RWMutex
	released  bool
	onRelease func() error
}

func NewSimpleLease(onRelease func() error) *SimpleLease {
	return &SimpleLease{onRelease: onRelease}
}

func (l *SimpleLease) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.released {
		return nil
	}
	l.released = true
	if l.onRelease != nil {
		return l.onRelease()
	}
	return nil
}

func (l *SimpleLease) Valid() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return !l.released
}
