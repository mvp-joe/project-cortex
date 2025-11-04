package mcp

// Test Plan for Stubbed File Watcher:
// 1. NewFileWatcher - Creates stub watcher (backward compatibility)
// 2. NewFileWatcherMulti - Creates stub watcher
// 3. NewFileWatcherAuto - Creates stub watcher
// 4. Start - Returns immediately (no actual watching)
// 5. Stop - Idempotent, no errors
// 6. Multiple Stop calls - No panic

import (
	"context"
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

// TestFileWatcher_NewFileWatcher tests stub watcher creation.
func TestFileWatcher_NewFileWatcher(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	assert.NotNil(t, watcher)
	defer watcher.Stop()
}

// TestFileWatcher_NewFileWatcherMulti tests stub multi-path watcher creation.
func TestFileWatcher_NewFileWatcherMulti(t *testing.T) {
	t.Parallel()

	dir1 := t.TempDir()
	dir2 := t.TempDir()
	mock := &mockReloadable{}
	watcher, err := NewFileWatcherMulti(mock, []string{dir1, dir2})
	require.NoError(t, err)
	assert.NotNil(t, watcher)
	defer watcher.Stop()
}

// TestFileWatcher_NewFileWatcherAuto tests stub auto-detection watcher creation.
func TestFileWatcher_NewFileWatcherAuto(t *testing.T) {
	t.Parallel()

	projectPath := t.TempDir()
	chunksDir := t.TempDir()
	mock := &mockReloadable{}
	watcher, err := NewFileWatcherAuto(mock, projectPath, chunksDir)
	require.NoError(t, err)
	assert.NotNil(t, watcher)
	defer watcher.Stop()
}

// TestFileWatcher_StartStop tests that Start/Stop complete immediately.
func TestFileWatcher_StartStop(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)

	ctx := context.Background()

	// Start should return immediately
	watcher.Start(ctx)

	// Give it a moment to ensure it's not doing anything
	time.Sleep(100 * time.Millisecond)

	// Should not trigger any reloads
	assert.Equal(t, 0, mock.getReloadCount())

	// Stop should be idempotent
	watcher.Stop()
	watcher.Stop() // Second call should not panic
}

// TestFileWatcher_NoReloadOnFileChanges tests that file changes don't trigger reloads.
func TestFileWatcher_NoReloadOnFileChanges(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx := context.Background()
	watcher.Start(ctx)

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Verify no reloads occurred (stub does nothing)
	assert.Equal(t, 0, mock.getReloadCount())
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

// TestFileWatcher_ContextCancellation tests context handling.
func TestFileWatcher_ContextCancellation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := &mockReloadable{}
	watcher, err := NewFileWatcher(mock, dir)
	require.NoError(t, err)
	defer watcher.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	watcher.Start(ctx)

	// Cancel immediately
	cancel()

	// Wait a bit
	time.Sleep(100 * time.Millisecond)

	// Verify no reloads
	assert.Equal(t, 0, mock.getReloadCount())
}
