package mcp

// Test Plan for Data Models:
// - ContextChunk MarshalJSON excludes embedding field
// - ContextChunk MarshalJSON includes all other fields
// - SearchOptions defaults are set correctly
// - MCPServerConfig defaults are set correctly
// - All model fields serialize/deserialize properly

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: ContextChunk MarshalJSON excludes embedding field
func TestContextChunk_MarshalJSON_ExcludesEmbedding(t *testing.T) {
	t.Parallel()

	now := time.Now()
	chunk := &ContextChunk{
		ID:        "test-id",
		Title:     "Test Chunk",
		Text:      "Test content",
		ChunkType: "symbols",
		Embedding: []float32{0.1, 0.2, 0.3},
		Tags:      []string{"go", "code"},
		Metadata: map[string]interface{}{
			"file_path": "test.go",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(chunk)
	require.NoError(t, err)

	// Parse back to verify embedding is excluded
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Verify embedding is not present
	_, hasEmbedding := result["embedding"]
	assert.False(t, hasEmbedding, "embedding field should be excluded from JSON")

	// Verify other fields are present
	assert.Equal(t, "test-id", result["id"])
	assert.Equal(t, "Test Chunk", result["title"])
	assert.Equal(t, "Test content", result["text"])
	assert.Equal(t, "symbols", result["chunk_type"])
}

// Test: ContextChunk MarshalJSON includes all non-embedding fields
func TestContextChunk_MarshalJSON_IncludesAllOtherFields(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 26, 12, 0, 0, 0, time.UTC)
	chunk := &ContextChunk{
		ID:        "test-id",
		Title:     "Test Chunk",
		Text:      "Test content",
		ChunkType: "documentation",
		Embedding: []float32{0.1, 0.2, 0.3}, // Should be excluded
		Tags:      []string{"docs", "readme"},
		Metadata: map[string]interface{}{
			"file_path": "README.md",
			"source":    "documentation",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	data, err := json.Marshal(chunk)
	require.NoError(t, err)

	var result ContextChunk
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, "test-id", result.ID)
	assert.Equal(t, "Test Chunk", result.Title)
	assert.Equal(t, "Test content", result.Text)
	assert.Equal(t, "documentation", result.ChunkType)
	assert.Equal(t, []string{"docs", "readme"}, result.Tags)
	assert.Equal(t, "README.md", result.Metadata["file_path"])
	assert.Empty(t, result.Embedding, "embedding should not be deserialized")
}

// Test: DefaultSearchOptions sets correct defaults
func TestDefaultSearchOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultSearchOptions()

	assert.NotNil(t, opts)
	assert.Equal(t, 15, opts.Limit)
	assert.Equal(t, 0.0, opts.MinScore)
	assert.Nil(t, opts.Tags)
	assert.Nil(t, opts.ChunkTypes)
}

// Test: DefaultMCPServerConfig sets correct defaults
func TestDefaultMCPServerConfig(t *testing.T) {
	t.Parallel()

	config := DefaultMCPServerConfig()

	assert.NotNil(t, config)
	assert.Equal(t, ".cortex/chunks", config.ChunksDir)
	assert.NotNil(t, config.EmbeddingService)
	assert.Equal(t, fmt.Sprintf("http://%s:%d", embed.DefaultEmbedServerHost, embed.DefaultEmbedServerPort), config.EmbeddingService.BaseURL)
	// Note: Dimensions are now read from chunk file metadata, not config
}

// Test: SearchOptions with custom values
func TestSearchOptions_CustomValues(t *testing.T) {
	t.Parallel()

	opts := &SearchOptions{
		Limit:      25,
		MinScore:   0.7,
		Tags:       []string{"go", "auth"},
		ChunkTypes: []string{"symbols", "definitions"},
	}

	data, err := json.Marshal(opts)
	require.NoError(t, err)

	var result SearchOptions
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	assert.Equal(t, 25, result.Limit)
	assert.Equal(t, 0.7, result.MinScore)
	assert.Equal(t, []string{"go", "auth"}, result.Tags)
	assert.Equal(t, []string{"symbols", "definitions"}, result.ChunkTypes)
}

// Test: SearchResult serialization
func TestSearchResult_Serialization(t *testing.T) {
	t.Parallel()

	now := time.Now()
	result := &SearchResult{
		Chunk: &ContextChunk{
			ID:        "result-1",
			Title:     "Result Chunk",
			Text:      "Result content",
			ChunkType: "symbols",
			Embedding: []float32{0.1, 0.2, 0.3}, // Should be excluded in JSON
			Tags:      []string{"go"},
			CreatedAt: now,
			UpdatedAt: now,
		},
		CombinedScore: 0.92,
	}

	data, err := json.Marshal(result)
	require.NoError(t, err)

	var parsed SearchResult
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "result-1", parsed.Chunk.ID)
	assert.Equal(t, 0.92, parsed.CombinedScore)
	assert.Empty(t, parsed.Chunk.Embedding, "embedding should be excluded")
}

// Test: CortexSearchRequest validation
func TestCortexSearchRequest_AllFields(t *testing.T) {
	t.Parallel()

	req := &CortexSearchRequest{
		Query:      "authentication handler",
		Limit:      20,
		Tags:       []string{"go", "auth"},
		ChunkTypes: []string{"symbols"},
	}

	data, err := json.Marshal(req)
	require.NoError(t, err)

	var parsed CortexSearchRequest
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, "authentication handler", parsed.Query)
	assert.Equal(t, 20, parsed.Limit)
	assert.Equal(t, []string{"go", "auth"}, parsed.Tags)
	assert.Equal(t, []string{"symbols"}, parsed.ChunkTypes)
}

// Test: CortexSearchResponse with multiple results
func TestCortexSearchResponse_MultipleResults(t *testing.T) {
	t.Parallel()

	resp := &CortexSearchResponse{
		Results: []*SearchResult{
			{
				Chunk: &ContextChunk{
					ID:    "chunk-1",
					Title: "First Result",
					Text:  "Content 1",
				},
				CombinedScore: 0.95,
			},
			{
				Chunk: &ContextChunk{
					ID:    "chunk-2",
					Title: "Second Result",
					Text:  "Content 2",
				},
				CombinedScore: 0.87,
			},
		},
		Total: 2,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var parsed CortexSearchResponse
	err = json.Unmarshal(data, &parsed)
	require.NoError(t, err)

	assert.Equal(t, 2, parsed.Total)
	assert.Len(t, parsed.Results, 2)
	assert.Equal(t, "chunk-1", parsed.Results[0].Chunk.ID)
	assert.Equal(t, 0.95, parsed.Results[0].CombinedScore)
}
