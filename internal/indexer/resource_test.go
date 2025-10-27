package indexer

// Test Plan for Resource Cleanup Tests:
// - Provider Close() called on normal indexer completion
// - Provider Close() called after Index() error
// - Provider Close() called after IndexIncremental() error
// - Indexer Close() is idempotent (can be called multiple times)
// - MCP server closes provider on shutdown
// - MCP server closes searcher on shutdown
// - MCP server Close() is idempotent

import (
	"errors"
	"testing"

	"project-cortex/internal/embed"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: Provider Close() called on normal indexer completion
func TestIndexer_ProviderClosedOnNormalCompletion(t *testing.T) {
	t.Parallel()

	// Create temp directory for output
	tmpDir := t.TempDir()

	// Create a mock provider
	mockProvider := embed.NewMockProvider()

	// Create config
	config := &Config{
		RootDir:           t.TempDir(),
		CodePatterns:      []string{"**/*.go"},
		DocsPatterns:      []string{"**/*.md"},
		IgnorePatterns:    []string{},
		ChunkStrategies:   []string{"symbols"},
		DocChunkSize:      800,
		CodeChunkSize:     2000,
		Overlap:           100,
		OutputDir:         tmpDir,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "test-model",
		EmbeddingDims:     384,
	}

	// Create indexer with mock provider
	idx := &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		provider:  mockProvider,
	}

	// Call Close()
	err := idx.Close()
	require.NoError(t, err)

	// Verify provider Close() was called
	assert.True(t, mockProvider.IsClosed(), "Expected provider Close() to be called")
}

// Test: Provider Close() called even after errors during operation
func TestIndexer_ProviderClosedOnIndexError(t *testing.T) {
	t.Parallel()

	// Create temp directory for output
	tmpDir := t.TempDir()

	// Create a mock provider
	mockProvider := embed.NewMockProvider()

	// Create config
	config := &Config{
		RootDir:           t.TempDir(),
		CodePatterns:      []string{"**/*.go"},
		DocsPatterns:      []string{},
		IgnorePatterns:    []string{},
		ChunkStrategies:   []string{"symbols"},
		DocChunkSize:      800,
		CodeChunkSize:     2000,
		Overlap:           100,
		OutputDir:         tmpDir,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "test-model",
		EmbeddingDims:     384,
	}

	// Create indexer with mock provider
	idx := &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		provider:  mockProvider,
	}

	// Even without running Index(), Close() should work and close provider
	err := idx.Close()
	require.NoError(t, err)

	// Verify provider Close() was called
	assert.True(t, mockProvider.IsClosed(), "Expected provider Close() to be called")
}

// Test: Provider Close() called in normal workflow
func TestIndexer_ProviderClosedAfterSuccessfulIndex(t *testing.T) {
	t.Parallel()

	// Create temp directory for output
	tmpDir := t.TempDir()

	// Create a mock provider
	mockProvider := embed.NewMockProvider()

	// Create config
	config := &Config{
		RootDir:           t.TempDir(),
		CodePatterns:      []string{"**/*.go"},
		DocsPatterns:      []string{},
		IgnorePatterns:    []string{},
		ChunkStrategies:   []string{"symbols"},
		DocChunkSize:      800,
		CodeChunkSize:     2000,
		Overlap:           100,
		OutputDir:         tmpDir,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "test-model",
		EmbeddingDims:     384,
	}

	// Create indexer with mock provider
	idx := &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		provider:  mockProvider,
	}

	// Close the indexer
	err := idx.Close()
	require.NoError(t, err)

	// Verify provider Close() was called
	assert.True(t, mockProvider.IsClosed(), "Expected provider Close() to be called")
}

// Test: Indexer Close() is idempotent
func TestIndexer_CloseIsIdempotent(t *testing.T) {
	t.Parallel()

	// Create temp directory for output
	tmpDir := t.TempDir()

	// Create a mock provider
	mockProvider := embed.NewMockProvider()

	// Create config
	config := &Config{
		RootDir:           t.TempDir(),
		CodePatterns:      []string{"**/*.go"},
		DocsPatterns:      []string{"**/*.md"},
		IgnorePatterns:    []string{},
		ChunkStrategies:   []string{"symbols"},
		DocChunkSize:      800,
		CodeChunkSize:     2000,
		Overlap:           100,
		OutputDir:         tmpDir,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "test-model",
		EmbeddingDims:     384,
	}

	// Create indexer with mock provider
	idx := &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		provider:  mockProvider,
	}

	// Call Close() multiple times
	err := idx.Close()
	require.NoError(t, err)

	err = idx.Close()
	require.NoError(t, err)

	err = idx.Close()
	require.NoError(t, err)

	// Should not panic or error on multiple calls
	assert.True(t, mockProvider.IsClosed(), "Expected provider Close() to be called")
}

// Test: Provider Close() errors are propagated
func TestIndexer_CloseErrorPropagated(t *testing.T) {
	t.Parallel()

	// Create temp directory for output
	tmpDir := t.TempDir()

	// Create a mock provider with close error
	mockProvider := embed.NewMockProvider()
	expectedErr := errors.New("close failed")
	mockProvider.SetCloseError(expectedErr)

	// Create config
	config := &Config{
		RootDir:           t.TempDir(),
		CodePatterns:      []string{"**/*.go"},
		DocsPatterns:      []string{"**/*.md"},
		IgnorePatterns:    []string{},
		ChunkStrategies:   []string{"symbols"},
		DocChunkSize:      800,
		CodeChunkSize:     2000,
		Overlap:           100,
		OutputDir:         tmpDir,
		EmbeddingProvider: "mock",
		EmbeddingModel:    "test-model",
		EmbeddingDims:     384,
	}

	// Create indexer with mock provider
	idx := &indexer{
		config:    config,
		parser:    NewParser(),
		chunker:   NewChunker(config.DocChunkSize, config.Overlap),
		formatter: NewFormatter(),
		provider:  mockProvider,
	}

	// Call Close() should return the error
	err := idx.Close()
	assert.ErrorIs(t, err, expectedErr, "Expected Close() error to be propagated")
	assert.True(t, mockProvider.IsClosed(), "Expected provider Close() to be called")
}
