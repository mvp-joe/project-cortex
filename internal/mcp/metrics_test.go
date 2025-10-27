package mcp

// Test Plan for ReloadMetrics:
// - RecordReload captures successful reload metrics (duration, chunk count, no error)
// - RecordReload captures failed reload metrics (duration, error message)
// - GetMetrics returns accurate snapshot of current metrics
// - Concurrent access is thread-safe (no data races with -race)
// - Metrics accumulate correctly over multiple reloads
// - Zero values are handled correctly (initial state)

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestReloadMetrics_RecordSuccessfulReload(t *testing.T) {
	t.Parallel()

	// Test: Recording a successful reload captures all metrics correctly
	metrics := NewReloadMetrics()
	duration := 150 * time.Millisecond
	chunkCount := 1250

	metrics.RecordReload(duration, nil, chunkCount)

	snapshot := metrics.GetMetrics()
	assert.Equal(t, int64(1), snapshot.TotalReloads, "total reloads should be 1")
	assert.Equal(t, int64(1), snapshot.SuccessfulReloads, "successful reloads should be 1")
	assert.Equal(t, int64(0), snapshot.FailedReloads, "failed reloads should be 0")
	assert.Equal(t, duration, snapshot.LastReloadDuration, "last reload duration should match")
	assert.Equal(t, chunkCount, snapshot.CurrentChunkCount, "chunk count should match")
	assert.Empty(t, snapshot.LastReloadError, "error should be empty on success")
	assert.False(t, snapshot.LastReloadTime.IsZero(), "last reload time should be set")
}

func TestReloadMetrics_RecordFailedReload(t *testing.T) {
	t.Parallel()

	// Test: Recording a failed reload captures error message and updates counters
	metrics := NewReloadMetrics()
	duration := 50 * time.Millisecond
	err := errors.New("failed to load chunks: file not found")

	metrics.RecordReload(duration, err, 0)

	snapshot := metrics.GetMetrics()
	assert.Equal(t, int64(1), snapshot.TotalReloads, "total reloads should be 1")
	assert.Equal(t, int64(0), snapshot.SuccessfulReloads, "successful reloads should be 0")
	assert.Equal(t, int64(1), snapshot.FailedReloads, "failed reloads should be 1")
	assert.Equal(t, duration, snapshot.LastReloadDuration, "duration should be recorded even on failure")
	assert.Equal(t, "failed to load chunks: file not found", snapshot.LastReloadError, "error message should be captured")
	assert.Equal(t, 0, snapshot.CurrentChunkCount, "chunk count should be 0 on failure")
}

func TestReloadMetrics_MultipleReloads(t *testing.T) {
	t.Parallel()

	// Test: Metrics accumulate correctly over multiple reload operations
	metrics := NewReloadMetrics()

	// First reload: success
	metrics.RecordReload(100*time.Millisecond, nil, 1000)

	// Second reload: success
	metrics.RecordReload(150*time.Millisecond, nil, 1200)

	// Third reload: failure
	metrics.RecordReload(75*time.Millisecond, errors.New("disk full"), 0)

	snapshot := metrics.GetMetrics()
	assert.Equal(t, int64(3), snapshot.TotalReloads, "total should be 3")
	assert.Equal(t, int64(2), snapshot.SuccessfulReloads, "successful should be 2")
	assert.Equal(t, int64(1), snapshot.FailedReloads, "failed should be 1")
	assert.Equal(t, 75*time.Millisecond, snapshot.LastReloadDuration, "should have latest duration")
	assert.Equal(t, "disk full", snapshot.LastReloadError, "should have latest error")
	assert.Equal(t, 0, snapshot.CurrentChunkCount, "should have latest chunk count (0 from failed reload)")
}

func TestReloadMetrics_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	// Test: Concurrent RecordReload and GetMetrics calls are thread-safe (run with -race)
	metrics := NewReloadMetrics()
	var wg sync.WaitGroup

	// Spawn 50 goroutines that record reloads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			var err error
			if id%5 == 0 {
				err = errors.New("simulated error")
			}
			metrics.RecordReload(time.Duration(id)*time.Millisecond, err, id*100)
		}(i)
	}

	// Spawn 20 goroutines that read metrics
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			snapshot := metrics.GetMetrics()
			// Just access fields to ensure no race
			_ = snapshot.TotalReloads
			_ = snapshot.LastReloadDuration
			_ = snapshot.CurrentChunkCount
		}()
	}

	wg.Wait()

	// Verify final state
	snapshot := metrics.GetMetrics()
	assert.Equal(t, int64(50), snapshot.TotalReloads, "should have 50 total reloads")
	assert.Equal(t, int64(40), snapshot.SuccessfulReloads, "should have 40 successful reloads")
	assert.Equal(t, int64(10), snapshot.FailedReloads, "should have 10 failed reloads")
}

func TestReloadMetrics_InitialState(t *testing.T) {
	t.Parallel()

	// Test: Newly created metrics have correct zero values
	metrics := NewReloadMetrics()
	snapshot := metrics.GetMetrics()

	assert.Equal(t, int64(0), snapshot.TotalReloads, "initial total should be 0")
	assert.Equal(t, int64(0), snapshot.SuccessfulReloads, "initial successful should be 0")
	assert.Equal(t, int64(0), snapshot.FailedReloads, "initial failed should be 0")
	assert.Equal(t, time.Duration(0), snapshot.LastReloadDuration, "initial duration should be 0")
	assert.True(t, snapshot.LastReloadTime.IsZero(), "initial time should be zero")
	assert.Empty(t, snapshot.LastReloadError, "initial error should be empty")
	assert.Equal(t, 0, snapshot.CurrentChunkCount, "initial chunk count should be 0")
}

func TestReloadMetrics_GetMetricsSnapshot(t *testing.T) {
	t.Parallel()

	// Test: GetMetrics returns independent snapshot (not affected by subsequent changes)
	metrics := NewReloadMetrics()
	metrics.RecordReload(100*time.Millisecond, nil, 1000)

	// Get first snapshot
	snapshot1 := metrics.GetMetrics()

	// Record another reload
	metrics.RecordReload(200*time.Millisecond, nil, 2000)

	// Get second snapshot
	snapshot2 := metrics.GetMetrics()

	// First snapshot should be unchanged
	assert.Equal(t, int64(1), snapshot1.TotalReloads, "snapshot1 should still show 1 reload")
	assert.Equal(t, 100*time.Millisecond, snapshot1.LastReloadDuration, "snapshot1 duration unchanged")
	assert.Equal(t, 1000, snapshot1.CurrentChunkCount, "snapshot1 chunk count unchanged")

	// Second snapshot should show updated values
	assert.Equal(t, int64(2), snapshot2.TotalReloads, "snapshot2 should show 2 reloads")
	assert.Equal(t, 200*time.Millisecond, snapshot2.LastReloadDuration, "snapshot2 has new duration")
	assert.Equal(t, 2000, snapshot2.CurrentChunkCount, "snapshot2 has new chunk count")
}
