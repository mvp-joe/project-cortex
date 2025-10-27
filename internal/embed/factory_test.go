package embed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for NewProvider():
// - Creates local provider when config.Provider is "local" or empty
// - Creates mock provider when config.Provider is "mock"
// - Returns error for unsupported provider types
// - Provider must be initialized via Initialize() before use
// - Binary installation is handled in Initialize(), not NewProvider()

// TestNewProvider_MockProvider verifies mock provider creation
func TestNewProvider_MockProvider(t *testing.T) {
	t.Parallel()

	config := Config{
		Provider: "mock",
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)

	// Verify it's a mock provider
	assert.Equal(t, 384, provider.Dimensions())

	// Should be able to call Close
	err = provider.Close()
	assert.NoError(t, err)
}

// TestNewProvider_LocalProvider verifies local provider creation
func TestNewProvider_LocalProvider(t *testing.T) {
	t.Parallel()

	config := Config{
		Provider: "local",
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)

	// Verify dimensions (before initialization)
	assert.Equal(t, 384, provider.Dimensions())

	// Note: We don't call Initialize() here because it would try to download
	// and start the actual binary. That's tested in integration tests.
}

// TestNewProvider_DefaultsToLocal verifies empty provider defaults to local
func TestNewProvider_DefaultsToLocal(t *testing.T) {
	t.Parallel()

	config := Config{
		Provider: "", // Empty string should default to local
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)
	assert.Equal(t, 384, provider.Dimensions())
}

// TestNewProvider_UnsupportedProvider verifies error handling for unsupported providers
func TestNewProvider_UnsupportedProvider(t *testing.T) {
	t.Parallel()

	config := Config{
		Provider: "unsupported-provider",
	}

	provider, err := NewProvider(config)
	assert.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "unsupported embedding provider")
}

// TestProviderInitialize_MockProvider verifies Initialize() works with mock provider
func TestProviderInitialize_MockProvider(t *testing.T) {
	t.Parallel()

	config := Config{
		Provider: "mock",
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)

	// Initialize should succeed immediately (no-op for mock)
	ctx := context.Background()
	err = provider.Initialize(ctx)
	require.NoError(t, err)

	// Should be able to use provider after initialization
	embeddings, err := provider.Embed(ctx, []string{"test"}, EmbedModeQuery)
	require.NoError(t, err)
	assert.Len(t, embeddings, 1)
	assert.Len(t, embeddings[0], 384)
}

// TestProviderInitialize_Idempotent verifies Initialize() can be called multiple times
func TestProviderInitialize_Idempotent(t *testing.T) {
	t.Parallel()

	config := Config{
		Provider: "mock",
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)

	ctx := context.Background()

	// First initialization
	err = provider.Initialize(ctx)
	require.NoError(t, err)

	// Second initialization should also succeed
	err = provider.Initialize(ctx)
	require.NoError(t, err)
}

// Note: TestNewProvider_DownloadFailure has been removed.
// Download failure testing is now handled by TestEnsureBinaryInstalled_DownloadFailure
// in downloader_test.go with proper mocking to avoid actual network calls.
