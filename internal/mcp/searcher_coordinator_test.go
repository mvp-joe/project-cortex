//go:build !integration

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmbeddingProvider is a simple mock for testing
type mockEmbeddingProvider struct{}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, texts []string, mode string) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))
	for i := range texts {
		embeddings[i] = make([]float32, 384)
	}
	return embeddings, nil
}

func (m *mockEmbeddingProvider) Dimensions() int {
	return 384
}

func (m *mockEmbeddingProvider) Close() error {
	return nil
}

func TestSearcherCoordinator_InitialLoad(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create temp directory and setup git repo (required for cache)
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	// Write test chunk files to SQLite cache
	baseTime := time.Now()
	allChunks := []*ContextChunk{}
	allChunks = append(allChunks, createTestChunksWithPrefix("symbols", 2, baseTime, "symbols")...)
	allChunks = append(allChunks, createTestChunksWithPrefix("definitions", 2, baseTime, "definitions")...)
	allChunks = append(allChunks, createTestChunksWithPrefix("data", 1, baseTime, "data")...)
	allChunks = append(allChunks, createTestChunksWithPrefix("documentation", 1, baseTime, "doc")...)
	require.NoError(t, writeChunkFile(tmpDir, "", allChunks, "mixed"))

	// Create chunk manager
	chunkManager := NewChunkManager(tmpDir)

	// Load initial chunks
	initialSet, err := chunkManager.Load(ctx)
	require.NoError(t, err)
	require.NotNil(t, initialSet)

	// Create mock provider
	provider := &mockEmbeddingProvider{}

	// Create config
	config := &MCPServerConfig{
		ProjectPath: tmpDir,
	}

	// Create chromem searcher
	chromemSearcher, err := newChromemSearcherWithChunkManager(ctx, config, provider, chunkManager, initialSet)
	require.NoError(t, err)
	require.NotNil(t, chromemSearcher)
	defer chromemSearcher.Close()

	// Create exact searcher
	exactSearcher, err := NewExactSearcher(ctx, initialSet)
	require.NoError(t, err)
	require.NotNil(t, exactSearcher)
	defer exactSearcher.Close()

	// Create coordinator
	coordinator := NewSearcherCoordinator(chunkManager, chromemSearcher, exactSearcher)
	require.NotNil(t, coordinator)
	defer coordinator.Close()

	// Verify coordinator provides access to searchers
	assert.NotNil(t, coordinator.GetChromemSearcher())
	assert.NotNil(t, coordinator.GetExactSearcher())
}

func TestSearcherCoordinator_Reload(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create temp directory and setup git repo
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	// Write initial chunk files to SQLite cache
	baseTime := time.Now()
	initialChunks := []*ContextChunk{}
	initialChunks = append(initialChunks, createTestChunksWithPrefix("symbols", 2, baseTime, "symbols")...)
	initialChunks = append(initialChunks, createTestChunksWithPrefix("definitions", 2, baseTime, "definitions")...)
	initialChunks = append(initialChunks, createTestChunksWithPrefix("data", 1, baseTime, "data")...)
	initialChunks = append(initialChunks, createTestChunksWithPrefix("documentation", 1, baseTime, "doc")...)
	require.NoError(t, writeChunkFile(tmpDir, "", initialChunks, "mixed"))

	// Create chunk manager
	chunkManager := NewChunkManager(tmpDir)

	// Load initial chunks
	initialSet, err := chunkManager.Load(ctx)
	require.NoError(t, err)

	// Create mock provider
	provider := &mockEmbeddingProvider{}

	// Create config
	config := &MCPServerConfig{
		ProjectPath: tmpDir,
	}

	// Create chromem searcher
	chromemSearcher, err := newChromemSearcherWithChunkManager(ctx, config, provider, chunkManager, initialSet)
	require.NoError(t, err)
	defer chromemSearcher.Close()

	// Create exact searcher
	exactSearcher, err := NewExactSearcher(ctx, initialSet)
	require.NoError(t, err)
	defer exactSearcher.Close()

	// Create coordinator
	coordinator := NewSearcherCoordinator(chunkManager, chromemSearcher, exactSearcher)
	defer coordinator.Close()

	// Get initial chunk count
	initialCount := chunkManager.GetCurrent().Len()

	// Modify chunk files (simulate file watcher trigger)
	time.Sleep(10 * time.Millisecond) // Ensure different timestamp
	updatedChunks := []*ContextChunk{}
	updatedChunks = append(updatedChunks, createTestChunksWithPrefix("symbols", 3, time.Now(), "symbols")...) // Add one more
	updatedChunks = append(updatedChunks, createTestChunksWithPrefix("definitions", 2, baseTime, "definitions")...)
	updatedChunks = append(updatedChunks, createTestChunksWithPrefix("data", 1, baseTime, "data")...)
	updatedChunks = append(updatedChunks, createTestChunksWithPrefix("documentation", 1, baseTime, "doc")...)
	require.NoError(t, writeChunkFile(tmpDir, "", updatedChunks, "mixed"))

	// Trigger reload
	err = coordinator.Reload(ctx)
	require.NoError(t, err)

	// Verify reload updated chunk manager (should have 1 more chunk)
	currentCount := chunkManager.GetCurrent().Len()
	assert.Equal(t, initialCount+1, currentCount, "Chunk count should increase by 1")

	// Verify metrics were recorded
	metrics := coordinator.GetMetrics()
	assert.NotZero(t, metrics.TotalReloads)
	assert.NotZero(t, metrics.SuccessfulReloads)
	assert.Zero(t, metrics.FailedReloads)
}

func TestSearcherCoordinator_IncrementalUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create temp directory and setup git repo
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	// Write initial chunks with specific content
	baseTime := time.Now()
	initialChunks := []*ContextChunk{
		createTestChunk("chunk-1", "definitions", "test1.go", baseTime),
		createTestChunk("chunk-2", "definitions", "test2.go", baseTime),
	}
	initialChunks = append(initialChunks, createTestChunksWithPrefix("symbols", 1, baseTime, "symbols")...)
	initialChunks = append(initialChunks, createTestChunksWithPrefix("data", 1, baseTime, "data")...)
	initialChunks = append(initialChunks, createTestChunksWithPrefix("documentation", 1, baseTime, "doc")...)
	require.NoError(t, writeChunkFile(tmpDir, "", initialChunks, "mixed"))

	// Create chunk manager
	chunkManager := NewChunkManager(tmpDir)

	// Load initial chunks
	initialSet, err := chunkManager.Load(ctx)
	require.NoError(t, err)

	// Create mock provider
	provider := &mockEmbeddingProvider{}

	// Create config
	config := &MCPServerConfig{
		ProjectPath: tmpDir,
	}

	// Create chromem searcher
	chromemSearcher, err := newChromemSearcherWithChunkManager(ctx, config, provider, chunkManager, initialSet)
	require.NoError(t, err)
	defer chromemSearcher.Close()

	// Create exact searcher
	exactSearcher, err := NewExactSearcher(ctx, initialSet)
	require.NoError(t, err)
	defer exactSearcher.Close()

	// Create coordinator
	coordinator := NewSearcherCoordinator(chunkManager, chromemSearcher, exactSearcher)
	defer coordinator.Close()

	// Wait for proper timestamp differentiation
	time.Sleep(10 * time.Millisecond)

	// Write updated chunks (add, update, delete)
	updateTime := time.Now()
	updatedChunks := []*ContextChunk{
		createTestChunk("chunk-1", "definitions", "test1.go", updateTime), // Updated (newer timestamp)
		createTestChunk("chunk-3", "definitions", "test3.go", updateTime), // Added
		// chunk-2 deleted
	}
	updatedChunks = append(updatedChunks, createTestChunksWithPrefix("symbols", 1, baseTime, "symbols")...)
	updatedChunks = append(updatedChunks, createTestChunksWithPrefix("data", 1, baseTime, "data")...)
	updatedChunks = append(updatedChunks, createTestChunksWithPrefix("documentation", 1, baseTime, "doc")...)
	require.NoError(t, writeChunkFile(tmpDir, "", updatedChunks, "mixed"))

	// Trigger reload
	err = coordinator.Reload(ctx)
	require.NoError(t, err)

	// Verify metrics show successful reload
	metrics := coordinator.GetMetrics()
	assert.Equal(t, int64(1), metrics.TotalReloads)
	assert.Equal(t, int64(1), metrics.SuccessfulReloads)
}

func TestSearcherCoordinator_GetMetrics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create minimal setup
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	baseTime := time.Now()
	allChunks := []*ContextChunk{}
	allChunks = append(allChunks, createTestChunksWithPrefix("symbols", 1, baseTime, "symbols")...)
	allChunks = append(allChunks, createTestChunksWithPrefix("definitions", 1, baseTime, "definitions")...)
	allChunks = append(allChunks, createTestChunksWithPrefix("data", 1, baseTime, "data")...)
	allChunks = append(allChunks, createTestChunksWithPrefix("documentation", 1, baseTime, "doc")...)
	require.NoError(t, writeChunkFile(tmpDir, "", allChunks, "mixed"))

	chunkManager := NewChunkManager(tmpDir)
	initialSet, err := chunkManager.Load(ctx)
	require.NoError(t, err)

	provider := &mockEmbeddingProvider{}
	config := &MCPServerConfig{ProjectPath: tmpDir}

	chromemSearcher, err := newChromemSearcherWithChunkManager(ctx, config, provider, chunkManager, initialSet)
	require.NoError(t, err)
	defer chromemSearcher.Close()

	exactSearcher, err := NewExactSearcher(ctx, initialSet)
	require.NoError(t, err)
	defer exactSearcher.Close()

	coordinator := NewSearcherCoordinator(chunkManager, chromemSearcher, exactSearcher)
	defer coordinator.Close()

	// Get initial metrics
	metrics := coordinator.GetMetrics()
	assert.Zero(t, metrics.TotalReloads, "Should have no reloads initially")

	// Perform reload
	err = coordinator.Reload(ctx)
	require.NoError(t, err)

	// Get updated metrics
	metrics = coordinator.GetMetrics()
	assert.Equal(t, int64(1), metrics.TotalReloads)
	assert.Equal(t, int64(1), metrics.SuccessfulReloads)
	assert.NotZero(t, metrics.CurrentChunkCount)
	assert.NotZero(t, metrics.LastReloadTime)
}

// Helper functions
// Note: Test helper functions (setupGitRepo, createTestChunk, etc.) are defined in chunk_manager_test.go
// and shared across test files in this package
