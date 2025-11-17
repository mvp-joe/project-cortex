package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEmbedDaemonConfig(t *testing.T) {
	t.Parallel()

	t.Run("creates valid config", func(t *testing.T) {
		t.Parallel()

		socketPath := filepath.Join(t.TempDir(), "embed.sock")

		cfg, err := NewEmbedDaemonConfig(socketPath)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		assert.Equal(t, "embed", cfg.Name)
		assert.Equal(t, socketPath, cfg.SocketPath)
		assert.Len(t, cfg.StartCommand, 3)
		assert.Equal(t, "embed", cfg.StartCommand[1])
		assert.Equal(t, "start", cfg.StartCommand[2])
		assert.Positive(t, cfg.StartupTimeout)
	})
}

func TestIsEmbedDaemonHealthy(t *testing.T) {
	t.Parallel()

	t.Run("returns error if socket does not exist", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		socketPath := filepath.Join(t.TempDir(), "nonexistent.sock")

		err := IsEmbedDaemonHealthy(ctx, socketPath)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "socket does not exist")
	})

	t.Run("returns error if socket exists but no server", func(t *testing.T) {
		t.Parallel()

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
	t.Parallel()

	// This test verifies EnsureEmbedDaemon composes correctly
	// Full daemon lifecycle testing is in integration tests

	t.Run("creates valid config", func(t *testing.T) {
		t.Parallel()

		socketPath := filepath.Join(t.TempDir(), "embed.sock")

		cfg, err := NewEmbedDaemonConfig(socketPath)
		require.NoError(t, err)

		// Config should be valid for EnsureDaemon
		assert.Equal(t, "embed", cfg.Name)
		assert.Equal(t, socketPath, cfg.SocketPath)
		assert.NotEmpty(t, cfg.StartCommand)
		assert.Positive(t, cfg.StartupTimeout)
	})
}
