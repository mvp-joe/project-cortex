package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan:
// 1. Test ChunkManager.Load() with all chunk types
// 2. Test ChunkManager.Load() with missing files (should return empty set)
// 3. Test ChunkManager.Load() with corrupted JSON (should return error)
// 4. Test ChunkSet lookup methods (GetByID, GetByFile, All, Len)
// 5. Test ChunkManager.DetectChanges() for added chunks
// 6. Test ChunkManager.DetectChanges() for updated chunks (timestamp after lastReload)
// 7. Test ChunkManager.DetectChanges() for deleted chunks
// 8. Test ChunkManager.DetectChanges() for unchanged chunks (timestamp before lastReload)
// 9. Test ChunkManager.DetectChanges() with nil current set (all added)
// 10. Test ChunkManager atomic operations (Update, GetCurrent, GetLastReloadTime)
// 11. Test thread-safety with concurrent GetCurrent() calls
// 12. Test thread-safety with concurrent Load() + GetCurrent()
// 13. Test thread-safety with Update() during GetCurrent()
// 14. Test context cancellation during Load()

func TestChunkManager_Load_AllChunkTypes(t *testing.T) {
	t.Parallel()

	// Create temp directory with test chunks
	tmpDir := t.TempDir()
	baseTime := time.Now().Add(-1 * time.Hour)

	// Create test chunks for each type
	symbolsChunks := createTestChunks("symbols", 2, baseTime)
	definitionsChunks := createTestChunks("definitions", 3, baseTime)
	dataChunks := createTestChunks("data", 1, baseTime)
	docChunks := createTestChunks("documentation", 2, baseTime)

	// Write chunk files
	require.NoError(t, writeChunkFile(tmpDir, "code-symbols.json", symbolsChunks, "symbols"))
	require.NoError(t, writeChunkFile(tmpDir, "code-definitions.json", definitionsChunks, "definitions"))
	require.NoError(t, writeChunkFile(tmpDir, "code-data.json", dataChunks, "data"))
	require.NoError(t, writeChunkFile(tmpDir, "doc-chunks.json", docChunks, "documentation"))

	// Test loading
	cm := NewChunkManager(tmpDir)
	ctx := context.Background()
	chunkSet, err := cm.Load(ctx)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, chunkSet)
	assert.Equal(t, 8, chunkSet.Len(), "should load all chunks")

	// Verify each chunk type is present
	allChunks := chunkSet.All()
	chunkTypes := make(map[string]int)
	for _, chunk := range allChunks {
		chunkTypes[chunk.ChunkType]++
	}
	assert.Equal(t, 2, chunkTypes["symbols"])
	assert.Equal(t, 3, chunkTypes["definitions"])
	assert.Equal(t, 1, chunkTypes["data"])
	assert.Equal(t, 2, chunkTypes["documentation"])
}

func TestChunkManager_Load_MissingFiles(t *testing.T) {
	t.Parallel()

	// Create temp directory with only some chunk files
	tmpDir := t.TempDir()
	baseTime := time.Now()

	// Only write symbols file
	symbolsChunks := createTestChunks("symbols", 2, baseTime)
	require.NoError(t, writeChunkFile(tmpDir, "code-symbols.json", symbolsChunks, "symbols"))

	// Test loading (other files missing)
	cm := NewChunkManager(tmpDir)
	ctx := context.Background()
	chunkSet, err := cm.Load(ctx)

	// Verify: should succeed with partial data
	require.NoError(t, err)
	require.NotNil(t, chunkSet)
	assert.Equal(t, 2, chunkSet.Len(), "should load only available chunks")
}

func TestChunkManager_Load_EmptyDirectory(t *testing.T) {
	t.Parallel()

	// Create temp directory with no chunk files
	tmpDir := t.TempDir()

	// Test loading from empty directory
	cm := NewChunkManager(tmpDir)
	ctx := context.Background()
	chunkSet, err := cm.Load(ctx)

	// Verify: should succeed with empty set
	require.NoError(t, err)
	require.NotNil(t, chunkSet)
	assert.Equal(t, 0, chunkSet.Len(), "should return empty set")
}

func TestChunkManager_Load_CorruptedJSON(t *testing.T) {
	t.Parallel()

	// Create temp directory with corrupted JSON
	tmpDir := t.TempDir()
	corruptedPath := filepath.Join(tmpDir, "code-symbols.json")
	require.NoError(t, os.WriteFile(corruptedPath, []byte("{invalid json"), 0644))

	// Test loading
	cm := NewChunkManager(tmpDir)
	ctx := context.Background()
	_, err := cm.Load(ctx)

	// Verify: should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "corrupted chunk file")
}

func TestChunkSet_GetByID(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	chunks := createTestChunks("symbols", 3, baseTime)
	chunkSet := createChunkSetFromChunks(chunks)

	// Test existing ID
	chunk := chunkSet.GetByID("symbols-0")
	require.NotNil(t, chunk)
	assert.Equal(t, "symbols-0", chunk.ID)

	// Test non-existing ID
	chunk = chunkSet.GetByID("nonexistent")
	assert.Nil(t, chunk)

	// Test nil ChunkSet
	var nilSet *ChunkSet
	chunk = nilSet.GetByID("symbols-0")
	assert.Nil(t, chunk)
}

func TestChunkSet_GetByFile(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	chunks := []*ContextChunk{
		createTestChunk("chunk-1", "symbols", "file1.go", baseTime),
		createTestChunk("chunk-2", "symbols", "file1.go", baseTime),
		createTestChunk("chunk-3", "definitions", "file2.go", baseTime),
	}
	chunkSet := createChunkSetFromChunks(chunks)

	// Test file with multiple chunks
	fileChunks := chunkSet.GetByFile("file1.go")
	require.Len(t, fileChunks, 2)
	assert.Equal(t, "chunk-1", fileChunks[0].ID)
	assert.Equal(t, "chunk-2", fileChunks[1].ID)

	// Test file with single chunk
	fileChunks = chunkSet.GetByFile("file2.go")
	require.Len(t, fileChunks, 1)
	assert.Equal(t, "chunk-3", fileChunks[0].ID)

	// Test non-existing file
	fileChunks = chunkSet.GetByFile("nonexistent.go")
	assert.Nil(t, fileChunks)

	// Test nil ChunkSet
	var nilSet *ChunkSet
	fileChunks = nilSet.GetByFile("file1.go")
	assert.Nil(t, fileChunks)
}

func TestChunkSet_All(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	chunks := createTestChunks("symbols", 3, baseTime)
	chunkSet := createChunkSetFromChunks(chunks)

	// Test All()
	allChunks := chunkSet.All()
	require.Len(t, allChunks, 3)
	assert.Equal(t, "symbols-0", allChunks[0].ID)
	assert.Equal(t, "symbols-1", allChunks[1].ID)
	assert.Equal(t, "symbols-2", allChunks[2].ID)

	// Test nil ChunkSet
	var nilSet *ChunkSet
	allChunks = nilSet.All()
	assert.Nil(t, allChunks)
}

func TestChunkSet_Len(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	chunks := createTestChunks("symbols", 3, baseTime)
	chunkSet := createChunkSetFromChunks(chunks)

	// Test Len()
	assert.Equal(t, 3, chunkSet.Len())

	// Test empty ChunkSet
	emptySet := createChunkSetFromChunks([]*ContextChunk{})
	assert.Equal(t, 0, emptySet.Len())

	// Test nil ChunkSet
	var nilSet *ChunkSet
	assert.Equal(t, 0, nilSet.Len())
}

func TestChunkManager_DetectChanges_Added(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	lastReload := baseTime

	// Old set with 2 chunks
	oldChunks := createTestChunks("symbols", 2, baseTime)
	oldSet := createChunkSetFromChunks(oldChunks)

	// New set with 3 chunks (one added)
	newChunks := createTestChunks("symbols", 3, baseTime)
	newSet := createChunkSetFromChunks(newChunks)

	// Setup ChunkManager with old state
	cm := NewChunkManager(t.TempDir())
	cm.Update(oldSet, lastReload)

	// Detect changes
	added, updated, deleted := cm.DetectChanges(newSet)

	// Verify: one added, none updated, none deleted
	require.Len(t, added, 1)
	assert.Equal(t, "symbols-2", added[0].ID)
	assert.Len(t, updated, 0)
	assert.Len(t, deleted, 0)
}

func TestChunkManager_DetectChanges_Updated(t *testing.T) {
	t.Parallel()

	baseTime := time.Now().Add(-2 * time.Hour)
	lastReload := time.Now().Add(-1 * time.Hour)
	updateTime := time.Now() // After lastReload

	// Old set with chunks created before lastReload
	oldChunks := createTestChunks("symbols", 2, baseTime)
	oldSet := createChunkSetFromChunks(oldChunks)

	// New set with same chunks but one updated after lastReload
	newChunks := createTestChunks("symbols", 2, baseTime)
	newChunks[1].UpdatedAt = updateTime // Mark second chunk as updated
	newSet := createChunkSetFromChunks(newChunks)

	// Setup ChunkManager with old state
	cm := NewChunkManager(t.TempDir())
	cm.Update(oldSet, lastReload)

	// Detect changes
	added, updated, deleted := cm.DetectChanges(newSet)

	// Verify: none added, one updated, none deleted
	assert.Len(t, added, 0)
	require.Len(t, updated, 1)
	assert.Equal(t, "symbols-1", updated[0].ID)
	assert.Len(t, deleted, 0)
}

func TestChunkManager_DetectChanges_Deleted(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	lastReload := baseTime

	// Old set with 3 chunks
	oldChunks := createTestChunks("symbols", 3, baseTime)
	oldSet := createChunkSetFromChunks(oldChunks)

	// New set with 2 chunks (one deleted)
	newChunks := createTestChunks("symbols", 2, baseTime)
	newSet := createChunkSetFromChunks(newChunks)

	// Setup ChunkManager with old state
	cm := NewChunkManager(t.TempDir())
	cm.Update(oldSet, lastReload)

	// Detect changes
	added, updated, deleted := cm.DetectChanges(newSet)

	// Verify: none added, none updated, one deleted
	assert.Len(t, added, 0)
	assert.Len(t, updated, 0)
	require.Len(t, deleted, 1)
	assert.Equal(t, "symbols-2", deleted[0])
}

func TestChunkManager_DetectChanges_Unchanged(t *testing.T) {
	t.Parallel()

	baseTime := time.Now().Add(-2 * time.Hour)
	lastReload := time.Now().Add(-1 * time.Hour)

	// Old and new sets identical, both before lastReload
	oldChunks := createTestChunks("symbols", 2, baseTime)
	oldSet := createChunkSetFromChunks(oldChunks)
	newChunks := createTestChunks("symbols", 2, baseTime)
	newSet := createChunkSetFromChunks(newChunks)

	// Setup ChunkManager with old state
	cm := NewChunkManager(t.TempDir())
	cm.Update(oldSet, lastReload)

	// Detect changes
	added, updated, deleted := cm.DetectChanges(newSet)

	// Verify: no changes
	assert.Len(t, added, 0)
	assert.Len(t, updated, 0)
	assert.Len(t, deleted, 0)
}

func TestChunkManager_DetectChanges_NilCurrentSet(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()

	// New set with chunks
	newChunks := createTestChunks("symbols", 3, baseTime)
	newSet := createChunkSetFromChunks(newChunks)

	// ChunkManager with no current set (first load)
	cm := NewChunkManager(t.TempDir())

	// Detect changes
	added, updated, deleted := cm.DetectChanges(newSet)

	// Verify: all chunks are added
	require.Len(t, added, 3)
	assert.Len(t, updated, 0)
	assert.Len(t, deleted, 0)
}

func TestChunkManager_Update(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	chunks := createTestChunks("symbols", 2, baseTime)
	chunkSet := createChunkSetFromChunks(chunks)

	cm := NewChunkManager(t.TempDir())
	reloadTime := time.Now()

	// Update
	cm.Update(chunkSet, reloadTime)

	// Verify
	currentSet := cm.GetCurrent()
	require.NotNil(t, currentSet)
	assert.Equal(t, 2, currentSet.Len())

	lastReload := cm.GetLastReloadTime()
	assert.Equal(t, reloadTime, lastReload)
}

func TestChunkManager_GetCurrent_Nil(t *testing.T) {
	t.Parallel()

	cm := NewChunkManager(t.TempDir())

	// GetCurrent before any Update
	currentSet := cm.GetCurrent()
	assert.Nil(t, currentSet)
}

func TestChunkManager_GetLastReloadTime_Zero(t *testing.T) {
	t.Parallel()

	cm := NewChunkManager(t.TempDir())

	// GetLastReloadTime before any Update
	lastReload := cm.GetLastReloadTime()
	assert.True(t, lastReload.IsZero())
}

func TestChunkManager_ThreadSafety_ConcurrentGetCurrent(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	chunks := createTestChunks("symbols", 10, baseTime)
	chunkSet := createChunkSetFromChunks(chunks)

	cm := NewChunkManager(t.TempDir())
	cm.Update(chunkSet, time.Now())

	// Launch 100 concurrent readers
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			currentSet := cm.GetCurrent()
			require.NotNil(t, currentSet)
			assert.Equal(t, 10, currentSet.Len())
		}()
	}

	wg.Wait()
}

func TestChunkManager_ThreadSafety_LoadAndGetCurrent(t *testing.T) {
	t.Parallel()

	// Create test data
	tmpDir := t.TempDir()
	baseTime := time.Now()
	chunks := createTestChunks("symbols", 5, baseTime)
	require.NoError(t, writeChunkFile(tmpDir, "code-symbols.json", chunks, "symbols"))

	cm := NewChunkManager(tmpDir)
	ctx := context.Background()

	// Initial load
	initialSet, err := cm.Load(ctx)
	require.NoError(t, err)
	cm.Update(initialSet, time.Now())

	// Launch concurrent readers and loaders
	var wg sync.WaitGroup

	// 50 readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			currentSet := cm.GetCurrent()
			assert.NotNil(t, currentSet)
		}()
	}

	// 10 loaders
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			newSet, err := cm.Load(ctx)
			assert.NoError(t, err)
			assert.NotNil(t, newSet)
		}()
	}

	wg.Wait()
}

func TestChunkManager_ThreadSafety_UpdateDuringGetCurrent(t *testing.T) {
	t.Parallel()

	baseTime := time.Now()
	chunks1 := createTestChunks("symbols", 5, baseTime)
	chunks2 := createTestChunks("symbols", 10, baseTime)
	set1 := createChunkSetFromChunks(chunks1)
	set2 := createChunkSetFromChunks(chunks2)

	cm := NewChunkManager(t.TempDir())
	cm.Update(set1, time.Now())

	// Launch concurrent readers and updaters
	var wg sync.WaitGroup

	// 50 readers
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			currentSet := cm.GetCurrent()
			require.NotNil(t, currentSet)
			// Should be either 5 or 10 chunks
			assert.True(t, currentSet.Len() == 5 || currentSet.Len() == 10)
		}()
	}

	// 10 updaters
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cm.Update(set2, time.Now())
		}()
	}

	wg.Wait()
}

func TestChunkManager_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Create test data
	tmpDir := t.TempDir()
	baseTime := time.Now()
	chunks := createTestChunks("symbols", 5, baseTime)
	require.NoError(t, writeChunkFile(tmpDir, "code-symbols.json", chunks, "symbols"))

	cm := NewChunkManager(tmpDir)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test loading with cancelled context
	_, err := cm.Load(ctx)

	// Verify: should return context error
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

// Helper functions

func createTestChunks(chunkType string, count int, baseTime time.Time) []*ContextChunk {
	chunks := make([]*ContextChunk, count)
	for i := 0; i < count; i++ {
		chunks[i] = createTestChunk(
			fmt.Sprintf("%s-%d", chunkType, i),
			chunkType,
			fmt.Sprintf("test%d.go", i),
			baseTime,
		)
	}
	return chunks
}

func createTestChunk(id, chunkType, filePath string, timestamp time.Time) *ContextChunk {
	return &ContextChunk{
		ID:        id,
		Title:     fmt.Sprintf("Test %s", id),
		Text:      fmt.Sprintf("Test content for %s", id),
		ChunkType: chunkType,
		Embedding: make([]float32, 384), // Standard embedding size
		Tags:      []string{"go", "test"},
		Metadata: map[string]interface{}{
			"file_path": filePath,
			"language":  "go",
		},
		CreatedAt: timestamp,
		UpdatedAt: timestamp,
	}
}

func createChunkSetFromChunks(chunks []*ContextChunk) *ChunkSet {
	byID := make(map[string]*ContextChunk, len(chunks))
	byFile := make(map[string][]*ContextChunk)

	for _, chunk := range chunks {
		byID[chunk.ID] = chunk
		if filePath, ok := chunk.Metadata["file_path"].(string); ok {
			byFile[filePath] = append(byFile[filePath], chunk)
		}
	}

	return &ChunkSet{
		chunks: chunks,
		byID:   byID,
		byFile: byFile,
	}
}

func writeChunkFile(dir, filename string, chunks []*ContextChunk, chunkType string) error {
	wrapper := ChunkFileWrapper{
		Metadata: ChunkFileMetadata{
			Model:      "BAAI/bge-small-en-v1.5",
			Dimensions: 384,
			ChunkType:  chunkType,
			Generated:  time.Now().Format(time.RFC3339),
			Version:    "1.0.0",
		},
		Chunks: chunks,
	}

	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return err
	}

	path := filepath.Join(dir, filename)
	return os.WriteFile(path, data, 0644)
}
