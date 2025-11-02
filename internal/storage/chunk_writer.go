package storage

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
	"time"

	sq "github.com/Masterminds/squirrel"
	_ "github.com/mattn/go-sqlite3"
)

// ChunkWriter handles writing chunks to SQLite database.
// Uses transactions for atomic updates and prepared statements for bulk inserts.
type ChunkWriter struct {
	db *sql.DB
}

// Chunk represents a semantic search chunk stored in SQLite.
// Maps to the chunks table schema with embedding serialization.
type Chunk struct {
	ID        string
	FilePath  string
	ChunkType string
	Title     string
	Text      string
	Embedding []float32
	StartLine int
	EndLine   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewChunkWriter opens or creates a SQLite database for chunk storage.
// Enables foreign keys and creates schema if needed.
func NewChunkWriter(dbPath string) (*ChunkWriter, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys (required for FK constraints)
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create schema if not exists
	version, err := GetSchemaVersion(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to check schema version: %w", err)
	}

	if version == "0" {
		if err := CreateSchema(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}
	}

	return &ChunkWriter{db: db}, nil
}

// WriteChunks performs a full replace of all chunks in the database.
// Use for initial indexing or complete rebuilds.
// All operations are atomic - either all chunks are written or none.
func (w *ChunkWriter) WriteChunks(chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Clear all existing chunks
	if _, err := sq.Delete("chunks").RunWith(tx).Exec(); err != nil {
		return fmt.Errorf("failed to clear chunks: %w", err)
	}

	// Insert all chunks
	for _, chunk := range chunks {
		embBytes := serializeEmbedding(chunk.Embedding)

		_, err := sq.Insert("chunks").
			Columns("chunk_id", "file_path", "chunk_type", "title", "text", "embedding", "start_line", "end_line", "created_at", "updated_at").
			Values(
				chunk.ID,
				chunk.FilePath,
				chunk.ChunkType,
				chunk.Title,
				chunk.Text,
				embBytes,
				nullableInt(chunk.StartLine),
				nullableInt(chunk.EndLine),
				chunk.CreatedAt.UTC().Format(time.RFC3339),
				chunk.UpdatedAt.UTC().Format(time.RFC3339),
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to insert chunk %s: %w", chunk.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// WriteChunksIncremental updates chunks for specific files only.
// Deletes existing chunks for the modified files, then inserts new chunks.
// Use for hot reload and incremental indexing updates.
func (w *ChunkWriter) WriteChunksIncremental(chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Collect unique file paths
	filePathsMap := make(map[string]bool)
	for _, chunk := range chunks {
		filePathsMap[chunk.FilePath] = true
	}

	// Delete existing chunks for these files
	for filePath := range filePathsMap {
		_, err := sq.Delete("chunks").
			Where(sq.Eq{"file_path": filePath}).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to delete chunks for file %s: %w", filePath, err)
		}
	}

	// Insert new chunks
	for _, chunk := range chunks {
		embBytes := serializeEmbedding(chunk.Embedding)

		_, err := sq.Insert("chunks").
			Columns("chunk_id", "file_path", "chunk_type", "title", "text", "embedding", "start_line", "end_line", "created_at", "updated_at").
			Values(
				chunk.ID,
				chunk.FilePath,
				chunk.ChunkType,
				chunk.Title,
				chunk.Text,
				embBytes,
				nullableInt(chunk.StartLine),
				nullableInt(chunk.EndLine),
				chunk.CreatedAt.UTC().Format(time.RFC3339),
				chunk.UpdatedAt.UTC().Format(time.RFC3339),
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to insert chunk %s: %w", chunk.ID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Close closes the database connection.
// Should be called when done writing chunks.
func (w *ChunkWriter) Close() error {
	if err := w.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// serializeEmbedding converts float32 slice to bytes using little-endian encoding.
// Each float32 is encoded as 4 bytes (32 bits).
// For 384-dimension embeddings: 384 * 4 = 1536 bytes.
func serializeEmbedding(emb []float32) []byte {
	bytes := make([]byte, len(emb)*4)
	for i, f := range emb {
		bits := math.Float32bits(f)
		binary.LittleEndian.PutUint32(bytes[i*4:], bits)
	}
	return bytes
}

// nullableInt converts int to sql.NullInt64.
// Zero values become NULL in database (for start_line/end_line).
func nullableInt(n int) interface{} {
	if n == 0 {
		return nil
	}
	return n
}
