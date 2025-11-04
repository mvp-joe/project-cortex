package storage

import (
	"database/sql"
	"fmt"
	"sort"
	"time"

	sq "github.com/Masterminds/squirrel"
)

// ChunkReader handles reading chunks from SQLite database.
// Opens database in read-only mode for safety and concurrent access.
type ChunkReader struct {
	db     *sql.DB
	ownsDB bool // true if we opened the connection, false if shared
}

// NewChunkReader opens a SQLite database for reading chunks.
// Uses read-only mode to prevent accidental modifications.
//
// Deprecated: Use NewChunkReaderWithDB to share database connections.
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

	return &ChunkReader{db: db, ownsDB: true}, nil
}

// NewChunkReaderWithDB creates a ChunkReader using an existing database connection.
// The caller is responsible for managing the database lifecycle (foreign keys, close).
// This is the preferred constructor when sharing a connection across multiple readers.
func NewChunkReaderWithDB(db *sql.DB) *ChunkReader {
	return &ChunkReader{db: db, ownsDB: false}
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

// SearchByEmbedding performs vector similarity search and returns full chunks.
// Combines sqlite-vec vector search with JOIN to chunks table.
//
// Parameters:
//   - queryEmb: Query embedding vector
//   - filters: Optional filters map (supports "chunk_type", "file_path", "tags")
//   - limit: Maximum number of results to return
//
// Returns chunks ordered by similarity (most similar first).
// This is a high-level API that combines QueryVectorSimilarity with chunk data.
func (r *ChunkReader) SearchByEmbedding(queryEmb []float32, filters map[string]interface{}, limit int) ([]*Chunk, error) {
	// First, get vector similarity results
	vectorResults, err := QueryVectorSimilarity(r.db, queryEmb, limit*2) // Fetch 2x for filtering headroom
	if err != nil {
		return nil, fmt.Errorf("vector similarity search failed: %w", err)
	}

	if len(vectorResults) == 0 {
		return []*Chunk{}, nil
	}

	// Extract chunk IDs for IN clause
	chunkIDs := make([]string, len(vectorResults))
	distanceMap := make(map[string]float64)
	for i, r := range vectorResults {
		chunkIDs[i] = r.ChunkID
		distanceMap[r.ChunkID] = r.Distance
	}

	// Build query with filters
	query := sq.Select("chunk_id", "file_path", "chunk_type", "title", "text", "embedding", "start_line", "end_line", "created_at", "updated_at").
		From("chunks").
		Where(sq.Eq{"chunk_id": chunkIDs})

	// Apply optional filters
	if chunkType, ok := filters["chunk_type"].(string); ok && chunkType != "" {
		query = query.Where(sq.Eq{"chunk_type": chunkType})
	}
	if filePath, ok := filters["file_path"].(string); ok && filePath != "" {
		query = query.Where(sq.Eq{"file_path": filePath})
	}
	// Note: "tags" filter would require tags column in chunks table (future enhancement)

	rows, err := query.RunWith(r.db).Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query chunks: %w", err)
	}
	defer rows.Close()

	// Scan chunks
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

	// Sort by vector distance (preserve similarity ordering)
	// SQLite doesn't guarantee order after JOIN, so we sort in Go
	sortChunksByDistance(chunks, distanceMap)

	// Apply limit after filtering
	if len(chunks) > limit {
		chunks = chunks[:limit]
	}

	return chunks, nil
}

// Close closes the database connection if owned by this reader.
// If created via NewChunkReaderWithDB (shared connection), this is a no-op.
func (r *ChunkReader) Close() error {
	if !r.ownsDB {
		// Shared connection - caller owns it
		return nil
	}
	if err := r.db.Close(); err != nil {
		return fmt.Errorf("failed to close database: %w", err)
	}
	return nil
}

// sortChunksByDistance sorts chunks by their vector distance (ascending).
// Performance: O(n log n) using sort.Slice instead of O(n²) bubble sort.
func sortChunksByDistance(chunks []*Chunk, distanceMap map[string]float64) {
	sort.Slice(chunks, func(i, j int) bool {
		return distanceMap[chunks[i].ID] < distanceMap[chunks[j].ID]
	})
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
	embedding, err := DeserializeEmbedding(embBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize embedding: %w", err)
	}

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
