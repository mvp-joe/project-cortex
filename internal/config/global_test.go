package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test Plan: GlobalConfig struct validation
// - Verify struct can be created with all fields
// - Verify zero values are correct type
// - YAML unmarshaling is tested in global_loader_test.go via Viper

func TestGlobalConfig_StructFields(t *testing.T) {
	t.Parallel()

	// Test: Verify all fields can be set
	cfg := GlobalConfig{
		IndexerDaemon: IndexerDaemonConfig{
			SocketPath:     "/tmp/indexer.sock",
			StartupTimeout: 30,
		},
		EmbedDaemon: EmbedDaemonConfig{
			SocketPath:  "/tmp/embed.sock",
			IdleTimeout: 600,
			ModelDir:    "/tmp/models",
		},
		Cache: GlobalCacheConfig{
			BaseDir: "/tmp/cache",
		},
	}

	assert.Equal(t, "/tmp/indexer.sock", cfg.IndexerDaemon.SocketPath)
	assert.Equal(t, 30, cfg.IndexerDaemon.StartupTimeout)
	assert.Equal(t, "/tmp/embed.sock", cfg.EmbedDaemon.SocketPath)
	assert.Equal(t, 600, cfg.EmbedDaemon.IdleTimeout)
	assert.Equal(t, "/tmp/models", cfg.EmbedDaemon.ModelDir)
	assert.Equal(t, "/tmp/cache", cfg.Cache.BaseDir)
}

func TestGlobalConfig_ZeroValues(t *testing.T) {
	t.Parallel()

	cfg := GlobalConfig{}

	assert.Empty(t, cfg.IndexerDaemon.SocketPath)
	assert.Equal(t, 0, cfg.IndexerDaemon.StartupTimeout)
	assert.Empty(t, cfg.EmbedDaemon.SocketPath)
	assert.Equal(t, 0, cfg.EmbedDaemon.IdleTimeout)
	assert.Empty(t, cfg.EmbedDaemon.ModelDir)
	assert.Empty(t, cfg.Cache.BaseDir)
}

func TestIndexerDaemonConfig_StructFields(t *testing.T) {
	t.Parallel()

	cfg := IndexerDaemonConfig{
		SocketPath:     "/tmp/test.sock",
		StartupTimeout: 60,
	}

	assert.Equal(t, "/tmp/test.sock", cfg.SocketPath)
	assert.Equal(t, 60, cfg.StartupTimeout)
}

func TestEmbedDaemonConfig_StructFields(t *testing.T) {
	t.Parallel()

	cfg := EmbedDaemonConfig{
		SocketPath:  "/tmp/embed.sock",
		IdleTimeout: 300,
		ModelDir:    "/opt/models",
	}

	assert.Equal(t, "/tmp/embed.sock", cfg.SocketPath)
	assert.Equal(t, 300, cfg.IdleTimeout)
	assert.Equal(t, "/opt/models", cfg.ModelDir)
}

func TestGlobalCacheConfig_StructFields(t *testing.T) {
	t.Parallel()

	cfg := GlobalCacheConfig{
		BaseDir: "/var/cache/cortex",
	}

	assert.Equal(t, "/var/cache/cortex", cfg.BaseDir)
}
