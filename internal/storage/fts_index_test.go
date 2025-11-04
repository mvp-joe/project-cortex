package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for FTS Index:
// - CreateFTSIndex creates FTS5 virtual table successfully
// - CreateFTSIndex is idempotent (IF NOT EXISTS)
// - UpdateFTSIndex inserts text entries for chunks
// - UpdateFTSIndex performs upsert (replaces existing entries)
// - UpdateFTSIndex handles empty chunk slice
// - DeleteFTSByFile removes FTS entries for specified chunk IDs
// - DeleteFTSByFile handles empty ID slice
// - QueryFTS finds chunks by simple keyword
// - QueryFTS supports phrase search (quoted strings)
// - QueryFTS supports boolean AND operator
// - QueryFTS supports boolean OR operator
// - QueryFTS supports boolean NOT operator
// - QueryFTS supports prefix matching with wildcard
// - QueryFTS returns results ordered by BM25 rank
// - QueryFTS returns snippets with highlighting (<mark> tags)
// - QueryFTS respects limit parameter
// - QueryFTS filters by chunk_type
// - QueryFTS filters by file_path
// - SearchText returns chunks without snippet metadata
// - BuildFTSQuery handles phrase queries
// - BuildFTSQuery escapes special characters
// - GetFTSStats returns correct entry count
// - Round-trip: Insert text → Search → Verify results
// - Integration: FTS search combined with chunks table data
// - Benchmark: FTS query performance with 10K entries

func TestCreateFTSIndex(t *testing.T) {
	t.Parallel()

	t.Run("creates FTS5 virtual table", func(t *testing.T) {
		t.Parallel()

		db := openFTSTestDB(t)
		defer db.Close()

		err := CreateFTSIndex(db)
		require.NoError(t, err)

		// Verify table exists
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='chunks_fts'").Scan(&tableName)
		require.NoError(t, err)
		assert.Equal(t, "chunks_fts", tableName)
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()

		db := openFTSTestDB(t)
		defer db.Close()

		// Create twice
		err := CreateFTSIndex(db)
		require.NoError(t, err)

		err = CreateFTSIndex(db)
		require.NoError(t, err) // Should not error
	})
}

func TestUpdateFTSIndex(t *testing.T) {
	t.Parallel()

	t.Run("inserts text entries for chunks", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDB(t)
		defer db.Close()

		tx, _ := db.Begin()
		defer tx.Rollback()

		chunks := []*Chunk{
			{
				ID:        "chunk-1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Handler",
				Text:      "func handleRequest(w http.ResponseWriter, r *http.Request)",
			},
			{
				ID:        "chunk-2",
				FilePath:  "file2.go",
				ChunkType: "definitions",
				Title:     "User",
				Text:      "type User struct { ID int; Name string }",
			},
		}

		err := UpdateFTSIndex(tx, chunks)
		require.NoError(t, err)
		tx.Commit()

		// Verify entries inserted
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks_fts").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("performs upsert", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDB(t)
		defer db.Close()

		// Insert initial
		tx1, _ := db.Begin()
		chunk1 := &Chunk{
			ID:   "chunk-1",
			Text: "Original text content",
		}
		UpdateFTSIndex(tx1, []*Chunk{chunk1})
		tx1.Commit()

		// Update with new text
		tx2, _ := db.Begin()
		chunk2 := &Chunk{
			ID:   "chunk-1",
			Text: "Updated text content",
		}
		err := UpdateFTSIndex(tx2, []*Chunk{chunk2})
		require.NoError(t, err)
		tx2.Commit()

		// Count should still be 1 (upsert)
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks_fts").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Verify updated text is searchable
		rows, _ := db.Query("SELECT chunk_id FROM chunks_fts WHERE text MATCH 'Updated'")
		defer rows.Close()
		assert.True(t, rows.Next())
	})

	t.Run("handles empty chunk slice", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDB(t)
		defer db.Close()

		tx, _ := db.Begin()
		defer tx.Rollback()

		err := UpdateFTSIndex(tx, []*Chunk{})
		require.NoError(t, err)
	})
}

func TestDeleteFTSByFile(t *testing.T) {
	t.Parallel()

	t.Run("removes FTS entries for specified chunk IDs", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDB(t)
		defer db.Close()

		// Insert entries
		tx1, _ := db.Begin()
		chunks := []*Chunk{
			{ID: "chunk-1", Text: "Text one"},
			{ID: "chunk-2", Text: "Text two"},
			{ID: "chunk-3", Text: "Text three"},
		}
		UpdateFTSIndex(tx1, chunks)
		tx1.Commit()

		// Delete chunk-1 and chunk-2
		tx2, _ := db.Begin()
		err := DeleteFTSByFile(tx2, []string{"chunk-1", "chunk-2"})
		require.NoError(t, err)
		tx2.Commit()

		// Verify only chunk-3 remains
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks_fts").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		var remainingID string
		err = db.QueryRow("SELECT chunk_id FROM chunks_fts").Scan(&remainingID)
		require.NoError(t, err)
		assert.Equal(t, "chunk-3", remainingID)
	})

	t.Run("handles empty ID slice", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDB(t)
		defer db.Close()

		tx, _ := db.Begin()
		defer tx.Rollback()

		err := DeleteFTSByFile(tx, []string{})
		require.NoError(t, err)
	})
}

func TestQueryFTS(t *testing.T) {
	t.Parallel()

	t.Run("finds chunks by simple keyword", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		results, err := QueryFTS(db, "handler", nil, 10)
		require.NoError(t, err)
		assert.NotEmpty(t, results)

		// Verify result contains expected chunk
		found := false
		for _, r := range results {
			if r.Chunk.Title == "Handler" || r.Chunk.Title == "ErrorHandler" {
				found = true
				assert.True(t, strings.Contains(strings.ToLower(r.Snippet), "handler"))
			}
		}
		assert.True(t, found)
	})

	t.Run("supports phrase search", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		// Search for exact phrase "error handler"
		results, err := QueryFTS(db, `"error handler"`, nil, 10)
		require.NoError(t, err)

		// Should only match chunks with that exact phrase
		for _, r := range results {
			assert.Contains(t, r.Chunk.Text, "error handler")
		}
	})

	t.Run("supports boolean AND operator", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		// Search for chunks with both "error" AND "handler"
		results, err := QueryFTS(db, "error AND handler", nil, 10)
		require.NoError(t, err)

		// Verify all results contain both terms
		for _, r := range results {
			text := r.Chunk.Text
			assert.Contains(t, text, "error")
			assert.Contains(t, text, "handler")
		}
	})

	t.Run("supports boolean OR operator", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		// Search for chunks with either "struct" OR "interface"
		results, err := QueryFTS(db, "struct OR interface", nil, 10)
		require.NoError(t, err)
		assert.NotEmpty(t, results)

		// Verify results contain at least one term
		for _, r := range results {
			text := r.Chunk.Text
			hasStruct := strings.Contains(text, "struct")
			hasInterface := strings.Contains(text, "interface")
			assert.True(t, hasStruct || hasInterface)
		}
	})

	t.Run("supports boolean NOT operator", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		// Search for "handler" but NOT "error"
		results, err := QueryFTS(db, "handler NOT error", nil, 10)
		require.NoError(t, err)

		// Verify results contain "handler" but not "error"
		for _, r := range results {
			text := r.Chunk.Text
			assert.Contains(t, text, "handler")
			assert.NotContains(t, text, "error")
		}
	})

	t.Run("supports prefix matching", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		// Search with prefix wildcard
		results, err := QueryFTS(db, "hand*", nil, 10)
		require.NoError(t, err)
		assert.NotEmpty(t, results)

		// Should match "handler", "handle", etc.
		found := false
		for _, r := range results {
			if strings.Contains(strings.ToLower(r.Chunk.Text), "hand") {
				found = true
			}
		}
		assert.True(t, found)
	})

	t.Run("returns results ordered by BM25 rank", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		results, err := QueryFTS(db, "handler", nil, 10)
		require.NoError(t, err)
		require.NotEmpty(t, results)

		// Verify ranks are in descending order (higher rank = more relevant)
		for i := 0; i < len(results)-1; i++ {
			// BM25 ranks are negative (lower is better in SQLite FTS5)
			assert.GreaterOrEqual(t, results[i].Rank, results[i+1].Rank)
		}
	})

	t.Run("returns snippets with highlighting", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		results, err := QueryFTS(db, "handler", nil, 10)
		require.NoError(t, err)
		require.NotEmpty(t, results)

		// Verify snippet contains <mark> tags
		found := false
		for _, r := range results {
			if strings.Contains(r.Snippet, "<mark>") {
				found = true
				assert.Contains(t, r.Snippet, "</mark>")
			}
		}
		assert.True(t, found)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		results, err := QueryFTS(db, "func", nil, 2)
		require.NoError(t, err)
		assert.LessOrEqual(t, len(results), 2)
	})

	t.Run("filters by chunk_type", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		filters := map[string]interface{}{
			"chunk_type": "symbols",
		}

		results, err := QueryFTS(db, "handler", filters, 10)
		require.NoError(t, err)

		// Verify all results are symbols type
		for _, r := range results {
			assert.Equal(t, "symbols", r.Chunk.ChunkType)
		}
	})

	t.Run("filters by file_path", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		filters := map[string]interface{}{
			"file_path": "handler.go",
		}

		results, err := QueryFTS(db, "handler", filters, 10)
		require.NoError(t, err)

		// Verify all results are from specified file
		for _, r := range results {
			assert.Equal(t, "handler.go", r.Chunk.FilePath)
		}
	})
}

func TestSearchText(t *testing.T) {
	t.Parallel()

	t.Run("returns chunks without snippet metadata", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDBWithChunks(t)
		defer db.Close()

		chunks, err := SearchText(db, "handler", nil, 10)
		require.NoError(t, err)
		assert.NotEmpty(t, chunks)

		// Verify chunks have data
		for _, chunk := range chunks {
			assert.NotEmpty(t, chunk.ID)
			assert.NotEmpty(t, chunk.Text)
		}
	})
}

func TestBuildFTSQuery(t *testing.T) {
	t.Parallel()

	t.Run("handles phrase queries", func(t *testing.T) {
		t.Parallel()

		query := BuildFTSQuery("error handler", true)
		assert.Equal(t, `"error handler"`, query)
	})

	t.Run("escapes special characters", func(t *testing.T) {
		t.Parallel()

		query := BuildFTSQuery(`test "quoted" text`, false)
		assert.Contains(t, query, `""`) // Double quotes escaped
	})
}

func TestGetFTSStats(t *testing.T) {
	t.Parallel()

	t.Run("returns correct entry count", func(t *testing.T) {
		t.Parallel()

		db := setupFTSDB(t)
		defer db.Close()

		// Insert entries
		tx, _ := db.Begin()
		chunks := []*Chunk{
			{ID: "chunk-1", Text: "Text one"},
			{ID: "chunk-2", Text: "Text two"},
			{ID: "chunk-3", Text: "Text three"},
		}
		UpdateFTSIndex(tx, chunks)
		tx.Commit()

		stats, err := GetFTSStats(db)
		require.NoError(t, err)
		assert.Equal(t, 3, stats.TotalEntries)
		assert.Greater(t, stats.IndexSize, int64(0))
	})
}

func BenchmarkQueryFTS(b *testing.B) {
	db := openFTSTestDB(b)
	defer db.Close()

	CreateFTSIndex(db)
	setupChunksTable(db)

	// Insert 10K FTS entries
	tx, _ := db.Begin()
	chunks := make([]*Chunk, 10000)
	for i := 0; i < 10000; i++ {
		chunks[i] = &Chunk{
			ID:        fmt.Sprintf("chunk-%d", i),
			FilePath:  "file.go",
			ChunkType: "symbols",
			Title:     "Test Chunk",
			Text:      fmt.Sprintf("This is test content for chunk %d with handler and error keywords", i),
			Embedding: makeTestEmbedding(384),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
	}

	// Insert into both chunks and FTS
	for _, chunk := range chunks {
		writeChunkToTable(db, chunk)
	}
	UpdateFTSIndex(tx, chunks)
	tx.Commit()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = QueryFTS(db, "handler AND error", nil, 10)
	}
}

// Test helpers

func setupFTSDB(t *testing.T) *sql.DB {
	t.Helper()
	db := openFTSTestDB(t)

	err := CreateFTSIndex(db)
	require.NoError(t, err)

	return db
}

func setupFTSDBWithChunks(t *testing.T) *sql.DB {
	t.Helper()
	db := openFTSTestDB(t)

	// Create FTS index and chunks table
	err := CreateFTSIndex(db)
	require.NoError(t, err)

	setupChunksTable(db)

	// Insert test data
	tx, _ := db.Begin()

	testChunks := []*Chunk{
		{
			ID:        "chunk-1",
			FilePath:  "handler.go",
			ChunkType: "symbols",
			Title:     "Handler",
			Text:      "func handleRequest(w http.ResponseWriter, r *http.Request) { /* handle request */ }",
			Embedding: makeTestEmbedding(384),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "chunk-2",
			FilePath:  "user.go",
			ChunkType: "definitions",
			Title:     "User",
			Text:      "type User struct { ID int; Name string; Email string }",
			Embedding: makeTestEmbedding(384),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
		{
			ID:        "chunk-3",
			FilePath:  "handler.go",
			ChunkType: "symbols",
			Title:     "ErrorHandler",
			Text:      "func handleError(w http.ResponseWriter, err error) { /* error handler logic */ }",
			Embedding: makeTestEmbedding(384),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}

	// Insert into chunks table
	for _, chunk := range testChunks {
		writeChunkToTable(db, chunk)
	}

	// Insert into FTS index
	UpdateFTSIndex(tx, testChunks)
	tx.Commit()

	return db
}

func setupChunksTable(db *sql.DB) {
	// Create chunks table for JOIN tests
	db.Exec(`
		CREATE TABLE IF NOT EXISTS chunks (
			chunk_id TEXT PRIMARY KEY,
			file_path TEXT NOT NULL,
			chunk_type TEXT NOT NULL,
			title TEXT NOT NULL,
			text TEXT NOT NULL,
			embedding BLOB NOT NULL,
			start_line INTEGER,
			end_line INTEGER,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)
	`)
}

func writeChunkToTable(db *sql.DB, chunk *Chunk) {
	embBytes := SerializeEmbedding(chunk.Embedding)
	db.Exec(`
		INSERT INTO chunks (chunk_id, file_path, chunk_type, title, text, embedding, start_line, end_line, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, chunk.ID, chunk.FilePath, chunk.ChunkType, chunk.Title, chunk.Text, embBytes,
		nullableInt(chunk.StartLine), nullableInt(chunk.EndLine),
		chunk.CreatedAt.UTC().Format(time.RFC3339), chunk.UpdatedAt.UTC().Format(time.RFC3339))
}

func openFTSTestDB(t testing.TB) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	return db
}
