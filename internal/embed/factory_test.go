package embed

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for NewProvider():
// - Creates local provider when config.Provider is "local" or empty
// - Uses explicit binary path when provided in config
// - Auto-downloads binary when path not provided and binary missing
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

// TestNewProvider_LocalWithAutoDownload verifies auto-download behavior
func TestNewProvider_LocalWithAutoDownload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping network-dependent test in short mode")
	}

	// Note: Not parallel because we modify HOME environment variable

	// Create temp directory to simulate home directory
	tmpHome := t.TempDir()

	// Set HOME for this test
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	require.NoError(t, os.Setenv("HOME", tmpHome))

	// Create pre-existing fake binary to simulate already-downloaded state
	// (avoids actual network call in test)
	binDir := filepath.Join(tmpHome, ".cortex", "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))

	binaryPath := filepath.Join(binDir, "cortex-embed")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	require.NoError(t, os.WriteFile(binaryPath, []byte("fake binary"), 0755))

	config := Config{
		Provider: "local",
		// BinaryPath intentionally empty to trigger auto-download path
		// But binary already exists, so it returns existing path
	}

	provider, err := NewProvider(config)
	require.NoError(t, err)
	assert.NotNil(t, provider)

	// Should use the pre-existing binary (EnsureBinaryInstalled found it)
	assert.Equal(t, 384, provider.Dimensions())
}

// TestNewProvider_DefaultsToLocal verifies empty provider defaults to local
func TestNewProvider_DefaultsToLocal(t *testing.T) {
	// Note: Not parallel because we modify HOME environment variable

	// Create temp directory to simulate home directory
	tmpHome := t.TempDir()

	// Set HOME for this test
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	require.NoError(t, os.Setenv("HOME", tmpHome))

	// Create pre-existing fake binary
	binDir := filepath.Join(tmpHome, ".cortex", "bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))

	binaryPath := filepath.Join(binDir, "cortex-embed")
	if runtime.GOOS == "windows" {
		binaryPath += ".exe"
	}
	require.NoError(t, os.WriteFile(binaryPath, []byte("fake binary"), 0755))

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

// TestNewProvider_DownloadFailure verifies error handling when download fails
func TestNewProvider_DownloadFailure(t *testing.T) {
	// Note: Not parallel because we modify HOME environment variable

	// Create temp directory with no binary and no way to download
	tmpHome := t.TempDir()

	// Set HOME for this test
	oldHome := os.Getenv("HOME")
	t.Cleanup(func() {
		_ = os.Setenv("HOME", oldHome)
	})
	require.NoError(t, os.Setenv("HOME", tmpHome))

	// Binary doesn't exist and can't be downloaded (network will fail in test)
	config := Config{
		Provider: "local",
		// BinaryPath empty - will try to download
	}

	provider, err := NewProvider(config)

	// Should fail with download error
	if err != nil {
		assert.Nil(t, provider)
		assert.Contains(t, err.Error(), "failed to ensure cortex-embed is installed")
	} else {
		// If download somehow succeeded (release exists), verify provider works
		require.NotNil(t, provider)
		assert.Equal(t, 384, provider.Dimensions())
	}
}
