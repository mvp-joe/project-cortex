package mcp

import "context"

// ContextSearcher defines the interface for searching code and documentation chunks.
// This interface allows different backend implementations (chromem-go, external vector DB, etc.)
type ContextSearcher interface {
	// Query executes a semantic search for the given query string.
	// Returns ranked search results based on vector similarity and filters.
	Query(ctx context.Context, query string, options *SearchOptions) ([]*SearchResult, error)

	// Reload reloads the chunk database from the configured directory.
	// Used for hot reload when chunk files are updated.
	Reload(ctx context.Context) error

	// GetMetrics returns current reload operation metrics.
	// Used for monitoring reload health and statistics.
	GetMetrics() MetricsSnapshot

	// Close releases resources held by the searcher.
	Close() error
}

// ExactSearcher defines the interface for full-text keyword search.
type ExactSearcher interface {
	// Search executes a keyword search using FTS query syntax.
	// Supports field scoping, boolean operators, phrase search, wildcards, and fuzzy matching.
	// Options parameter may be nil (defaults will be applied).
	Search(ctx context.Context, queryStr string, options *ExactSearchOptions) ([]*ExactSearchResult, error)

	// UpdateIncremental applies incremental updates to the search index.
	// Uses batch operations for optimal performance.
	UpdateIncremental(ctx context.Context, added, updated []*ContextChunk, deleted []string) error

	// Close releases resources held by the searcher.
	Close() error
}

// ExactSearchResult represents a single keyword search result with highlighting.
type ExactSearchResult struct {
	Chunk      *ContextChunk `json:"chunk"`
	Score      float64       `json:"score"`      // Match quality (0-1)
	Highlights []string      `json:"highlights"` // Matching snippets with <mark> tags
}
