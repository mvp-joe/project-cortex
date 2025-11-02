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

// ChunkReader handles reading chunks from SQLite database.
// Opens database in read-only mode for safety and concurrent access.
type ChunkReader struct {
	db *sql.DB
}

// NewChunkReader opens a SQLite database for reading chunks.
// Uses read-only mode to prevent accidental modifications.
func NewChunkReader(dbPath string) (*ChunkReader, error) {
	// Open in read-only mode with query param
	db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys for consistency
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	return &ChunkReader{db: db}, nil
}

// ReadAllChunks loads all chunks from the database.
// Primary use case: Loading chunks into chromem-go in-memory index.
func (r *ChunkReader) ReadAllChunks() ([]*Chunk, error) {
	rows, err := sq.Select("chunk_id", "file_path", "chunk_type", "title", "text", "embedding", "start_line", "end_line", "created_at", "updated_at").
		From("chunks").
		OrderBy("chunk_id").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*Chunk

	for rows.Next() {
		chunk, err := scanChunk(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chunks: %w", err)
	}

	return chunks, nil
}

// ReadChunksByFile loads all chunks for a specific file.
// Use for file-level operations and debugging.
func (r *ChunkReader) ReadChunksByFile(filePath string) ([]*Chunk, error) {
	rows, err := sq.Select("chunk_id", "file_path", "chunk_type", "title", "text", "embedding", "start_line", "end_line", "created_at", "updated_at").
		From("chunks").
		Where(sq.Eq{"file_path": filePath}).
		OrderBy("start_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks by file: %w", err)
	}
	defer rows.Close()

	var chunks []*Chunk

	for rows.Next() {
		chunk, err := scanChunk(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chunks: %w", err)
	}

	return chunks, nil
}

// ReadChunksByType loads all chunks of a specific type.
// Use for filtering by chunk_type (symbols, definitions, data, documentation).
func (r *ChunkReader) ReadChunksByType(chunkType string) ([]*Chunk, error) {
	rows, err := sq.Select("chunk_id", "file_path", "chunk_type", "title", "text", "embedding", "start_line", "end_line", "created_at", "updated_at").
		From("chunks").
		Where(sq.Eq{"chunk_type": chunkType}).
		OrderBy("file_path", "start_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks by type: %w", err)
	}
	defer rows.Close()

	var chunks []*Chunk

	for rows.Next() {
		chunk, err := scanChunk(rows)
		if err != nil {
			return nil, fmt.Errorf("failed to scan chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating chunks: %w", err)
	}

	return chunks, nil
}

// Close closes the database connection.
func (r *ChunkReader) Close() error {
	if err := r.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// scanChunk scans a chunk from a SQL row.
// Handles nullable integers (start_line, end_line) and deserializes embeddings.
func scanChunk(rows *sql.Rows) (*Chunk, error) {
	var (
		id, filePath, chunkType, title, text string
		embBytes                             []byte
		startLine, endLine                   sql.NullInt64
		createdAtStr, updatedAtStr           string
	)

	err := rows.Scan(
		&id, &filePath, &chunkType, &title, &text,
		&embBytes, &startLine, &endLine, &createdAtStr, &updatedAtStr,
	)
	if err != nil {
		return nil, err
	}

	// Deserialize embedding
	embedding := deserializeEmbedding(embBytes)

	// Parse timestamps
	createdAt, err := time.Parse(time.RFC3339, createdAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse created_at: %w", err)
	}

	updatedAt, err := time.Parse(time.RFC3339, updatedAtStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse updated_at: %w", err)
	}

	chunk := &Chunk{
		ID:        id,
		FilePath:  filePath,
		ChunkType: chunkType,
		Title:     title,
		Text:      text,
		Embedding: embedding,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}

	// Handle nullable integers
	if startLine.Valid {
		chunk.StartLine = int(startLine.Int64)
	}
	if endLine.Valid {
		chunk.EndLine = int(endLine.Int64)
	}

	return chunk, nil
}

// deserializeEmbedding converts bytes back to float32 slice using little-endian encoding.
// Reverses the serialization performed by serializeEmbedding in chunk_writer.go.
func deserializeEmbedding(bytes []byte) []float32 {
	if len(bytes)%4 != 0 {
		// Should never happen if data is valid
		return nil
	}

	floats := make([]float32, len(bytes)/4)
	for i := range floats {
		bits := binary.LittleEndian.Uint32(bytes[i*4:])
		floats[i] = math.Float32frombits(bits)
	}
	return floats
}
