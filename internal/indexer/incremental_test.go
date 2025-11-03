package indexer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Incremental Indexing:
// - IndexIncremental detects no changes and returns early
// - IndexIncremental processes only changed files (not unchanged files)
// - IndexIncremental handles new files correctly
// - IndexIncremental handles deleted files correctly
// - IndexIncremental preserves chunks from unchanged files (including embeddings)
// - IndexIncremental preserves chunk IDs for unchanged files
// - IndexIncremental updates metadata with new checksums
// - IndexIncremental handles mixed scenarios (changed + new + deleted)
// - IndexIncremental handles parse errors gracefully
// - loadAllChunks handles missing chunk files
// - buildFileChunksIndex creates correct index
// - filterChunks removes correct chunks

func TestIndexIncremental_NoChanges(t *testing.T) {
	t.Parallel()

	// Test: When no files have changed, IndexIncremental should return early without reprocessing

	ctx := context.Background()
	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	// Create test files
	require.NoError(t, os.MkdirAll(rootDir, 0755))
	testFile := filepath.Join(rootDir, "test.go")
	testContent := []byte(`package main

func Hello() string {
	return "world"
}
`)
	require.NoError(t, os.WriteFile(testFile, testContent, 0644))

	// Create indexer
	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// First full index
	stats1, err := indexer.Index(ctx)
	require.NoError(t, err)
	require.Greater(t, stats1.CodeFilesProcessed, 0)

	// Second incremental index (no changes)
	stats2, err := indexer.IndexIncremental(ctx)
	require.NoError(t, err)

	// Stats should match (no processing happened)
	assert.Equal(t, 0, stats2.CodeFilesProcessed, "Should not process any code files")
	assert.Equal(t, 0, stats2.DocsProcessed, "Should not process any docs")
}

func TestIndexIncremental_ChangedFilesOnly(t *testing.T) {
	t.Parallel()

	// Test: Only changed files should be reprocessed, unchanged files should keep their chunks

	ctx := context.Background()
	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	// Create test files
	require.NoError(t, os.MkdirAll(rootDir, 0755))

	file1 := filepath.Join(rootDir, "file1.go")
	file1Content := []byte(`package main

func File1() string {
	return "file1"
}
`)
	require.NoError(t, os.WriteFile(file1, file1Content, 0644))

	file2 := filepath.Join(rootDir, "file2.go")
	file2Content := []byte(`package main

func File2() string {
	return "file2"
}
`)
	require.NoError(t, os.WriteFile(file2, file2Content, 0644))

	// Create indexer
	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// First full index
	stats1, err := indexer.Index(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, stats1.CodeFilesProcessed)

	// Read original chunks
	writer := GetWriter(indexer)
	originalSymbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)
	require.Len(t, originalSymbols.Chunks, 2)

	// Find chunk for file1
	var file1ChunkID string
	for _, chunk := range originalSymbols.Chunks {
		if filePath, ok := chunk.Metadata["file_path"].(string); ok && filePath == "file1.go" {
			file1ChunkID = chunk.ID
			break
		}
	}
	require.NotEmpty(t, file1ChunkID, "Should find chunk for file1.go")

	// Modify only file1
	file1ModifiedContent := []byte(`package main

func File1() string {
	return "file1-modified"
}
`)
	require.NoError(t, os.WriteFile(file1, file1ModifiedContent, 0644))

	// Sleep to ensure timestamp difference
	time.Sleep(10 * time.Millisecond)

	// Incremental index
	stats2, err := indexer.IndexIncremental(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, stats2.CodeFilesProcessed, "Should process only 1 changed file")

	// Read updated chunks
	updatedSymbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)
	require.Len(t, updatedSymbols.Chunks, 2, "Should still have 2 chunks")

	// Verify file2 chunk is unchanged
	var file2Chunk *Chunk
	for i, chunk := range updatedSymbols.Chunks {
		if filePath, ok := chunk.Metadata["file_path"].(string); ok && filePath == "file2.go" {
			file2Chunk = &updatedSymbols.Chunks[i]
			break
		}
	}
	require.NotNil(t, file2Chunk, "Should find chunk for file2.go")

	// Compare with original file2 chunk
	var originalFile2Chunk *Chunk
	for i, chunk := range originalSymbols.Chunks {
		if filePath, ok := chunk.Metadata["file_path"].(string); ok && filePath == "file2.go" {
			originalFile2Chunk = &originalSymbols.Chunks[i]
			break
		}
	}
	require.NotNil(t, originalFile2Chunk)

	// File2 chunk should be identical (ID, timestamp, embedding preserved)
	assert.Equal(t, originalFile2Chunk.ID, file2Chunk.ID, "Chunk ID should be preserved")
	assert.Equal(t, originalFile2Chunk.CreatedAt, file2Chunk.CreatedAt, "Created timestamp should be preserved")
	assert.Equal(t, originalFile2Chunk.UpdatedAt, file2Chunk.UpdatedAt, "Updated timestamp should be preserved")
	assert.Equal(t, originalFile2Chunk.Embedding, file2Chunk.Embedding, "Embedding should be preserved")

	// Verify FileMtimes are populated in metadata
	metadata, err := writer.ReadMetadata()
	require.NoError(t, err)
	assert.NotEmpty(t, metadata.FileMtimes, "FileMtimes should be populated")
	assert.Contains(t, metadata.FileMtimes, "file1.go")
	assert.Contains(t, metadata.FileMtimes, "file2.go")
}

func TestIndexIncremental_NewFiles(t *testing.T) {
	t.Parallel()

	// Test: New files should be added to the index

	ctx := context.Background()
	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	// Create initial file
	file1 := filepath.Join(rootDir, "file1.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main

func File1() string {
	return "file1"
}
`), 0644))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// First index
	stats1, err := indexer.Index(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, stats1.CodeFilesProcessed)

	// Add new file
	file2 := filepath.Join(rootDir, "file2.go")
	require.NoError(t, os.WriteFile(file2, []byte(`package main

func File2() string {
	return "file2"
}
`), 0644))

	// Incremental index
	stats2, err := indexer.IndexIncremental(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, stats2.CodeFilesProcessed, "Should process 1 new file")

	// Verify both files are in chunks
	writer := GetWriter(indexer)
	symbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)
	require.Len(t, symbols.Chunks, 2, "Should have chunks for both files")

	filePaths := make(map[string]bool)
	for _, chunk := range symbols.Chunks {
		if fp, ok := chunk.Metadata["file_path"].(string); ok {
			filePaths[fp] = true
		}
	}

	assert.True(t, filePaths["file1.go"], "Should have chunk for file1.go")
	assert.True(t, filePaths["file2.go"], "Should have chunk for file2.go")
}

func TestIndexIncremental_DeletedFiles(t *testing.T) {
	t.Parallel()

	// Test: Deleted files should have their chunks removed

	ctx := context.Background()
	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	// Create two files
	file1 := filepath.Join(rootDir, "file1.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main

func File1() string {
	return "file1"
}
`), 0644))

	file2 := filepath.Join(rootDir, "file2.go")
	require.NoError(t, os.WriteFile(file2, []byte(`package main

func File2() string {
	return "file2"
}
`), 0644))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// First index
	stats1, err := indexer.Index(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, stats1.CodeFilesProcessed)

	// Delete file2
	require.NoError(t, os.Remove(file2))

	// Incremental index
	stats2, err := indexer.IndexIncremental(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, stats2.CodeFilesProcessed, "Should not process any files")

	// Verify only file1 remains in chunks
	writer := GetWriter(indexer)
	symbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)
	require.Len(t, symbols.Chunks, 1, "Should have chunk for only one file")

	filePath, ok := symbols.Chunks[0].Metadata["file_path"].(string)
	require.True(t, ok)
	assert.Equal(t, "file1.go", filePath, "Should keep only file1.go")
}

func TestIndexIncremental_MixedScenario(t *testing.T) {
	t.Parallel()

	// Test: Handle changed + new + deleted + unchanged files in one run

	ctx := context.Background()
	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	// Create initial files: unchanged, toChange, toDelete
	unchanged := filepath.Join(rootDir, "unchanged.go")
	require.NoError(t, os.WriteFile(unchanged, []byte(`package main

func Unchanged() string {
	return "unchanged"
}
`), 0644))

	toChange := filepath.Join(rootDir, "toChange.go")
	require.NoError(t, os.WriteFile(toChange, []byte(`package main

func ToChange() string {
	return "before"
}
`), 0644))

	toDelete := filepath.Join(rootDir, "toDelete.go")
	require.NoError(t, os.WriteFile(toDelete, []byte(`package main

func ToDelete() string {
	return "delete-me"
}
`), 0644))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// First index (3 files)
	stats1, err := indexer.Index(ctx)
	require.NoError(t, err)
	require.Equal(t, 3, stats1.CodeFilesProcessed)

	// Get original unchanged chunk
	writer := GetWriter(indexer)
	originalSymbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)
	require.Len(t, originalSymbols.Chunks, 3)

	var unchangedChunk *Chunk
	for i, chunk := range originalSymbols.Chunks {
		if fp, ok := chunk.Metadata["file_path"].(string); ok && fp == "unchanged.go" {
			unchangedChunk = &originalSymbols.Chunks[i]
			break
		}
	}
	require.NotNil(t, unchangedChunk)

	// Perform changes:
	// 1. Modify toChange
	require.NoError(t, os.WriteFile(toChange, []byte(`package main

func ToChange() string {
	return "after"
}
`), 0644))

	// 2. Delete toDelete
	require.NoError(t, os.Remove(toDelete))

	// 3. Add new file
	newFile := filepath.Join(rootDir, "new.go")
	require.NoError(t, os.WriteFile(newFile, []byte(`package main

func New() string {
	return "new"
}
`), 0644))

	time.Sleep(10 * time.Millisecond)

	// Incremental index
	stats2, err := indexer.IndexIncremental(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, stats2.CodeFilesProcessed, "Should process 1 changed + 1 new file")

	// Verify final state: unchanged, toChange (modified), new (3 total)
	updatedSymbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)
	require.Len(t, updatedSymbols.Chunks, 3, "Should have 3 chunks")

	filePaths := make(map[string]*Chunk)
	for i, chunk := range updatedSymbols.Chunks {
		if fp, ok := chunk.Metadata["file_path"].(string); ok {
			filePaths[fp] = &updatedSymbols.Chunks[i]
		}
	}

	// Verify unchanged chunk is preserved
	assert.NotNil(t, filePaths["unchanged.go"], "Should have unchanged.go")
	assert.Equal(t, unchangedChunk.ID, filePaths["unchanged.go"].ID, "Unchanged chunk ID should be preserved")
	assert.Equal(t, unchangedChunk.Embedding, filePaths["unchanged.go"].Embedding, "Unchanged embedding should be preserved")

	// Verify toChange exists
	assert.NotNil(t, filePaths["toChange.go"], "Should have toChange.go")

	// Verify new file exists
	assert.NotNil(t, filePaths["new.go"], "Should have new.go")

	// Verify toDelete is gone
	assert.Nil(t, filePaths["toDelete.go"], "Should not have toDelete.go")
}

func TestIndexIncremental_PreservesEmbeddings(t *testing.T) {
	t.Parallel()

	// Test: Embeddings for unchanged files should not be regenerated

	ctx := context.Background()
	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	file1 := filepath.Join(rootDir, "file1.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main

func File1() string {
	return "file1"
}
`), 0644))

	file2 := filepath.Join(rootDir, "file2.go")
	require.NoError(t, os.WriteFile(file2, []byte(`package main

func File2() string {
	return "file2"
}
`), 0644))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// First index
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	// Get original embeddings
	writer := GetWriter(indexer)
	originalSymbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)

	var file1OriginalEmbedding []float32
	for _, chunk := range originalSymbols.Chunks {
		if fp, ok := chunk.Metadata["file_path"].(string); ok && fp == "file1.go" {
			file1OriginalEmbedding = chunk.Embedding
			break
		}
	}
	require.NotNil(t, file1OriginalEmbedding, "Should have embedding for file1")

	// Modify file2 only
	require.NoError(t, os.WriteFile(file2, []byte(`package main

func File2() string {
	return "file2-modified"
}
`), 0644))

	time.Sleep(10 * time.Millisecond)

	// Incremental index
	_, err = indexer.IndexIncremental(ctx)
	require.NoError(t, err)

	// Get updated embeddings
	updatedSymbols, err := writer.ReadChunkFile("code-symbols.json")
	require.NoError(t, err)

	var file1UpdatedEmbedding []float32
	for _, chunk := range updatedSymbols.Chunks {
		if fp, ok := chunk.Metadata["file_path"].(string); ok && fp == "file1.go" {
			file1UpdatedEmbedding = chunk.Embedding
			break
		}
	}
	require.NotNil(t, file1UpdatedEmbedding, "Should have embedding for file1")

	// File1 embedding should be exactly the same (no regeneration)
	assert.Equal(t, file1OriginalEmbedding, file1UpdatedEmbedding, "Embedding should not be regenerated for unchanged file")
}

func TestLoadAllChunks_MissingFiles(t *testing.T) {
	t.Parallel()

	// Test: loadAllChunks should handle missing chunk files gracefully

	tempDir := t.TempDir()
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	storage, err := NewJSONStorage(outputDir)
	require.NoError(t, err)

	idx := &indexer{
		storage: storage,
	}

	// No chunk files exist yet
	chunks, err := idx.loadAllChunks()
	require.NoError(t, err)
	assert.NotNil(t, chunks)
	assert.Len(t, chunks[ChunkTypeSymbols], 0)
	assert.Len(t, chunks[ChunkTypeDefinitions], 0)
	assert.Len(t, chunks[ChunkTypeData], 0)
	assert.Len(t, chunks[ChunkTypeDocumentation], 0)
}

func TestBuildFileChunksIndex(t *testing.T) {
	t.Parallel()

	// Test: buildFileChunksIndex creates correct file_path → [chunk_ids] mapping

	idx := &indexer{}

	chunks := map[ChunkType][]Chunk{
		ChunkTypeSymbols: {
			{
				ID: "chunk1",
				Metadata: map[string]interface{}{
					"file_path": "file1.go",
				},
			},
			{
				ID: "chunk2",
				Metadata: map[string]interface{}{
					"file_path": "file2.go",
				},
			},
		},
		ChunkTypeDefinitions: {
			{
				ID: "chunk3",
				Metadata: map[string]interface{}{
					"file_path": "file1.go",
				},
			},
		},
	}

	index := idx.buildFileChunksIndex(chunks)

	assert.Len(t, index, 2, "Should have 2 files")
	assert.Contains(t, index["file1.go"], "chunk1")
	assert.Contains(t, index["file1.go"], "chunk3")
	assert.Contains(t, index["file2.go"], "chunk2")
	assert.Len(t, index["file1.go"], 2, "file1.go should have 2 chunks")
	assert.Len(t, index["file2.go"], 1, "file2.go should have 1 chunk")
}

func TestFilterChunks(t *testing.T) {
	t.Parallel()

	// Test: filterChunks removes chunks for changed and deleted files

	idx := &indexer{}

	chunks := map[ChunkType][]Chunk{
		ChunkTypeSymbols: {
			{
				ID: "unchanged-chunk",
				Metadata: map[string]interface{}{
					"file_path": "unchanged.go",
				},
			},
			{
				ID: "changed-chunk",
				Metadata: map[string]interface{}{
					"file_path": "changed.go",
				},
			},
			{
				ID: "deleted-chunk",
				Metadata: map[string]interface{}{
					"file_path": "deleted.go",
				},
			},
		},
	}

	changedFiles := map[string]bool{
		"changed.go": true,
	}

	deletedFiles := map[string]bool{
		"deleted.go": true,
	}

	filtered := idx.filterChunks(chunks, changedFiles, deletedFiles)

	require.Len(t, filtered[ChunkTypeSymbols], 1, "Should keep only unchanged chunk")
	assert.Equal(t, "unchanged-chunk", filtered[ChunkTypeSymbols][0].ID)
}

func TestIndexIncremental_UpdatesMetadata(t *testing.T) {
	t.Parallel()

	// Test: Metadata should be updated with new checksums after incremental index

	ctx := context.Background()
	tempDir := t.TempDir()
	rootDir := filepath.Join(tempDir, "project")
	outputDir := filepath.Join(tempDir, ".cortex", "chunks")

	require.NoError(t, os.MkdirAll(rootDir, 0755))

	file1 := filepath.Join(rootDir, "file1.go")
	require.NoError(t, os.WriteFile(file1, []byte(`package main

func File1() string {
	return "file1"
}
`), 0644))

	config := &Config{
		RootDir:           rootDir,
		OutputDir:         outputDir,
		StorageBackend:    "json", // Use JSON for tests (SQLite requires FTS5)
		CodePatterns:      []string{"*.go"},
		DocsPatterns:      []string{"*.md"},
		IgnorePatterns:    []string{},
		DocChunkSize:      1000,
		Overlap:           100,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "mock-model",
	}

	indexer, err := New(config)
	require.NoError(t, err)
	defer indexer.Close()

	// First index
	_, err = indexer.Index(ctx)
	require.NoError(t, err)

	writer := GetWriter(indexer)
	metadata1, err := writer.ReadMetadata()
	require.NoError(t, err)
	oldChecksum := metadata1.FileChecksums["file1.go"]
	require.NotEmpty(t, oldChecksum)

	// Modify file
	require.NoError(t, os.WriteFile(file1, []byte(`package main

func File1() string {
	return "file1-modified"
}
`), 0644))

	time.Sleep(10 * time.Millisecond)

	// Incremental index
	_, err = indexer.IndexIncremental(ctx)
	require.NoError(t, err)

	// Check metadata updated
	metadata2, err := writer.ReadMetadata()
	require.NoError(t, err)
	newChecksum := metadata2.FileChecksums["file1.go"]
	require.NotEmpty(t, newChecksum)

	assert.NotEqual(t, oldChecksum, newChecksum, "Checksum should be updated")

	// Verify FileMtimes are populated
	assert.NotEmpty(t, metadata2.FileMtimes, "FileMtimes should be populated")
	assert.Contains(t, metadata2.FileMtimes, "file1.go")
}

// Test helper: Create a mock chunk file for testing
func createMockChunkFile(t *testing.T, outputDir, filename string, chunks []Chunk) {
	require.NoError(t, os.MkdirAll(outputDir, 0755))

	chunkFile := &ChunkFile{
		Metadata: ChunkFileMetadata{
			Model:      "mock-model",
			Dimensions: 384,
			ChunkType:  ChunkTypeSymbols,
			Generated:  time.Now(),
			Version:    "2.0.0",
		},
		Chunks: chunks,
	}

	data, err := json.MarshalIndent(chunkFile, "", "  ")
	require.NoError(t, err)

	filePath := filepath.Join(outputDir, filename)
	require.NoError(t, os.WriteFile(filePath, data, 0644))
}
