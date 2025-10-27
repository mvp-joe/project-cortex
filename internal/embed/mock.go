package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
)

// mockProvider is a test implementation that generates deterministic embeddings.
type mockProvider struct {
	dimensions int
}

// newMockProvider creates a mock embedding provider for testing.
// It generates deterministic embeddings based on text content.
func newMockProvider() Provider {
	return &mockProvider{
		dimensions: 384, // Standard dimension for sentence transformers
	}
}

// Embed generates mock embeddings by hashing the input text.
// This ensures deterministic, reproducible embeddings for testing.
func (p *mockProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
	embeddings := make([][]float32, len(texts))

	for i, text := range texts {
		// Generate deterministic embedding from text hash
		hash := sha256.Sum256([]byte(text))

		embedding := make([]float32, p.dimensions)
		for j := 0; j < p.dimensions; j++ {
			// Use hash bytes to generate float32 values
			offset := (j * 4) % len(hash)
			val := binary.BigEndian.Uint32(hash[offset : offset+4])
			// Normalize to [-1, 1] range
			embedding[j] = (float32(val)/float32(1<<32))*2.0 - 1.0
		}

		embeddings[i] = embedding
	}

	return embeddings, nil
}

// Dimensions returns the dimensionality of mock embeddings.
func (p *mockProvider) Dimensions() int {
	return p.dimensions
}

// Close is a no-op for mock provider.
func (p *mockProvider) Close() error {
	return nil
}
