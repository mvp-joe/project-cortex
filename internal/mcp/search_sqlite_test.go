package mcp

// Test Plan for SQLite Searcher:
// - NewSQLiteSearcher requires database connection
// - NewSQLiteSearcher requires embedding provider
// - Query generates embeddings using provider
// - Query executes vector similarity search with sqlite-vec
// - Query applies chunk_type filters (native SQL)
// - Query applies tag filters (derived from language and chunk_type)
// - Query applies min_score threshold (post-filter)
// - Query returns results ordered by similarity
// - Query converts distance to similarity score
// - Query builds tags from language and chunk_type
// - Reload is no-op (always returns nil)
// - GetMetrics returns metrics snapshot
// - Close is no-op (database externally managed)
// - Helper: isLanguageTag recognizes common languages
// - Helper: isContentTag recognizes code/documentation
// - Helper: buildTags constructs tag arrays correctly

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sqliteMockEmbeddingProvider implements embed.Provider for testing
type sqliteMockEmbeddingProvider struct {
	dimensions int
	embeddings [][]float32
}

func newSQLiteMockProvider(dims int) *sqliteMockEmbeddingProvider {
	return &sqliteMockEmbeddingProvider{
		dimensions: dims,
		embeddings: make([][]float32, 0),
	}
}

func (m *sqliteMockEmbeddingProvider) Initialize(ctx context.Context) error {
	return nil
}

func (m *sqliteMockEmbeddingProvider) Embed(ctx context.Context, texts []string, mode embed.EmbedMode) ([][]float32, error) {
	// Return consistent embeddings for testing
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = makeTestEmbedding(m.dimensions)
	}
	m.embeddings = append(m.embeddings, results...)
	return results, nil
}

func (m *sqliteMockEmbeddingProvider) Dimensions() int {
	return m.dimensions
}

func (m *sqliteMockEmbeddingProvider) Close() error {
	return nil
}

// Test helpers

func setupSQLiteSearcherTest(t *testing.T) (*sql.DB, *sqliteMockEmbeddingProvider) {
	t.Helper()

	// Initialize sqlite-vec extension
	storage.InitVectorExtension()

	// Create temporary database
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)

	// Enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema
	createSchema(t, db)

	// Create mock provider
	provider := newSQLiteMockProvider(384)

	return db, provider
}

func createSchema(t *testing.T, db *sql.DB) {
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

	// Create vector index
	err = storage.CreateVectorIndex(db, 384)
	require.NoError(t, err)
}

func insertTestFile(t *testing.T, db *sql.DB, filePath, language string) {
	t.Helper()

	_, err := db.Exec(
		"INSERT INTO files (file_path, language, module_path, line_count_total) VALUES (?, ?, ?, ?)",
		filePath, language, "internal/test", 100,
	)
	require.NoError(t, err)
}

func insertTestChunk(t *testing.T, db *sql.DB, chunk *storage.Chunk) {
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

	// Insert into vector index (within transaction for consistency)
	tx, err := db.Begin()
	require.NoError(t, err)
	defer tx.Rollback()

	err = storage.UpdateVectorIndex(tx, []*storage.Chunk{chunk})
	require.NoError(t, err)

	err = tx.Commit()
	require.NoError(t, err)
}

func nullableInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}

func makeTestEmbedding(dims int) []float32 {
	emb := make([]float32, dims)
	for i := range emb {
		emb[i] = float32(i) / float32(dims)
	}
	return emb
}

// Constructor Tests

func TestNewSQLiteSearcher(t *testing.T) {
	t.Parallel()

	t.Run("requires database connection", func(t *testing.T) {
		t.Parallel()
		provider := newSQLiteMockProvider(384)

		searcher, err := NewSQLiteSearcher(nil, provider)
		assert.Error(t, err)
		assert.Nil(t, searcher)
		assert.Contains(t, err.Error(), "database connection is required")
	})

	t.Run("requires embedding provider", func(t *testing.T) {
		t.Parallel()
		db, _ := setupSQLiteSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteSearcher(db, nil)
		assert.Error(t, err)
		assert.Nil(t, searcher)
		assert.Contains(t, err.Error(), "embedding provider is required")
	})

	t.Run("creates searcher successfully", func(t *testing.T) {
		t.Parallel()
		db, provider := setupSQLiteSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteSearcher(db, provider)
		require.NoError(t, err)
		require.NotNil(t, searcher)
		defer searcher.Close()
	})
}

// Query Tests

func TestSQLiteSearcherQuery(t *testing.T) {
	t.Parallel()

	t.Run("executes vector similarity search", func(t *testing.T) {
		t.Parallel()
		db, provider := setupSQLiteSearcherTest(t)
		defer db.Close()

		// Insert test data
		insertTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()
		chunk := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Test Chunk",
			Text:      "func TestFunction() {}",
			Embedding: makeTestEmbedding(384),
			StartLine: 10,
			EndLine:   20,
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertTestChunk(t, db, chunk)

		// Create searcher and query
		searcher, err := NewSQLiteSearcher(db, provider)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Query(ctx, "test query", &SearchOptions{Limit: 10})
		require.NoError(t, err)
		assert.NotEmpty(t, results)

		// Verify provider was called
		assert.NotEmpty(t, provider.embeddings)
	})

	t.Run("applies chunk_type filters", func(t *testing.T) {
		t.Parallel()
		db, provider := setupSQLiteSearcherTest(t)
		defer db.Close()

		// Insert test data with different chunk types
		insertTestFile(t, db, "file1.go", "go")
		now := time.Now().UTC()

		chunk1 := &storage.Chunk{
			ID:        "chunk-1",
			FilePath:  "file1.go",
			ChunkType: "definitions",
			Title:     "Definitions",
			Text:      "type Handler struct {}",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		chunk2 := &storage.Chunk{
			ID:        "chunk-2",
			FilePath:  "file1.go",
			ChunkType: "symbols",
			Title:     "Symbols",
			Text:      "Handler (struct)",
			Embedding: makeTestEmbedding(384),
			CreatedAt: now,
			UpdatedAt: now,
		}
		insertTestChunk(t, db, chunk1)
		insertTestChunk(t, db, chunk2)

		// Query with chunk type filter
		searcher, err := NewSQLiteSearcher(db, provider)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Query(ctx, "test", &SearchOptions{
			Limit:      10,
			ChunkTypes: []string{"definitions"},
		})
		require.NoError(t, err)
		assert.Len(t, results, 1)
		assert.Equal(t, "definitions", results[0].Chunk.ChunkType)
	})

	t.Run("applies min_score threshold", func(t *testing.T) {
		t.Parallel()
		db, provider := setupSQLiteSearcherTest(t)
		defer db.Close()

		// Insert test data
		insertTestFile(t, db, "file1.go", "go")
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
		insertTestChunk(t, db, chunk)

		// Query with high min_score (should filter out low-similarity results)
		searcher, err := NewSQLiteSearcher(db, provider)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		results, err := searcher.Query(ctx, "test", &SearchOptions{
			Limit:    10,
			MinScore: 0.99, // Very high threshold
		})
		require.NoError(t, err)
		// Results may be empty if similarity is below threshold
		for _, r := range results {
			assert.GreaterOrEqual(t, r.CombinedScore, 0.99)
		}
	})
}

// Lifecycle Tests

func TestSQLiteSearcherReload(t *testing.T) {
	t.Parallel()

	t.Run("reload is no-op", func(t *testing.T) {
		t.Parallel()
		db, provider := setupSQLiteSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteSearcher(db, provider)
		require.NoError(t, err)
		defer searcher.Close()

		ctx := context.Background()
		err = searcher.Reload(ctx)
		assert.NoError(t, err)
	})
}

func TestSQLiteSearcherGetMetrics(t *testing.T) {
	t.Parallel()

	t.Run("returns metrics snapshot", func(t *testing.T) {
		t.Parallel()
		db, provider := setupSQLiteSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteSearcher(db, provider)
		require.NoError(t, err)
		defer searcher.Close()

		metrics := searcher.GetMetrics()
		assert.NotNil(t, metrics)
	})
}

func TestSQLiteSearcherClose(t *testing.T) {
	t.Parallel()

	t.Run("close is no-op", func(t *testing.T) {
		t.Parallel()
		db, provider := setupSQLiteSearcherTest(t)
		defer db.Close()

		searcher, err := NewSQLiteSearcher(db, provider)
		require.NoError(t, err)

		err = searcher.Close()
		assert.NoError(t, err)

		// Database should still be usable (not closed)
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&count)
		assert.NoError(t, err)
	})
}

// Helper Function Tests

func TestIsLanguageTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag      string
		expected bool
	}{
		{"go", true},
		{"typescript", true},
		{"javascript", true},
		{"python", true},
		{"rust", true},
		{"unknown", false},
		{"code", false},
		{"documentation", false},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			result := isLanguageTag(tt.tag)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsContentTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		tag      string
		expected bool
	}{
		{"code", true},
		{"documentation", true},
		{"go", false},
		{"typescript", false},
		{"unknown", false},
	}

	for _, tt := range tests {
		t.Run(tt.tag, func(t *testing.T) {
			result := isContentTag(tt.tag)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildTags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		language  string
		chunkType string
		expected  []string
	}{
		{
			name:      "go code with definitions",
			language:  "go",
			chunkType: "definitions",
			expected:  []string{"go", "code", "definitions"},
		},
		{
			name:      "documentation chunk",
			language:  "",
			chunkType: "documentation",
			expected:  []string{"documentation", "documentation"},
		},
		{
			name:      "typescript symbols",
			language:  "typescript",
			chunkType: "symbols",
			expected:  []string{"typescript", "code", "symbols"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildTags(tt.language, tt.chunkType)
			assert.Equal(t, tt.expected, result)
		})
	}
}
