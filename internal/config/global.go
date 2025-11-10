// Package config provides configuration loading for Project Cortex.
//
// It supports two distinct configuration scopes:
//
// 1. Global Configuration (~/.cortex/config.yml)
//   - Machine-wide daemon settings
//   - Socket paths, timeouts, model directories
//   - Shared cache location
//   - Loaded via LoadGlobalConfig()
//   - Controls daemon behavior across all projects
//
// 2. Project Configuration (.cortex/config.yml)
//   - Project-specific settings (existing functionality)
//   - Embedding model, dimensions, endpoint
//   - Path patterns, chunking strategies
//   - Loaded via Load() (existing loader)
//   - Isolated per project
//
// Configuration Hierarchy (highest to lowest priority):
//   1. Environment variables (CORTEX_*)
//   2. Global config (~/.cortex/config.yml)
//   3. Project config (.cortex/config.yml)
//   4. Built-in defaults
//
// Environment Variable Convention:
//   - Prefix: CORTEX_
//   - Nested fields: Use underscores (CORTEX_INDEXER_DAEMON_SOCKET_PATH)
//   - Automatic mapping via Viper's SetEnvKeyReplacer
//
// Example usage:
//
//	// Load global daemon config
//	globalCfg, err := config.LoadGlobalConfig()
//	if err != nil {
//	    return err
//	}
//
//	// Use daemon settings
//	socketPath := globalCfg.IndexerDaemon.SocketPath
//	timeout := time.Duration(globalCfg.IndexerDaemon.StartupTimeout) * time.Second
//
// See also:
//   - specs/2025-11-09_daemon-foundation.md for architecture details
//   - LoadGlobalConfig() for global config loading
//   - Load() for project config loading (existing)
package config

// GlobalConfig holds machine-wide daemon configuration.
// Loaded from ~/.cortex/config.yml (not project .cortex/config.yml).
//
// This configuration is separate from per-project settings and controls
// daemon behavior across all projects on the machine.
type GlobalConfig struct {
	IndexerDaemon IndexerDaemonConfig `yaml:"indexer_daemon" mapstructure:"indexer_daemon"`
	EmbedDaemon   EmbedDaemonConfig   `yaml:"embed_daemon" mapstructure:"embed_daemon"`
	Cache         GlobalCacheConfig   `yaml:"cache" mapstructure:"cache"`
}

// IndexerDaemonConfig holds indexer daemon settings.
type IndexerDaemonConfig struct {
	SocketPath     string `yaml:"socket_path" mapstructure:"socket_path"`           // Unix domain socket path
	StartupTimeout int    `yaml:"startup_timeout" mapstructure:"startup_timeout"` // Timeout in seconds for daemon startup
}

// EmbedDaemonConfig holds embedding server daemon settings.
type EmbedDaemonConfig struct {
	SocketPath  string `yaml:"socket_path" mapstructure:"socket_path"`     // Unix domain socket path
	IdleTimeout int    `yaml:"idle_timeout" mapstructure:"idle_timeout"`   // Idle timeout in seconds before shutdown
	ModelDir    string `yaml:"model_dir" mapstructure:"model_dir"`         // Directory for ONNX model files
}

// GlobalCacheConfig holds global cache settings.
type GlobalCacheConfig struct {
	BaseDir string `yaml:"base_dir" mapstructure:"base_dir"` // Base directory for cache (~/.cortex/cache)
}
