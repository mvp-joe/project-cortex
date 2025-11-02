package mcp

// Test Plan for Enhanced File Watcher:
// 1. NewFileWatcher - Creates watcher for single directory (backward compatibility)
// 2. NewFileWatcherMulti - Creates watcher for multiple paths
// 3. Debouncing - Multiple rapid changes trigger single reload after 500ms
// 4. onChange callback - Triggered on file write/create events
// 5. Stop - Cleanly stops watcher and releases resources
// 6. Multiple paths - Watches both JSON and SQLite simultaneously
// 7. Error cases - Handle non-existent paths gracefully

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReloadable implements Reloadable interface for testing.
type mockReloadable struct {
	reloadCount atomic.Int32
	reloadErr   error
	reloadDelay time.Duration // Simulate slow reload
}

func (m *mockReloadable) Reload(ctx context.Context) error {
	if m.reloadDelay > 0 {
		time.Sleep(m.reloadDelay)
	}
	m.reloadCount.Add(1)
	return m.reloadErr
}

func (m *mockReloadable) getReloadCount() int {
	return int(m.reloadCount.Load())
}

// TestFileWatcher_SinglePath tests basic watcher functionality with one path.
func TestFileWatcher_SinglePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.json")

	// Create initial file
	require.NoError(t, os.WriteFile(testFile, []byte("initial"), 0644))

	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Modify file
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))

	// Wait for debounce + reload (500ms debounce + buffer)
	time.Sleep(800 * time.Millisecond)

	assert.Equal(t, 1, mock.getReloadCount(), "Expected exactly one reload after debounce")
}

// TestFileWatcher_MultiPath tests watching multiple paths simultaneously.
func TestFileWatcher_MultiPath(t *testing.T) {
	t.Parallel()

	dir1 := t.TempDir()
	dir2 := t.TempDir()
	file1 := filepath.Join(dir1, "file1.json")
	file2 := filepath.Join(dir2, "file2.db")

	// Create initial files
	require.NoError(t, os.WriteFile(file1, []byte("data1"), 0644))
	require.NoError(t, os.WriteFile(file2, []byte("data2"), 0644))

	mock := &mockReloadable{}
	watcher, err := NewFileWatcherMulti(mock, []string{dir1, dir2})
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Modify file in first directory
	require.NoError(t, os.WriteFile(file1, []byte("modified1"), 0644))
	time.Sleep(800 * time.Millisecond)
	assert.Equal(t, 1, mock.getReloadCount(), "Expected reload after modifying file1")

	// Modify file in second directory
	require.NoError(t, os.WriteFile(file2, []byte("modified2"), 0644))
	time.Sleep(800 * time.Millisecond)
	assert.Equal(t, 2, mock.getReloadCount(), "Expected second reload after modifying file2")
}

// TestFileWatcher_Debouncing tests that rapid changes are debounced to single reload.
func TestFileWatcher_Debouncing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.json")

	require.NoError(t, os.WriteFile(testFile, []byte("initial"), 0644))

	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Make multiple rapid changes (within debounce window)
	for i := 0; i < 5; i++ {
		require.NoError(t, os.WriteFile(testFile, []byte("change"), 0644))
		time.Sleep(50 * time.Millisecond) // Rapid changes (50ms < 500ms debounce)
	}

	// Wait for debounce + reload
	time.Sleep(800 * time.Millisecond)

	// Should only trigger ONE reload despite 5 file changes
	assert.Equal(t, 1, mock.getReloadCount(), "Expected single reload despite multiple rapid changes")
}

// TestFileWatcher_CreateEvent tests that CREATE events also trigger reload.
func TestFileWatcher_CreateEvent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	newFile := filepath.Join(dir, "new-file.json")

	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Create new file
	require.NoError(t, os.WriteFile(newFile, []byte("new content"), 0644))

	// Wait for debounce + reload
	time.Sleep(800 * time.Millisecond)

	assert.Equal(t, 1, mock.getReloadCount(), "Expected reload after creating new file")
}

// TestFileWatcher_Stop tests graceful shutdown.
func TestFileWatcher_Stop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.json")
	require.NoError(t, os.WriteFile(testFile, []byte("initial"), 0644))

	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)

	ctx := context.Background()
	watcher.Start(ctx)

	// Stop watcher
	watcher.Stop()

	// Modify file after stop - should NOT trigger reload
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))
	time.Sleep(800 * time.Millisecond)

	assert.Equal(t, 0, mock.getReloadCount(), "Expected no reloads after Stop()")
}

// TestFileWatcher_ContextCancellation tests that context cancellation stops watcher.
func TestFileWatcher_ContextCancellation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.json")
	require.NoError(t, os.WriteFile(testFile, []byte("initial"), 0644))

	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	watcher.Start(ctx)

	// Cancel context
	cancel()

	// Wait for shutdown
	time.Sleep(100 * time.Millisecond)

	// Modify file after cancellation - should NOT trigger reload
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))
	time.Sleep(800 * time.Millisecond)

	assert.Equal(t, 0, mock.getReloadCount(), "Expected no reloads after context cancellation")
}

// TestFileWatcher_ReloadError tests that reload errors are handled gracefully.
func TestFileWatcher_ReloadError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.json")
	require.NoError(t, os.WriteFile(testFile, []byte("initial"), 0644))

	mock := &mockReloadable{
		reloadErr: assert.AnError, // Simulate reload failure
	}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Modify file
	require.NoError(t, os.WriteFile(testFile, []byte("modified"), 0644))
	time.Sleep(800 * time.Millisecond)

	// Should have attempted reload (even though it failed)
	assert.Equal(t, 1, mock.getReloadCount(), "Expected reload attempt despite error")
}

// TestFileWatcher_InvalidPath tests error handling for invalid watch paths.
func TestFileWatcher_InvalidPath(t *testing.T) {
	t.Parallel()

	mock := &mockReloadable{}
	_, err := NewFileWatcher(mock, "/nonexistent/path/that/does/not/exist")
	assert.Error(t, err, "Expected error when watching non-existent path")
}

// TestFileWatcher_MultipleStopCalls tests that Stop() is idempotent.
func TestFileWatcher_MultipleStopCalls(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)

	ctx := context.Background()
	watcher.Start(ctx)

	// Call Stop multiple times - should not panic
	watcher.Stop()
	watcher.Stop()
	watcher.Stop()

	// No assertion needed - just verify no panic
}

// TestGetSQLiteDatabasePath tests database path detection.
func TestGetSQLiteDatabasePath(t *testing.T) {
	t.Parallel()

	// Create temp directory to act as project root
	projectPath := t.TempDir()

	// getSQLiteDatabasePath requires valid git repo and cache settings
	// For this test, we just verify it handles missing settings gracefully
	_, err := getSQLiteDatabasePath(projectPath)
	assert.Error(t, err, "Expected error for project without cache settings")
}

// Benchmark tests

// BenchmarkFileWatcher_SingleReload benchmarks single file change -> reload cycle.
func BenchmarkFileWatcher_SingleReload(b *testing.B) {
	dir := b.TempDir()
	testFile := filepath.Join(dir, "test.json")
	require.NoError(b, os.WriteFile(testFile, []byte("initial"), 0644))

	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(b, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		require.NoError(b, os.WriteFile(testFile, []byte("modified"), 0644))
		time.Sleep(800 * time.Millisecond) // Wait for debounce
	}
}
