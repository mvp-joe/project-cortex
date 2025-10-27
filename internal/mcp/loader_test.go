package mcp

// Test Plan for Chunk Loader:
// - LoadChunks successfully loads valid chunk files
// - LoadChunks handles missing directory gracefully
// - LoadChunks skips malformed JSON files with warning
// - LoadChunks validates chunk structure (ID, text, embedding)
// - LoadChunks validates embedding dimensions (384)
// - LoadChunks handles empty directory
// - LoadChunks loads multiple files and combines results

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: LoadChunks successfully loads valid chunk files
func TestLoadChunks_ValidFiles(t *testing.T) {
	t.Parallel()

	// Use the existing testdata
	chunksDir := filepath.Join("..", "..", "testdata", "mcp", "chunks")

	chunks, err := LoadChunks(chunksDir)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)

	// Verify we loaded chunks from both files
	assert.GreaterOrEqual(t, len(chunks), 2, "should load chunks from multiple files")

	// Verify chunk structure
	for _, chunk := range chunks {
		assert.NotEmpty(t, chunk.ID)
		assert.NotEmpty(t, chunk.Text)
		assert.Len(t, chunk.Embedding, 384, "embedding should have 384 dimensions")
	}
}

// Test: LoadChunks handles missing directory
func TestLoadChunks_MissingDirectory(t *testing.T) {
	t.Parallel()

	chunks, err := LoadChunks("/nonexistent/directory")
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "not accessible")
}

// Test: LoadChunks handles empty directory
func TestLoadChunks_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	chunks, err := LoadChunks(tmpDir)
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "no chunk files found")
}

// Test: LoadChunks validates chunk ID
func TestLoadChunks_MissingID(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid.json")

	// Create chunk without ID (wrapped format)
	invalidJSON := `{
		"_metadata": {
			"model": "BAAI/bge-small-en-v1.5",
			"dimensions": 384,
			"chunk_type": "test",
			"generated": "2025-10-26T00:00:00Z",
			"version": "2.0.0"
		},
		"chunks": [{
			"title": "Test",
			"text": "Content",
			"embedding": [` + generateEmbeddingJSON(384) + `]
		}]
	}`

	err := os.WriteFile(filePath, []byte(invalidJSON), 0644)
	require.NoError(t, err)

	chunks, err := LoadChunks(tmpDir)
	assert.Error(t, err)
	assert.Nil(t, chunks)
}

// Test: LoadChunks validates chunk text
func TestLoadChunks_MissingText(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid.json")

	invalidJSON := `{
		"_metadata": {
			"model": "BAAI/bge-small-en-v1.5",
			"dimensions": 384,
			"chunk_type": "test",
			"generated": "2025-10-26T00:00:00Z",
			"version": "2.0.0"
		},
		"chunks": [{
			"id": "test-id",
			"title": "Test",
			"embedding": [` + generateEmbeddingJSON(384) + `]
		}]
	}`

	err := os.WriteFile(filePath, []byte(invalidJSON), 0644)
	require.NoError(t, err)

	chunks, err := LoadChunks(tmpDir)
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "no valid chunks")
}

// Test: LoadChunks validates embedding dimensions
func TestLoadChunks_InvalidEmbeddingDimensions(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "invalid.json")

	// Create chunk with wrong embedding dimensions
	invalidJSON := `{
		"_metadata": {
			"model": "BAAI/bge-small-en-v1.5",
			"dimensions": 384,
			"chunk_type": "test",
			"generated": "2025-10-26T00:00:00Z",
			"version": "2.0.0"
		},
		"chunks": [{
			"id": "test-id",
			"title": "Test",
			"text": "Content",
			"embedding": [0.1, 0.2, 0.3]
		}]
	}`

	err := os.WriteFile(filePath, []byte(invalidJSON), 0644)
	require.NoError(t, err)

	chunks, err := LoadChunks(tmpDir)
	assert.Error(t, err)
	assert.Nil(t, chunks)
	assert.Contains(t, err.Error(), "no valid chunks")
}

// Test: LoadChunks loads multiple files
func TestLoadChunks_MultipleFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create first file (wrapped format)
	file1 := filepath.Join(tmpDir, "file1.json")
	json1 := `{
		"_metadata": {
			"model": "BAAI/bge-small-en-v1.5",
			"dimensions": 384,
			"chunk_type": "test",
			"generated": "2025-10-26T00:00:00Z",
			"version": "2.0.0"
		},
		"chunks": [{
			"id": "chunk-1",
			"title": "Chunk 1",
			"text": "Content 1",
			"embedding": [` + generateEmbeddingJSON(384) + `],
			"created_at": "2025-10-26T00:00:00Z",
			"updated_at": "2025-10-26T00:00:00Z"
		}]
	}`
	err := os.WriteFile(file1, []byte(json1), 0644)
	require.NoError(t, err)

	// Create second file (wrapped format)
	file2 := filepath.Join(tmpDir, "file2.json")
	json2 := `{
		"_metadata": {
			"model": "BAAI/bge-small-en-v1.5",
			"dimensions": 384,
			"chunk_type": "test",
			"generated": "2025-10-26T00:00:00Z",
			"version": "2.0.0"
		},
		"chunks": [{
			"id": "chunk-2",
			"title": "Chunk 2",
			"text": "Content 2",
			"embedding": [` + generateEmbeddingJSON(384) + `],
			"created_at": "2025-10-26T00:00:00Z",
			"updated_at": "2025-10-26T00:00:00Z"
		}]
	}`
	err = os.WriteFile(file2, []byte(json2), 0644)
	require.NoError(t, err)

	chunks, err := LoadChunks(tmpDir)
	require.NoError(t, err)
	assert.Len(t, chunks, 2)

	ids := []string{chunks[0].ID, chunks[1].ID}
	assert.Contains(t, ids, "chunk-1")
	assert.Contains(t, ids, "chunk-2")
}

// Test: LoadChunks handles malformed JSON gracefully
func TestLoadChunks_MalformedJSONSkipped(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create valid file (wrapped format)
	validFile := filepath.Join(tmpDir, "valid.json")
	validJSON := `{
		"_metadata": {
			"model": "BAAI/bge-small-en-v1.5",
			"dimensions": 384,
			"chunk_type": "test",
			"generated": "2025-10-26T00:00:00Z",
			"version": "2.0.0"
		},
		"chunks": [{
			"id": "valid-chunk",
			"title": "Valid",
			"text": "Content",
			"embedding": [` + generateEmbeddingJSON(384) + `],
			"created_at": "2025-10-26T00:00:00Z",
			"updated_at": "2025-10-26T00:00:00Z"
		}]
	}`
	err := os.WriteFile(validFile, []byte(validJSON), 0644)
	require.NoError(t, err)

	// Create malformed file
	invalidFile := filepath.Join(tmpDir, "invalid.json")
	err = os.WriteFile(invalidFile, []byte("not valid json"), 0644)
	require.NoError(t, err)

	// Should load valid chunks and skip invalid file
	chunks, err := LoadChunks(tmpDir)
	require.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, "valid-chunk", chunks[0].ID)
}

// Test: LoadChunks preserves chunk metadata
func TestLoadChunks_PreservesMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "chunks.json")

	now := time.Now().UTC().Format(time.RFC3339)
	chunkJSON := `{
		"_metadata": {
			"model": "BAAI/bge-small-en-v1.5",
			"dimensions": 384,
			"chunk_type": "symbols",
			"generated": "2025-10-26T00:00:00Z",
			"version": "2.0.0"
		},
		"chunks": [{
			"id": "test-chunk",
			"title": "Test Chunk",
			"text": "Test content",
			"chunk_type": "symbols",
			"embedding": [` + generateEmbeddingJSON(384) + `],
			"tags": ["go", "code"],
			"metadata": {
				"file_path": "test.go",
				"language": "go"
			},
			"created_at": "` + now + `",
			"updated_at": "` + now + `"
		}]
	}`

	err := os.WriteFile(filePath, []byte(chunkJSON), 0644)
	require.NoError(t, err)

	chunks, err := LoadChunks(tmpDir)
	require.NoError(t, err)
	require.Len(t, chunks, 1)

	chunk := chunks[0]
	assert.Equal(t, "test-chunk", chunk.ID)
	assert.Equal(t, "Test Chunk", chunk.Title)
	assert.Equal(t, "symbols", chunk.ChunkType)
	assert.Equal(t, []string{"go", "code"}, chunk.Tags)
	assert.Equal(t, "test.go", chunk.Metadata["file_path"])
	assert.Equal(t, "go", chunk.Metadata["language"])
}

// Helper: Generate embedding JSON with specified dimensions
func generateEmbeddingJSON(dims int) string {
	result := ""
	for i := 0; i < dims; i++ {
		if i > 0 {
			result += ","
		}
		result += "0.1"
	}
	return result
}
