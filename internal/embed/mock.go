package embed

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"sync"
)

// MockProvider is a test implementation that generates deterministic embeddings.
// It tracks Close() calls and can simulate errors.
type MockProvider struct {
	mu          sync.Mutex
	dimensions  int
	closeCalled bool
	closeError  error
	embedError  error
}

// NewMockProvider creates a mock embedding provider for testing.
// It generates deterministic embeddings based on text content.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		dimensions: 384, // Standard dimension for sentence transformers
	}
}

// SetCloseError configures the mock to return an error on Close().
func (p *MockProvider) SetCloseError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeError = err
}

// SetEmbedError configures the mock to return an error on Embed().
func (p *MockProvider) SetEmbedError(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.embedError = err
}

// newMockProvider creates a mock embedding provider for testing (internal use).
func newMockProvider() Provider {
	return NewMockProvider()
}

// Embed generates mock embeddings by hashing the input text.
// This ensures deterministic, reproducible embeddings for testing.
func (p *MockProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.embedError != nil {
		return nil, p.embedError
	}

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
func (p *MockProvider) Dimensions() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dimensions
}

// Close tracks that close was called and returns configured error if set.
func (p *MockProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closeCalled = true
	return p.closeError
}

// IsClosed returns whether Close() has been called.
func (p *MockProvider) IsClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closeCalled
}
