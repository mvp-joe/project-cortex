package watcher

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for WatchCoordinator:
// - Start successfully with both watchers
// - File change event triggers Index() with hint
// - Branch switch event: pauses files, syncs, switches, resumes (verify call order)
// - Context cancellation (clean shutdown of both watchers)
// - File change during branch switch (queued until resume)
// - Multiple file changes (batched into single Index call)
// - Error handling: indexer.Index() fails (log, continue)
// - Error handling: branchSync.PrepareDB() fails (log, continue)
// - Error handling: git watcher.Start() fails (propagate error)
// - Error handling: file watcher.Start() fails (propagate error)
// - Cleanup called on context cancellation

// mockGitWatcher implements GitWatcher for testing.
type mockGitWatcher struct {
	startErr      error
	stopErr       error
	startCallback func(oldBranch, newBranch string)
	stopCalled    bool
	mu            sync.Mutex
}

func (m *mockGitWatcher) Start(ctx context.Context, callback func(oldBranch, newBranch string)) error {
	m.mu.Lock()
	m.startCallback = callback
	startErr := m.startErr
	m.mu.Unlock()

	if startErr != nil {
		return startErr
	}

	// Block until context done (simulates watcher behavior)
	<-ctx.Done()
	return nil
}

func (m *mockGitWatcher) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true
	return m.stopErr
}

func (m *mockGitWatcher) triggerBranchSwitch(oldBranch, newBranch string) {
	m.mu.Lock()
	callback := m.startCallback
	m.mu.Unlock()

	if callback != nil {
		callback(oldBranch, newBranch)
	}
}

// mockFileWatcher implements FileWatcher for testing.
type mockFileWatcher struct {
	startErr       error
	stopErr        error
	startCallback  func(files []string)
	pauseCount     int
	resumeCount    int
	stopCalled     bool
	paused         bool
	accumulatedEvents [][]string // Track events during pause
	mu             sync.Mutex
}

func (m *mockFileWatcher) Start(ctx context.Context, callback func(files []string)) error {
	m.mu.Lock()
	m.startCallback = callback
	startErr := m.startErr
	m.mu.Unlock()

	if startErr != nil {
		return startErr
	}

	// Block until context done (simulates watcher behavior)
	<-ctx.Done()
	return nil
}

func (m *mockFileWatcher) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true
	return m.stopErr
}

func (m *mockFileWatcher) Pause() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pauseCount++
	m.paused = true
}

func (m *mockFileWatcher) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resumeCount++
	m.paused = false

	// Fire accumulated events on resume
	if len(m.accumulatedEvents) > 0 && m.startCallback != nil {
		for _, files := range m.accumulatedEvents {
			m.startCallback(files)
		}
		m.accumulatedEvents = nil
	}
}

func (m *mockFileWatcher) triggerFileChange(files []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.paused {
		// Accumulate events during pause
		m.accumulatedEvents = append(m.accumulatedEvents, files)
		return
	}

	if m.startCallback != nil {
		m.startCallback(files)
	}
}

// mockBranchSynchronizer implements BranchSynchronizer for testing.
type mockBranchSynchronizer struct {
	prepareDBErr    error
	preparedBranches []string
	mu              sync.Mutex
}

func (m *mockBranchSynchronizer) PrepareDB(ctx context.Context, branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.preparedBranches = append(m.preparedBranches, branch)
	return m.prepareDBErr
}

// mockIndexer implements IndexerInterface for testing.
type mockIndexer struct {
	indexErr         error
	switchBranchErr  error
	indexCalls       [][]string // Track hint arguments
	switchBranchCalls []string
	mu               sync.Mutex
}

func (m *mockIndexer) Index(ctx context.Context, hint []string) (*IndexStats, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.indexCalls = append(m.indexCalls, hint)

	if m.indexErr != nil {
		return nil, m.indexErr
	}

	return &IndexStats{
		FilesProcessed: len(hint),
		CodeChunks:     len(hint) * 10,
		DocChunks:      len(hint) * 5,
	}, nil
}

func (m *mockIndexer) SwitchBranch(branch string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.switchBranchCalls = append(m.switchBranchCalls, branch)
	return m.switchBranchErr
}

// Helper to create coordinator with mocks
func setupCoordinator() (*WatchCoordinator, *mockGitWatcher, *mockFileWatcher, *mockBranchSynchronizer, *mockIndexer) {
	git := &mockGitWatcher{}
	files := &mockFileWatcher{}
	branchSync := &mockBranchSynchronizer{}
	indexer := &mockIndexer{}

	coord := NewWatchCoordinator(git, files, branchSync, indexer)

	return coord, git, files, branchSync, indexer
}

// Test: Start successfully with both watchers
func TestWatchCoordinator_StartsSuccessfully(t *testing.T) {
	t.Parallel()

	coord, git, files, _, _ := setupCoordinator()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator in background
	done := make(chan error, 1)
	go func() {
		done <- coord.Start(ctx)
	}()

	// Give watchers time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context to trigger shutdown
	cancel()

	// Wait for coordinator to stop
	err := <-done
	assert.Equal(t, context.Canceled, err)

	// Verify cleanup called
	assert.True(t, git.stopCalled, "git watcher should be stopped")
	assert.True(t, files.stopCalled, "file watcher should be stopped")
}

// Test: File change event triggers Index() with hint
func TestWatchCoordinator_FileChangeTriggersIndex(t *testing.T) {
	t.Parallel()

	coord, _, files, _, indexer := setupCoordinator()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger file change
	changedFiles := []string{"src/main.go", "src/util.go"}
	files.triggerFileChange(changedFiles)

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify Index() called with hint
	indexer.mu.Lock()
	require.Len(t, indexer.indexCalls, 1, "Index should be called once")
	assert.Equal(t, changedFiles, indexer.indexCalls[0], "Index should receive correct hint")
	indexer.mu.Unlock()
}

// Test: Branch switch event: pauses files, syncs, switches, resumes (verify call order)
func TestWatchCoordinator_BranchSwitchCallOrder(t *testing.T) {
	t.Parallel()

	coord, git, files, branchSync, indexer := setupCoordinator()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger branch switch
	git.triggerBranchSwitch("main", "feature")

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify call sequence
	files.mu.Lock()
	assert.Equal(t, 1, files.pauseCount, "File watcher should be paused once")
	assert.Equal(t, 1, files.resumeCount, "File watcher should be resumed once")
	files.mu.Unlock()

	branchSync.mu.Lock()
	require.Len(t, branchSync.preparedBranches, 1, "PrepareDB should be called once")
	assert.Equal(t, "feature", branchSync.preparedBranches[0])
	branchSync.mu.Unlock()

	indexer.mu.Lock()
	require.Len(t, indexer.switchBranchCalls, 1, "SwitchBranch should be called once")
	assert.Equal(t, "feature", indexer.switchBranchCalls[0])
	indexer.mu.Unlock()
}

// Test: Context cancellation (clean shutdown of both watchers)
func TestWatchCoordinator_ContextCancellation(t *testing.T) {
	t.Parallel()

	coord, git, files, _, _ := setupCoordinator()

	ctx, cancel := context.WithCancel(context.Background())

	// Start coordinator
	done := make(chan error, 1)
	go func() {
		done <- coord.Start(ctx)
	}()

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for shutdown
	err := <-done
	assert.Equal(t, context.Canceled, err)

	// Verify both watchers stopped
	assert.True(t, git.stopCalled, "git watcher should be stopped")
	assert.True(t, files.stopCalled, "file watcher should be stopped")
}

// Test: File change during branch switch (queued until resume)
func TestWatchCoordinator_FileChangeDuringBranchSwitch(t *testing.T) {
	t.Parallel()

	coord, git, files, _, indexer := setupCoordinator()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger branch switch (pauses file watcher)
	git.triggerBranchSwitch("main", "feature")

	// Simulate file change during branch switch (should be queued)
	files.triggerFileChange([]string{"src/file.go"})

	// Give handler time to process
	time.Sleep(100 * time.Millisecond)

	// Verify Index() called with queued event
	indexer.mu.Lock()
	require.Len(t, indexer.indexCalls, 1, "Index should be called with queued event")
	assert.Equal(t, []string{"src/file.go"}, indexer.indexCalls[0])
	indexer.mu.Unlock()
}

// Test: Multiple file changes (batched into single Index call)
func TestWatchCoordinator_MultipleFileChanges(t *testing.T) {
	t.Parallel()

	coord, _, files, _, indexer := setupCoordinator()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger multiple file changes
	files1 := []string{"src/a.go", "src/b.go"}
	files2 := []string{"src/c.go"}

	files.triggerFileChange(files1)
	time.Sleep(50 * time.Millisecond)

	files.triggerFileChange(files2)
	time.Sleep(50 * time.Millisecond)

	// Verify Index() called for each batch
	indexer.mu.Lock()
	require.Len(t, indexer.indexCalls, 2, "Index should be called twice")
	assert.Equal(t, files1, indexer.indexCalls[0])
	assert.Equal(t, files2, indexer.indexCalls[1])
	indexer.mu.Unlock()
}

// Test: Error handling: indexer.Index() fails (log, continue)
func TestWatchCoordinator_IndexErrorDoesNotCrash(t *testing.T) {
	t.Parallel()

	coord, _, files, _, indexer := setupCoordinator()

	// Configure indexer to return error
	indexer.indexErr = errors.New("index failed")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger file change (should not crash coordinator)
	files.triggerFileChange([]string{"src/file.go"})

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify Index() was called despite error
	indexer.mu.Lock()
	assert.Len(t, indexer.indexCalls, 1, "Index should be called")
	indexer.mu.Unlock()

	// Coordinator should still be running
	cancel()
}

// Test: Error handling: branchSync.PrepareDB() fails (log, continue)
func TestWatchCoordinator_BranchSyncErrorDoesNotCrash(t *testing.T) {
	t.Parallel()

	coord, git, _, branchSync, indexer := setupCoordinator()

	// Configure branchSync to return error
	branchSync.prepareDBErr = errors.New("sync failed")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger branch switch (should not crash coordinator)
	git.triggerBranchSwitch("main", "feature")

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify SwitchBranch() still called despite sync error
	indexer.mu.Lock()
	assert.Len(t, indexer.switchBranchCalls, 1, "SwitchBranch should still be called")
	indexer.mu.Unlock()

	// Coordinator should still be running
	cancel()
}

// Test: Error handling: git watcher.Start() fails (propagate error)
func TestWatchCoordinator_GitWatcherStartError(t *testing.T) {
	t.Parallel()

	coord, git, _, _, _ := setupCoordinator()

	// Configure git watcher to fail on start
	git.startErr = errors.New("git watcher failed")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator (should return error immediately)
	err := coord.Start(ctx)
	require.Error(t, err)
	assert.Equal(t, "git watcher failed", err.Error())
}

// Test: Error handling: file watcher.Start() fails (propagate error)
func TestWatchCoordinator_FileWatcherStartError(t *testing.T) {
	t.Parallel()

	coord, _, files, _, _ := setupCoordinator()

	// Configure file watcher to fail on start
	files.startErr = errors.New("file watcher failed")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator (should return error immediately)
	err := coord.Start(ctx)
	require.Error(t, err)
	assert.Equal(t, "file watcher failed", err.Error())
}

// Test: Empty file change list (no-op)
func TestWatchCoordinator_EmptyFileChangeList(t *testing.T) {
	t.Parallel()

	coord, _, files, _, indexer := setupCoordinator()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger empty file change
	files.triggerFileChange([]string{})

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify Index() NOT called for empty list
	indexer.mu.Lock()
	assert.Len(t, indexer.indexCalls, 0, "Index should not be called for empty file list")
	indexer.mu.Unlock()
}

// Test: SwitchBranch error (should log but not crash)
func TestWatchCoordinator_SwitchBranchError(t *testing.T) {
	t.Parallel()

	coord, git, files, _, indexer := setupCoordinator()

	// Configure indexer to fail on branch switch
	indexer.switchBranchErr = errors.New("switch failed")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start coordinator
	go coord.Start(ctx)

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Trigger branch switch (should not crash coordinator)
	git.triggerBranchSwitch("main", "feature")

	// Give handler time to process
	time.Sleep(50 * time.Millisecond)

	// Verify resume still called despite error
	files.mu.Lock()
	assert.Equal(t, 1, files.resumeCount, "Resume should still be called")
	files.mu.Unlock()

	// Coordinator should still be running
	cancel()
}

// Test: Cleanup errors don't panic
func TestWatchCoordinator_CleanupErrorsDontPanic(t *testing.T) {
	t.Parallel()

	coord, git, files, _, _ := setupCoordinator()

	// Configure both watchers to fail on stop
	git.stopErr = errors.New("git stop failed")
	files.stopErr = errors.New("file stop failed")

	ctx, cancel := context.WithCancel(context.Background())

	// Start coordinator
	done := make(chan error, 1)
	go func() {
		done <- coord.Start(ctx)
	}()

	// Give coordinator time to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context (should not panic despite stop errors)
	cancel()

	// Wait for shutdown
	err := <-done
	assert.Equal(t, context.Canceled, err)
}
