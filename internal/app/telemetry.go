package app

import (
	"context"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

type Tracer interface {
	StartSpan(ctx context.Context, name string, attrs ...zap.Field) (context.Context, Span)
	RecordMetric(name string, value float64, attrs ...zap.Field)
}

type Span interface {
	End()
	SetAttribute(key string, value any)
	AddEvent(name string, attrs ...zap.Field)
}

type NoOpTracer struct{}

func (t *NoOpTracer) StartSpan(ctx context.Context, name string, attrs ...zap.Field) (context.Context, Span) {
	return ctx, &NoOpSpan{}
}

func (t *NoOpTracer) RecordMetric(name string, value float64, attrs ...zap.Field) {}

type NoOpSpan struct{}

func (s *NoOpSpan) End()                                     {}
func (s *NoOpSpan) SetAttribute(key string, value any)       {}
func (s *NoOpSpan) AddEvent(name string, attrs ...zap.Field) {}

type TelemetryMetrics struct {
	ToolCallsTotal    atomic.Int64
	ToolCallsSuccess  atomic.Int64
	ToolCallsFailed   atomic.Int64
	ActiveAgents      atomic.Int64
	TotalTokensUsed   atomic.Int64
	ContextTokensUsed atomic.Int64
	GenerationTokens  atomic.Int64
	AvgLatencyMS      atomic.Int64
	SessionCount      atomic.Int64
	SessionActive     atomic.Int64
}

func NewTelemetryMetrics() *TelemetryMetrics {
	return &TelemetryMetrics{}
}

func (m *TelemetryMetrics) RecordToolCall(success bool, latencyMS float64) {
	m.ToolCallsTotal.Add(1)
	if success {
		m.ToolCallsSuccess.Add(1)
	} else {
		m.ToolCallsFailed.Add(1)
	}
	prev := m.AvgLatencyMS.Load()
	m.AvgLatencyMS.Store(int64((float64(prev) + latencyMS) / 2))
}

func (m *TelemetryMetrics) RecordTokens(contextTokens, generationTokens int64) {
	m.ContextTokensUsed.Add(contextTokens)
	m.GenerationTokens.Add(generationTokens)
	m.TotalTokensUsed.Add(contextTokens + generationTokens)
}

func (m *TelemetryMetrics) SetActiveSessions(total, active int64) {
	m.SessionCount.Store(total)
	m.SessionActive.Store(active)
}

func (m *TelemetryMetrics) IncrementActiveAgents() {
	m.ActiveAgents.Add(1)
}

func (m *TelemetryMetrics) DecrementActiveAgents() {
	m.ActiveAgents.Add(-1)
}

type SpanContext struct {
	TraceID string
	SpanID  string
	Start   time.Time
	Attrs   map[string]any
}

func (s *SpanContext) End() {
	s.Attrs["duration_ms"] = time.Since(s.Start).Milliseconds()
}

func (s *SpanContext) SetAttribute(key string, value any) {
	if s.Attrs == nil {
		s.Attrs = make(map[string]any)
	}
	s.Attrs[key] = value
}

func (s *SpanContext) AddEvent(name string, attrs ...zap.Field) {
	if s.Attrs == nil {
		s.Attrs = make(map[string]any)
	}
	events := append([]zap.Field{zap.String("event", name)}, attrs...)
	s.Attrs["events"] = append(s.Attrs["events"].([]zap.Field), events...)
}
