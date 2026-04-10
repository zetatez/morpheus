package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

var ErrNotFound = errors.New("resource not found")

type Error struct {
	Err error
}

func (e *Error) Error() string { return e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

type MigrationFunc func(dir string, fs FileSystem) error

type FileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	Remove(path string) error
	IsNotExist(err error) bool
	Glob(pattern string) ([]string, error)
}

type osFileSystem struct{}

func (fs *osFileSystem) ReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
func (fs *osFileSystem) WriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, perm)
}
func (fs *osFileSystem) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (fs *osFileSystem) Remove(path string) error                     { return os.Remove(path) }
func (fs *osFileSystem) IsNotExist(err error) bool                    { return os.IsNotExist(err) }
func (fs *osFileSystem) Glob(pattern string) ([]string, error)        { return filepath.Glob(pattern) }

var DefaultFileSystem FileSystem = &osFileSystem{}

type ReentrantLock struct {
	mu       sync.Mutex
	cond     *sync.Cond
	holders  int
	maxDepth int
}

func NewReentrantLock() *ReentrantLock {
	return &ReentrantLock{maxDepth: 1}
}

func (l *ReentrantLock) Lock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	for l.holders >= l.maxDepth {
		l.cond.Wait()
	}
	l.holders++
}

func (l *ReentrantLock) Unlock() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holders > 0 {
		l.holders--
		l.cond.Signal()
	}
}

func (l *ReentrantLock) RLock()   { l.Lock() }
func (l *ReentrantLock) RUnlock() { l.Unlock() }

type ReadLockFunc func() error
type WriteLockFunc func() error

func (l *ReentrantLock) WithReadLock(fn ReadLockFunc) error {
	l.RLock()
	defer l.RUnlock()
	return fn()
}

func (l *ReentrantLock) WithWriteLock(fn WriteLockFunc) error {
	l.Lock()
	defer l.Unlock()
	return fn()
}

type Storage struct {
	mu    sync.RWMutex
	dir   string
	locks map[string]*ReentrantLock
	fs    FileSystem
}

func New(dir string, fs FileSystem) (*Storage, error) {
	if fs == nil {
		fs = DefaultFileSystem
	}
	s := &Storage{
		dir:   dir,
		locks: make(map[string]*ReentrantLock),
		fs:    fs,
	}
	if err := s.runMigrations(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Storage) file(key ...string) string {
	return filepath.Join(s.dir, filepath.Join(key...)) + ".json"
}

func (s *Storage) getLock(key string) *ReentrantLock {
	s.mu.Lock()
	defer s.mu.Unlock()
	lock, ok := s.locks[key]
	if !ok {
		lock = NewReentrantLock()
		s.locks[key] = lock
	}
	return lock
}

func (s *Storage) Remove(key []string) error {
	path := s.file(key...)
	lock := s.getLock(path)
	return lock.WithWriteLock(func() error {
		err := s.fs.Remove(path)
		if err != nil && !s.fs.IsNotExist(err) {
			return &Error{Err: err}
		}
		return nil
	})
}

func (s *Storage) Read(v any, key ...string) error {
	path := s.file(key...)
	lock := s.getLock(path)
	return lock.WithReadLock(func() error {
		data, err := s.fs.ReadFile(path)
		if err != nil {
			if s.fs.IsNotExist(err) {
				return ErrNotFound
			}
			return &Error{Err: err}
		}
		if err := json.Unmarshal(data, v); err != nil {
			return &Error{Err: err}
		}
		return nil
	})
}

func (s *Storage) Update(v any, key []string, fn func(any) error) error {
	path := s.file(key...)
	lock := s.getLock(path)
	return lock.WithWriteLock(func() error {
		data, err := s.fs.ReadFile(path)
		if err != nil && !s.fs.IsNotExist(err) {
			return &Error{Err: err}
		}
		if err == nil {
			if err := json.Unmarshal(data, v); err != nil {
				return &Error{Err: err}
			}
		}
		if err := fn(v); err != nil {
			return err
		}
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return &Error{Err: err}
		}
		if err := s.fs.WriteFile(path, out, 0o644); err != nil {
			return &Error{Err: err}
		}
		return nil
	})
}

func (s *Storage) Write(v any, key ...string) error {
	path := s.file(key...)
	lock := s.getLock(path)
	return lock.WithWriteLock(func() error {
		out, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return &Error{Err: err}
		}
		if err := s.fs.WriteFile(path, out, 0o644); err != nil {
			return &Error{Err: err}
		}
		return nil
	})
}

func (s *Storage) List(prefix ...string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dir := s.dir
	if len(prefix) > 0 {
		dir = filepath.Join(dir, filepath.Join(prefix...))
	}

	pattern := filepath.Join(dir, "*.json")
	matches, err := s.fs.Glob(pattern)
	if err != nil {
		return nil, &Error{Err: err}
	}

	result := make([]string, 0, len(matches))
	for _, match := range matches {
		rel, err := filepath.Rel(s.dir, match)
		if err != nil {
			continue
		}
		ext := filepath.Ext(rel)
		key := rel[:len(rel)-len(ext)]
		result = append(result, filepath.SplitList(key)...)
	}

	return result, nil
}

func (s *Storage) runMigrations() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	migrationFile := filepath.Join(s.dir, "migration")
	data, err := s.fs.ReadFile(migrationFile)
	currentVersion := 0
	if err == nil {
		fmt.Sscanf(string(data), "%d", &currentVersion)
	}

	for i := currentVersion; i < len(migrations); i++ {
		if err := migrations[i](s.dir, s.fs); err != nil {
			return fmt.Errorf("migration %d failed: %w", i, err)
		}
		s.fs.WriteFile(migrationFile, []byte(fmt.Sprintf("%d", i+1)), 0o644)
	}

	return nil
}

var migrations []MigrationFunc

func RegisterMigration(fn MigrationFunc) {
	migrations = append(migrations, fn)
}

type JSONStorage struct {
	*Storage
}

func NewJSON(dir string) (*JSONStorage, error) {
	s, err := New(dir, nil)
	if err != nil {
		return nil, err
	}
	return &JSONStorage{Storage: s}, nil
}

func (s *JSONStorage) ReadJSON(v any, key ...string) error {
	return s.Read(v, key...)
}

func (s *JSONStorage) WriteJSON(v any, key ...string) error {
	return s.Write(v, key...)
}

type MultiStorage struct {
	mu     sync.RWMutex
	stores map[string]*Storage
	fs     FileSystem
}

func NewMulti(fs FileSystem) *MultiStorage {
	if fs == nil {
		fs = DefaultFileSystem
	}
	return &MultiStorage{
		stores: make(map[string]*Storage),
		fs:     fs,
	}
}

func (m *MultiStorage) Register(name, dir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.stores[name]; exists {
		return fmt.Errorf("storage already registered: %s", name)
	}

	s, err := New(dir, m.fs)
	if err != nil {
		return err
	}

	m.stores[name] = s
	return nil
}

func (m *MultiStorage) Get(name string) (*Storage, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.stores[name]
	return s, ok
}

func (m *MultiStorage) List() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	names := make([]string, 0, len(m.stores))
	for name := range m.stores {
		names = append(names, name)
	}
	return names
}

type WriteOptimizedStorage struct {
	mu    sync.RWMutex
	cache map[string][]byte
	dir   string
	fs    FileSystem
}

func NewWriteOptimized(dir string, fs FileSystem) (*WriteOptimizedStorage, error) {
	if fs == nil {
		fs = DefaultFileSystem
	}
	w := &WriteOptimizedStorage{
		cache: make(map[string][]byte),
		dir:   dir,
		fs:    fs,
	}
	return w, nil
}

func (w *WriteOptimizedStorage) Write(v any, key ...string) error {
	path := w.file(key...)

	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.cache[path] = out

	if err := w.flush(path); err != nil {
		return err
	}

	return nil
}

func (w *WriteOptimizedStorage) Read(v any, key ...string) error {
	path := w.file(key...)

	w.mu.RLock()
	data, cached := w.cache[path]
	w.mu.RUnlock()

	if !cached {
		var err error
		data, err = w.fs.ReadFile(path)
		if err != nil {
			if w.fs.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
	}

	return json.Unmarshal(data, v)
}

func (w *WriteOptimizedStorage) file(key ...string) string {
	return filepath.Join(w.dir, filepath.Join(key...)) + ".json"
}

func (w *WriteOptimizedStorage) flush(path string) error {
	w.mu.RLock()
	data, ok := w.cache[path]
	w.mu.RUnlock()

	if !ok {
		return nil
	}

	return w.fs.WriteFile(path, data, 0o644)
}

func (w *WriteOptimizedStorage) FlushAll() error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	for path, data := range w.cache {
		if err := w.fs.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (w *WriteOptimizedStorage) Close() error {
	return w.FlushAll()
}

type Reader interface {
	Read(v any, key ...string) error
}

type Writer interface {
	Write(v any, key ...string) error
}

type ReadWriter interface {
	Reader
	Writer
}

type StorageAdapter struct {
	Storage *Storage
}

func (a *StorageAdapter) Get(v any, key ...string) error {
	return a.Storage.Read(v, key...)
}

func (a *StorageAdapter) Set(v any, key ...string) error {
	return a.Storage.Write(v, key...)
}

func Copy(src, dst *Storage, keys ...string) error {
	for _, key := range keys {
		var v any
		if err := src.Read(&v, key); err != nil {
			if errors.Is(err, ErrNotFound) {
				continue
			}
			return err
		}
		if err := dst.Write(v, key); err != nil {
			return err
		}
	}
	return nil
}

func CopyAll(src, dst *Storage) error {
	keys, err := src.List()
	if err != nil {
		return err
	}
	return Copy(src, dst, keys...)
}

type Transaction struct {
	mu    sync.Mutex
	ops   []func() error
	store *Storage
}

func NewTransaction(store *Storage) *Transaction {
	return &Transaction{store: store}
}

func (t *Transaction) Write(v any, key ...string) *Transaction {
	t.ops = append(t.ops, func() error {
		return t.store.Write(v, key...)
	})
	return t
}

func (t *Transaction) Remove(key ...string) *Transaction {
	t.ops = append(t.ops, func() error {
		return t.store.Remove(key)
	})
	return t
}

func (t *Transaction) Commit() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	for _, op := range t.ops {
		if err := op(); err != nil {
			return err
		}
	}
	return nil
}

func ReadAll(r io.Reader, v any) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
