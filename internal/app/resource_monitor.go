package app

import (
	"runtime"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

type resourceMonitor struct {
	mu             sync.Mutex
	lastSample     time.Time
	lastCPUIdle    uint64
	lastCPUTotal   uint64
	lastCPUPercent float64
	memUsedMB      float64
	memTotalMB     float64
	memPercent     float64
	interval       time.Duration
}

type resourceSnapshot struct {
	CPUPercent   float64
	MemUsedMB    float64
	MemTotalMB   float64
	MemPercent   float64
	CPUAvailable bool
}

func newResourceMonitor(interval time.Duration) *resourceMonitor {
	if interval <= 0 {
		interval = time.Second
	}
	return &resourceMonitor{interval: interval}
}

func (m *resourceMonitor) Snapshot() resourceSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	if time.Since(m.lastSample) < m.interval {
		return resourceSnapshot{
			CPUPercent:   m.lastCPUPercent,
			MemUsedMB:    m.memUsedMB,
			MemTotalMB:   m.memTotalMB,
			MemPercent:   m.memPercent,
			CPUAvailable: m.lastCPUPercent > 0,
		}
	}

	if percent, ok := readCPUPercent(); ok {
		m.lastCPUPercent = percent
	}

	memUsed, memTotal, memPercent := readMemoryUsage()
	m.memUsedMB = memUsed
	m.memTotalMB = memTotal
	m.memPercent = memPercent
	m.lastSample = time.Now()

	return resourceSnapshot{
		CPUPercent:   m.lastCPUPercent,
		MemUsedMB:    m.memUsedMB,
		MemTotalMB:   m.memTotalMB,
		MemPercent:   m.memPercent,
		CPUAvailable: m.lastCPUPercent > 0,
	}
}

func readCPUPercent() (float64, bool) {
	percent, err := cpu.Percent(0, false)
	if err != nil || len(percent) == 0 {
		return 0, false
	}
	return percent[0], true
}

func readMemoryUsage() (float64, float64, float64) {
	vmem, err := mem.VirtualMemory()
	if err == nil {
		usedMB := float64(vmem.Used) / 1024.0 / 1024.0
		totalMB := float64(vmem.Total) / 1024.0 / 1024.0
		return usedMB, totalMB, vmem.UsedPercent
	}
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	used := float64(stats.HeapAlloc) / 1024.0 / 1024.0
	return used, 0, 0
}
