package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// CreateFTSIndex creates FTS5 virtual table for full-text search.
// FTS5 is built into SQLite and provides fast full-text search with ranking.
//
// The virtual table indexes chunk text for:
// - Fast keyword search
// - Phrase queries
// - Boolean operators (AND, OR, NOT)
// - Snippet extraction with highlighting
// - BM25 ranking
//
// This complements vector search by enabling exact keyword matching.
func CreateFTSIndex(db *sql.DB) error {
	// Create FTS5 virtual table
	// - chunk_id: For joining with chunks table (UNINDEXED = not searched)
	// - text: Full chunk text content (indexed for search)
	// - tokenize: unicode61 with custom separators for code-aware tokenization
	createSQL := `
		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_fts USING fts5(
			chunk_id UNINDEXED,
			text,
			tokenize = 'unicode61 remove_diacritics 0'
		)
	`

	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("failed to create FTS5 index: %w", err)
	}

	return nil
}

// UpdateFTSIndex syncs FTS5 index with chunks table.
// Inserts or replaces text entries for full-text search.
//
// This should be called in the same transaction as chunk writes
// to maintain consistency between chunks and FTS5 index.
//
// Note: FTS5 virtual tables don't support INSERT OR REPLACE properly,
// so we delete first, then insert to achieve upsert semantics.
func UpdateFTSIndex(tx *sql.Tx, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Prepare delete and insert statements
	deleteStmt, err := tx.Prepare("DELETE FROM chunks_fts WHERE chunk_id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare FTS5 delete statement: %w", err)
	}
	defer deleteStmt.Close()

	insertStmt, err := tx.Prepare("INSERT INTO chunks_fts (chunk_id, text) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare FTS5 insert statement: %w", err)
	}
	defer insertStmt.Close()

	// Delete then insert each chunk (upsert pattern for FTS5)
	for _, chunk := range chunks {
		// Delete existing entry (no error if doesn't exist)
		if _, err := deleteStmt.Exec(chunk.ID); err != nil {
			return fmt.Errorf("failed to delete FTS5 entry for chunk %s: %w", chunk.ID, err)
		}

		// Insert new entry
		if _, err := insertStmt.Exec(chunk.ID, chunk.Text); err != nil {
			return fmt.Errorf("failed to insert FTS5 entry for chunk %s: %w", chunk.ID, err)
		}
	}

	return nil
}

// DeleteFTSByFile removes FTS5 entries for specified chunk IDs.
// Used during incremental updates when chunks are deleted.
func DeleteFTSByFile(tx *sql.Tx, chunkIDs []string) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	stmt, err := tx.Prepare("DELETE FROM chunks_fts WHERE chunk_id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare FTS5 delete statement: %w", err)
	}
	defer stmt.Close()

	for _, id := range chunkIDs {
		if _, err := stmt.Exec(id); err != nil {
			return fmt.Errorf("failed to delete FTS5 entry for chunk %s: %w", id, err)
		}
	}

	return nil
}

// FTSResult represents a full-text search result.
type FTSResult struct {
	ChunkID string
	Rank    float64 // BM25 rank (higher is better)
	Snippet string  // Text snippet with highlighted matches
	Chunk   *Chunk  // Full chunk data (if joined)
}

// QueryFTS performs full-text search with snippets and highlighting.
// Returns results ordered by BM25 rank (most relevant first).
//
// Query syntax supports:
//   - Simple keywords: "error handler"
//   - Phrase search: "error handler" (with quotes in SQL)
//   - Boolean AND: "error AND handler"
//   - Boolean OR: "error OR exception"
//   - Boolean NOT: "error NOT test"
//   - Prefix: "hand*" (matches handler, handle, etc.)
//
// Parameters:
//   - db: Database connection
//   - query: FTS5 query string
//   - filters: Optional filters (supports "chunk_type", "file_path")
//   - limit: Maximum number of results
//
// Returns results with snippets and BM25 ranking.
func QueryFTS(db *sql.DB, query string, filters map[string]interface{}, limit int) ([]*FTSResult, error) {
	// Build FTS5 query with JOIN to chunks table for filtering and full data
	// Use snippet() for highlighted excerpts and rank for BM25 scoring
	sqlQuery := `
		SELECT
			chunks_fts.chunk_id,
			rank,
			snippet(chunks_fts, 1, '<mark>', '</mark>', '...', 32) as snippet,
			chunks.file_path,
			chunks.chunk_type,
			chunks.title,
			chunks.text,
			chunks.embedding,
			chunks.start_line,
			chunks.end_line,
			chunks.created_at,
			chunks.updated_at
		FROM chunks_fts
		INNER JOIN chunks ON chunks_fts.chunk_id = chunks.chunk_id
		WHERE chunks_fts.text MATCH ?
	`

	// Apply filters
	var args []interface{}
	args = append(args, query)

	if chunkType, ok := filters["chunk_type"].(string); ok && chunkType != "" {
		sqlQuery += " AND chunks.chunk_type = ?"
		args = append(args, chunkType)
	}
	if filePath, ok := filters["file_path"].(string); ok && filePath != "" {
		sqlQuery += " AND chunks.file_path = ?"
		args = append(args, filePath)
	}

	sqlQuery += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query FTS5: %w", err)
	}
	defer rows.Close()

	var results []*FTSResult
	for rows.Next() {
		var (
			chunkID, snippet                 string
			rank                             float64
			filePath, chunkType, title, text string
			embBytes                         []byte
			startLine, endLine               sql.NullInt64
			createdAtStr, updatedAtStr       string
		)

		err := rows.Scan(
			&chunkID, &rank, &snippet,
			&filePath, &chunkType, &title, &text, &embBytes,
			&startLine, &endLine, &createdAtStr, &updatedAtStr,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan FTS5 result: %w", err)
		}

		// Deserialize embedding
		embedding, err := DeserializeEmbedding(embBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize embedding: %w", err)
		}

		// Parse timestamps
		createdAt, _ := parseTimestamp(createdAtStr)
		updatedAt, _ := parseTimestamp(updatedAtStr)

		chunk := &Chunk{
			ID:        chunkID,
			FilePath:  filePath,
			ChunkType: chunkType,
			Title:     title,
			Text:      text,
			Embedding: embedding,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}

		if startLine.Valid {
			chunk.StartLine = int(startLine.Int64)
		}
		if endLine.Valid {
			chunk.EndLine = int(endLine.Int64)
		}

		result := &FTSResult{
			ChunkID: chunkID,
			Rank:    rank,
			Snippet: snippet,
			Chunk:   chunk,
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating FTS5 results: %w", err)
	}

	return results, nil
}

// SearchText is a high-level API for full-text search.
// Returns just the chunks (without snippet metadata) for simpler usage.
func SearchText(db *sql.DB, query string, filters map[string]interface{}, limit int) ([]*Chunk, error) {
	results, err := QueryFTS(db, query, filters, limit)
	if err != nil {
		return nil, err
	}

	chunks := make([]*Chunk, len(results))
	for i, r := range results {
		chunks[i] = r.Chunk
	}

	return chunks, nil
}

// BuildFTSQuery constructs an FTS5 query from user input.
// Handles escaping and query syntax construction.
//
// Examples:
//   - Simple: "error handler" → "error handler"
//   - Phrase: BuildFTSQuery("error handler", true) → "\"error handler\""
//   - Prefix: BuildFTSQuery("hand", false) + "*" → "hand*"
func BuildFTSQuery(input string, isPhrase bool) string {
	// Escape special FTS5 characters
	input = escapeFTSQuery(input)

	if isPhrase {
		return fmt.Sprintf(`"%s"`, input)
	}

	return input
}

// escapeFTSQuery escapes FTS5 special characters.
// FTS5 special chars: " ( ) AND OR NOT
func escapeFTSQuery(input string) string {
	// Replace double quotes with escaped quotes
	input = strings.ReplaceAll(input, `"`, `""`)
	return input
}

// GetFTSIndexStats returns statistics about the FTS5 index.
type FTSIndexStats struct {
	TotalEntries int
	IndexSize    int64 // Size in bytes
}

// GetFTSStats retrieves FTS5 index statistics.
func GetFTSStats(db *sql.DB) (*FTSIndexStats, error) {
	var stats FTSIndexStats

	// Get entry count
	err := db.QueryRow("SELECT COUNT(*) FROM chunks_fts").Scan(&stats.TotalEntries)
	if err != nil {
		return nil, fmt.Errorf("failed to query FTS5 entry count: %w", err)
	}

	// Get index size (pages * page_size from PRAGMA)
	// This is approximate - FTS5 stores data in multiple tables
	var pageCount, pageSize int64
	_ = db.QueryRow("PRAGMA page_count").Scan(&pageCount)
	_ = db.QueryRow("PRAGMA page_size").Scan(&pageSize)
	stats.IndexSize = pageCount * pageSize

	return &stats, nil
}

// parseTimestamp is a helper to parse RFC3339 timestamps from database.
// Used internally by QueryFTS for timestamp deserialization.
func parseTimestamp(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
