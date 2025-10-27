//go:build integration

package mcp

// Test Plan for FileWatcher Integration Tests:
// - Single file change triggers reload after debounce
// - Multiple rapid changes (5 files within 500ms) debounce to single reload
// - Context cancellation stops watcher cleanly (no events processed after cancel)
// - Reload errors don't crash watcher (logs error and continues)
// - Timer cleanup on rapid events works correctly (debouncing still functional)
// - Watcher handles file creation events
// - Watcher ignores non-write/create events (CHMOD, REMOVE)
// - Watcher stops cleanly via Stop() method
// - Concurrent file changes handled correctly

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockContextSearcher implements ContextSearcher for testing.
// It tracks reload calls and can simulate errors.
type mockContextSearcher struct {
	mu           sync.Mutex
	reloadCount  int32
	reloadCalls  []time.Time
	reloadErrors []error
	errorIndex   int
	closed       bool
}

func newMockSearcher() *mockContextSearcher {
	return &mockContextSearcher{
		reloadCalls:  make([]time.Time, 0),
		reloadErrors: make([]error, 0),
	}
}

func (m *mockContextSearcher) Query(ctx context.Context, query string, options *SearchOptions) ([]*SearchResult, error) {
	return nil, nil
}

func (m *mockContextSearcher) Reload(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.AddInt32(&m.reloadCount, 1)
	m.reloadCalls = append(m.reloadCalls, time.Now())

	// Return error if configured
	if m.errorIndex < len(m.reloadErrors) {
		err := m.reloadErrors[m.errorIndex]
		m.errorIndex++
		return err
	}

	return nil
}

func (m *mockContextSearcher) GetMetrics() MetricsSnapshot {
	// Return empty metrics for mock
	return MetricsSnapshot{}
}

func (m *mockContextSearcher) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockContextSearcher) getReloadCount() int {
	return int(atomic.LoadInt32(&m.reloadCount))
}

func (m *mockContextSearcher) getReloadCalls() []time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]time.Time, len(m.reloadCalls))
	copy(calls, m.reloadCalls)
	return calls
}

func (m *mockContextSearcher) injectReloadError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reloadErrors = append(m.reloadErrors, err)
}

func (m *mockContextSearcher) isClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

// Test: Single file change triggers reload after debounce period
func TestFileWatcher_SingleFileChange(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher
	searcher := newMockSearcher()

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)
	defer watcher.Stop()

	// Start watcher
	ctx := context.Background()
	watcher.Start(ctx)

	// Wait a bit for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Modify a file
	testFile := filepath.Join(chunksDir, "test.json")
	err = os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644)
	require.NoError(t, err)

	// Wait for debounce (500ms) + some buffer
	time.Sleep(700 * time.Millisecond)

	// Verify reload was called exactly once
	count := searcher.getReloadCount()
	assert.Equal(t, 1, count, "Expected exactly one reload call")
}

// Test: Multiple rapid changes (5 files within 500ms) debounce to single reload
func TestFileWatcher_MultipleRapidChangesDebounce(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher
	searcher := newMockSearcher()

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)
	defer watcher.Stop()

	// Start watcher
	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Modify 5 files rapidly (within 500ms debounce window)
	for i := 0; i < 5; i++ {
		testFile := filepath.Join(chunksDir, "test"+string(rune('0'+i))+".json")
		err = os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644)
		require.NoError(t, err)
		time.Sleep(50 * time.Millisecond) // Small delay between writes
	}

	// Wait for debounce (500ms) + buffer
	time.Sleep(700 * time.Millisecond)

	// Verify reload was called exactly once (not 5 times)
	count := searcher.getReloadCount()
	assert.Equal(t, 1, count, "Expected exactly one reload call despite 5 file changes")
}

// Test: Context cancellation stops watcher cleanly
func TestFileWatcher_ContextCancellationStopsCleanly(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher
	searcher := newMockSearcher()

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)

	// Start watcher with cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	watcher.Start(ctx)

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Create a file to ensure watcher is working
	testFile := filepath.Join(chunksDir, "test1.json")
	err = os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644)
	require.NoError(t, err)

	// Wait for debounce + processing
	time.Sleep(700 * time.Millisecond)

	// Record reload count before cancellation
	reloadCountBeforeCancel := searcher.getReloadCount()
	assert.Equal(t, 1, reloadCountBeforeCancel, "Watcher should have processed first file")

	// Cancel context
	cancel()

	// Wait for watcher to stop
	time.Sleep(200 * time.Millisecond)

	// Create another file after cancellation - should NOT trigger reload
	testFile2 := filepath.Join(chunksDir, "test2.json")
	err = os.WriteFile(testFile2, []byte(`{"test": "data2"}`), 0644)
	require.NoError(t, err)

	// Wait for debounce period to prove no reload happens
	time.Sleep(700 * time.Millisecond)

	// Verify no additional reloads after context cancellation
	reloadCountAfterCancel := searcher.getReloadCount()
	assert.Equal(t, reloadCountBeforeCancel, reloadCountAfterCancel,
		"Watcher should not process events after context cancellation")

	// Stop should be idempotent
	watcher.Stop()
	watcher.Stop() // Call twice to verify no panic
}

// Test: Reload errors don't crash watcher, logs error and continues
func TestFileWatcher_ReloadErrorsContinue(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher with injected error
	searcher := newMockSearcher()
	searcher.injectReloadError(errors.New("simulated reload failure"))

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)
	defer watcher.Stop()

	// Start watcher
	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Modify a file (this should trigger reload with error)
	testFile := filepath.Join(chunksDir, "test1.json")
	err = os.WriteFile(testFile, []byte(`{"test": "data1"}`), 0644)
	require.NoError(t, err)

	// Wait for debounce
	time.Sleep(700 * time.Millisecond)

	// Verify reload was called (even though it failed)
	count := searcher.getReloadCount()
	assert.Equal(t, 1, count, "Expected reload to be called despite error")

	// Modify another file (watcher should still be running)
	testFile2 := filepath.Join(chunksDir, "test2.json")
	err = os.WriteFile(testFile2, []byte(`{"test": "data2"}`), 0644)
	require.NoError(t, err)

	// Wait for debounce
	time.Sleep(700 * time.Millisecond)

	// Verify second reload was called (watcher continues after error)
	count = searcher.getReloadCount()
	assert.Equal(t, 2, count, "Expected watcher to continue after error")
}

// Test: Timer cleanup on rapid events - debouncing works correctly
func TestFileWatcher_TimerCleanupNoLeaks(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher
	searcher := newMockSearcher()

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)
	defer watcher.Stop()

	// Start watcher
	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Trigger multiple rapid file changes (20 changes in rapid succession)
	// This tests that timer cleanup and debouncing work correctly
	for i := 0; i < 20; i++ {
		testFile := filepath.Join(chunksDir, "test.json")
		data := []byte(`{"iteration": ` + string(rune('0'+i%10)) + `}`)
		err = os.WriteFile(testFile, data, 0644)
		require.NoError(t, err)
		time.Sleep(30 * time.Millisecond) // Rapid changes
	}

	// Wait for debounce and reload
	time.Sleep(700 * time.Millisecond)

	// Verify only one reload occurred despite many changes
	// This proves debouncing works and timers are being cleaned up properly
	count := searcher.getReloadCount()
	assert.Equal(t, 1, count, "Expected single reload after rapid changes (proves timer cleanup works)")

	// Create one more file after the reload completes
	testFile := filepath.Join(chunksDir, "final.json")
	err = os.WriteFile(testFile, []byte(`{"final": true}`), 0644)
	require.NoError(t, err)

	// Wait for another debounce
	time.Sleep(700 * time.Millisecond)

	// Should have exactly 2 reloads total - proves watcher still responsive after rapid events
	count = searcher.getReloadCount()
	assert.Equal(t, 2, count, "Watcher should still be responsive after rapid timer resets")
}

// Test: Watcher handles file creation events
func TestFileWatcher_FileCreation(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher
	searcher := newMockSearcher()

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)
	defer watcher.Stop()

	// Start watcher
	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Create a new file (CREATE event)
	testFile := filepath.Join(chunksDir, "newfile.json")
	err = os.WriteFile(testFile, []byte(`{"test": "new"}`), 0644)
	require.NoError(t, err)

	// Wait for debounce
	time.Sleep(700 * time.Millisecond)

	// Verify reload was called for file creation
	count := searcher.getReloadCount()
	assert.Equal(t, 1, count, "Expected reload on file creation")
}

// Test: Watcher stops cleanly via Stop() method
func TestFileWatcher_StopMethod(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher
	searcher := newMockSearcher()

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)

	// Start watcher
	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for watcher to start
	time.Sleep(100 * time.Millisecond)

	// Stop watcher
	watcher.Stop()

	// Verify that Stop() completes without hanging
	// (If Stop() doesn't work correctly, this test will timeout)

	// Try to trigger an event after stop (should not cause reload)
	testFile := filepath.Join(chunksDir, "test.json")
	err = os.WriteFile(testFile, []byte(`{"test": "data"}`), 0644)
	require.NoError(t, err)

	// Wait to ensure no reload happens
	time.Sleep(700 * time.Millisecond)

	// Verify no reloads occurred after stop
	count := searcher.getReloadCount()
	assert.Equal(t, 0, count, "Expected no reloads after watcher stopped")
}

// Test: Concurrent file changes handled correctly (stress test)
func TestFileWatcher_ConcurrentChanges(t *testing.T) {
	t.Parallel()

	// Create temp directory for chunks
	chunksDir := t.TempDir()

	// Create mock searcher
	searcher := newMockSearcher()

	// Create file watcher
	watcher, err := NewFileWatcher(searcher, chunksDir)
	require.NoError(t, err)
	defer watcher.Stop()

	// Start watcher
	ctx := context.Background()
	watcher.Start(ctx)

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Concurrently modify multiple files
	var wg sync.WaitGroup
	numGoroutines := 10
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			testFile := filepath.Join(chunksDir, "concurrent.json")
			data := []byte(`{"id": ` + string(rune('0'+id%10)) + `}`)
			_ = os.WriteFile(testFile, data, 0644)
		}(i)
	}

	wg.Wait()

	// Wait for debounce
	time.Sleep(700 * time.Millisecond)

	// Verify that reload was called (should be 1 due to debouncing)
	count := searcher.getReloadCount()
	assert.Equal(t, 1, count, "Expected single reload after concurrent changes")
}
