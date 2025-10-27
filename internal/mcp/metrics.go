package mcp

// Implementation Plan:
// 1. ReloadMetrics - thread-safe metrics tracking with RWMutex
// 2. MetricsSnapshot - immutable snapshot for safe concurrent reads
// 3. RecordReload - updates all metrics atomically
// 4. GetMetrics - returns snapshot under read lock
// 5. NewReloadMetrics - constructor for initialization

import (
	"sync"
	"time"
)

// ReloadMetrics tracks reload operation statistics for the MCP server.
// All methods are thread-safe and can be called concurrently.
type ReloadMetrics struct {
	lastReloadTime     time.Time
	lastReloadDuration time.Duration
	lastReloadError    string
	totalReloads       int64
	successfulReloads  int64
	failedReloads      int64
	currentChunkCount  int
	mu                 sync.RWMutex
}

// MetricsSnapshot is an immutable snapshot of reload metrics at a point in time.
// It can be safely shared across goroutines without synchronization.
type MetricsSnapshot struct {
	LastReloadTime     time.Time     `json:"last_reload_time"`
	LastReloadDuration time.Duration `json:"last_reload_duration_ms"`
	LastReloadError    string        `json:"last_reload_error,omitempty"`
	TotalReloads       int64         `json:"total_reloads"`
	SuccessfulReloads  int64         `json:"successful_reloads"`
	FailedReloads      int64         `json:"failed_reloads"`
	CurrentChunkCount  int           `json:"current_chunk_count"`
}

// NewReloadMetrics creates a new ReloadMetrics instance with zero values.
func NewReloadMetrics() *ReloadMetrics {
	return &ReloadMetrics{}
}

// RecordReload records the outcome of a reload operation.
// It updates all relevant metrics atomically under a write lock.
//
// Parameters:
//   - duration: how long the reload operation took
//   - err: error if reload failed, nil if successful
//   - chunkCount: number of chunks loaded (0 if reload failed)
func (m *ReloadMetrics) RecordReload(duration time.Duration, err error, chunkCount int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.lastReloadTime = time.Now()
	m.lastReloadDuration = duration
	m.totalReloads++
	m.currentChunkCount = chunkCount

	if err != nil {
		m.failedReloads++
		m.lastReloadError = err.Error()
	} else {
		m.successfulReloads++
		m.lastReloadError = ""
	}
}

// GetMetrics returns an immutable snapshot of current metrics.
// The snapshot is independent of the underlying metrics and won't change
// even if new reloads are recorded.
func (m *ReloadMetrics) GetMetrics() MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return MetricsSnapshot{
		LastReloadTime:     m.lastReloadTime,
		LastReloadDuration: m.lastReloadDuration,
		LastReloadError:    m.lastReloadError,
		TotalReloads:       m.totalReloads,
		SuccessfulReloads:  m.successfulReloads,
		FailedReloads:      m.failedReloads,
		CurrentChunkCount:  m.currentChunkCount,
	}
}
