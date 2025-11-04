package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChunkToChromemMetadata(t *testing.T) {
	t.Parallel()

	t.Run("empty chunk", func(t *testing.T) {
		chunk := &ContextChunk{}
		metadata := chunkToChromemMetadata(chunk)
		assert.Empty(t, metadata)
	})

	t.Run("chunk with chunk_type", func(t *testing.T) {
		chunk := &ContextChunk{
			ChunkType: "definitions",
		}
		metadata := chunkToChromemMetadata(chunk)
		require.Contains(t, metadata, "chunk_type")
		assert.Equal(t, "definitions", metadata["chunk_type"])
	})

	t.Run("chunk with string metadata", func(t *testing.T) {
		chunk := &ContextChunk{
			ChunkType: "symbols",
			Metadata: map[string]interface{}{
				"file_path": "internal/mcp/helpers.go",
				"language":  "go",
				"tag_0":     "code",
				"tag_1":     "go",
			},
		}
		metadata := chunkToChromemMetadata(chunk)
		require.Contains(t, metadata, "chunk_type")
		assert.Equal(t, "symbols", metadata["chunk_type"])
		assert.Equal(t, "internal/mcp/helpers.go", metadata["file_path"])
		assert.Equal(t, "go", metadata["language"])
		assert.Equal(t, "code", metadata["tag_0"])
		assert.Equal(t, "go", metadata["tag_1"])
	})

	t.Run("chunk with non-string metadata", func(t *testing.T) {
		chunk := &ContextChunk{
			ChunkType: "data",
			Metadata: map[string]interface{}{
				"file_path": "test.go",
				"line":      42,                                // int, should be skipped
				"flag":      true,                              // bool, should be skipped
				"nested":    map[string]string{"key": "value"}, // map, should be skipped
			},
		}
		metadata := chunkToChromemMetadata(chunk)
		require.Contains(t, metadata, "chunk_type")
		assert.Equal(t, "data", metadata["chunk_type"])
		assert.Equal(t, "test.go", metadata["file_path"])
		// Non-string values should be excluded
		assert.NotContains(t, metadata, "line")
		assert.NotContains(t, metadata, "flag")
		assert.NotContains(t, metadata, "nested")
	})
}

func TestChunkToChromemDocument(t *testing.T) {
	t.Parallel()

	t.Run("complete chunk", func(t *testing.T) {
		chunk := &ContextChunk{
			ID:        "test-chunk-1",
			Text:      "This is test content",
			ChunkType: "definitions",
			Embedding: []float32{0.1, 0.2, 0.3},
			Metadata: map[string]interface{}{
				"file_path": "test.go",
				"language":  "go",
			},
		}

		doc := chunkToChromemDocument(chunk)
		assert.Equal(t, "test-chunk-1", doc.ID)
		assert.Equal(t, "This is test content", doc.Content)
		assert.Equal(t, []float32{0.1, 0.2, 0.3}, doc.Embedding)
		assert.Equal(t, "definitions", doc.Metadata["chunk_type"])
		assert.Equal(t, "test.go", doc.Metadata["file_path"])
		assert.Equal(t, "go", doc.Metadata["language"])
	})

	t.Run("minimal chunk", func(t *testing.T) {
		chunk := &ContextChunk{
			ID:   "minimal",
			Text: "Minimal content",
		}

		doc := chunkToChromemDocument(chunk)
		assert.Equal(t, "minimal", doc.ID)
		assert.Equal(t, "Minimal content", doc.Content)
		assert.Empty(t, doc.Embedding)
		assert.Empty(t, doc.Metadata)
	})
}
