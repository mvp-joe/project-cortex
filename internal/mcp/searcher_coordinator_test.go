//go:build !integration

package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	// Create temp directory with test chunk files
	tmpDir := t.TempDir()

	// Write test chunk files
	writeTestChunkFile(t, tmpDir, "code-symbols.json")
	writeTestChunkFile(t, tmpDir, "code-definitions.json")
	writeTestChunkFile(t, tmpDir, "code-data.json")
	writeTestChunkFile(t, tmpDir, "doc-chunks.json")

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
		ChunksDir: tmpDir,
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

	// Create temp directory with test chunk files
	tmpDir := t.TempDir()

	// Write initial chunk files
	writeTestChunkFile(t, tmpDir, "code-symbols.json")
	writeTestChunkFile(t, tmpDir, "code-definitions.json")
	writeTestChunkFile(t, tmpDir, "code-data.json")
	writeTestChunkFile(t, tmpDir, "doc-chunks.json")

	// Create chunk manager
	chunkManager := NewChunkManager(tmpDir)

	// Load initial chunks
	initialSet, err := chunkManager.Load(ctx)
	require.NoError(t, err)

	// Create mock provider
	provider := &mockEmbeddingProvider{}

	// Create config
	config := &MCPServerConfig{
		ChunksDir: tmpDir,
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
	writeTestChunkFile(t, tmpDir, "code-symbols.json")

	// Trigger reload
	err = coordinator.Reload(ctx)
	require.NoError(t, err)

	// Verify reload updated chunk manager
	currentCount := chunkManager.GetCurrent().Len()
	assert.Equal(t, initialCount, currentCount, "Chunk count should remain same for same file content")

	// Verify metrics were recorded
	metrics := coordinator.GetMetrics()
	assert.NotZero(t, metrics.TotalReloads)
	assert.NotZero(t, metrics.SuccessfulReloads)
	assert.Zero(t, metrics.FailedReloads)
}

func TestSearcherCoordinator_IncrementalUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create temp directory
	tmpDir := t.TempDir()

	// Write initial chunks with specific content
	writeTestChunkFileWithContent(t, tmpDir, "code-definitions.json", []TestChunk{
		{
			ID:   "chunk-1",
			Text: "Initial text",
		},
		{
			ID:   "chunk-2",
			Text: "Another chunk",
		},
	})
	writeTestChunkFile(t, tmpDir, "code-symbols.json")
	writeTestChunkFile(t, tmpDir, "code-data.json")
	writeTestChunkFile(t, tmpDir, "doc-chunks.json")

	// Create chunk manager
	chunkManager := NewChunkManager(tmpDir)

	// Load initial chunks
	initialSet, err := chunkManager.Load(ctx)
	require.NoError(t, err)

	// Create mock provider
	provider := &mockEmbeddingProvider{}

	// Create config
	config := &MCPServerConfig{
		ChunksDir: tmpDir,
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
	writeTestChunkFileWithContent(t, tmpDir, "code-definitions.json", []TestChunk{
		{
			ID:   "chunk-1",
			Text: "Updated text", // Updated
		},
		{
			ID:   "chunk-3",
			Text: "New chunk", // Added
		},
		// chunk-2 deleted
	})

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
	writeTestChunkFile(t, tmpDir, "code-symbols.json")
	writeTestChunkFile(t, tmpDir, "code-definitions.json")
	writeTestChunkFile(t, tmpDir, "code-data.json")
	writeTestChunkFile(t, tmpDir, "doc-chunks.json")

	chunkManager := NewChunkManager(tmpDir)
	initialSet, err := chunkManager.Load(ctx)
	require.NoError(t, err)

	provider := &mockEmbeddingProvider{}
	config := &MCPServerConfig{ChunksDir: tmpDir}

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

type TestChunk struct {
	ID   string
	Text string
}

func writeTestChunkFile(t *testing.T, dir, filename string) {
	t.Helper()
	writeTestChunkFileWithContent(t, dir, filename, nil)
}

func writeTestChunkFileWithContent(t *testing.T, dir, filename string, chunks []TestChunk) {
	t.Helper()

	// Default chunks if none provided
	if chunks == nil {
		chunks = []TestChunk{
			{ID: "test-1", Text: "Test chunk content"},
		}
	}

	// Build JSON content
	content := `{
  "_metadata": {
    "model": "test-model",
    "dimensions": 384,
    "chunk_type": "test",
    "generated": "2025-01-01T00:00:00Z",
    "version": "1.0.0"
  },
  "chunks": [`

	for i, chunk := range chunks {
		if i > 0 {
			content += ","
		}
		content += fmt.Sprintf(`
    {
      "id": "%s",
      "title": "Test",
      "text": "%s",
      "chunk_type": "definitions",
      "tags": ["test"],
      "metadata": {"file_path": "test.go"},
      "embedding": %s,
      "created_at": "2025-01-01T00:00:00Z",
      "updated_at": "%s"
    }`, chunk.ID, chunk.Text, buildZeroEmbedding(), time.Now().Format(time.RFC3339))
	}

	content += `
  ]
}`

	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}

func buildZeroEmbedding() string {
	// Build a JSON array of 384 zeros
	result := "["
	for i := 0; i < 384; i++ {
		if i > 0 {
			result += ","
		}
		result += "0.0"
	}
	result += "]"
	return result
}
