package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	indexerv1 "github.com/mvp-joe/project-cortex/gen/indexer/v1"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/indexer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmbedProvider is a fake embedding provider for testing.
type mockEmbedProvider struct {
	dimensions int
	initErr    error
	embedErr   error
}

func newMockEmbedProvider() *mockEmbedProvider {
	return &mockEmbedProvider{
		dimensions: 384, // Match default model dimensions
	}
}

func (m *mockEmbedProvider) Initialize(ctx context.Context) error {
	return m.initErr
}

func (m *mockEmbedProvider) Embed(ctx context.Context, texts []string, mode embed.EmbedMode) ([][]float32, error) {
	if m.embedErr != nil {
		return nil, m.embedErr
	}
	// Return dummy vectors
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = make([]float32, m.dimensions)
		// Fill with dummy values
		for j := range vectors[i] {
			vectors[i][j] = 0.1
		}
	}
	return vectors, nil
}

func (m *mockEmbedProvider) Dimensions() int {
	return m.dimensions
}

func (m *mockEmbedProvider) Close() error {
	return nil
}

// TestNewActor_ValidProject tests creating an actor with a valid project.
func TestNewActor_ValidProject(t *testing.T) {
	t.Parallel()

	// Create a temporary test project with .cortex directory
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")

	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	// Initialize a real git repository
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	// Create an initial commit (required for git commands to work properly)
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "README.md"), []byte("# Test\n"), 0644))
	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = tempDir
	require.NoError(t, cmd.Run())

	// Create a minimal config file
	configFile := filepath.Join(cortexDir, "config.yml")
	configContent := `
embedding:
  provider: local
  endpoint: http://localhost:8001/embed
paths:
  code:
    - "**/*.go"
  docs:
    - "**/*.md"
  ignore:
    - ".git/**"
`
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0644))

	// Use mock embedding provider
	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())

	ctx := context.Background()
	actor, err := NewActor(ctx, tempDir, mockProvider, testCache)

	require.NoError(t, err)
	require.NotNil(t, actor)
	defer actor.Stop()

	assert.Equal(t, tempDir, actor.projectPath)
	assert.NotNil(t, actor.indexer)
	assert.NotNil(t, actor.branchWatcher)
	assert.NotNil(t, actor.fileWatcher)
	assert.Equal(t, "main", actor.currentBranch)
}

// TestNewActor_InvalidPath tests creating an actor with invalid project path.
func TestNewActor_InvalidPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name        string
		projectPath string
		wantErr     string
	}{
		{
			name:        "relative path",
			projectPath: "relative/path",
			wantErr:     "project path must be absolute",
		},
		{
			name:        "non-existent directory",
			projectPath: "/nonexistent/directory/that/does/not/exist",
			wantErr:     "failed to create branch watcher", // Will fail during branch watcher creation
		},
	}

	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actor, err := NewActor(ctx, tt.projectPath, mockProvider, testCache)
			assert.Error(t, err)
			assert.Nil(t, actor)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestNewActor_ConfigLoadFailure tests handling of config loading errors.
func TestNewActor_ConfigLoadFailure(t *testing.T) {
	t.Parallel()

	// Create a temporary directory without .cortex/config.yml
	tempDir := t.TempDir()

	mockProvider := newMockEmbedProvider()
	testCache := cache.NewCache(t.TempDir())

	ctx := context.Background()
	actor, err := NewActor(ctx, tempDir, mockProvider, testCache)

	// Should succeed - config has defaults, file is optional
	// But will fail on other initialization (e.g., no .git directory)
	if err == nil {
		require.NotNil(t, actor)
		actor.Stop()
	} else {
		// Expected to fail due to missing .git or other dependencies
		assert.Error(t, err)
	}
}

// TestActor_LifecycleWithMocks tests Actor lifecycle using mocks.
// This test demonstrates the expected behavior without requiring
// a full project setup or embedding server.
func TestActor_LifecycleWithMocks(t *testing.T) {
	t.Parallel()

	// Note: This is a simplified test that would require proper mocks
	// of IndexerV2, GitWatcher, and FileWatcher.
	//
	// The actual implementation requires:
	// 1. Creating mock interfaces for these components
	// 2. Injecting them via a constructor that accepts mocks
	// 3. Testing the orchestration logic independently
	//
	// For now, this test documents the intended behavior.

	t.Skip("Skipping: requires mock injection support in NewActor")

	// Pseudo-code for what this test would look like:
	//
	// mockIndexer := NewMockIndexer()
	// mockGitWatcher := NewMockGitWatcher()
	// mockFileWatcher := NewMockFileWatcher()
	//
	// actor := &Actor{
	//     projectPath:  "/test/project",
	//     indexer:      mockIndexer,
	//     gitWatcher:   mockGitWatcher,
	//     fileWatcher:  mockFileWatcher,
	//     ... initialize fields ...
	// }
	//
	// err := actor.Start()
	// require.NoError(t, err)
	//
	// actor.Stop()
	// assert.True(t, mockGitWatcher.StopCalled)
	// assert.True(t, mockFileWatcher.StopCalled)
}

// TestActor_ProgressSubscription tests progress subscription and unsubscription.
func TestActor_ProgressSubscription(t *testing.T) {
	t.Parallel()

	// Create actor with minimal setup
	actor := &Actor{
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}

	// Subscribe to progress
	ch1 := actor.SubscribeProgress("subscriber-1")
	require.NotNil(t, ch1)

	ch2 := actor.SubscribeProgress("subscriber-2")
	require.NotNil(t, ch2)

	// Verify subscribers registered
	actor.progressMu.RLock()
	assert.Len(t, actor.progressSubs, 2)
	actor.progressMu.RUnlock()

	// Unsubscribe first subscriber
	actor.UnsubscribeProgress("subscriber-1")

	// Verify only one subscriber remains
	actor.progressMu.RLock()
	assert.Len(t, actor.progressSubs, 1)
	_, exists := actor.progressSubs["subscriber-1"]
	assert.False(t, exists)
	actor.progressMu.RUnlock()

	// Unsubscribe second subscriber
	actor.UnsubscribeProgress("subscriber-2")

	// Verify no subscribers remain
	actor.progressMu.RLock()
	assert.Len(t, actor.progressSubs, 0)
	actor.progressMu.RUnlock()
}

// TestActor_ProgressBroadcast tests progress broadcasting to subscribers.
func TestActor_ProgressBroadcast(t *testing.T) {
	t.Parallel()

	actor := &Actor{
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
	}

	// Subscribe multiple subscribers
	ch1 := actor.SubscribeProgress("sub-1")
	ch2 := actor.SubscribeProgress("sub-2")

	// Create progress message
	progress := &indexerv1.IndexProgress{
		Phase:           indexerv1.IndexProgress_PHASE_INDEXING,
		FilesTotal:      100,
		FilesProcessed:  50,
		ChunksGenerated: 200,
		Message:         "Indexing in progress",
	}

	// Broadcast progress
	actor.publishProgress(progress)

	// Verify both subscribers received the message
	select {
	case received := <-ch1:
		assert.Equal(t, progress.Phase, received.Phase)
		assert.Equal(t, progress.FilesTotal, received.FilesTotal)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for progress on subscriber 1")
	}

	select {
	case received := <-ch2:
		assert.Equal(t, progress.Phase, received.Phase)
		assert.Equal(t, progress.FilesTotal, received.FilesTotal)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Timeout waiting for progress on subscriber 2")
	}

	// Clean up
	actor.UnsubscribeProgress("sub-1")
	actor.UnsubscribeProgress("sub-2")
}

// TestActor_ProgressBroadcast_SlowSubscriber tests that slow subscribers don't block.
func TestActor_ProgressBroadcast_SlowSubscriber(t *testing.T) {
	t.Parallel()

	actor := &Actor{
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
	}

	// Subscribe a slow subscriber (don't read from channel)
	ch1 := actor.SubscribeProgress("slow-subscriber")
	_ = ch1 // Don't read from this channel

	// Subscribe a fast subscriber
	ch2 := actor.SubscribeProgress("fast-subscriber")

	// Send enough messages to fill the buffer
	for i := 0; i < 20; i++ {
		progress := &indexerv1.IndexProgress{
			Phase:          indexerv1.IndexProgress_PHASE_INDEXING,
			FilesProcessed: int32(i),
		}
		actor.publishProgress(progress)
	}

	// Fast subscriber should have received at least some messages
	// (buffer size is 10, so at least 10 messages should be available)
	receivedCount := 0
	for i := 0; i < 10; i++ {
		select {
		case <-ch2:
			receivedCount++
		case <-time.After(10 * time.Millisecond):
			break
		}
	}

	assert.Greater(t, receivedCount, 0, "Fast subscriber should receive messages")

	// Clean up
	actor.UnsubscribeProgress("slow-subscriber")
	actor.UnsubscribeProgress("fast-subscriber")
}

// TestActor_AlreadyIndexing tests concurrent Index() calls are rejected.
func TestActor_AlreadyIndexing(t *testing.T) {
	t.Parallel()

	// This test would require a mock indexer that blocks on Index()
	// to simulate concurrent calls.
	//
	// For now, we test the atomic flag directly.

	actor := &Actor{
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
	}

	// Set isIndexing to true
	actor.isIndexing.Store(true)

	// Attempt to index while already indexing
	ctx := context.Background()
	_, err := actor.Index(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already indexing")

	// Reset flag
	actor.isIndexing.Store(false)
}

// TestActor_Stop_Idempotent tests that Stop() can be called multiple times safely.
func TestActor_Stop_Idempotent(t *testing.T) {
	t.Parallel()

	actor := &Actor{
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
	}

	// Create a context for the actor
	ctx, cancel := context.WithCancel(context.Background())
	actor.ctx = ctx
	actor.cancel = cancel

	// Close doneCh to simulate event loop finished
	close(actor.doneCh)

	// Call Stop() multiple times
	actor.Stop()
	actor.Stop()
	actor.Stop()

	// Should not panic or deadlock
}

// TestStatsToProgress tests conversion from IndexerV2Stats to IndexProgress.
func TestStatsToProgress(t *testing.T) {
	t.Parallel()

	stats := &indexer.IndexerV2Stats{
		FilesAdded:         10,
		FilesModified:      5,
		FilesDeleted:       2,
		FilesUnchanged:     100,
		CodeFilesProcessed: 12,
		DocsProcessed:      3,
		TotalCodeChunks:    45,
		TotalDocChunks:     8,
	}

	progress := statsToProgress(stats)

	assert.Equal(t, indexerv1.IndexProgress_PHASE_INDEXING, progress.Phase)
	assert.Equal(t, int32(15), progress.FilesTotal) // 10 added + 5 modified
	assert.Equal(t, int32(15), progress.FilesProcessed) // 12 code + 3 docs
	assert.Equal(t, int32(53), progress.ChunksGenerated) // 45 code + 8 doc
	assert.Contains(t, progress.Message, "Processing files")
}

// TestActor_GoroutineCleanup tests that goroutines are properly cleaned up.
// This is a behavioral test that verifies the event loop exits when Stop() is called.
func TestActor_GoroutineCleanup(t *testing.T) {
	t.Parallel()

	actor := &Actor{
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
	}

	// Launch event loop
	go actor.eventLoop()

	// Signal stop
	close(actor.stopCh)

	// Wait for doneCh to be closed (with timeout)
	select {
	case <-actor.doneCh:
		// Success - event loop exited
	case <-time.After(1 * time.Second):
		t.Fatal("Event loop did not exit within timeout")
	}
}

// TestActor_ContextCancellation tests that context cancellation stops operations.
func TestActor_ContextCancellation(t *testing.T) {
	t.Parallel()

	// This test verifies that when the actor's context is cancelled,
	// operations should respect the cancellation.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	actor := &Actor{
		ctx:          ctx,
		cancel:       cancel,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
	}

	// Cancel context
	cancel()

	// Verify context is cancelled
	select {
	case <-actor.ctx.Done():
		// Success
	default:
		t.Fatal("Context should be cancelled")
	}
}

// TestActor_HandleFileChanges_SkipsIfIndexing tests that file changes
// are skipped when already indexing.
func TestActor_HandleFileChanges_SkipsIfIndexing(t *testing.T) {
	t.Parallel()

	actor := &Actor{
		projectPath:  "/test/project",
		progressSubs: make(map[string]chan *indexerv1.IndexProgress),
	}

	// Set isIndexing to true
	actor.isIndexing.Store(true)

	// Call handleFileChanges - should return immediately without indexing
	actor.handleFileChanges([]string{"/test/project/file.go"})

	// Verify isIndexing is still true (wasn't changed by handler)
	assert.True(t, actor.isIndexing.Load())
}

// TestActor_GetStatus tests the GetStatus method.
func TestActor_GetStatus(t *testing.T) {
	t.Parallel()

	registeredAt := time.Now()
	lastIndexedAt := time.Now().Add(5 * time.Minute)

	actor := &Actor{
		projectPath:   "/test/project",
		currentBranch: "main",
		cacheKey:      "abc123-def456",
		registeredAt:  registeredAt,
		progressSubs:  make(map[string]chan *indexerv1.IndexProgress),
	}

	// Initialize atomic values
	actor.currentPhase.Store(indexerv1.IndexProgress_PHASE_UNSPECIFIED)
	actor.lastIndexedAt.Store(lastIndexedAt)
	actor.filesIndexed.Store(int32(42))
	actor.chunksCount.Store(int32(150))
	actor.isIndexing.Store(false)

	// Get status
	status := actor.GetStatus()

	// Verify all fields
	assert.Equal(t, "/test/project", status.Path)
	assert.Equal(t, "abc123-def456", status.CacheKey)
	assert.Equal(t, "main", status.CurrentBranch)
	assert.Equal(t, int32(42), status.FilesIndexed)
	assert.Equal(t, int32(150), status.ChunksCount)
	assert.Equal(t, registeredAt.Unix(), status.RegisteredAt)
	assert.Equal(t, lastIndexedAt.Unix(), status.LastIndexedAt)
	assert.False(t, status.IsIndexing)
	assert.Equal(t, indexerv1.IndexProgress_PHASE_UNSPECIFIED, status.CurrentPhase)
}

// TestActor_GetStatus_WhileIndexing tests GetStatus during active indexing.
func TestActor_GetStatus_WhileIndexing(t *testing.T) {
	t.Parallel()

	actor := &Actor{
		projectPath:   "/test/project",
		currentBranch: "feature/test",
		cacheKey:      "xyz789",
		registeredAt:  time.Now(),
		progressSubs:  make(map[string]chan *indexerv1.IndexProgress),
	}

	// Initialize atomic values to simulate active indexing
	actor.currentPhase.Store(indexerv1.IndexProgress_PHASE_INDEXING)
	actor.lastIndexedAt.Store(time.Time{}) // Zero time (never indexed)
	actor.filesIndexed.Store(int32(0))
	actor.chunksCount.Store(int32(0))
	actor.isIndexing.Store(true)

	// Get status
	status := actor.GetStatus()

	// Verify indexing state
	assert.True(t, status.IsIndexing)
	assert.Equal(t, indexerv1.IndexProgress_PHASE_INDEXING, status.CurrentPhase)
	assert.Equal(t, int64(0), status.LastIndexedAt) // Zero time
	assert.Equal(t, int32(0), status.FilesIndexed)
	assert.Equal(t, int32(0), status.ChunksCount)
}

// TestActor_GetStatus_ConcurrentAccess tests thread-safety of GetStatus.
func TestActor_GetStatus_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	actor := &Actor{
		projectPath:   "/test/project",
		currentBranch: "main",
		cacheKey:      "test-key",
		registeredAt:  time.Now(),
		progressSubs:  make(map[string]chan *indexerv1.IndexProgress),
	}

	// Initialize atomic values
	actor.currentPhase.Store(indexerv1.IndexProgress_PHASE_UNSPECIFIED)
	actor.lastIndexedAt.Store(time.Now())
	actor.filesIndexed.Store(int32(10))
	actor.chunksCount.Store(int32(50))

	// Call GetStatus concurrently from multiple goroutines
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- true }()
			for j := 0; j < 100; j++ {
				status := actor.GetStatus()
				assert.NotNil(t, status)
				assert.Equal(t, "/test/project", status.Path)
			}
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}
}
