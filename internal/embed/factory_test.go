package embed

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for NewProvider():
// - Creates local provider when config.Provider is "local" or empty
// - Uses explicit binary path when provided in config
// - Returns error when binary path is empty (no auto-download)
// - Creates mock provider when config.Provider is "mock"
// - Returns error for unsupported provider types

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

// TestNewProvider_LocalWithExplicitPath verifies local provider with explicit binary path
func TestNewProvider_LocalWithExplicitPath(t *testing.T) {
	t.Parallel()

	// Create a fake binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "cortex-embed")
	require.NoError(t, os.WriteFile(binaryPath, []byte("fake binary"), 0755))

	config := Config{
		Provider:   "local",
		BinaryPath: binaryPath,
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)

	// Verify dimensions
	assert.Equal(t, 384, provider.Dimensions())
}

// TestNewProvider_EmptyBinaryPath verifies error when binary path is empty
func TestNewProvider_EmptyBinaryPath(t *testing.T) {
	t.Parallel()

	config := Config{
		Provider: "local",
		// BinaryPath intentionally empty - should now error
	}

	provider, err := NewProvider(config)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Contains(t, err.Error(), "embedding binary path not specified")
}

// TestNewProvider_DefaultsToLocal verifies empty provider defaults to local with explicit binary
func TestNewProvider_DefaultsToLocal(t *testing.T) {
	t.Parallel()

	// Create a fake binary
	tmpDir := t.TempDir()
	binaryPath := filepath.Join(tmpDir, "cortex-embed")
	require.NoError(t, os.WriteFile(binaryPath, []byte("fake binary"), 0755))

	config := Config{
		Provider:   "", // Empty string should default to local
		BinaryPath: binaryPath,
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

// Note: TestNewProvider_DownloadFailure has been removed.
// Download failure testing is now handled by TestEnsureBinaryInstalled_DownloadFailure
// in downloader_test.go with proper mocking to avoid actual network calls.
