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
	"path/filepath"
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

	// Create temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	// Enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema
	createFTSSchema(t, db)

	return db
}

func createFTSSchema(t *testing.T, db *sql.DB) {
	t.Helper()

	// Create files table
	_, err := db.Exec(`
		CREATE TABLE files (
			file_path TEXT PRIMARY KEY,
			language TEXT NOT NULL,
			module_path TEXT,
			line_count_total INTEGER NOT NULL DEFAULT 0
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

	_, err := db.Exec(
		"INSERT INTO files (file_path, language, module_path, line_count_total) VALUES (?, ?, ?, ?)",
		filePath, language, "internal/test", 100,
	)
	require.NoError(t, err)
}

func insertFTSTestChunk(t *testing.T, db *sql.DB, chunk *storage.Chunk) {
	t.Helper()

	// Insert chunk
	embBytes := storage.SerializeEmbedding(chunk.Embedding)

	_, err := db.Exec(
		`INSERT INTO chunks (chunk_id, file_path, chunk_type, title, text, embedding, start_line, end_line, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chunk.ID, chunk.FilePath, chunk.ChunkType, chunk.Title, chunk.Text,
		embBytes, nullableInt(chunk.StartLine), nullableInt(chunk.EndLine),
		chunk.CreatedAt.Format(time.RFC3339), chunk.UpdatedAt.Format(time.RFC3339),
	)
	require.NoError(t, err)

	// Insert into FTS5 index (within transaction for consistency)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	err = storage.UpdateFTSIndex(tx, []*storage.Chunk{chunk})
	require.NoError(t, err)

	err = tx.Commit()
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

		// Insert test data
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()
		chunk := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Test Function",
			Text:      "func TestHandler(w http.ResponseWriter, r *http.Request) { /* handler code */ }",
			Embedding: makeTestEmbedding(384),
			StartLine: 10,
			EndLine:   20,
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk)

		// Create searcher and query
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "handler", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
		assert.Equal(t, "chunk-1", results[0].Chunk.ID)
	})

	t.Run("returns results with snippets and highlighting", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with searchable content
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()
		chunk := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Authentication Handler",
			Text:      "func AuthHandler(w http.ResponseWriter, r *http.Request) error { return authenticate(r) }",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk)

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

		// Insert multiple test chunks
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()

		for i := 0; i < 5; i++ {
			chunk := &storage.Chunk{
				ID:        fmt.Sprintf("chunk-%d", i),
				FilePath:  "file1.go",
				ChunkType: "definitions",
				Title:     fmt.Sprintf("Function %d", i),
				Text:      fmt.Sprintf("func Handler%d() { /* handler code */ }", i),
				Embedding: makeTestEmbedding(384),
				CreatedAt: now,
				UpdatedAt: now,
			}
			insertFTSTestChunk(t, db, chunk)
		}

		// Search with limit (use prefix wildcard to match Handler0, Handler1, etc.)
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "handler*", &ExactSearchOptions{Limit: 3})
		require.NoError(t, err)
		assert.Len(t, results, 3)
	})

	t.Run("handles empty results gracefully", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()
		chunk := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Test",
			Text:      "func TestFunction() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk)

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

		// Insert test data
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()
		chunk := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Handler",
			Text:      "func Handler() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk)

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

	t.Run("builds tags from language and chunk_type", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()
		chunk := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Test",
			Text:      "test content",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk)

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
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()
		chunk := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Error Handler",
			Text:      "func HandleError(err error) { log.Printf(\"error occurred: %v\", err) }",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk)

		// Search with phrase query (FTS5 syntax)
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "\"error occurred\"", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results)
	})

	t.Run("boolean operators", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data
		insertFTSTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()

		chunk1 := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Handler",
			Text:      "func Handler(w http.ResponseWriter) {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunk2 := &storage.Chunk{
			ID:        "chunk-2",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Controller",
			Text:      "func Controller(req *Request) {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk1)
		insertFTSTestChunk(t, db, chunk2)

		// Search with boolean AND
		searcher, err := NewSQLiteExactSearcher(db)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Search(ctx, "Handler AND http", &ExactSearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "chunk-1", results[0].Chunk.ID)
	})

	t.Run("filters by language", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with different languages
		insertFTSTestFile(t, db, "file1.go", "go")
		insertFTSTestFile(t, db, "file2.ts", "typescript")
		now := time.Now().UTC()

		chunk1 := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Go Handler",
			Text:      "func Handler() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunk2 := &storage.Chunk{
			ID:        "chunk-2",
			FilePath:  "file2.ts",
			ChunkType: "definitions",
			Title:     "TS Handler",
			Text:      "function handler() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk1)
		insertFTSTestChunk(t, db, chunk2)

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
		assert.Equal(t, "chunk-1", results[0].Chunk.ID)
		assert.Contains(t, results[0].Chunk.Tags, "go")
	})

	t.Run("filters by file path", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with different file paths
		insertFTSTestFile(t, db, "internal/handler.go", "go")
		insertFTSTestFile(t, db, "pkg/controller.go", "go")
		now := time.Now().UTC()

		chunk1 := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "internal/handler.go",
			ChunkType: "definitions",
			Title:     "Internal Handler",
			Text:      "func Handler() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunk2 := &storage.Chunk{
			ID:        "chunk-2",
			FilePath:  "pkg/controller.go",
			ChunkType: "definitions",
			Title:     "Controller",
			Text:      "func Controller() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk1)
		insertFTSTestChunk(t, db, chunk2)

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
		assert.Equal(t, "chunk-1", results[0].Chunk.ID)
	})

	t.Run("filters by language and file path", func(t *testing.T) {
		t.Parallel()
		db := setupSQLiteExactSearcherTest(t)
		defer db.Close()

		// Insert test data with different languages and paths
		insertFTSTestFile(t, db, "internal/handler.go", "go")
		insertFTSTestFile(t, db, "internal/handler.ts", "typescript")
		insertFTSTestFile(t, db, "pkg/handler.go", "go")
		now := time.Now().UTC()

		chunk1 := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "internal/handler.go",
			ChunkType: "definitions",
			Title:     "Go Handler",
			Text:      "func Handler() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunk2 := &storage.Chunk{
			ID:        "chunk-2",
			FilePath:  "internal/handler.ts",
			ChunkType: "definitions",
			Title:     "TS Handler",
			Text:      "function handler() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunk3 := &storage.Chunk{
			ID:        "chunk-3",
			FilePath:  "pkg/handler.go",
			ChunkType: "definitions",
			Title:     "Pkg Handler",
			Text:      "func Handler() {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertFTSTestChunk(t, db, chunk1)
		insertFTSTestChunk(t, db, chunk2)
		insertFTSTestChunk(t, db, chunk3)

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
		assert.Equal(t, "chunk-1", results[0].Chunk.ID)
	})
}
