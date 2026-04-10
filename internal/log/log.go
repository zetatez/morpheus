package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Level string

const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

type Logger interface {
	Debug(message string, fields ...any)
	Info(message string, fields ...any)
	Warn(message string, fields ...any)
	Error(message string, fields ...any)
	With(fields ...any) Logger
	Time(label string, fields ...any) Timer
}

type Timer interface {
	Stop()
}

type Fields map[string]any

type logger struct {
	service string
	fields  Fields
	level   Level
	writer  io.Writer
	mu      sync.Mutex
}

var (
	defaultLevel  = LevelInfo
	globalLogger  Logger
	globalMu      sync.RWMutex
	loggers       = make(map[string]*logger)
	loggersMu     sync.RWMutex
	logFileCount  = 10
	logFileMaxAge = 5 * 24 * time.Hour
)

type Options struct {
	Service string
	Level   Level
	Writer  io.Writer
	Fields  Fields
}

func New(opts Options) Logger {
	if opts.Writer == nil {
		opts.Writer = os.Stdout
	}
	if opts.Level == "" {
		opts.Level = defaultLevel
	}
	return &logger{
		service: opts.Service,
		fields:  opts.Fields,
		level:   opts.Level,
		writer:  opts.Writer,
	}
}

func Create(opts Options) Logger {
	loggersMu.Lock()
	defer loggersMu.Unlock()

	key := opts.Service
	if key == "" {
		key = "default"
	}

	if l, ok := loggers[key]; ok {
		if opts.Level != "" {
			l.level = opts.Level
		}
		if opts.Fields != nil {
			for k, v := range opts.Fields {
				l.fields[k] = v
			}
		}
		return l
	}

	l := &logger{
		service: opts.Service,
		fields:  make(Fields),
		level:   opts.Level,
		writer:  opts.Writer,
	}
	for k, v := range opts.Fields {
		l.fields[k] = v
	}
	loggers[key] = l
	return l
}

func (l *logger) shouldLog(level Level) bool {
	levels := map[Level]int{
		LevelDebug: 0,
		LevelInfo:  1,
		LevelWarn:  2,
		LevelError: 3,
	}
	return levels[level] >= levels[l.level]
}

func (l *logger) log(level Level, message string, fields ...any) {
	if !l.shouldLog(level) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	fieldsMap := make(Fields)

	for k, v := range l.fields {
		fieldsMap[k] = v
	}

	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			fieldsMap[fmt.Sprintf("%v", fields[i])] = fields[i+1]
		}
	}

	if len(fieldsMap) > 0 {
		fieldsStr := formatFields(fieldsMap)
		fmt.Fprintf(l.writer, "%s %s %s %s\n",
			now.Format(time.RFC3339),
			level,
			l.service,
			fieldsStr)
	} else {
		fmt.Fprintf(l.writer, "%s %s %s %s\n",
			now.Format(time.RFC3339),
			level,
			l.service,
			message)
	}
}

func formatFields(fields Fields) string {
	if len(fields) == 0 {
		return ""
	}
	var parts []string
	for k, v := range fields {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}

func (l *logger) Debug(message string, fields ...any) {
	l.log(LevelDebug, message, fields...)
}

func (l *logger) Info(message string, fields ...any) {
	l.log(LevelInfo, message, fields...)
}

func (l *logger) Warn(message string, fields ...any) {
	l.log(LevelWarn, message, fields...)
}

func (l *logger) Error(message string, fields ...any) {
	l.log(LevelError, message, fields...)
}

func (l *logger) With(fields ...any) Logger {
	newFields := make(Fields)
	for k, v := range l.fields {
		newFields[k] = v
	}
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			newFields[fmt.Sprintf("%v", fields[i])] = fields[i+1]
		}
	}
	return &logger{
		service: l.service,
		fields:  newFields,
		level:   l.level,
		writer:  l.writer,
	}
}

func (l *logger) Time(label string, fields ...any) Timer {
	start := time.Now()
	extra := make(Fields)
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			extra[fmt.Sprintf("%v", fields[i])] = fields[i+1]
		}
	}
	l.Info(fmt.Sprintf("started: %s", label), "labels", label, "fields", extra)
	return &timer{
		logger: l,
		label:  label,
		start:  start,
		fields: extra,
	}
}

type timer struct {
	logger *logger
	label  string
	start  time.Time
	fields Fields
}

func (t *timer) Stop() {
	duration := time.Since(t.start)
	t.logger.Info(fmt.Sprintf("stopped: %s", t.label), "duration_ms", duration.Milliseconds(), "labels", t.label, "fields", t.fields)
}

type fileLogger struct {
	*logger
	file *os.File
	path string
}

func NewFileLogger(service, path string, level Level) (Logger, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}

	return &fileLogger{
		logger: &logger{
			service: service,
			fields:  make(Fields),
			level:   level,
			writer:  f,
		},
		file: f,
		path: path,
	}, nil
}

func Init(opts Options) error {
	globalMu.Lock()
	defer globalMu.Unlock()

	globalLogger = Create(opts)
	return nil
}

func Default() Logger {
	globalMu.RLock()
	defer globalMu.RUnlock()

	if globalLogger == nil {
		globalLogger = Create(Options{
			Service: "app",
			Level:   LevelInfo,
			Writer:  os.Stdout,
		})
	}
	return globalLogger
}

func SetDefault(level Level) {
	defaultLevel = level
}

func Debug(message string, fields ...any) {
	Default().Debug(message, fields...)
}

func Info(message string, fields ...any) {
	Default().Info(message, fields...)
}

func Warn(message string, fields ...any) {
	Default().Warn(message, fields...)
}

func Error(message string, fields ...any) {
	Default().Error(message, fields...)
}

type timingKey struct{}

func StartTiming(label string) func(fields ...any) {
	pc, _, _, _ := runtime.Caller(1)
	funcName := runtime.FuncForPC(pc).Name()

	start := time.Now()
	return func(fields ...any) {
		duration := time.Since(start)
		Default().Info(fmt.Sprintf("timing: %s", label),
			"func", funcName,
			"duration_ms", duration.Milliseconds(),
			"label", label,
			"fields", fields,
		)
	}
}

type structuredLogger struct {
	mu      sync.Mutex
	service string
	fields  Fields
	level   Level
	writer  io.Writer
	tags    []string
}

func (s *structuredLogger) shouldLog(level Level) bool {
	levels := map[Level]int{
		LevelDebug: 0,
		LevelInfo:  1,
		LevelWarn:  2,
		LevelError: 3,
	}
	return levels[level] >= levels[s.level]
}

type StructuredLogger struct {
	logger *structuredLogger
}

func NewStructured(service string) *StructuredLogger {
	return &StructuredLogger{
		logger: &structuredLogger{
			service: service,
			fields:  make(Fields),
			level:   LevelInfo,
			writer:  os.Stdout,
		},
	}
}

func (s *StructuredLogger) WithField(key, value string) *StructuredLogger {
	s.logger.mu.Lock()
	defer s.logger.mu.Unlock()
	s.logger.fields[key] = value
	return s
}

func (s *StructuredLogger) WithFields(fields map[string]any) *StructuredLogger {
	s.logger.mu.Lock()
	defer s.logger.mu.Unlock()
	for k, v := range fields {
		s.logger.fields[k] = v
	}
	return s
}

func (s *StructuredLogger) Tag(key, value string) *StructuredLogger {
	s.logger.mu.Lock()
	defer s.logger.mu.Unlock()
	s.logger.tags = append(s.logger.tags, key, value)
	return s
}

func (s *StructuredLogger) Log(level Level, message string, fields ...any) {
	if !s.logger.shouldLog(level) {
		return
	}

	s.logger.mu.Lock()
	defer s.logger.mu.Unlock()

	now := time.Now()
	allFields := make(Fields)
	for k, v := range s.logger.fields {
		allFields[k] = v
	}
	for i := 0; i < len(fields); i += 2 {
		if i+1 < len(fields) {
			allFields[fmt.Sprintf("%v", fields[i])] = fields[i+1]
		}
	}
	for i := 0; i < len(s.logger.tags); i += 2 {
		if i+1 < len(s.logger.tags) {
			allFields[s.logger.tags[i]] = s.logger.tags[i+1]
		}
	}

	fieldsStr := formatFields(allFields)
	fmt.Fprintf(s.logger.writer, "%s %s %s %s %s\n",
		now.Format(time.RFC3339),
		level,
		s.logger.service,
		fieldsStr,
		message)
}

func (s *StructuredLogger) Debug(message string, fields ...any) {
	s.Log(LevelDebug, message, fields...)
}

func (s *StructuredLogger) Info(message string, fields ...any) {
	s.Log(LevelInfo, message, fields...)
}

func (s *StructuredLogger) Warn(message string, fields ...any) {
	s.Log(LevelWarn, message, fields...)
}

func (s *StructuredLogger) Error(message string, fields ...any) {
	s.Log(LevelError, message, fields...)
}

func CleanupOldLogs(dir string, maxFiles int, maxAge time.Duration) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	cutoff := time.Now().Add(-maxAge)
	var logFiles []os.FileInfo

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".log") && !strings.HasSuffix(name, ".log") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		logFiles = append(logFiles, info)
	}

	for _, f := range logFiles {
		if f.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(dir, f.Name()))
		}
	}

	if len(logFiles) > maxFiles {
		oldestFirst := make([]os.FileInfo, len(logFiles))
		copy(oldestFirst, logFiles)
		for i := 0; i < len(oldestFirst); i++ {
			for j := i + 1; j < len(oldestFirst); j++ {
				if oldestFirst[i].ModTime().After(oldestFirst[j].ModTime()) {
					oldestFirst[i], oldestFirst[j] = oldestFirst[j], oldestFirst[i]
				}
			}
		}
		toDelete := len(oldestFirst) - maxFiles
		for i := 0; i < toDelete; i++ {
			os.Remove(filepath.Join(dir, oldestFirst[i].Name()))
		}
	}

	return nil
}
