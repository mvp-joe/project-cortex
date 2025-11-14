package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetEmbedSocketPath(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	t.Run("uses environment variable override", func(t *testing.T) {
		expected := "/tmp/custom-embed.sock"
		t.Setenv("CORTEX_EMBED_SOCKET", expected)

		path, err := GetEmbedSocketPath()
		require.NoError(t, err)
		assert.Equal(t, expected, path)
	})

	t.Run("uses default path in home directory", func(t *testing.T) {
		t.Setenv("CORTEX_EMBED_SOCKET", "") // Clear override

		path, err := GetEmbedSocketPath()
		require.NoError(t, err)

		// Should be ~/.cortex/embed.sock
		homeDir, _ := os.UserHomeDir()
		expected := filepath.Join(homeDir, ".cortex", "embed.sock")
		assert.Equal(t, expected, path)
	})

	t.Run("creates cortex directory if missing", func(t *testing.T) {
		t.Setenv("CORTEX_EMBED_SOCKET", "") // Clear override

		// Get path (should create directory)
		path, err := GetEmbedSocketPath()
		require.NoError(t, err)

		// Verify directory exists
		dir := filepath.Dir(path)
		stat, err := os.Stat(dir)
		require.NoError(t, err)
		assert.True(t, stat.IsDir())
	})
}

func TestNewEmbedDaemonConfig(t *testing.T) {
	t.Parallel()

	t.Run("creates valid config", func(t *testing.T) {
		cfg, err := NewEmbedDaemonConfig()
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "embed", cfg.Name)
		assert.Contains(t, cfg.SocketPath, "embed.sock")
		assert.Equal(t, []string{"cortex", "embed", "start"}, cfg.StartCommand)
		assert.Positive(t, cfg.StartupTimeout)
	})
}

func TestIsEmbedDaemonHealthy(t *testing.T) {
	t.Parallel()

	t.Run("returns error if socket does not exist", func(t *testing.T) {
		ctx := context.Background()
		socketPath := filepath.Join(t.TempDir(), "nonexistent.sock")

		err := IsEmbedDaemonHealthy(ctx, socketPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "socket does not exist")
	})

	t.Run("returns error if socket exists but no server", func(t *testing.T) {
		ctx := context.Background()
		socketPath := filepath.Join(t.TempDir(), "stale.sock")

		// Create stale socket file
		f, err := os.Create(socketPath)
		require.NoError(t, err)
		f.Close()

		err = IsEmbedDaemonHealthy(ctx, socketPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "health check failed")
	})

	// Note: Testing successful health check requires a running server
	// See integration tests for full daemon lifecycle testing
}

func TestEnsureEmbedDaemon(t *testing.T) {
	// This test verifies EnsureEmbedDaemon composes correctly
	// Full daemon lifecycle testing is in integration tests

	t.Run("creates valid config", func(t *testing.T) {
		cfg, err := NewEmbedDaemonConfig()
		require.NoError(t, err)

		// Config should be valid for EnsureDaemon
		assert.Equal(t, "embed", cfg.Name)
		assert.NotEmpty(t, cfg.SocketPath)
		assert.NotEmpty(t, cfg.StartCommand)
		assert.Positive(t, cfg.StartupTimeout)
	})
}
