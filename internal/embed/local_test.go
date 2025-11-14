//go:build integration

package embed

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for localProvider with ConnectRPC:
// 1. Initialize - Start daemon, initialize ONNX model
// 2. Embed - Basic embedding functionality
// 3. Dimensions - Returns 768 for ONNX model
// 4. Resurrection - Auto-restart crashed daemon on connection error
// 5. Close - No-op (daemon manages lifecycle)

// TestLocalProvider_NewProvider verifies provider construction.
func TestLocalProvider_NewProvider(t *testing.T) {
	t.Parallel()

	provider, err := newLocalProvider()
	require.NoError(t, err, "Failed to create local provider")
	require.NotNil(t, provider)
	assert.NotEmpty(t, provider.socketPath)
	assert.NotNil(t, provider.client)
	assert.NotNil(t, provider.daemonConfig)
	assert.False(t, provider.initialized)
}

// TestLocalProvider_Initialize verifies daemon auto-start and model initialization.
func TestLocalProvider_Initialize(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	provider, err := newLocalProvider()
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Initialize should start daemon and load model
	err = provider.Initialize(ctx)
	require.NoError(t, err, "Initialize failed")
	assert.True(t, provider.initialized)

	// Second Initialize should be idempotent
	err = provider.Initialize(ctx)
	assert.NoError(t, err, "Initialize should be idempotent")
}

// TestLocalProvider_Embed verifies basic embedding functionality.
func TestLocalProvider_Embed(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	provider, err := newLocalProvider()
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Initialize provider
	err = provider.Initialize(ctx)
	require.NoError(t, err)

	// Test embedding single text
	texts := []string{"Hello, world!"}
	embeddings, err := provider.Embed(ctx, texts, EmbedModeQuery)
	require.NoError(t, err, "Embed failed")
	require.Len(t, embeddings, 1, "Should return one embedding")
	require.Len(t, embeddings[0], 768, "Embedding should be 768-dimensional")

	// Verify embedding is normalized (approximately unit length)
	sum := float32(0)
	for _, val := range embeddings[0] {
		sum += val * val
	}
	assert.InDelta(t, 1.0, sum, 0.01, "Embedding should be approximately unit length")
}

// TestLocalProvider_EmbedBatch verifies batch embedding.
func TestLocalProvider_EmbedBatch(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	provider, err := newLocalProvider()
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = provider.Initialize(ctx)
	require.NoError(t, err)

	// Test batch embedding
	texts := []string{
		"The quick brown fox",
		"jumps over the lazy dog",
		"Machine learning is fascinating",
	}
	embeddings, err := provider.Embed(ctx, texts, EmbedModePassage)
	require.NoError(t, err, "Batch embed failed")
	require.Len(t, embeddings, 3, "Should return three embeddings")

	for i, emb := range embeddings {
		assert.Len(t, emb, 768, "Embedding %d should be 768-dimensional", i)
	}
}

// TestLocalProvider_EmbedNotInitialized verifies error when not initialized.
func TestLocalProvider_EmbedNotInitialized(t *testing.T) {
	t.Parallel()

	provider, err := newLocalProvider()
	require.NoError(t, err)

	ctx := context.Background()
	_, err = provider.Embed(ctx, []string{"test"}, EmbedModeQuery)
	assert.Error(t, err, "Should error when not initialized")
	assert.Contains(t, err.Error(), "not initialized", "Error should mention initialization")
}

// TestLocalProvider_Dimensions verifies dimensions match ONNX model (768).
func TestLocalProvider_Dimensions(t *testing.T) {
	t.Parallel()

	provider, err := newLocalProvider()
	require.NoError(t, err)

	assert.Equal(t, 768, provider.Dimensions(), "Should return 768 for ONNX model")
}

// TestLocalProvider_Close verifies Close is a no-op.
func TestLocalProvider_Close(t *testing.T) {
	t.Parallel()

	provider, err := newLocalProvider()
	require.NoError(t, err)

	err = provider.Close()
	assert.NoError(t, err, "Close should be no-op")
}

// TestLocalProvider_EmbedModes verifies both query and passage modes work.
func TestLocalProvider_EmbedModes(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	provider, err := newLocalProvider()
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err = provider.Initialize(ctx)
	require.NoError(t, err)

	text := []string{"semantic search query"}

	// Test query mode
	queryEmb, err := provider.Embed(ctx, text, EmbedModeQuery)
	require.NoError(t, err)
	require.Len(t, queryEmb, 1)
	require.Len(t, queryEmb[0], 768)

	// Test passage mode
	passageEmb, err := provider.Embed(ctx, text, EmbedModePassage)
	require.NoError(t, err)
	require.Len(t, passageEmb, 1)
	require.Len(t, passageEmb[0], 768)

	// Currently both modes use same model, so embeddings should be identical
	// (Future versions may differentiate)
	for i := range queryEmb[0] {
		assert.Equal(t, queryEmb[0][i], passageEmb[0][i],
			"Currently both modes should produce identical embeddings")
	}
}

// TestLocalProvider_ConcurrentEmbeds verifies thread safety.
func TestLocalProvider_ConcurrentEmbeds(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	provider, err := newLocalProvider()
	require.NoError(t, err)
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	err = provider.Initialize(ctx)
	require.NoError(t, err)

	// Run multiple concurrent embeds
	const numConcurrent = 5
	done := make(chan error, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func(id int) {
			text := []string{"concurrent test"}
			embeddings, err := provider.Embed(ctx, text, EmbedModeQuery)
			if err != nil {
				done <- err
				return
			}
			if len(embeddings) != 1 || len(embeddings[0]) != 768 {
				done <- assert.AnError
				return
			}
			done <- nil
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < numConcurrent; i++ {
		err := <-done
		assert.NoError(t, err, "Concurrent embed %d failed", i)
	}
}

// TODO: TestLocalProvider_Resurrection - Test resurrection pattern
// This requires the ability to stop the daemon mid-test, which needs
// daemon management utilities. Will be implemented once daemon
// lifecycle management tools are available.
