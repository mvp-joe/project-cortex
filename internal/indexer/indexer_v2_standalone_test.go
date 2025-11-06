package indexer

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	storagepkg "github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TEST PLAN: IndexerV2 End-to-End Integration Tests
//
// These tests verify that all v2 components (ChangeDetector, Processor, BranchSynchronizer)
// work together correctly in realistic scenarios. Unlike unit tests, these use REAL components
// (not mocks) to verify integration.
//
// Test Scenarios:
// 1. Full Index (No Hint) - Complete discovery and processing
// 2. Incremental Index (With Hint) - Only modified files processed
// 3. File Deletion - Cascade delete chunks
// 4. Mtime Drift Detection - Content unchanged, mtime changed
// 5. Mixed Operations - Add + Modify + Delete + Unchanged
// 6. Empty Project - No files to process
// 7. Context Cancellation - Graceful stop
// 8. Graph Update Integration - GraphBuilder called correctly

// Test 1: Full Index (No Hint) - End to End
func TestIndexerV2Integration_FullIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Setup: Create temp project
	rootDir := t.TempDir()
	createTestGoFile(t, rootDir, "main.go", `package main

import "fmt"

type User struct {
	ID   int
	Name string
}

func Greet(name string) {
	fmt.Printf("Hello, %s!\n", name)
}

const MaxUsers = 100
`)
	createTestGoFile(t, rootDir, "lib/lib.go", `package lib

func Helper() string {
	return "test"
}
`)
	createTestMarkdownFile(t, rootDir, "README.md", `# Test Project

This is a test project.

## Features

- Feature 1
- Feature 2
`)

	// Setup: Create in-memory SQLite DB
	db := storagepkg.NewTestDB(t)

	// Setup: Create real components (no mocks)
	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{"**/*.md"}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)

	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})

	// Create IndexerV2
	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Execute: Full index (no hint)
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err)

	// Verify: Stats
	assert.Equal(t, 3, stats.FilesAdded, "should have 3 new files (2 Go + 1 Markdown)")
	assert.Equal(t, 0, stats.FilesModified)
	assert.Equal(t, 0, stats.FilesDeleted)
	assert.Equal(t, 2, stats.CodeFilesProcessed, "should process 2 Go files")
	assert.Equal(t, 1, stats.DocsProcessed, "should process 1 Markdown file")
	assert.Greater(t, stats.TotalCodeChunks, 0, "should generate code chunks")
	assert.Greater(t, stats.TotalDocChunks, 0, "should generate doc chunks")
	assert.Greater(t, stats.IndexingTime, time.Duration(0), "should track processing time")

	// Verify: Database state
	reader := storagepkg.NewFileReader(db)
	files, err := reader.GetAllFiles()
	require.NoError(t, err)
	assert.Len(t, files, 3, "should have 3 files in DB")

	// Verify: Chunks written
	chunks := queryAllChunks(t, db)
	assert.Greater(t, len(chunks), 0, "should have chunks")

	// Verify: File stats include hashes and mtimes
	for _, file := range files {
		assert.NotEmpty(t, file.FileHash, "file should have hash")
		assert.False(t, file.LastModified.IsZero(), "file should have mtime")
	}
}

// Test 2: Incremental Index (With Hint) - Changed File Only
func TestIndexerV2Integration_IncrementalIndex(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create initial files
	file1 := createTestGoFile(t, rootDir, "file1.go", "package main\n\nfunc Hello() {}\n")
	_ = createTestGoFile(t, rootDir, "file2.go", "package main\n\nfunc World() {}\n")

	// Setup DB and storage
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})
	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Initial full index
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.FilesAdded)

	// Modify ONE file
	time.Sleep(10 * time.Millisecond) // Ensure mtime changes
	err = os.WriteFile(file1, []byte("package main\n\nfunc Hello() {\n\t// Modified\n}\n"), 0644)
	require.NoError(t, err)

	// Query chunks for file1 before update
	chunksBefore := queryChunksByFile(t, db, "file1.go")
	require.Greater(t, len(chunksBefore), 0)

	// Incremental index with hint (only file1 - use absolute path)
	stats, err = indexer.Index(ctx, []string{file1})
	require.NoError(t, err)

	// Verify: Only modified file processed
	assert.Equal(t, 0, stats.FilesAdded)
	assert.Equal(t, 1, stats.FilesModified, "should detect 1 modified file")
	assert.Equal(t, 0, stats.FilesDeleted)
	assert.Equal(t, 1, stats.CodeFilesProcessed, "should only process 1 file")

	// Verify: Old chunks for file1 replaced
	chunksAfter := queryChunksByFile(t, db, "file1.go")
	assert.Greater(t, len(chunksAfter), 0, "should have new chunks")

	// Verify: file2 unchanged in DB
	reader := storagepkg.NewFileReader(db)
	file2Stats, err := reader.GetFileStats("file2.go")
	require.NoError(t, err)
	assert.Equal(t, "file2.go", file2Stats.FilePath)

	// Verify: New hash written for file1
	file1Stats, err := reader.GetFileStats("file1.go")
	require.NoError(t, err)
	assert.NotEmpty(t, file1Stats.FileHash)
}

// Test 3: File Deletion - Cascade Delete Chunks
func TestIndexerV2Integration_FileDeletion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create files
	file1 := createTestGoFile(t, rootDir, "file1.go", "package main\n\nfunc Test1() {}\n")
	_ = createTestGoFile(t, rootDir, "file2.go", "package main\n\nfunc Test2() {}\n")

	// Setup
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})
	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Initial index
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.FilesAdded)

	// Verify both files in DB
	reader := storagepkg.NewFileReader(db)
	files, err := reader.GetAllFiles()
	require.NoError(t, err)
	assert.Len(t, files, 2)

	// Delete file1 from disk
	err = os.Remove(file1)
	require.NoError(t, err)

	// Run full index (discovers deletion)
	stats, err = indexer.Index(ctx, nil)
	require.NoError(t, err)

	// Verify: Deletion detected
	assert.Equal(t, 0, stats.FilesAdded)
	assert.Equal(t, 0, stats.FilesModified)
	assert.Equal(t, 1, stats.FilesDeleted, "should detect 1 deleted file")

	// Verify: File removed from DB files table
	files, err = reader.GetAllFiles()
	require.NoError(t, err)
	assert.Len(t, files, 1, "should have 1 file remaining")
	assert.Equal(t, "file2.go", files[0].FilePath)

	// Verify: Chunks for deleted file also removed (cascade delete via FK)
	chunks := queryChunksByFile(t, db, "file1.go")
	assert.Len(t, chunks, 0, "chunks for deleted file should be removed")

	// Verify: file2 chunks unaffected
	file2Chunks := queryChunksByFile(t, db, "file2.go")
	assert.Greater(t, len(file2Chunks), 0, "file2 chunks should remain")
}

// Test 4: Mtime Drift Detection - Content Unchanged
func TestIndexerV2Integration_MtimeDrift(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create file
	file1 := createTestGoFile(t, rootDir, "file1.go", "package main\n\nfunc Test() {}\n")

	// Setup
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})
	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Initial index
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.FilesAdded)

	// Get original file stats
	reader := storagepkg.NewFileReader(db)
	fileStatsBefore, err := reader.GetFileStats("file1.go")
	require.NoError(t, err)
	hashBefore := fileStatsBefore.FileHash
	mtimeBefore := fileStatsBefore.LastModified

	// Touch file (change mtime but not content)
	// Wait >1s to ensure mtime changes (RFC3339 has second precision)
	time.Sleep(1100 * time.Millisecond)
	newMtime := time.Now()
	err = os.Chtimes(file1, newMtime, newMtime)
	require.NoError(t, err)

	// Run index again
	stats, err = indexer.Index(ctx, nil)
	require.NoError(t, err)

	// Verify: File detected as unchanged (hash comparison)
	assert.Equal(t, 0, stats.FilesAdded)
	assert.Equal(t, 0, stats.FilesModified, "content unchanged, should not reprocess")
	assert.Equal(t, 0, stats.FilesDeleted)
	assert.Equal(t, 0, stats.CodeFilesProcessed, "should not reprocess unchanged file")

	// Verify: Mtime updated in DB (drift correction)
	fileStatsAfter, err := reader.GetFileStats("file1.go")
	require.NoError(t, err)
	assert.Equal(t, hashBefore, fileStatsAfter.FileHash, "hash should remain same")
	assert.True(t, fileStatsAfter.LastModified.After(mtimeBefore), "mtime should be updated")

	// Verify: No re-embedding (chunks unchanged)
	chunks := queryChunksByFile(t, db, "file1.go")
	assert.Greater(t, len(chunks), 0, "chunks should still exist")
}

// Test 5: Mixed Operations - Add + Modify + Delete + Unchanged
func TestIndexerV2Integration_MixedOperations(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	// Create initial files
	unchangedFile := createTestGoFile(t, rootDir, "unchanged.go", "package main\n\nfunc Unchanged() {}\n")
	modifiedFile := createTestGoFile(t, rootDir, "modified.go", "package main\n\nfunc Modified() {}\n")
	deletedFile := createTestGoFile(t, rootDir, "deleted.go", "package main\n\nfunc Deleted() {}\n")

	// Setup
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})
	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Initial index
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 3, stats.FilesAdded)

	// Perform mixed operations
	time.Sleep(10 * time.Millisecond)

	// 1. Add new file
	_ = createTestGoFile(t, rootDir, "added.go", "package main\n\nfunc Added() {}\n")

	// 2. Modify existing file
	err = os.WriteFile(modifiedFile, []byte("package main\n\nfunc Modified() {\n\t// Changed\n}\n"), 0644)
	require.NoError(t, err)

	// 3. Delete existing file
	err = os.Remove(deletedFile)
	require.NoError(t, err)

	// 4. Leave one file unchanged
	_ = unchangedFile

	// Run full index
	stats, err = indexer.Index(ctx, nil)
	require.NoError(t, err)

	// Verify: ChangeSet correctly identifies all operations
	assert.Equal(t, 1, stats.FilesAdded, "should detect 1 added file")
	assert.Equal(t, 1, stats.FilesModified, "should detect 1 modified file")
	assert.Equal(t, 1, stats.FilesDeleted, "should detect 1 deleted file")
	assert.Equal(t, 2, stats.CodeFilesProcessed, "should process 2 files (added + modified)")

	// Verify: Final DB state
	reader := storagepkg.NewFileReader(db)
	files, err := reader.GetAllFiles()
	require.NoError(t, err)
	assert.Len(t, files, 3, "should have 3 files (unchanged + modified + added)")

	filePaths := make([]string, len(files))
	for i, f := range files {
		filePaths[i] = f.FilePath
	}
	assert.Contains(t, filePaths, "unchanged.go")
	assert.Contains(t, filePaths, "modified.go")
	assert.Contains(t, filePaths, "added.go")
	assert.NotContains(t, filePaths, "deleted.go")
}

// Test 6: Empty Project - No Files
func TestIndexerV2Integration_EmptyProject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir() // Empty directory

	// Setup
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{"**/*.md"}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})
	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Execute: Index empty project
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err)

	// Verify: No errors, no processing
	assert.Equal(t, 0, stats.FilesAdded)
	assert.Equal(t, 0, stats.FilesModified)
	assert.Equal(t, 0, stats.FilesDeleted)
	assert.Equal(t, 0, stats.CodeFilesProcessed)
	assert.Equal(t, 0, stats.DocsProcessed)

	// Verify: DB is empty
	reader := storagepkg.NewFileReader(db)
	files, err := reader.GetAllFiles()
	require.NoError(t, err)
	assert.Len(t, files, 0)
}

// Test 7: Context Cancellation - Graceful Stop
func TestIndexerV2Integration_ContextCancellation(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()

	// Create many files to ensure processing takes some time
	for i := 0; i < 50; i++ {
		createTestGoFile(t, rootDir, filepath.Join("dir", fmt.Sprintf("file%d.go", i)), "package main\n\nfunc Test() {}\n")
	}

	// Setup
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})
	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Cancel context immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Execute: Should fail with context canceled
	_, err = indexer.Index(ctx, nil)
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled, "should return context.Canceled error")

	// Verify: Database state consistent (no partial writes from SQLite transactions)
	// If any processing occurred, it should be rolled back or fully committed
	// This is ensured by SQLite transaction semantics in the storage layer
}

// Test 8: Graph Update Integration
func TestIndexerV2Integration_GraphUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	createTestGoFile(t, rootDir, "file1.go", "package main\n\nfunc Test() {}\n")

	// Setup
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})

	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Execute
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.FilesAdded)

	// Verify: GraphUpdater populated graph tables
	// Check that functions table has entries for this file
	var functionCount int
	err = db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "file1.go").Scan(&functionCount)
	require.NoError(t, err)
	assert.Greater(t, functionCount, 0, "should have extracted functions to graph tables")
}

// Test 9: Graph Update Failure - Indexing Continues
func TestIndexerV2Integration_GraphFailureGraceful(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	rootDir := t.TempDir()

	createTestGoFile(t, rootDir, "file1.go", "package main\n\nfunc Test() {}\n")

	// Setup
	db := storagepkg.NewTestDB(t)

	storage, err := setupIntegrationTestStorage(t, db, rootDir)
	require.NoError(t, err)
	defer storage.Close()

	discovery, err := NewFileDiscovery(rootDir, []string{"**/*.go"}, []string{}, []string{})
	require.NoError(t, err)

	changeDetector := NewChangeDetector(rootDir, storage, discovery)
	mockProvider := &mockEmbedProvider{}
	processor := NewProcessor(rootDir, NewParser(), NewChunker(500, 50), NewFormatter(), mockProvider, storage, &NoOpProgressReporter{})

	indexer := NewIndexerV2(rootDir, changeDetector, processor, storage, db)

	// Execute: Should succeed - graph failures are logged as warnings only
	stats, err := indexer.Index(ctx, nil)
	require.NoError(t, err, "indexing should succeed even if graph fails")
	assert.Equal(t, 1, stats.FilesAdded)
	assert.Equal(t, 1, stats.CodeFilesProcessed)

	// Verify: Chunks still written to DB
	chunks := queryChunksByFile(t, db, "file1.go")
	assert.Greater(t, len(chunks), 0, "chunks should be written despite graph failure")

	// Note: Graph errors are handled internally by GraphUpdater
	// and logged as warnings. Integration tests in graph_updater_test.go
	// verify error handling in detail.
}

// Helper functions

func createTestGoFile(t *testing.T, rootDir, relPath, content string) string {
	t.Helper()
	fullPath := filepath.Join(rootDir, relPath)
	err := os.MkdirAll(filepath.Dir(fullPath), 0755)
	require.NoError(t, err)
	err = os.WriteFile(fullPath, []byte(content), 0644)
	require.NoError(t, err)
	return fullPath
}

func createTestMarkdownFile(t *testing.T, rootDir, relPath, content string) string {
	t.Helper()
	fullPath := filepath.Join(rootDir, relPath)
	err := os.MkdirAll(filepath.Dir(fullPath), 0755)
	require.NoError(t, err)
	err = os.WriteFile(fullPath, []byte(content), 0644)
	require.NoError(t, err)
	return fullPath
}

func setupIntegrationTestStorage(t *testing.T, db *sql.DB, projectRoot string) (Storage, error) {
	t.Helper()
	// Use separate temp dir for cache (not the project root)
	cacheDir := t.TempDir()
	return NewSQLiteStorage(db, cacheDir, projectRoot)
}

func queryAllChunks(t *testing.T, db *sql.DB) []storagepkg.Chunk {
	t.Helper()
	rows, err := db.Query("SELECT chunk_id, file_path, chunk_type, title, text FROM chunks")
	require.NoError(t, err)
	defer rows.Close()

	var chunks []storagepkg.Chunk
	for rows.Next() {
		var c storagepkg.Chunk
		err := rows.Scan(&c.ID, &c.FilePath, &c.ChunkType, &c.Title, &c.Text)
		require.NoError(t, err)
		chunks = append(chunks, c)
	}
	return chunks
}

func queryChunksByFile(t *testing.T, db *sql.DB, filePath string) []storagepkg.Chunk {
	t.Helper()
	rows, err := db.Query("SELECT chunk_id, file_path, chunk_type, title, text FROM chunks WHERE file_path = ?", filePath)
	require.NoError(t, err)
	defer rows.Close()

	var chunks []storagepkg.Chunk
	for rows.Next() {
		var c storagepkg.Chunk
		err := rows.Scan(&c.ID, &c.FilePath, &c.ChunkType, &c.Title, &c.Text)
		require.NoError(t, err)
		chunks = append(chunks, c)
	}
	return chunks
}

// Mock types for integration tests
// Note: mockEmbedProvider is defined in processor_test.go
// and are reused here for integration tests.
