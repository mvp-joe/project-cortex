//go:build integration

package mcp

// Test Plan for Metrics Integration:
// - Search response includes metrics when include_stats=true
// - Search response excludes metrics when include_stats=false
// - Metrics reflect actual reload operations
// - Metrics track chunk count correctly

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsIntegration_SearchResponseIncludesMetrics(t *testing.T) {
	t.Parallel()

	// Test: When include_stats=true, search response includes reload metrics
	ctx := context.Background()
	chunksDir := setupTestChunksDir(t, 10)

	// Create searcher (triggers initial reload)
	searcher, err := NewChromemSearcher(ctx, &MCPServerConfig{ProjectPath: chunksDir}, &mockEmbeddingProvider{dims: 384})
	require.NoError(t, err)
	defer searcher.Close()

	// Wait for initial load to complete
	time.Sleep(50 * time.Millisecond)

	// Get metrics
	metrics := searcher.GetMetrics()

	// Verify metrics are populated
	assert.Equal(t, int64(1), metrics.TotalReloads, "should have 1 reload from initialization")
	assert.Equal(t, int64(1), metrics.SuccessfulReloads, "initial reload should succeed")
	assert.Equal(t, int64(0), metrics.FailedReloads, "no failed reloads")
	assert.Equal(t, 10, metrics.CurrentChunkCount, "should have 10 chunks loaded")
	assert.Greater(t, metrics.LastReloadDuration, time.Duration(0), "reload duration should be > 0")
	assert.False(t, metrics.LastReloadTime.IsZero(), "reload time should be set")
	assert.Empty(t, metrics.LastReloadError, "no error on successful reload")
}

func TestMetricsIntegration_ReloadUpdatesMetrics(t *testing.T) {
	t.Parallel()

	// Test: Calling Reload() updates metrics correctly
	ctx := context.Background()
	chunksDir := setupTestChunksDir(t, 5)

	searcher, err := NewChromemSearcher(ctx, &MCPServerConfig{ProjectPath: chunksDir}, &mockEmbeddingProvider{dims: 384})
	require.NoError(t, err)
	defer searcher.Close()

	// Perform additional reload
	time.Sleep(10 * time.Millisecond) // Small delay to differentiate timestamps
	err = searcher.Reload(ctx)
	require.NoError(t, err)

	metrics := searcher.GetMetrics()
	assert.Equal(t, int64(2), metrics.TotalReloads, "should have 2 reloads (init + manual)")
	assert.Equal(t, int64(2), metrics.SuccessfulReloads, "both should succeed")
	assert.Equal(t, 5, metrics.CurrentChunkCount, "chunk count should remain 5")
}

func TestMetricsIntegration_ReloadFailureTracked(t *testing.T) {
	t.Parallel()

	// Test: Failed reload is tracked in metrics with error message
	ctx := context.Background()
	chunksDir := setupTestChunksDir(t, 3)

	searcher, err := NewChromemSearcher(ctx, &MCPServerConfig{ProjectPath: chunksDir}, &mockEmbeddingProvider{dims: 384})
	require.NoError(t, err)
	defer searcher.Close()

	// Delete chunks directory to cause reload failure
	require.NoError(t, os.RemoveAll(chunksDir))

	// Attempt reload (should fail)
	err = searcher.Reload(ctx)
	assert.Error(t, err, "reload should fail after directory removal")

	metrics := searcher.GetMetrics()
	assert.Equal(t, int64(2), metrics.TotalReloads, "should have 2 reload attempts")
	assert.Equal(t, int64(1), metrics.SuccessfulReloads, "only initial load succeeded")
	assert.Equal(t, int64(1), metrics.FailedReloads, "manual reload failed")
	assert.NotEmpty(t, metrics.LastReloadError, "error message should be captured")
	assert.Equal(t, 0, metrics.CurrentChunkCount, "chunk count should be 0 after failed reload")
}

func TestMetricsIntegration_JSONSerialization(t *testing.T) {
	t.Parallel()

	// Test: MetricsSnapshot serializes correctly to JSON
	ctx := context.Background()
	chunksDir := setupTestChunksDir(t, 8)

	searcher, err := NewChromemSearcher(ctx, &MCPServerConfig{ProjectPath: chunksDir}, &mockEmbeddingProvider{dims: 384})
	require.NoError(t, err)
	defer searcher.Close()

	metrics := searcher.GetMetrics()

	// Serialize to JSON
	jsonData, err := json.Marshal(metrics)
	require.NoError(t, err)

	// Verify JSON structure
	var decoded map[string]interface{}
	require.NoError(t, json.Unmarshal(jsonData, &decoded))

	assert.Contains(t, decoded, "total_reloads")
	assert.Contains(t, decoded, "successful_reloads")
	assert.Contains(t, decoded, "failed_reloads")
	assert.Contains(t, decoded, "current_chunk_count")
	assert.Contains(t, decoded, "last_reload_duration_ms")
	assert.Contains(t, decoded, "last_reload_time")

	// Verify values
	assert.Equal(t, float64(1), decoded["total_reloads"])
	assert.Equal(t, float64(1), decoded["successful_reloads"])
	assert.Equal(t, float64(0), decoded["failed_reloads"])
	assert.Equal(t, float64(8), decoded["current_chunk_count"])
}

func TestMetricsIntegration_ResponseWithAndWithoutStats(t *testing.T) {
	t.Parallel()

	// Test: Response serialization includes/excludes metrics based on flag
	ctx := context.Background()
	chunksDir := setupTestChunksDir(t, 5)

	searcher, err := NewChromemSearcher(ctx, &MCPServerConfig{ProjectPath: chunksDir}, &mockEmbeddingProvider{dims: 384})
	require.NoError(t, err)
	defer searcher.Close()

	// Create response WITHOUT metrics
	response1 := &CortexSearchResponse{
		Results: []*SearchResult{},
		Total:   0,
		Metrics: nil,
	}
	json1, err := json.Marshal(response1)
	require.NoError(t, err)
	assert.NotContains(t, string(json1), "metrics", "should not include metrics field when nil")

	// Create response WITH metrics
	metrics := searcher.GetMetrics()
	response2 := &CortexSearchResponse{
		Results: []*SearchResult{},
		Total:   0,
		Metrics: &metrics,
	}
	json2, err := json.Marshal(response2)
	require.NoError(t, err)
	assert.Contains(t, string(json2), "metrics", "should include metrics field when set")
	assert.Contains(t, string(json2), "total_reloads")
	assert.Contains(t, string(json2), "current_chunk_count")
}

// Helper functions

func setupTestChunksDir(t *testing.T, numChunks int) string {
	t.Helper()

	chunksDir := t.TempDir()
	require.NoError(t, os.MkdirAll(chunksDir, 0755))

	// Build chunks JSON manually (ContextChunk.MarshalJSON excludes embeddings)
	var chunksJSON string
	for i := 0; i < numChunks; i++ {
		if i > 0 {
			chunksJSON += ","
		}
		chunksJSON += `{
			"id": "test-chunk-` + string(rune('a'+i)) + `",
			"text": "test content ` + string(rune('a'+i)) + `",
			"embedding": [` + generateEmbeddingJSON(384) + `],
			"created_at": "` + time.Now().Format(time.RFC3339) + `",
			"updated_at": "` + time.Now().Format(time.RFC3339) + `"
		}`
	}

	// Wrap in chunk file format with metadata
	fileJSON := `{
		"_metadata": {
			"model": "test-model",
			"dimensions": 384,
			"chunk_type": "test",
			"generated": "` + time.Now().Format(time.RFC3339) + `",
			"version": "1.0.0"
		},
		"chunks": [` + chunksJSON + `]
	}`

	chunkFile := filepath.Join(chunksDir, "test.json")
	require.NoError(t, os.WriteFile(chunkFile, []byte(fileJSON), 0644))

	return chunksDir
}

type mockEmbeddingProvider struct {
	dims int
}

func (m *mockEmbeddingProvider) Initialize(ctx context.Context) error {
	return nil
}

func (m *mockEmbeddingProvider) Embed(ctx context.Context, texts []string, mode embed.EmbedMode) ([][]float32, error) {
	result := make([][]float32, len(texts))
	for i := range texts {
		result[i] = make([]float32, m.dims)
	}
	return result, nil
}

func (m *mockEmbeddingProvider) Dimensions() int {
	return m.dims
}

func (m *mockEmbeddingProvider) Close() error {
	return nil
}
