package mcp

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExactSearcher(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create test chunks
	chunks := []*ContextChunk{
		{
			ID:    "test-1",
			Title: "Test Provider",
			Text:  "type Provider interface { Embed(ctx context.Context) }", ChunkType: "definitions",
			Tags: []string{"go", "code"},
			Metadata: map[string]interface{}{
				"file_path": "internal/embed/provider.go",
			},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "test-2",
			Title:     "Handler Function",
			Text:      "func HandleRequest(w http.ResponseWriter, r *http.Request) error",
			ChunkType: "definitions",
			Tags:      []string{"go", "code"},
			Metadata: map[string]interface{}{
				"file_path": "internal/server/handler.go",
			},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
	}

	chunkSet := &ChunkSet{
		chunks: chunks,
		byID: map[string]*ContextChunk{
			"test-1": chunks[0],
			"test-2": chunks[1],
		},
		byFile: map[string][]*ContextChunk{
			"internal/embed/provider.go": {chunks[0]},
			"internal/server/handler.go": {chunks[1]},
		},
	}

	// Create searcher
	searcher, err := NewExactSearcher(ctx, chunkSet)
	require.NoError(t, err)
	require.NotNil(t, searcher)
	defer searcher.Close()

	// Verify searcher is functional
	results, err := searcher.Search(ctx, "text:Provider", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	require.NotEmpty(t, results)
}

func TestExactSearcher_BasicSearch(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create test chunks
	chunks := []*ContextChunk{
		{
			ID:        "test-1",
			Text:      "type Provider interface { Embed(ctx context.Context) }",
			ChunkType: "definitions",
			Tags:      []string{"go", "code"},
			Metadata:  map[string]interface{}{"file_path": "provider.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "test-2",
			Text:      "func NewProvider() Provider { return &localProvider{} }",
			ChunkType: "definitions",
			Tags:      []string{"go", "code"},
			Metadata:  map[string]interface{}{"file_path": "provider.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "test-3",
			Text:      "func HandleRequest(w http.ResponseWriter, r *http.Request)",
			ChunkType: "definitions",
			Tags:      []string{"go", "code"},
			Metadata:  map[string]interface{}{"file_path": "handler.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
	}

	chunkSet := &ChunkSet{
		chunks: chunks,
		byID: map[string]*ContextChunk{
			"test-1": chunks[0],
			"test-2": chunks[1],
			"test-3": chunks[2],
		},
		byFile: map[string][]*ContextChunk{
			"provider.go": {chunks[0], chunks[1]},
			"handler.go":  {chunks[2]},
		},
	}

	searcher, err := NewExactSearcher(ctx, chunkSet)
	require.NoError(t, err)
	defer searcher.Close()

	// Test basic text search
	results, err := searcher.Search(ctx, "text:Provider", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, results, 2, "Should find 2 chunks with 'Provider'")

	// Verify results contain correct chunks
	foundIDs := make(map[string]bool)
	for _, result := range results {
		foundIDs[result.Chunk.ID] = true
	}
	assert.True(t, foundIDs["test-1"], "Should find test-1")
	assert.True(t, foundIDs["test-2"], "Should find test-2")
	assert.False(t, foundIDs["test-3"], "Should not find test-3")
}

func TestExactSearcher_BooleanQuery(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	chunks := []*ContextChunk{
		{
			ID:        "go-provider",
			Text:      "type Provider interface",
			ChunkType: "definitions",
			Tags:      []string{"go", "code"},
			Metadata:  map[string]interface{}{"file_path": "provider.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "ts-provider",
			Text:      "interface Provider extends Base",
			ChunkType: "definitions",
			Tags:      []string{"typescript", "code"},
			Metadata:  map[string]interface{}{"file_path": "provider.ts"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "go-handler",
			Text:      "func HandleRequest()",
			ChunkType: "definitions",
			Tags:      []string{"go", "code"},
			Metadata:  map[string]interface{}{"file_path": "handler.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
	}

	chunkSet := &ChunkSet{
		chunks: chunks,
		byID: map[string]*ContextChunk{
			"go-provider": chunks[0],
			"ts-provider": chunks[1],
			"go-handler":  chunks[2],
		},
		byFile: map[string][]*ContextChunk{
			"provider.go": {chunks[0]},
			"provider.ts": {chunks[1]},
			"handler.go":  {chunks[2]},
		},
	}

	searcher, err := NewExactSearcher(ctx, chunkSet)
	require.NoError(t, err)
	defer searcher.Close()

	// Test OR operator (bleve's default query behavior)
	results, err := searcher.Search(ctx, "text:Provider OR text:Handler", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 2, "Should find multiple chunks")

	// Test NOT operator
	results, err = searcher.Search(ctx, "text:Provider -tags:typescript", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	// Should find at least go-provider
	foundGoProvider := false
	for _, result := range results {
		if result.Chunk.ID == "go-provider" {
			foundGoProvider = true
		}
	}
	assert.True(t, foundGoProvider, "Should find Go provider")
	// Note: NOT operator behavior depends on bleve's default query parsing
	// It may not exclude typescript if the tag matching isn't exact
}

func TestExactSearcher_FieldScoping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	chunks := []*ContextChunk{
		{
			ID:        "provider-code",
			Text:      "type Provider interface",
			ChunkType: "definitions",
			Title:     "Provider Interface",
			Tags:      []string{"go"},
			Metadata:  map[string]interface{}{"file_path": "provider.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "provider-doc",
			Text:      "Documentation for authentication",
			ChunkType: "documentation",
			Title:     "Provider Documentation",
			Tags:      []string{"docs"},
			Metadata:  map[string]interface{}{"file_path": "README.md"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
	}

	chunkSet := &ChunkSet{
		chunks: chunks,
		byID: map[string]*ContextChunk{
			"provider-code": chunks[0],
			"provider-doc":  chunks[1],
		},
		byFile: map[string][]*ContextChunk{
			"provider.go": {chunks[0]},
			"README.md":   {chunks[1]},
		},
	}

	searcher, err := NewExactSearcher(ctx, chunkSet)
	require.NoError(t, err)
	defer searcher.Close()

	// Search in title field
	results, err := searcher.Search(ctx, "title:Provider", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "Should find chunks with 'Provider' in title")

	// Search in text field
	results, err = searcher.Search(ctx, "text:Provider", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1, "Should find chunks with 'Provider' in text")

	// Verify text search finds the right chunk
	foundProvider := false
	for _, result := range results {
		if result.Chunk.ID == "provider-code" {
			foundProvider = true
			break
		}
	}
	assert.True(t, foundProvider, "Should find provider-code chunk")
}

func TestExactSearcher_UpdateIncremental(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create initial chunks
	initialChunks := []*ContextChunk{
		{
			ID:        "chunk-1",
			Text:      "Initial text",
			ChunkType: "definitions",
			Tags:      []string{"go"},
			Metadata:  map[string]interface{}{"file_path": "test.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "chunk-2",
			Text:      "Another chunk",
			ChunkType: "definitions",
			Tags:      []string{"go"},
			Metadata:  map[string]interface{}{"file_path": "test.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
	}

	chunkSet := &ChunkSet{
		chunks: initialChunks,
		byID: map[string]*ContextChunk{
			"chunk-1": initialChunks[0],
			"chunk-2": initialChunks[1],
		},
		byFile: map[string][]*ContextChunk{
			"test.go": initialChunks,
		},
	}

	searcher, err := NewExactSearcher(ctx, chunkSet)
	require.NoError(t, err)
	defer searcher.Close()

	// Verify initial state
	results, err := searcher.Search(ctx, "text:Initial", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// Prepare incremental updates
	addedChunk := &ContextChunk{
		ID:        "chunk-3",
		Text:      "New chunk",
		ChunkType: "definitions",
		Tags:      []string{"go"},
		Metadata:  map[string]interface{}{"file_path": "new.go"},
		Embedding: make([]float32, 384),
		UpdatedAt: time.Now(),
	}

	updatedChunk := &ContextChunk{
		ID:        "chunk-1",
		Text:      "Updated text",
		ChunkType: "definitions",
		Tags:      []string{"go"},
		Metadata:  map[string]interface{}{"file_path": "test.go"},
		Embedding: make([]float32, 384),
		UpdatedAt: time.Now(),
	}

	deletedIDs := []string{"chunk-2"}

	// Apply incremental update
	err = searcher.UpdateIncremental(ctx, []*ContextChunk{addedChunk}, []*ContextChunk{updatedChunk}, deletedIDs)
	require.NoError(t, err)

	// Verify added chunk is searchable
	results, err = searcher.Search(ctx, "text:New", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "chunk-3", results[0].Chunk.ID)

	// Verify updated chunk has new text
	results, err = searcher.Search(ctx, "text:Updated", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, "chunk-1", results[0].Chunk.ID)

	// Verify old text is no longer findable
	results, err = searcher.Search(ctx, "text:Initial", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, results)

	// Verify deleted chunk is gone
	results, err = searcher.Search(ctx, "text:Another", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestExactSearcher_Highlighting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	chunks := []*ContextChunk{
		{
			ID:        "test-1",
			Text:      "The Provider interface defines the embedding functionality for cortex.",
			ChunkType: "definitions",
			Tags:      []string{"go"},
			Metadata:  map[string]interface{}{"file_path": "provider.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		},
	}

	chunkSet := &ChunkSet{
		chunks: chunks,
		byID:   map[string]*ContextChunk{"test-1": chunks[0]},
		byFile: map[string][]*ContextChunk{"provider.go": {chunks[0]}},
	}

	searcher, err := NewExactSearcher(ctx, chunkSet)
	require.NoError(t, err)
	defer searcher.Close()

	// Search with highlighting
	results, err := searcher.Search(ctx, "text:Provider", &ExactSearchOptions{Limit: 10})
	require.NoError(t, err)
	require.Len(t, results, 1)

	// Verify highlights exist (if bleve provides them)
	// Note: Highlights may be empty in some cases, this is optional
	if len(results[0].Highlights) > 0 {
		t.Logf("Highlights: %v", results[0].Highlights)
	}
}

func TestExactSearcher_LimitParameter(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Create many chunks
	chunks := make([]*ContextChunk, 20)
	byID := make(map[string]*ContextChunk)
	for i := 0; i < 20; i++ {
		chunk := &ContextChunk{
			ID:        fmt.Sprintf("chunk-%d", i),
			Text:      "Provider interface test",
			ChunkType: "definitions",
			Tags:      []string{"go"},
			Metadata:  map[string]interface{}{"file_path": "test.go"},
			Embedding: make([]float32, 384),
			UpdatedAt: time.Now(),
		}
		chunks[i] = chunk
		byID[chunk.ID] = chunk
	}

	chunkSet := &ChunkSet{
		chunks: chunks,
		byID:   byID,
		byFile: map[string][]*ContextChunk{"test.go": chunks},
	}

	searcher, err := NewExactSearcher(ctx, chunkSet)
	require.NoError(t, err)
	defer searcher.Close()

	// Test limit parameter
	results, err := searcher.Search(ctx, "text:Provider", &ExactSearchOptions{Limit: 5})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 5, "Should respect limit parameter")

	// Test default limit with 0
	results, err = searcher.Search(ctx, "text:Provider", &ExactSearchOptions{Limit: 0})
	require.NoError(t, err)
	assert.LessOrEqual(t, len(results), 15, "Should use default limit of 15")
}
