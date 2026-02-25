package modules

import (
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type MetricsData struct {
	ActiveListeners   int64   `json:"active_listeners"`
	TotalBytesStreamed int64  `json:"total_bytes_streamed"`
	StreamStartTime   int64   `json:"stream_start_time"`
	MemoryUsage       uint64  `json:"memory_usage_mb"`
	MemoryHeapAlloc   uint64  `json:"memory_heap_alloc_mb"`
	MemoryHeapSys     uint64  `json:"memory_heap_sys_mb"`
	MemoryTotalAlloc  uint64  `json:"memory_total_alloc_mb"`
	MemorySys         uint64  `json:"memory_sys_mb"`
	GCRuns            uint32  `json:"gc_runs"`
	GCPauseMs         float64 `json:"gc_pause_ms"`
	NumGoroutines     int     `json:"num_goroutines"`
	CPUUsagePercent   float64 `json:"cpu_usage_percent"`
	BandwidthMbps     float64 `json:"bandwidth_mbps"`
}

var (
	metrics = struct {
		activeListeners   int64
		totalBytesStreamed int64
		streamStartTime   int64
		lastBytesCheckTime int64
		lastBytesCount    int64
		mu                sync.RWMutex
	}{
		streamStartTime:   time.Now().UnixMilli(),
		lastBytesCheckTime: time.Now().UnixMilli(),
	}
)

// IncrementListener increments the active listener count
func IncrementListener() {
	atomic.AddInt64(&metrics.activeListeners, 1)
}

// DecrementListener decrements the active listener count
func DecrementListener() {
	atomic.AddInt64(&metrics.activeListeners, -1)
}

// AddBytesStreamed adds bytes to the total streamed count
func AddBytesStreamed(bytes int64) {
	atomic.AddInt64(&metrics.totalBytesStreamed, bytes)
}

// GetMetrics returns current metrics
func GetMetrics() MetricsData {
	// Get memory stats
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	// Convert bytes to MB
	memoryMB := m.Alloc / 1024 / 1024
	heapAllocMB := m.HeapAlloc / 1024 / 1024
	heapSysMB := m.HeapSys / 1024 / 1024
	totalAllocMB := m.TotalAlloc / 1024 / 1024
	sysMB := m.Sys / 1024 / 1024
	
	// Get last GC pause in milliseconds
	gcPauseMs := float64(m.PauseNs[(m.NumGC+255)%256]) / 1_000_000.0

	// Calculate bandwidth
	currentTime := time.Now().UnixMilli()
	currentBytes := atomic.LoadInt64(&metrics.totalBytesStreamed)
	
	metrics.mu.RLock()
	timeDiff := currentTime - metrics.lastBytesCheckTime
	bytesDiff := currentBytes - metrics.lastBytesCount
	metrics.mu.RUnlock()

	var bandwidthMbps float64
	if timeDiff > 0 {
		// Convert bytes per millisecond to Mbps
		// bytes/ms * 1000 = bytes/s
		// bytes/s / 125000 = Mbps (since 1 Mbps = 125000 bytes/s)
		bytesPerSecond := (float64(bytesDiff) / float64(timeDiff)) * 1000.0
		bandwidthMbps = bytesPerSecond / 125000.0
	}

	// Update last check
	metrics.mu.Lock()
	metrics.lastBytesCheckTime = currentTime
	metrics.lastBytesCount = currentBytes
	metrics.mu.Unlock()

	return MetricsData{
		ActiveListeners:    atomic.LoadInt64(&metrics.activeListeners),
		TotalBytesStreamed: currentBytes,
		StreamStartTime:    metrics.streamStartTime,
		MemoryUsage:        memoryMB,
		MemoryHeapAlloc:    heapAllocMB,
		MemoryHeapSys:      heapSysMB,
		MemoryTotalAlloc:   totalAllocMB,
		MemorySys:          sysMB,
		GCRuns:             m.NumGC,
		GCPauseMs:          gcPauseMs,
		NumGoroutines:      runtime.NumGoroutine(),
		BandwidthMbps:      bandwidthMbps,
	}
}

// ResetMetrics resets the metrics (useful for testing)
func ResetMetrics() {
	atomic.StoreInt64(&metrics.activeListeners, 0)
	atomic.StoreInt64(&metrics.totalBytesStreamed, 0)
	metrics.mu.Lock()
	metrics.streamStartTime = time.Now().UnixMilli()
	metrics.lastBytesCheckTime = time.Now().UnixMilli()
	metrics.lastBytesCount = 0
	metrics.mu.Unlock()
}
