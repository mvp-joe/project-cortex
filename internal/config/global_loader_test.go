package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Global Config Loader:
// - LoadGlobalConfig() returns defaults when file doesn't exist (not an error)
// - LoadGlobalConfig() loads from ~/.cortex/config.yml when present
// - LoadGlobalConfig() environment variables override YAML values
// - LoadGlobalConfig() returns error for malformed YAML

func TestLoadGlobalConfig_MissingFile(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Returns defaults without error when config file doesn't exist
	// Setup: Use temporary home directory with no config file
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cfg, err := LoadGlobalConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify defaults (paths should be under ~/.cortex)
	cortexDir := filepath.Join(tempHome, ".cortex")
	assert.Equal(t, filepath.Join(cortexDir, "indexer.sock"), cfg.IndexerDaemon.SocketPath)
	assert.Equal(t, 30, cfg.IndexerDaemon.StartupTimeout)
	assert.Equal(t, filepath.Join(cortexDir, "embed.sock"), cfg.EmbedDaemon.SocketPath)
	assert.Equal(t, 600, cfg.EmbedDaemon.IdleTimeout)
	assert.Equal(t, filepath.Join(cortexDir, "models"), cfg.EmbedDaemon.ModelDir)
	assert.Equal(t, filepath.Join(cortexDir, "cache"), cfg.Cache.BaseDir)
}

func TestLoadGlobalConfig_WithFile(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Loads from ~/.cortex/config.yml when present
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cortexDir := filepath.Join(tempHome, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	configContent := `
indexer_daemon:
  socket_path: /custom/indexer.sock
  startup_timeout: 60

embed_daemon:
  socket_path: /custom/embed.sock
  idle_timeout: 1200
  model_dir: /custom/onnx

cache:
  base_dir: /custom/cache
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cfg, err := LoadGlobalConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify loaded values
	assert.Equal(t, "/custom/indexer.sock", cfg.IndexerDaemon.SocketPath)
	assert.Equal(t, 60, cfg.IndexerDaemon.StartupTimeout)
	assert.Equal(t, "/custom/embed.sock", cfg.EmbedDaemon.SocketPath)
	assert.Equal(t, 1200, cfg.EmbedDaemon.IdleTimeout)
	assert.Equal(t, "/custom/onnx", cfg.EmbedDaemon.ModelDir)
	assert.Equal(t, "/custom/cache", cfg.Cache.BaseDir)
}

func TestLoadGlobalConfig_EnvOverrides(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Environment variables override YAML values
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cortexDir := filepath.Join(tempHome, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	configContent := `
indexer_daemon:
  socket_path: /file/indexer.sock
  startup_timeout: 60

embed_daemon:
  socket_path: /file/embed.sock
  idle_timeout: 1200
  model_dir: /file/onnx

cache:
  base_dir: /file/cache
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Set environment variables (these should override file values)
	t.Setenv("CORTEX_INDEXER_DAEMON_SOCKET_PATH", "/env/indexer.sock")
	t.Setenv("CORTEX_INDEXER_DAEMON_STARTUP_TIMEOUT", "120")
	t.Setenv("CORTEX_EMBED_DAEMON_SOCKET_PATH", "/env/embed.sock")
	t.Setenv("CORTEX_EMBED_DAEMON_IDLE_TIMEOUT", "300")
	t.Setenv("CORTEX_EMBED_DAEMON_MODEL_DIR", "/env/onnx")
	t.Setenv("CORTEX_CACHE_BASE_DIR", "/env/cache")

	cfg, err := LoadGlobalConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Environment variables should win
	assert.Equal(t, "/env/indexer.sock", cfg.IndexerDaemon.SocketPath)
	assert.Equal(t, 120, cfg.IndexerDaemon.StartupTimeout)
	assert.Equal(t, "/env/embed.sock", cfg.EmbedDaemon.SocketPath)
	assert.Equal(t, 300, cfg.EmbedDaemon.IdleTimeout)
	assert.Equal(t, "/env/onnx", cfg.EmbedDaemon.ModelDir)
	assert.Equal(t, "/env/cache", cfg.Cache.BaseDir)
}

func TestLoadGlobalConfig_InvalidYAML(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Returns error for malformed YAML
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cortexDir := filepath.Join(tempHome, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	malformedContent := `
indexer_daemon:
  socket_path: /path/to/socket
  startup_timeout: "not-a-number
  unclosed_quote_above
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(malformedContent), 0644))

	cfg, err := LoadGlobalConfig()

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "failed to")
}

func TestLoadGlobalConfig_PartialConfig(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Partial config file merges with defaults
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cortexDir := filepath.Join(tempHome, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	// Only override indexer daemon settings, rest should come from defaults
	configContent := `
indexer_daemon:
  startup_timeout: 90
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	cfg, err := LoadGlobalConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Should have custom indexer timeout
	assert.Equal(t, 90, cfg.IndexerDaemon.StartupTimeout)

	// Should have default socket path
	assert.Equal(t, filepath.Join(cortexDir, "indexer.sock"), cfg.IndexerDaemon.SocketPath)

	// Should have default embed daemon config
	assert.Equal(t, filepath.Join(cortexDir, "embed.sock"), cfg.EmbedDaemon.SocketPath)
	assert.Equal(t, 600, cfg.EmbedDaemon.IdleTimeout)
	assert.Equal(t, filepath.Join(cortexDir, "models"), cfg.EmbedDaemon.ModelDir)
	assert.Equal(t, filepath.Join(cortexDir, "cache"), cfg.Cache.BaseDir)
}

func TestLoadGlobalConfig_EnvOverridesDefaults(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Environment variables override defaults when no config file
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	cortexDir := filepath.Join(tempHome, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	// No config file, only env vars
	t.Setenv("CORTEX_INDEXER_DAEMON_STARTUP_TIMEOUT", "45")
	t.Setenv("CORTEX_EMBED_DAEMON_IDLE_TIMEOUT", "900")
	t.Setenv("CORTEX_CACHE_BASE_DIR", "/custom/cache")

	cfg, err := LoadGlobalConfig()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Environment variables should override defaults
	assert.Equal(t, 45, cfg.IndexerDaemon.StartupTimeout)
	assert.Equal(t, 900, cfg.EmbedDaemon.IdleTimeout)
	assert.Equal(t, "/custom/cache", cfg.Cache.BaseDir)

	// Non-overridden values should be defaults
	assert.Equal(t, filepath.Join(cortexDir, "indexer.sock"), cfg.IndexerDaemon.SocketPath)
	assert.Equal(t, filepath.Join(cortexDir, "embed.sock"), cfg.EmbedDaemon.SocketPath)
	assert.Equal(t, filepath.Join(cortexDir, "models"), cfg.EmbedDaemon.ModelDir)
}
