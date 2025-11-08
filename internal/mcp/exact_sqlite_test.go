package mcp

// Test Plan for SQLite Exact Searcher:
// - NewSQLiteExactSearcher requires database connection
// - Search executes FTS5 full-text search
// - Search returns results with snippets and highlighting
// - Search applies limit correctly
// - Search handles empty results gracefully
// - Search converts BM25 rank to score
// - Search builds tags from language and chunk_type
// - UpdateIncremental is no-op (returns nil)
// - Close is no-op (database externally managed)
// - Integration: Search with phrase queries
// - Integration: Search with boolean operators

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helpers for FTS5 searcher

func setupSQLiteExactSearcherTest(t *testing.T) *sql.DB {
	t.Helper()

	// Use storage package test DB which creates full schema including FTS5
	db := storage.NewTestDB(t)

	return db
}

func createFTSSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	// Create files table with all required columns for WriteFile
	_, err := db.Exec(`
		CREATE TABLE files (
			file_path TEXT PRIMARY KEY,
			language TEXT NOT NULL,
			module_path TEXT NOT NULL,
			is_test INTEGER NOT NULL DEFAULT 0,
			line_count_total INTEGER NOT NULL DEFAULT 0,
			line_count_code INTEGER NOT NULL DEFAULT 0,
			line_count_comment INTEGER NOT NULL DEFAULT 0,
			line_count_blank INTEGER NOT NULL DEFAULT 0,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			file_hash TEXT NOT NULL,
			last_modified TEXT NOT NULL,
			indexed_at TEXT NOT NULL,
			content TEXT
		)
	`)
	require.NoError(t, err)

	// Create chunks table
	_, err = db.Exec(`
		CREATE TABLE chunks (
			chunk_id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			chunk_type TEXT NOT NULL,
			title TEXT NOT NULL,
			text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			start_line INTEGER,
			end_line INTEGER,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
		)
	`)
	require.NoError(t, err)

	// Create FTS5 index
	err = storage.CreateFTSIndex(db)
	require.NoError(t, err)
}

func insertFTSTestFile(t *testing.T, db *sql.DB, filePath, language string) {
	t.Helper()

	fileWriter := storage.NewFileWriter(db)
	err := fileWriter.WriteFileStats(&storage.FileStats{
		FilePath:   filePath,
		Language:   language,
		ModulePath: "internal/test",
		FileHash:   "test-hash",
	})
	require.NoError(t, err)
}

func insertFTSTestFileWithContent(t *testing.T, db *sql.DB, filePath, language, content string) {
	t.Helper()

	// Write file stats and content atomically using new API
	fileWriter := storage.NewFileWriter(db)
	now := time.Now()
	err := fileWriter.WriteFile(&storage.FileStats{
		FilePath:     filePath,
		Language:     language,
		ModulePath:   "internal/test",
		FileHash:     "test-hash",
		LastModified: now,
		IndexedAt:    now,
	}, &content)
	require.NoError(t, err)
}

func insertFTSTestChunk(t *testing.T, db *sql.DB, chunk *storage.Chunk) {
	t.Helper()

	// Use ChunkWriter to insert chunk (handles FTS automatically)
	chunkWriter := storage.NewChunkWriterWithDB(db)
	err := chunkWriter.WriteChunksIncremental([]*storage.Chunk{chunk})
	require.NoError(t, err)
}

// Constructor Tests

func TestNewSQLiteExactSearcher(t *testing.T) {
	t.Parallel()

	t.Run("requires database connection", func(t *testing.T) {
		t.Parallel()

		searcher, err := NewSQLiteExactSearcher(nil)
		assert.Error(t, err)
		assert.Nil(t, searcher)
		assert.Contains(t, err.Error(), "database connection is required")
	})

	t.Run("creates searcher successfully", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		require.NotNil(t, searcher)
		defer searcher.Close()
	})
}

// Search Tests

func TestSQLiteExactSearcherSearch(t *testing.T) {
	t.Parallel()

	t.Run("executes FTS5 full-text search", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data - FILE content for FTS5 search
		fileContent := "func TestHandler(w http.ResponseWriter, r *http.Request) { /* handler code */ }"
		insertFTSTestFileWithContent(t, db, "file1.go", "go", fileContent)

		// Create searcher and query
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "handler", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
		filePath, _ := results[0].Chunk.Metadata["file_path"].(string)
		assert.Equal(t, "file1.go", filePath)
	})

	t.Run("returns results with snippets and highlighting", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data - FILE content for FTS5 search
		fileContent := "func AuthHandler(w http.ResponseWriter, r *http.Request) error { return authenticate(r) }"
		insertFTSTestFileWithContent(t, db, "file1.go", "go", fileContent)

		// Search for keyword
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "AuthHandler", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		require.Len(t, results, 1)

		// Verify highlights
		result := results[0]
		assert.NotEmpty(t, result.Highlights)
		assert.Contains(t, result.Highlights[0], "<mark>")
		assert.Contains(t, result.Highlights[0], "</mark>")
	})

	t.Run("applies limit correctly", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert multiple test files (file-level search)
		for i := 0; i < 5; i++ {
			fileContent := fmt.Sprintf("func Handler%d() { /* handler code */ }", i)
			insertFTSTestFileWithContent(t, db, fmt.Sprintf("file%d.go", i), "go", fileContent)
		}

		// Search with limit
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "handler", &ExactSearchOptions{Limit: 3})
		require.NoError(t, err)
		assert.Len(t, results, 3, "should limit results to 3")
	})

	t.Run("handles empty results gracefully", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data - FILE content
		fileContent := "func TestFunction() {}"
		insertFTSTestFileWithContent(t, db, "file1.go", "go", fileContent)

		// Search for non-existent term
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "nonexistent", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.Empty(t, results)
	})

	t.Run("converts BM25 rank to score", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data - FILE content
		fileContent := "func Handler() {}"
		insertFTSTestFileWithContent(t, db, "file1.go", "go", fileContent)

		// Search
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "Handler", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		require.NotEmpty(t, results)

		// Verify score is positive (BM25 rank is negative)
		assert.Greater(t, results[0].Score, 0.0)
	})

	t.Run("builds tags from language", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data - FILE content
		fileContent := "test content"
		insertFTSTestFileWithContent(t, db, "file1.go", "go", fileContent)

		// Search
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "test", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		require.NotEmpty(t, results)

		// Verify tags
		tags := results[0].Chunk.Tags
		assert.Contains(t, tags, "go")
		assert.Contains(t, tags, "code")
	})
}

// Lifecycle Tests

func TestSQLiteExactSearcherUpdateIncremental(t *testing.T) {
	t.Parallel()

	t.Run("is no-op", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		err = searcher.UpdateIncremental(ctx, nil, nil, nil)
		assert.NoError(t, err)
	})
}

func TestSQLiteExactSearcherClose(t *testing.T) {
	t.Parallel()

	t.Run("is no-op", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)

		err = searcher.Close()
		assert.NoError(t, err)

		// Database should still be usable (not closed)
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&count)
		assert.NoError(t, err)
	})
}

// Integration Tests

func TestSQLiteExactSearcherIntegration(t *testing.T) {
	t.Parallel()

	t.Run("phrase query", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with exact phrase
		fileContent := "package main\n\nfunc HandleError(err error) { log.Printf(\"error occurred: %v\", err) }\n"
		insertFTSTestFileWithContent(t, db, "file1.go", "go", fileContent)

		// Search with phrase query (FTS5 syntax)
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "\"error occurred\"", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results, "should find file containing phrase")
		if len(results) > 0 {
			filePath, _ := results[0].Chunk.Metadata["file_path"].(string)
			assert.Equal(t, "file1.go", filePath)
			assert.Contains(t, results[0].Highlights[0], "error occurred")
		}
	})

	t.Run("boolean operators", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with content that matches boolean query
		fileContent := "package main\n\nfunc Handler(w http.ResponseWriter) {}\nfunc Controller(req *Request) {}\n"
		insertFTSTestFileWithContent(t, db, "file1.go", "go", fileContent)

		// Search with boolean AND
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "Handler AND http", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
		if len(results) > 0 {
			filePath, _ := results[0].Chunk.Metadata["file_path"].(string)
			assert.Equal(t, "file1.go", filePath)
		}
	})

	t.Run("filters by language", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with different languages
		insertFTSTestFileWithContent(t, db, "file1.go", "go", "package main\n\nfunc Handler() {}\n")
		insertFTSTestFileWithContent(t, db, "file2.ts", "typescript", "function handler() {}\n")

		// Search with language filter
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "handler", &ExactSearchOptions{
			Limit:    10,
			Language: "go",
		})
		require.NoError(t, err)
		assert.Len(t, results, 1)
		filePath, _ := results[0].Chunk.Metadata["file_path"].(string)
		assert.Equal(t, "file1.go", filePath)
		language, _ := results[0].Chunk.Metadata["language"].(string)
		assert.Equal(t, "go", language)
	})

	t.Run("filters by file path", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with different file paths
		insertFTSTestFileWithContent(t, db, "internal/handler.go", "go", "func Handler() {}\n")
		insertFTSTestFileWithContent(t, db, "pkg/controller.go", "go", "func Controller() {}\n")

		// Search with file path filter (SQL LIKE pattern)
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "func", &ExactSearchOptions{
			Limit:    10,
			FilePath: "internal/%",
		})
		require.NoError(t, err)
		assert.Len(t, results, 1)
		filePath, _ := results[0].Chunk.Metadata["file_path"].(string)
		assert.Equal(t, "internal/handler.go", filePath)
	})

	t.Run("filters by language and file path", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with different languages and paths
		insertFTSTestFileWithContent(t, db, "internal/handler.go", "go", "func Handler() {}\n")
		insertFTSTestFileWithContent(t, db, "internal/handler.ts", "typescript", "function handler() {}\n")
		insertFTSTestFileWithContent(t, db, "pkg/handler.go", "go", "func Handler() {}\n")

		// Search with both filters
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "handler", &ExactSearchOptions{
			Limit:    10,
			Language: "go",
			FilePath: "internal/%",
		})
		require.NoError(t, err)
		assert.Len(t, results, 1)
		filePath, _ := results[0].Chunk.Metadata["file_path"].(string)
		assert.Equal(t, "internal/handler.go", filePath)
	})
}
