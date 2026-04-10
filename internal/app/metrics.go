package app

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type serverMetrics struct {
	startedAt time.Time
	active    int64
	processed int64
	errors    int64
	mu        sync.Mutex
	latency   []time.Duration
}

func newServerMetrics() *serverMetrics {
	return &serverMetrics{startedAt: time.Now().UTC(), latency: make([]time.Duration, 0, 128)}
}

func (m *serverMetrics) record(duration time.Duration, err bool) {
	atomic.AddInt64(&m.processed, 1)
	if err {
		atomic.AddInt64(&m.errors, 1)
	}
	m.mu.Lock()
	if len(m.latency) >= 512 {
		m.latency = m.latency[len(m.latency)-256:]
	}
	m.latency = append(m.latency, duration)
	m.mu.Unlock()
}

func (m *serverMetrics) snapshot() map[string]any {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	m.mu.Lock()
	latencyCopy := append([]time.Duration(nil), m.latency...)
	m.mu.Unlock()

	latencyMs := percentileMillis(latencyCopy)
	output := map[string]any{
		"uptime_seconds":     int64(time.Since(m.startedAt).Seconds()),
		"active_requests":    atomic.LoadInt64(&m.active),
		"processed_requests": atomic.LoadInt64(&m.processed),
		"error_requests":     atomic.LoadInt64(&m.errors),
		"latency_ms":         latencyMs,
		"memory": map[string]any{
			"heap_alloc_mb": bytesToMB(mem.HeapAlloc),
			"heap_sys_mb":   bytesToMB(mem.HeapSys),
			"sys_mb":        bytesToMB(mem.Sys),
			"num_gc":        mem.NumGC,
		},
		"runtime": map[string]any{
			"goroutines": runtime.NumGoroutine(),
			"cpu":        runtime.NumCPU(),
		},
	}
	return output
}

func percentileMillis(values []time.Duration) map[string]float64 {
	if len(values) == 0 {
		return map[string]float64{"p50": 0, "p90": 0, "p99": 0}
	}
	sorted := append([]time.Duration(nil), values...)
	quickSort(sorted)
	return map[string]float64{
		"p50": toMillis(sorted[int(float64(len(sorted)-1)*0.50)]),
		"p90": toMillis(sorted[int(float64(len(sorted)-1)*0.90)]),
		"p99": toMillis(sorted[int(float64(len(sorted)-1)*0.99)]),
	}
}

func quickSort(values []time.Duration) {
	if len(values) < 2 {
		return
	}
	pivot := values[len(values)/2]
	left := 0
	right := len(values) - 1
	for left <= right {
		for values[left] < pivot {
			left++
		}
		for values[right] > pivot {
			right--
		}
		if left <= right {
			values[left], values[right] = values[right], values[left]
			left++
			right--
		}
	}
	if right > 0 {
		quickSort(values[:right+1])
	}
	if left < len(values) {
		quickSort(values[left:])
	}
}

func toMillis(d time.Duration) float64 {
	return float64(d.Milliseconds())
}

func bytesToMB(v uint64) float64 {
	return float64(v) / 1024.0 / 1024.0
}

func (s *APIServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	metrics := s.metrics.snapshot()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(metrics)
}
