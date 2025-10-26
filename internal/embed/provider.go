package embed

import "context"

// Provider defines the interface for embedding text into vectors.
// Implementations may use local models, remote APIs, or other embedding services.
type Provider interface {
	// Embed converts a slice of text strings into their vector representations.
	// Returns a slice of vectors where each vector is a slice of float32 values.
	Embed(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of the embedding vectors produced by this provider.
	Dimensions() int

	// Close releases any resources held by the provider.
	// For local providers, this may include stopping background processes.
	Close() error
}
