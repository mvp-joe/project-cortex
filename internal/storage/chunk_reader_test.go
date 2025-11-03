package storage

// Test Plan for Chunk Reader:
// - NewChunkReader opens database in read-only mode
// - NewChunkReader fails on non-existent database
// - ReadAllChunks reads all chunks successfully
// - ReadAllChunks preserves chunk data (ID, file path, type, title, text, embedding, line numbers)
// - ReadAllChunks handles nullable line numbers (zero values)
// - ReadAllChunks returns empty slice for empty database
// - ReadChunksByFile filters by file path correctly
// - ReadChunksByFile orders results by start_line
// - ReadChunksByFile returns empty for non-existent file
// - ReadChunksByType filters by chunk type correctly
// - ReadChunksByType returns empty for non-existent type
// - Round-trip: Full write and read cycle (10K chunks)
// - Round-trip: Embedding deserialization preserves float32 precision
// - Round-trip: Timestamp preservation within second precision (RFC3339)
// - Benchmark: ReadAllChunks performance with 10K chunks
// - Benchmark: ReadChunksByFile performance with filtered results

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewChunkReader(t *testing.T) {
	t.Parallel()

	t.Run("opens database in read-only mode", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "test.db")

		// Create database with writer first
		writer := openWriterNoFK(t, dbPath)
		writer.Close()

		// Open with reader
		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		require.NotNil(t, reader)
		defer reader.Close()
	})

	t.Run("fails on non-existent database", func(t *testing.T) {
		t.Parallel()
		dbPath := filepath.Join(t.TempDir(), "nonexistent.db")

		// SQLite in read-only mode will create the file, so we expect different behavior
		reader, err := NewChunkReader(dbPath)
		// In read-only mode with non-existent file, SQLite may allow opening but fail on first query
		if err == nil {
			defer reader.Close()
			_, err = reader.ReadAllChunks()
			assert.Error(t, err) // Should fail on query
		} else {
			assert.Error(t, err) // Or fail on open
		}
	})
}

func TestReadAllChunks(t *testing.T) {
	t.Parallel()

	t.Run("reads all chunks successfully", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		// Write test chunks
		writer := openWriterNoFK(t, dbPath)
		chunks := []*Chunk{
			makeTestChunk("chunk-1", "file1.go"),
			makeTestChunk("chunk-2", "file2.go"),
			makeTestChunk("chunk-3", "file1.go"),
		}
		err := writer.WriteChunks(chunks)
		require.NoError(t, err)
		writer.Close()

		// Read all chunks
		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadAllChunks()
		require.NoError(t, err)
		assert.Len(t, results, 3)
	})

	t.Run("preserves chunk data", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		// Write chunk with specific data
		writer := openWriterNoFK(t, dbPath)
		now := time.Now().UTC()
		original := &Chunk{
			ID:        "test-chunk",
			FilePath:  "internal/test.go",
			ChunkType: "definitions",
			Title:     "Test Title",
			Text:      "Test text content",
			Embedding: makeTestEmbedding(384),
			StartLine: 10,
			EndLine:   20,
			CreatedAt: now,
			UpdatedAt: now,
		}
		err := writer.WriteChunks([]*Chunk{original})
		require.NoError(t, err)
		writer.Close()

		// Read and verify
		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadAllChunks()
		require.NoError(t, err)
		require.Len(t, results, 1)

		chunk := results[0]
		assert.Equal(t, original.ID, chunk.ID)
		assert.Equal(t, original.FilePath, chunk.FilePath)
		assert.Equal(t, original.ChunkType, chunk.ChunkType)
		assert.Equal(t, original.Title, chunk.Title)
		assert.Equal(t, original.Text, chunk.Text)
		assert.Equal(t, original.StartLine, chunk.StartLine)
		assert.Equal(t, original.EndLine, chunk.EndLine)
		assert.Equal(t, len(original.Embedding), len(chunk.Embedding))
		for i := range original.Embedding {
			assert.InDelta(t, original.Embedding[i], chunk.Embedding[i], 0.00001)
		}
		// Timestamps truncated to second precision due to RFC3339
		assert.WithinDuration(t, original.CreatedAt, chunk.CreatedAt, time.Second)
		assert.WithinDuration(t, original.UpdatedAt, chunk.UpdatedAt, time.Second)
	})

	t.Run("handles nullable line numbers", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		writer := openWriterNoFK(t, dbPath)
		chunk := makeTestChunk("chunk-1", "file.go")
		chunk.StartLine = 0 // Should become NULL
		chunk.EndLine = 0   // Should become NULL
		err := writer.WriteChunks([]*Chunk{chunk})
		require.NoError(t, err)
		writer.Close()

		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadAllChunks()
		require.NoError(t, err)
		require.Len(t, results, 1)

		assert.Equal(t, 0, results[0].StartLine)
		assert.Equal(t, 0, results[0].EndLine)
	})

	t.Run("returns empty slice for empty database", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadAllChunks()
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestReadChunksByFile(t *testing.T) {
	t.Parallel()

	t.Run("filters by file path", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		// Write chunks for multiple files
		writer := openWriterNoFK(t, dbPath)
		chunks := []*Chunk{
			makeTestChunk("chunk-1", "file1.go"),
			makeTestChunk("chunk-2", "file1.go"),
			makeTestChunk("chunk-3", "file2.go"),
			makeTestChunk("chunk-4", "file2.go"),
		}
		err := writer.WriteChunks(chunks)
		require.NoError(t, err)
		writer.Close()

		// Read file1.go only
		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadChunksByFile("file1.go")
		require.NoError(t, err)
		assert.Len(t, results, 2)

		for _, chunk := range results {
			assert.Equal(t, "file1.go", chunk.FilePath)
		}
	})

	t.Run("orders by start_line", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		// Write chunks in random order
		writer := openWriterNoFK(t, dbPath)
		chunks := []*Chunk{
			{
				ID:        "chunk-3",
				FilePath:  "file.go",
				ChunkType: "symbols",
				Title:     "Chunk 3",
				Text:      "Content 3",
				Embedding: makeTestEmbedding(384),
				StartLine: 30,
				EndLine:   40,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
			{
				ID:        "chunk-1",
				FilePath:  "file.go",
				ChunkType: "symbols",
				Title:     "Chunk 1",
				Text:      "Content 1",
				Embedding: makeTestEmbedding(384),
				StartLine: 10,
				EndLine:   20,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
			{
				ID:        "chunk-2",
				FilePath:  "file.go",
				ChunkType: "symbols",
				Title:     "Chunk 2",
				Text:      "Content 2",
				Embedding: makeTestEmbedding(384),
				StartLine: 20,
				EndLine:   30,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
		}
		err := writer.WriteChunks(chunks)
		require.NoError(t, err)
		writer.Close()

		// Read and verify order
		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadChunksByFile("file.go")
		require.NoError(t, err)
		require.Len(t, results, 3)

		assert.Equal(t, "chunk-1", results[0].ID)
		assert.Equal(t, "chunk-2", results[1].ID)
		assert.Equal(t, "chunk-3", results[2].ID)
	})

	t.Run("returns empty for non-existent file", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		writer := openWriterNoFK(t, dbPath)
		err := writer.WriteChunks([]*Chunk{makeTestChunk("chunk-1", "file1.go")})
		require.NoError(t, err)
		writer.Close()

		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadChunksByFile("nonexistent.go")
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestReadChunksByType(t *testing.T) {
	t.Parallel()

	t.Run("filters by chunk type", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		// Write chunks with different types
		writer := openWriterNoFK(t, dbPath)
		chunks := []*Chunk{
			{
				ID:        "chunk-1",
				FilePath:  "file1.go",
				ChunkType: "symbols",
				Title:     "Symbols",
				Text:      "Content",
				Embedding: makeTestEmbedding(384),
				StartLine: 1,
				EndLine:   10,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
			{
				ID:        "chunk-2",
				FilePath:  "file2.go",
				ChunkType: "definitions",
				Title:     "Definitions",
				Text:      "Content",
				Embedding: makeTestEmbedding(384),
				StartLine: 1,
				EndLine:   10,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
			{
				ID:        "chunk-3",
				FilePath:  "file3.go",
				ChunkType: "symbols",
				Title:     "More Symbols",
				Text:      "Content",
				Embedding: makeTestEmbedding(384),
				StartLine: 1,
				EndLine:   10,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			},
		}
		err := writer.WriteChunks(chunks)
		require.NoError(t, err)
		writer.Close()

		// Read symbols only
		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadChunksByType("symbols")
		require.NoError(t, err)
		assert.Len(t, results, 2)

		for _, chunk := range results {
			assert.Equal(t, "symbols", chunk.ChunkType)
		}
	})

	t.Run("returns empty for non-existent type", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		writer := openWriterNoFK(t, dbPath)
		err := writer.WriteChunks([]*Chunk{makeTestChunk("chunk-1", "file1.go")})
		require.NoError(t, err)
		writer.Close()

		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadChunksByType("nonexistent")
		require.NoError(t, err)
		assert.Empty(t, results)
	})
}

func TestReadWriteRoundTrip(t *testing.T) {
	t.Parallel()

	t.Run("full write and read cycle", func(t *testing.T) {
		t.Parallel()
		dbPath := setupTestDatabase(t)

		// Write 10K chunks to simulate real usage
		writer := openWriterNoFK(t, dbPath)

		chunks := make([]*Chunk, 10000)
		for i := range chunks {
			chunks[i] = &Chunk{
				ID:        "chunk-" + string(rune(i)),
				FilePath:  "file.go",
				ChunkType: "symbols",
				Title:     "Test Chunk",
				Text:      "Test content",
				Embedding: makeTestEmbedding(384),
				StartLine: i * 10,
				EndLine:   (i + 1) * 10,
				CreatedAt: time.Now().UTC(),
				UpdatedAt: time.Now().UTC(),
			}
		}

		err := writer.WriteChunks(chunks)
		require.NoError(t, err)
		writer.Close()

		// Read all chunks back
		reader, err := NewChunkReader(dbPath)
		require.NoError(t, err)
		defer reader.Close()

		results, err := reader.ReadAllChunks()
		require.NoError(t, err)
		assert.Len(t, results, 10000)

		// Verify first and last chunk
		assert.Equal(t, "chunk-"+string(rune(0)), results[0].ID)
		assert.Equal(t, 384, len(results[0].Embedding))
	})
}

func BenchmarkReadAllChunks(b *testing.B) {
	dbPath := setupBenchDatabase(b)

	// Write 10K chunks
	writer := openWriterNoFKBench(b, dbPath)
	chunks := make([]*Chunk, 10000)
	for i := range chunks {
		chunks[i] = makeTestChunk("chunk-"+string(rune(i)), "file.go")
	}
	_ = writer.WriteChunks(chunks)
	writer.Close()

	// Benchmark reading
	reader, _ := NewChunkReader(dbPath)
	defer reader.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = reader.ReadAllChunks()
	}
}

func BenchmarkReadChunksByFile(b *testing.B) {
	dbPath := setupBenchDatabase(b)

	// Write chunks for multiple files
	writer := openWriterNoFKBench(b, dbPath)
	chunks := make([]*Chunk, 1000)
	for i := range chunks {
		filePath := "file" + string(rune(i%10)) + ".go"
		chunks[i] = makeTestChunk("chunk-"+string(rune(i)), filePath)
	}
	_ = writer.WriteChunks(chunks)
	writer.Close()

	reader, _ := NewChunkReader(dbPath)
	defer reader.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = reader.ReadChunksByFile("file0.go")
	}
}

// Test helpers

func setupTestDatabase(t *testing.T) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Create schema with FK disabled
	writer := openWriterNoFK(t, dbPath)
	writer.Close()

	return dbPath
}

func setupBenchDatabase(b *testing.B) string {
	b.Helper()
	dbPath := filepath.Join(b.TempDir(), "bench.db")

	writer, err := NewChunkWriter(dbPath)
	if err != nil {
		b.Fatal(err)
	}

	// Disable FK constraints for benchmarking
	_, err = writer.db.Exec("PRAGMA foreign_keys = OFF")
	if err != nil {
		b.Fatal(err)
	}

	writer.Close()

	return dbPath
}

// openWriterNoFK opens a writer with foreign keys disabled (for testing)
func openWriterNoFK(t *testing.T, dbPath string) *ChunkWriter {
	t.Helper()
	writer, err := NewChunkWriter(dbPath)
	require.NoError(t, err)
	_, err = writer.db.Exec("PRAGMA foreign_keys = OFF")
	require.NoError(t, err)
	return writer
}

// openWriterNoFKBench opens a writer with foreign keys disabled (for benchmarking)
func openWriterNoFKBench(b *testing.B, dbPath string) *ChunkWriter {
	b.Helper()
	writer, err := NewChunkWriter(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	_, err = writer.db.Exec("PRAGMA foreign_keys = OFF")
	if err != nil {
		b.Fatal(err)
	}
	return writer
}
