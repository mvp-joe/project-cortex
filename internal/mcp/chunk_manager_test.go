package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan:
// 1. Test ChunkManager.Load() with all chunk types from SQLite
// 2. Test ChunkManager.Load() with missing files (should return partial data)
// 3. Test ChunkManager.Load() with empty directory (should error: cache not found)
// 4. Test ChunkManager.Load() with corrupted SQLite (should return error)
// 5. Test ChunkSet lookup methods (GetByID, GetByFile, All, Len)
// 6. Test ChunkManager.DetectChanges() for added chunks
// 7. Test ChunkManager.DetectChanges() for updated chunks (timestamp after lastReload)
// 8. Test ChunkManager.DetectChanges() for deleted chunks
// 9. Test ChunkManager.DetectChanges() for unchanged chunks (timestamp before lastReload)
// 10. Test ChunkManager.DetectChanges() with nil current set (all added)
// 11. Test ChunkManager atomic operations (Update, GetCurrent, GetLastReloadTime)
// 12. Test thread-safety with concurrent GetCurrent() calls
// 13. Test thread-safety with concurrent Load() + GetCurrent()
// 14. Test thread-safety with Update() during GetCurrent()
// 15. Test context cancellation during Load()

func TestChunkManager_Load_AllChunkTypes(t *testing.T) {
	t.Parallel()

	// Create temp directory and setup git repo
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)
	baseTime := time.Now().Add(-1 * time.Hour)

	// Create test chunks for each type with unique file names to avoid overwrites
	symbolsChunks := createTestChunksWithPrefix("symbols", 2, baseTime, "symbols")
	definitionsChunks := createTestChunksWithPrefix("definitions", 3, baseTime, "definitions")
	dataChunks := createTestChunksWithPrefix("data", 1, baseTime, "data")
	docChunks := createTestChunksWithPrefix("documentation", 2, baseTime, "doc")

	// Collect all chunks and write once
	allChunks := append([]*ContextChunk{}, symbolsChunks...)
	allChunks = append(allChunks, definitionsChunks...)
	allChunks = append(allChunks, dataChunks...)
	allChunks = append(allChunks, docChunks...)

	// Write all chunks to SQLite cache in one call
	require.NoError(t, writeChunkFile(tmpDir, "", allChunks, "mixed"))

	// Test loading
	cm := NewChunkManager(tmpDir)
	ctx := context.Background()
	chunkSet, err := cm.Load(ctx)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, chunkSet)
	assert.Equal(t, 8, chunkSet.Len(), "should load all chunks")

	// Verify each chunk type is present
	loadedChunks := chunkSet.All()
	chunkTypes := make(map[string]int)
	for _, chunk := range loadedChunks {
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

	// Create temp directory with git repo but no indexed chunks
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	// Test loading from empty directory (cache not initialized)
	cm := NewChunkManager(tmpDir)
	ctx := context.Background()
	_, err := cm.Load(ctx)

	// Verify: should fail with "cache not found" error (need to run 'cortex index' first)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SQLite cache not found")
	assert.Contains(t, err.Error(), "run 'cortex index' first")
}

func TestChunkManager_Load_CorruptedSQLite(t *testing.T) {
	t.Parallel()

	// Create temp directory with git repo
	tmpDir := t.TempDir()
	setupGitRepo(t, tmpDir)

	// Create cache settings and corrupt the SQLite database
	settings, err := cache.LoadOrCreateSettings(tmpDir)
	require.NoError(t, err)

	branchesDir := filepath.Join(settings.CacheLocation, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	branch := cache.GetCurrentBranch(tmpDir)
	dbPath := filepath.Join(branchesDir, fmt.Sprintf("%s.db", branch))

	// Write corrupted SQLite file (not a valid database)
	require.NoError(t, os.WriteFile(dbPath, []byte("not a valid sqlite database"), 0644))

	// Test loading
	cm := NewChunkManager(tmpDir)
	ctx := context.Background()
	_, err = cm.Load(ctx)

	// Verify: should return error about corrupted/invalid database
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open chunk reader")
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

func createTestChunksWithPrefix(chunkType string, count int, baseTime time.Time, prefix string) []*ContextChunk {
	chunks := make([]*ContextChunk, count)
	for i := 0; i < count; i++ {
		chunks[i] = createTestChunk(
			fmt.Sprintf("%s-%d", chunkType, i),
			chunkType,
			fmt.Sprintf("%s-%d.go", prefix, i),
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

func writeChunkFile(projectDir, filename string, chunks []*ContextChunk, chunkType string) error {
	// For SQLite storage, write to cache location (not projectDir)
	// This matches how LoadChunksFromSQLite expects to find chunks
	settings, err := cache.LoadOrCreateSettings(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load cache settings: %w", err)
	}

	branch := cache.GetCurrentBranch(projectDir)
	branchesDir := filepath.Join(settings.CacheLocation, "branches")
	if err := os.MkdirAll(branchesDir, 0755); err != nil {
		return fmt.Errorf("failed to create branches directory: %w", err)
	}

	dbPath := filepath.Join(branchesDir, fmt.Sprintf("%s.db", branch))

	// Initialize vector extension and open database
	storage.InitVectorExtension()

	// Open database (let ChunkWriter create schema if needed)
	writer, err := storage.NewChunkWriter(dbPath)
	if err != nil {
		return fmt.Errorf("failed to create chunk writer: %w", err)
	}
	defer writer.Close()

	// Collect unique file paths to create file records
	filePathsMap := make(map[string]bool)
	for _, c := range chunks {
		if fp, ok := c.Metadata["file_path"].(string); ok {
			filePathsMap[fp] = true
		}
	}

	// Insert file records first (required for FK constraint)
	// Use the same dbPath to open another connection temporarily for file inserts
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	fileWriter := storage.NewFileWriter(db)
	for filePath := range filePathsMap {
		// Determine language from file extension
		language := "unknown"
		if ext := filepath.Ext(filePath); ext != "" {
			switch ext {
			case ".go":
				language = "go"
			case ".ts", ".tsx":
				language = "typescript"
			case ".js", ".jsx":
				language = "javascript"
			case ".py":
				language = "python"
			case ".md":
				language = "markdown"
			}
		}

		fileStats := &storage.FileStats{
			FilePath:     filePath,
			FileHash:     "", // Not needed for tests
			Language:     language,
			ModulePath:   "", // Not needed for tests
			LastModified: time.Now(),
			IndexedAt:    time.Now(),
			IsTest:       false,
		}
		if err := fileWriter.WriteFileStats(fileStats); err != nil {
			return fmt.Errorf("failed to insert file %s: %w", filePath, err)
		}
	}

	// Convert ContextChunk to storage.Chunk
	storageChunks := make([]*storage.Chunk, len(chunks))
	for i, c := range chunks {
		filePath := ""
		if fp, ok := c.Metadata["file_path"].(string); ok {
			filePath = fp
		}

		startLine := 0
		if sl, ok := c.Metadata["start_line"].(int); ok {
			startLine = sl
		}

		endLine := 0
		if el, ok := c.Metadata["end_line"].(int); ok {
			endLine = el
		}

		storageChunks[i] = &storage.Chunk{
			ID:        c.ID,
			FilePath:  filePath,
			ChunkType: c.ChunkType,
			Title:     c.Title,
			Text:      c.Text,
			Embedding: c.Embedding,
			StartLine: startLine,
			EndLine:   endLine,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
		}
	}

	// Write chunks using incremental mode to avoid clearing existing chunks
	return writer.WriteChunksIncremental(storageChunks)
}
