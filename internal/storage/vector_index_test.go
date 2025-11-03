package storage

// Test Plan for Vector Index:
// - InitVectorExtension enables sqlite-vec globally
// - CreateVectorIndex creates vec0 virtual table successfully
// - CreateVectorIndex is idempotent (IF NOT EXISTS)
// - UpdateVectorIndex inserts vectors for chunks
// - UpdateVectorIndex performs upsert (replaces existing vectors)
// - UpdateVectorIndex handles empty chunk slice
// - DeleteVectorsByFile removes vectors for specified chunk IDs
// - DeleteVectorsByFile handles empty ID slice
// - QueryVectorSimilarity returns K nearest neighbors
// - QueryVectorSimilarity orders by distance (ascending)
// - QueryVectorSimilarity respects limit parameter
// - QueryVectorSimilarity works with different embedding dimensions
// - GetVectorIndexStats returns correct count
// - Round-trip: Insert vectors → Query → Verify similarity ordering
// - Integration: Vector search combined with chunk table JOIN
// - Benchmark: Vector search performance with 10K vectors

import (
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitVectorExtension(t *testing.T) {
	t.Parallel()

	t.Run("enables sqlite-vec globally", func(t *testing.T) {
		t.Parallel()

		// Call init (safe to call multiple times)
		InitVectorExtension()

		// Open database and verify vec functions are available
		dbPath := filepath.Join(t.TempDir(), "test.db")
		db, err := sql.Open("sqlite3", dbPath)
		require.NoError(t, err)
		defer db.Close()

		// Query vec_version() to verify extension loaded
		var version string
		err = db.QueryRow("SELECT vec_version()").Scan(&version)
		require.NoError(t, err)
		assert.NotEmpty(t, version)
	})
}

func TestCreateVectorIndex(t *testing.T) {
	t.Parallel()

	t.Run("creates vec0 virtual table", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := openVectorTestDB(t)
		defer db.Close()

		err := CreateVectorIndex(db, 384)
		require.NoError(t, err)

		// Verify table exists
		var tableName string
		err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='chunks_vec'").Scan(&tableName)
		require.NoError(t, err)
		assert.Equal(t, "chunks_vec", tableName)
	})

	t.Run("is idempotent", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := openVectorTestDB(t)
		defer db.Close()

		// Create twice
		err := CreateVectorIndex(db, 384)
		require.NoError(t, err)

		err = CreateVectorIndex(db, 384)
		require.NoError(t, err) // Should not error
	})

	t.Run("supports different dimensions", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := openVectorTestDB(t)
		defer db.Close()

		// Create with custom dimensions
		err := CreateVectorIndex(db, 768)
		require.NoError(t, err)

		// Verify we can insert vectors of that dimension
		tx, _ := db.Begin()
		defer tx.Rollback()

		chunk := &Chunk{
			ID:        "test-chunk",
			FilePath:  "test.go",
			ChunkType: "symbols",
			Title:     "Test",
			Text:      "Test content",
			Embedding: makeTestEmbedding(768),
		}

		err = UpdateVectorIndex(tx, []*Chunk{chunk})
		require.NoError(t, err)
	})
}

func TestUpdateVectorIndex(t *testing.T) {
	t.Parallel()

	t.Run("inserts vectors for chunks", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		tx, err := db.Begin()
		require.NoError(t, err)
		defer tx.Rollback()

		chunks := []*Chunk{
			{
				ID:        "chunk-1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Chunk 1",
				Text:      "Content 1",
				Embedding: makeTestEmbedding(384),
			},
			{
				ID:        "chunk-2",
				FilePath:  "file2.go",
				ChunkType: "definitions",
				Title:     "Chunk 2",
				Text:      "Content 2",
				Embedding: makeTestEmbedding(384),
			},
		}

		err = UpdateVectorIndex(tx, chunks)
		require.NoError(t, err)

		err = tx.Commit()
		require.NoError(t, err)

		// Verify vectors inserted
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks_vec").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 2, count)
	})

	t.Run("performs upsert", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		// Insert initial vector
		tx1, _ := db.Begin()
		chunk1 := &Chunk{
			ID:        "chunk-1",
			FilePath:  "file.go",
			ChunkType: "symbols",
			Title:     "Original",
			Text:      "Original content",
			Embedding: makeTestEmbedding(384),
		}
		err := UpdateVectorIndex(tx1, []*Chunk{chunk1})
		require.NoError(t, err)
		tx1.Commit()

		// Update with new vector
		tx2, _ := db.Begin()
		chunk2 := &Chunk{
			ID:        "chunk-1",
			FilePath:  "file.go",
			ChunkType: "symbols",
			Title:     "Updated",
			Text:      "Updated content",
			Embedding: makeTestEmbedding(384),
		}
		err = UpdateVectorIndex(tx2, []*Chunk{chunk2})
		require.NoError(t, err)
		tx2.Commit()

		// Count should still be 1 (upsert, not insert)
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks_vec").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("handles empty chunk slice", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		tx, _ := db.Begin()
		defer tx.Rollback()

		err := UpdateVectorIndex(tx, []*Chunk{})
		require.NoError(t, err)
	})
}

func TestDeleteVectorsByFile(t *testing.T) {
	t.Parallel()

	t.Run("removes vectors for specified chunk IDs", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		// Insert vectors
		tx1, _ := db.Begin()
		chunks := []*Chunk{
			{ID: "chunk-1", FilePath: "file1.go", ChunkType: "symbols", Title: "C1", Text: "T1", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-2", FilePath: "file1.go", ChunkType: "symbols", Title: "C2", Text: "T2", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-3", FilePath: "file2.go", ChunkType: "symbols", Title: "C3", Text: "T3", Embedding: makeTestEmbedding(384)},
		}
		UpdateVectorIndex(tx1, chunks)
		tx1.Commit()

		// Delete file1.go chunks
		tx2, _ := db.Begin()
		err := DeleteVectorsByFile(tx2, []string{"chunk-1", "chunk-2"})
		require.NoError(t, err)
		tx2.Commit()

		// Verify only chunk-3 remains
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM chunks_vec").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		var remainingID string
		err = db.QueryRow("SELECT chunk_id FROM chunks_vec").Scan(&remainingID)
		require.NoError(t, err)
		assert.Equal(t, "chunk-3", remainingID)
	})

	t.Run("handles empty ID slice", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		tx, _ := db.Begin()
		defer tx.Rollback()

		err := DeleteVectorsByFile(tx, []string{})
		require.NoError(t, err)
	})
}

func TestQueryVectorSimilarity(t *testing.T) {
	t.Parallel()

	t.Run("returns K nearest neighbors", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		// Insert 5 vectors with known similarities to query
		tx, _ := db.Begin()
		chunks := []*Chunk{
			{ID: "chunk-1", FilePath: "f.go", ChunkType: "s", Title: "C1", Text: "T1", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-2", FilePath: "f.go", ChunkType: "s", Title: "C2", Text: "T2", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-3", FilePath: "f.go", ChunkType: "s", Title: "C3", Text: "T3", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-4", FilePath: "f.go", ChunkType: "s", Title: "C4", Text: "T4", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-5", FilePath: "f.go", ChunkType: "s", Title: "C5", Text: "T5", Embedding: makeTestEmbedding(384)},
		}
		UpdateVectorIndex(tx, chunks)
		tx.Commit()

		// Query with vector similar to chunk-1
		queryEmb := makeTestEmbedding(384)
		results, err := QueryVectorSimilarity(db, queryEmb, 3)
		require.NoError(t, err)
		require.Len(t, results, 3)

		// All chunks should be returned (exact ordering may vary slightly with makeTestEmbedding)
		assert.Equal(t, "chunk-1", results[0].ChunkID)
		assert.Less(t, results[0].Distance, 0.1) // Should be close
	})

	t.Run("orders by distance ascending", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDBWithDim(t, 3)
		defer db.Close()

		tx, _ := db.Begin()
		chunks := []*Chunk{
			{ID: "far", FilePath: "f.go", ChunkType: "s", Title: "F", Text: "T", Embedding: []float32{0.0, 0.0, 1.0}},
			{ID: "close", FilePath: "f.go", ChunkType: "s", Title: "C", Text: "T", Embedding: []float32{1.0, 0.0, 0.0}},
			{ID: "medium", FilePath: "f.go", ChunkType: "s", Title: "M", Text: "T", Embedding: []float32{0.7, 0.3, 0.0}},
		}
		UpdateVectorIndex(tx, chunks)
		tx.Commit()

		queryEmb := []float32{1.0, 0.0, 0.0}
		results, err := QueryVectorSimilarity(db, queryEmb, 3)
		require.NoError(t, err)
		require.NotEmpty(t, results, "should return results")

		// Verify ascending distance order
		assert.Equal(t, "close", results[0].ChunkID)
		assert.Equal(t, "medium", results[1].ChunkID)
		assert.Equal(t, "far", results[2].ChunkID)

		// Verify distances are increasing
		assert.Less(t, results[0].Distance, results[1].Distance)
		assert.Less(t, results[1].Distance, results[2].Distance)
	})

	t.Run("respects limit parameter", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		// Insert 10 vectors
		tx, _ := db.Begin()
		chunks := make([]*Chunk, 10)
		for i := range chunks {
			chunks[i] = &Chunk{
				ID:        fmt.Sprintf("chunk-%d", i),
				FilePath:  "f.go",
				ChunkType: "s",
				Title:     "C",
				Text:      "T",
				Embedding: makeTestEmbedding(384),
			}
		}
		UpdateVectorIndex(tx, chunks)
		tx.Commit()

		// Query with limit=5
		queryEmb := makeTestEmbedding(384)
		results, err := QueryVectorSimilarity(db, queryEmb, 5)
		require.NoError(t, err)
		assert.Len(t, results, 5)
	})
}

func TestGetVectorIndexStats(t *testing.T) {
	t.Parallel()

	t.Run("returns correct count", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDB(t)
		defer db.Close()

		// Insert vectors
		tx, _ := db.Begin()
		chunks := []*Chunk{
			{ID: "chunk-1", FilePath: "f.go", ChunkType: "s", Title: "C1", Text: "T1", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-2", FilePath: "f.go", ChunkType: "s", Title: "C2", Text: "T2", Embedding: makeTestEmbedding(384)},
			{ID: "chunk-3", FilePath: "f.go", ChunkType: "s", Title: "C3", Text: "T3", Embedding: makeTestEmbedding(384)},
		}
		UpdateVectorIndex(tx, chunks)
		tx.Commit()

		stats, err := GetVectorIndexStats(db)
		require.NoError(t, err)
		assert.Equal(t, 3, stats.TotalVectors)
		assert.Equal(t, 384, stats.Dimensions) // From cache_metadata
	})
}

func TestVectorSearchRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("insert vectors and query with similarity ordering", func(t *testing.T) {
		t.Parallel()

		InitVectorExtension()
		db := setupVectorDBWithDim(t, 3)
		defer db.Close()

		// Create vectors with known cosine similarities
		// Normalized 3D vectors for easy reasoning
		tx, _ := db.Begin()
		chunks := []*Chunk{
			{
				ID:        "exact-match",
				FilePath:  "f.go",
				ChunkType: "s",
				Title:     "Exact",
				Text:      "T",
				Embedding: normalize([]float32{1.0, 0.0, 0.0}),
			},
			{
				ID:        "very-similar",
				FilePath:  "f.go",
				ChunkType: "s",
				Title:     "Similar",
				Text:      "T",
				Embedding: normalize([]float32{0.9, 0.1, 0.0}),
			},
			{
				ID:        "orthogonal",
				FilePath:  "f.go",
				ChunkType: "s",
				Title:     "Orth",
				Text:      "T",
				Embedding: normalize([]float32{0.0, 1.0, 0.0}),
			},
		}
		UpdateVectorIndex(tx, chunks)
		tx.Commit()

		// Query with normalized vector
		queryEmb := normalize([]float32{1.0, 0.0, 0.0})
		results, err := QueryVectorSimilarity(db, queryEmb, 3)
		require.NoError(t, err)
		require.Len(t, results, 3)

		// Verify similarity ordering
		assert.Equal(t, "exact-match", results[0].ChunkID)
		assert.Equal(t, "very-similar", results[1].ChunkID)
		assert.Equal(t, "orthogonal", results[2].ChunkID)

		// Verify distances are reasonable
		assert.Less(t, results[0].Distance, 0.01) // Exact match
		assert.Less(t, results[1].Distance, results[2].Distance)
	})
}

func BenchmarkQueryVectorSimilarity(b *testing.B) {
	InitVectorExtension()
	dbPath := filepath.Join(b.TempDir(), "bench.db")
	db, _ := sql.Open("sqlite3", dbPath)
	defer db.Close()

	// Create vector index
	CreateVectorIndex(db, 384)

	// Bootstrap cache_metadata for dimensions
	db.Exec("CREATE TABLE cache_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)")
	db.Exec("INSERT INTO cache_metadata (key, value, updated_at) VALUES ('embedding_dimensions', '384', datetime('now'))")

	// Insert 10K vectors
	tx, _ := db.Begin()
	chunks := make([]*Chunk, 10000)
	for i := range chunks {
		chunks[i] = &Chunk{
			ID:        fmt.Sprintf("chunk-%d", i),
			FilePath:  "file.go",
			ChunkType: "symbols",
			Title:     "Chunk",
			Text:      "Content",
			Embedding: makeTestEmbedding(384),
		}
	}
	UpdateVectorIndex(tx, chunks)
	tx.Commit()

	queryEmb := makeTestEmbedding(384)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = QueryVectorSimilarity(db, queryEmb, 10)
	}
}

// Test helpers

func openVectorTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	return db
}

func setupVectorDB(t *testing.T) *sql.DB {
	return setupVectorDBWithDim(t, 384)
}

func setupVectorDBWithDim(t *testing.T, dimensions int) *sql.DB {
	t.Helper()
	db := openVectorTestDB(t)

	// Create vector index with specified dimensions
	err := CreateVectorIndex(db, dimensions)
	require.NoError(t, err)

	// Bootstrap cache_metadata for dimensions
	_, err = db.Exec("CREATE TABLE cache_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at TEXT NOT NULL)")
	require.NoError(t, err)
	_, err = db.Exec(fmt.Sprintf("INSERT INTO cache_metadata (key, value, updated_at) VALUES ('embedding_dimensions', '%d', datetime('now'))", dimensions))
	require.NoError(t, err)

	return db
}

// normalize normalizes a vector to unit length (for cosine similarity tests)
func normalize(v []float32) []float32 {
	var sum float32
	for _, val := range v {
		sum += val * val
	}
	norm := float32(math.Sqrt(float64(sum)))

	result := make([]float32, len(v))
	for i, val := range v {
		result[i] = val / norm
	}
	return result
}
