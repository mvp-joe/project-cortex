package storage

import (
	"database/sql"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

// InitVectorExtension initializes the sqlite-vec extension globally.
// Must be called once before using any vector search functionality.
// This registers the extension with all future database connections.
func InitVectorExtension() {
	sqlite_vec.Auto()
}

// CreateVectorIndex creates a virtual table for vector similarity search.
// Uses sqlite-vec's vec0 virtual table to enable efficient vector queries.
//
// The virtual table mirrors the chunks table structure but optimizes for:
// - Fast cosine similarity search on embeddings
// - K-nearest neighbors queries
// - Distance-based filtering
//
// Note: This does NOT store chunk data, only indexes for vector search.
// Join with chunks table to get full chunk details.
func CreateVectorIndex(db *sql.DB, dimensions int) error {
	// Create vec0 virtual table for vector similarity search
	// vec0 provides efficient KNN and distance queries
	createSQL := fmt.Sprintf(`
		CREATE VIRTUAL TABLE IF NOT EXISTS chunks_vec USING vec0(
			chunk_id TEXT PRIMARY KEY,
			embedding float[%d]
		)
	`, dimensions)

	if _, err := db.Exec(createSQL); err != nil {
		return fmt.Errorf("failed to create vector index: %w", err)
	}

	return nil
}

// UpdateVectorIndex inserts or updates vectors in the index.
// Performs upsert operation - replaces existing vectors for same chunk_id.
//
// This is called after chunks are written to maintain index consistency.
// Operations are typically done in the same transaction as chunk writes.
//
// Note: sqlite-vec's vec0 virtual tables don't support INSERT OR REPLACE,
// so we delete first, then insert to achieve upsert semantics.
func UpdateVectorIndex(tx *sql.Tx, chunks []*Chunk) error {
	if len(chunks) == 0 {
		return nil
	}

	// Prepare delete and insert statements
	deleteStmt, err := tx.Prepare("DELETE FROM chunks_vec WHERE chunk_id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare vector delete statement: %w", err)
	}
	defer deleteStmt.Close()

	insertStmt, err := tx.Prepare("INSERT INTO chunks_vec (chunk_id, embedding) VALUES (?, ?)")
	if err != nil {
		return fmt.Errorf("failed to prepare vector insert statement: %w", err)
	}
	defer insertStmt.Close()

	// Delete then insert each vector (upsert pattern for vec0)
	for _, chunk := range chunks {
		// Delete existing entry (no error if doesn't exist)
		if _, err := deleteStmt.Exec(chunk.ID); err != nil {
			return fmt.Errorf("failed to delete vector for chunk %s: %w", chunk.ID, err)
		}

		// Serialize embedding using sqlite-vec's format
		embBytes, err := sqlite_vec.SerializeFloat32(chunk.Embedding)
		if err != nil {
			return fmt.Errorf("failed to serialize embedding for chunk %s: %w", chunk.ID, err)
		}

		// Insert new entry
		if _, err := insertStmt.Exec(chunk.ID, embBytes); err != nil {
			return fmt.Errorf("failed to insert vector for chunk %s: %w", chunk.ID, err)
		}
	}

	return nil
}

// DeleteVectorsByFile removes vectors for all chunks in a file.
// Used during incremental updates when file content changes.
func DeleteVectorsByFile(tx *sql.Tx, chunkIDs []string) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	// Delete vectors for these chunk IDs
	stmt, err := tx.Prepare("DELETE FROM chunks_vec WHERE chunk_id = ?")
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer stmt.Close()

	for _, id := range chunkIDs {
		if _, err := stmt.Exec(id); err != nil {
			return fmt.Errorf("failed to delete vector for chunk %s: %w", id, err)
		}
	}

	return nil
}

// VectorSearchResult represents a vector similarity search result.
type VectorSearchResult struct {
	ChunkID  string
	Distance float64 // Lower is better (cosine distance)
}

// QueryVectorSimilarity performs K-nearest neighbors search using cosine distance.
// Returns chunk IDs sorted by similarity (most similar first).
//
// This is a low-level query that only returns chunk IDs and distances.
// Callers should JOIN with chunks table to get full chunk data.
//
// Parameters:
//   - db: Database connection
//   - queryEmb: Query embedding vector (must match index dimensions)
//   - limit: Number of results to return (K in KNN)
//
// Returns results ordered by distance (ascending - closest first).
func QueryVectorSimilarity(db *sql.DB, queryEmb []float32, limit int) ([]*VectorSearchResult, error) {
	// Serialize query embedding
	queryBytes, err := sqlite_vec.SerializeFloat32(queryEmb)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize query embedding: %w", err)
	}

	// Use vec_distance_cosine for similarity search
	// sqlite-vec automatically indexes for fast KNN
	query := `
		SELECT
			chunk_id,
			vec_distance_cosine(embedding, ?) as distance
		FROM chunks_vec
		ORDER BY distance
		LIMIT ?
	`

	rows, err := db.Query(query, queryBytes, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query vector index: %w", err)
	}
	defer rows.Close()

	var results []*VectorSearchResult
	for rows.Next() {
		var r VectorSearchResult
		if err := rows.Scan(&r.ChunkID, &r.Distance); err != nil {
			return nil, fmt.Errorf("failed to scan vector result: %w", err)
		}
		results = append(results, &r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating vector results: %w", err)
	}

	return results, nil
}

// VectorIndexStats returns statistics about the vector index.
// Useful for monitoring and debugging.
type VectorIndexStats struct {
	TotalVectors int
	Dimensions   int
}

// GetVectorIndexStats retrieves statistics about the vector index.
func GetVectorIndexStats(db *sql.DB) (*VectorIndexStats, error) {
	var stats VectorIndexStats

	// Get count
	err := db.QueryRow("SELECT COUNT(*) FROM chunks_vec").Scan(&stats.TotalVectors)
	if err != nil {
		return nil, fmt.Errorf("failed to query vector count: %w", err)
	}

	// Dimensions can be inferred from table schema or metadata
	// For now, we'll use the standard dimension from cache_metadata
	err = db.QueryRow("SELECT value FROM cache_metadata WHERE key = 'embedding_dimensions'").Scan(&stats.Dimensions)
	if err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("failed to query dimensions: %w", err)
	}

	return &stats, nil
}
