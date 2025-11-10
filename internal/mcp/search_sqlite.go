package mcp

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// sqliteSearcher implements ContextSearcher using SQLite with sqlite-vec.
// This replaces chromem-go in-memory vector DB with direct SQLite queries.
type sqliteSearcher struct {
	db       *sql.DB
	provider embed.Provider
	metrics  *ReloadMetrics
	mu       sync.RWMutex // Protects database during operations
}

// NewSQLiteSearcher creates a new ContextSearcher backed by SQLite with sqlite-vec.
// Uses direct SQL queries instead of loading chunks into memory.
//
// Parameters:
//   - db: SQLite database connection with chunks, chunks_vec, and files tables
//   - provider: Embedding provider for generating query embeddings
//
// Benefits over chromem-go:
//   - Zero memory overhead (no in-memory index)
//   - Instant startup (no index building)
//   - Native SQL filtering (no post-filtering needed)
func NewSQLiteSearcher(db *sql.DB, provider embed.Provider) (ContextSearcher, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}

	return &sqliteSearcher{
		db:       db,
		provider: provider,
		metrics:  NewReloadMetrics(),
	}, nil
}

// Query executes a semantic search using sqlite-vec for vector similarity.
// Combines vector search with SQL filtering for chunk types, tags (via files.language), and file paths.
func (s *sqliteSearcher) Query(ctx context.Context, query string, options *SearchOptions) ([]*SearchResult, error) {
	if options == nil {
		options = DefaultSearchOptions()
	}

	// Validate and normalize options
	if options.Limit <= 0 || options.Limit > 100 {
		options.Limit = 15
	}

	// Generate query embedding (use "query" mode for search queries)
	embeddings, err := s.provider.Embed(ctx, []string{query}, embed.EmbedModeQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned for query")
	}
	queryEmbedding := embeddings[0]

	// Serialize embedding for SQLite
	queryBytes := storage.SerializeEmbedding(queryEmbedding)

	// Acquire read lock for query
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build vector similarity query with filters
	// Fetch 2x limit for filtering headroom (same as chromem implementation)
	topK := options.Limit * 2

	// Base query: vector similarity + JOIN to chunks and files
	sqlQuery := sq.Select(
		"c.chunk_id",
		"c.file_path",
		"c.chunk_type",
		"c.title",
		"c.text",
		"c.embedding",
		"c.start_line",
		"c.end_line",
		"c.created_at",
		"c.updated_at",
		"f.language",
		"vec.distance",
	).
		From("chunks_vec vec").
		Join("chunks c ON vec.chunk_id = c.chunk_id").
		Join("files f ON c.file_path = f.file_path").
		Where(sq.Expr("vec.embedding MATCH ?", queryBytes)).
		Where(sq.Expr("k = ?", topK))

	// Apply chunk type filter (native SQL)
	if len(options.ChunkTypes) > 0 {
		sqlQuery = sqlQuery.Where(sq.Eq{"c.chunk_type": options.ChunkTypes})
	}

	// Apply tag filter (derive from files.language and chunk_type)
	// Tags in MCP are typically: ["go", "code"], ["typescript", "documentation"]
	// We derive tags from files.language and chunk_type columns
	if len(options.Tags) > 0 {
		for _, tag := range options.Tags {
			// Check if tag matches language
			if isLanguageTag(tag) {
				sqlQuery = sqlQuery.Where(sq.Eq{"f.language": tag})
			}
			// Check if tag matches content type
			if isContentTag(tag) {
				if tag == "code" {
					sqlQuery = sqlQuery.Where(sq.NotEq{"c.chunk_type": "documentation"})
				} else if tag == "documentation" {
					sqlQuery = sqlQuery.Where(sq.Eq{"c.chunk_type": "documentation"})
				}
			}
		}
	}

	// Order by distance (ascending - lower distance = better match)
	sqlQuery = sqlQuery.OrderBy("vec.distance").
		Limit(uint64(options.Limit))

	// Execute query
	rows, err := sqlQuery.RunWith(s.db).QueryContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("vector search query failed: %w", err)
	}
	defer rows.Close()

	// Scan results and build SearchResult structs
	results := make([]*SearchResult, 0, options.Limit)
	for rows.Next() {
		var (
			id, filePath, chunkType, title, text string
			embBytes                             []byte
			startLine, endLine                   sql.NullInt64
			createdAtStr, updatedAtStr           string
			language                             string
			distance                             float64
		)

		err := rows.Scan(
			&id, &filePath, &chunkType, &title, &text,
			&embBytes, &startLine, &endLine, &createdAtStr, &updatedAtStr,
			&language, &distance,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan result: %w", err)
		}

		// Deserialize embedding
		embedding, err := storage.DeserializeEmbedding(embBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to deserialize embedding: %w", err)
		}

		// Parse timestamps
		createdAt, _ := parseTimestamp(createdAtStr)
		updatedAt, _ := parseTimestamp(updatedAtStr)

		// Build tags from language and chunk_type
		tags := buildTags(language, chunkType)

		// Create ContextChunk
		chunk := &ContextChunk{
			ID:        id,
			Title:     title,
			Text:      text,
			ChunkType: chunkType,
			Embedding: embedding,
			Tags:      tags,
			Metadata: map[string]interface{}{
				"file_path":  filePath,
				"start_line": startLine.Int64,
				"end_line":   endLine.Int64,
			},
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}

		// Convert distance to similarity score (lower distance = higher similarity)
		// sqlite-vec uses cosine distance, where 0 = identical, 2 = opposite
		// Convert to similarity: 1.0 - (distance / 2.0)
		similarityScore := 1.0 - (distance / 2.0)

		// Apply min score filter (post-filter for threshold)
		if options.MinScore > 0 && similarityScore < options.MinScore {
			continue
		}

		results = append(results, &SearchResult{
			Chunk:         chunk,
			CombinedScore: similarityScore,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating results: %w", err)
	}

	return results, nil
}

// Reload is a no-op for SQLite searcher (data is always current).
// SQLite-backed searcher doesn't need reload - database is always up-to-date.
func (s *sqliteSearcher) Reload(ctx context.Context) error {
	// No-op: SQLite database is always current (no in-memory cache to reload)
	return nil
}

// GetMetrics returns reload metrics (always healthy for SQLite).
func (s *sqliteSearcher) GetMetrics() MetricsSnapshot {
	return s.metrics.GetMetrics()
}

// Close releases resources (no-op as database is externally managed).
func (s *sqliteSearcher) Close() error {
	// Database is externally managed - caller owns lifecycle
	return nil
}

// Helper functions

// isLanguageTag checks if a tag is a programming language.
func isLanguageTag(tag string) bool {
	languages := []string{
		"go", "typescript", "javascript", "python", "rust",
		"c", "cpp", "java", "php", "ruby", "tsx", "jsx",
	}
	for _, lang := range languages {
		if tag == lang {
			return true
		}
	}
	return false
}

// isContentTag checks if a tag is a content type indicator.
func isContentTag(tag string) bool {
	return tag == "code" || tag == "documentation"
}

// buildTags constructs tags array from language and chunk_type.
// Tags format: ["<language>", "<content_type>", "<chunk_type>"]
func buildTags(language, chunkType string) []string {
	tags := make([]string, 0, 3)

	// Add language tag if present
	if language != "" {
		tags = append(tags, language)
	}

	// Add content type tag
	if chunkType == "documentation" {
		tags = append(tags, "documentation")
	} else {
		tags = append(tags, "code")
	}

	// Add chunk type as tag
	if chunkType != "" {
		tags = append(tags, chunkType)
	}

	return tags
}

// parseTimestamp parses RFC3339 timestamp strings from database.
func parseTimestamp(s string) (time.Time, error) {
	return time.Parse(time.RFC3339, s)
}
