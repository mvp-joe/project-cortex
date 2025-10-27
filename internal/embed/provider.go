package embed

import "context"

// EmbedMode specifies the type of embedding to generate.
type EmbedMode string

const (
	// EmbedModeQuery generates embeddings optimized for search queries.
	// Use this when embedding user questions or search terms.
	EmbedModeQuery EmbedMode = "query"

	// EmbedModePassage generates embeddings optimized for document passages.
	// Use this when embedding code chunks, documentation, or any searchable content.
	EmbedModePassage EmbedMode = "passage"
)

// Provider defines the interface for embedding text into vectors.
// Implementations may use local models, remote APIs, or other embedding services.
type Provider interface {
	// Initialize prepares the provider and blocks until ready.
	// For LocalProvider: detects/downloads binary, starts process, waits for health check.
	// For other providers: validates credentials, checks connectivity.
	// Must be called before Embed().
	Initialize(ctx context.Context) error

	// Embed converts a slice of text strings into their vector representations.
	// The mode parameter specifies whether embeddings are for queries or passages.
	// Returns a slice of vectors where each vector is a slice of float32 values.
	// Initialize() must be called first.
	Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error)

	// Dimensions returns the dimensionality of the embedding vectors produced by this provider.
	Dimensions() int

	// Close releases any resources held by the provider.
	// For local providers, this may include stopping background processes.
	Close() error
}
