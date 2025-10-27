package mcp

import "context"

// EmbeddingProvider defines the interface for generating text embeddings.
// This is a minimal interface to avoid import cycles with internal/embed.
type EmbeddingProvider interface {
	// Embed converts a slice of text strings into their vector representations.
	// The mode parameter specifies whether embeddings are for queries or passages.
	Embed(ctx context.Context, texts []string, mode string) ([][]float32, error)

	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int

	// Close releases any resources held by the provider.
	Close() error
}

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
