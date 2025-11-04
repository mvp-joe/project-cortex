package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
)

// sqliteExactSearcher implements ExactSearcher using SQLite FTS5.
// This replaces bleve in-memory full-text index with direct SQLite queries.
type sqliteExactSearcher struct {
	db *sql.DB
	mu sync.RWMutex // Protects database during operations
}

// NewSQLiteExactSearcher creates a new ExactSearcher backed by SQLite FTS5.
// Uses direct SQL queries instead of loading files into memory.
//
// Parameters:
//   - db: SQLite database connection with files_fts and files tables
//
// Benefits over bleve:
//   - Zero memory overhead (no in-memory index)
//   - Instant startup (no index building)
//   - Native SQL filtering (JOIN with files table)
//   - Automatic BM25 ranking
func NewSQLiteExactSearcher(db *sql.DB) (ExactSearcher, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}

	return &sqliteExactSearcher{
		db: db,
	}, nil
}

// Search executes a keyword search using FTS5 QueryStringQuery syntax.
// Supports field scoping, boolean operators, phrase search, wildcards, and fuzzy matching.
func (s *sqliteExactSearcher) Search(ctx context.Context, queryStr string, options *ExactSearchOptions) ([]*ExactSearchResult, error) {
	// Apply defaults if options not provided
	if options == nil {
		options = DefaultExactSearchOptions()
	}

	limit := options.Limit
	if limit <= 0 || limit > 100 {
		limit = 15
	}

	// Acquire read lock for query
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build FTS5 query with JOIN to files table
	// Use snippet() for highlighted excerpts and rank for BM25 scoring
	// Note: snippet(table, column_index, ...) where column_index is 0-based
	// files_fts has columns: file_path (0), content (1)
	sqlQuery := sq.Select(
		"files_fts.file_path",
		"rank",
		"snippet(files_fts, 1, '<mark>', '</mark>', '...', 32) as snippet",
		"f.language",
		"f.line_count_total",
		"f.line_count_code",
	).
		From("files_fts").
		Join("files f ON files_fts.file_path = f.file_path").
		Where(sq.Expr("files_fts.content MATCH ?", queryStr))

	// Add optional filters
	if options.Language != "" {
		sqlQuery = sqlQuery.Where(sq.Eq{"f.language": options.Language})
	}
	if options.FilePath != "" {
		sqlQuery = sqlQuery.Where(sq.Like{"f.file_path": options.FilePath})
	}

	sqlQuery = sqlQuery.OrderBy("rank").Limit(uint64(limit))

	// Execute query
	rows, err := sqlQuery.RunWith(s.db).QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("FTS5 search query failed: %w", err)
	}
	defer rows.Close()

	// Scan results and build ExactSearchResult structs
	// Note: Each result represents a FILE, not a chunk
	results := make([]*ExactSearchResult, 0, limit)
	for rows.Next() {
		var (
			filePath                         string
			rank                             float64
			snippet                          string
			language                         string
			lineCountTotal, lineCountCode    sql.NullInt64
		)

		err := rows.Scan(
			&filePath, &rank, &snippet,
			&language, &lineCountTotal, &lineCountCode,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}

		// Build tags from language (file-level results)
		tags := []string{"code", language}

		// Create ContextChunk representing the file
		// Use file_path as chunk ID for file-level results
		chunk := &ContextChunk{
			ID:        fmt.Sprintf("file-%s", filePath),
			Title:     fmt.Sprintf("File: %s", filePath),
			Text:      snippet, // Use snippet as text (actual content is in FTS5)
			ChunkType: "file",  // File-level result
			Embedding: nil,     // No embedding for FTS results
			Tags:      tags,
			Metadata: map[string]interface{}{
				"file_path": filePath,
				"language":  language,
			},
			CreatedAt: time.Time{}, // Zero time for file-level results
			UpdatedAt: time.Time{}, // Zero time for file-level results
		}

		// Add line counts to metadata if present
		if lineCountTotal.Valid {
			chunk.Metadata["line_count_total"] = lineCountTotal.Int64
		}
		if lineCountCode.Valid {
			chunk.Metadata["line_count_code"] = lineCountCode.Int64
		}

		// Extract highlights from snippet
		// FTS5 snippet() returns a single string with <mark> tags
		highlights := []string{snippet}
		if snippet == "" {
			highlights = []string{}
		}

		// Convert BM25 rank to score (higher rank = better match, invert for score)
		// BM25 rank in SQLite FTS5 is negative (lower = better), so negate for score
		score := -rank

		results = append(results, &ExactSearchResult{
			Chunk:      chunk,
			Score:      score,
			Highlights: highlights,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return results, nil
}

// UpdateIncremental is a no-op for SQLite searcher (FTS5 always current).
// FTS5 index is automatically maintained by SQLite triggers or explicit updates.
func (s *sqliteExactSearcher) UpdateIncremental(ctx context.Context, added, updated []*ContextChunk, deleted []string) error {
	// No-op: FTS5 index is maintained by storage layer during writes
	// This method exists for interface compatibility with bleve-based implementation
	return nil
}

// Close releases resources (no-op as database is externally managed).
func (s *sqliteExactSearcher) Close() error {
	// Database is externally managed - caller owns lifecycle
	return nil
}
