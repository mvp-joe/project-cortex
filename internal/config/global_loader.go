package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// LoadGlobalConfig loads global configuration from ~/.cortex/config.yml.
// Returns default values if file doesn't exist (not an error).
// Environment variables override file values (CORTEX_* prefix).
func LoadGlobalConfig() (*GlobalConfig, error) {
	v := viper.New()

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}
	cortexDir := filepath.Join(home, ".cortex")

	// Look for ~/.cortex/config.yml (NOT project .cortex/config.yml)
	v.SetConfigName("config")
	v.SetConfigType("yml")
	v.AddConfigPath(cortexDir)

	// Environment variable support (same pattern as project config)
	v.SetEnvPrefix("CORTEX")
	v.AutomaticEnv()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind environment variables
	bindGlobalEnvVars(v)

	// Set defaults
	setGlobalDefaults(v, cortexDir)

	// Read config (not an error if file doesn't exist)
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	cfg := &GlobalConfig{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return cfg, nil
}

// bindGlobalEnvVars binds all environment variables for global config.
func bindGlobalEnvVars(v *viper.Viper) {
	// Indexer daemon configuration
	v.BindEnv("indexer_daemon.socket_path")
	v.BindEnv("indexer_daemon.startup_timeout")

	// Embed daemon configuration
	v.BindEnv("embed_daemon.socket_path")
	v.BindEnv("embed_daemon.idle_timeout")
	v.BindEnv("embed_daemon.lib_dir")
	v.BindEnv("embed_daemon.model_dir")

	// Cache configuration
	v.BindEnv("cache.base_dir")
}

// setGlobalDefaults configures viper with default values for global config.
func setGlobalDefaults(v *viper.Viper, cortexDir string) {
	// Indexer daemon defaults
	v.SetDefault("indexer_daemon.socket_path", filepath.Join(cortexDir, "indexer.sock"))
	v.SetDefault("indexer_daemon.startup_timeout", 30)

	// Embed daemon defaults
	v.SetDefault("embed_daemon.socket_path", filepath.Join(cortexDir, "embed.sock"))
	v.SetDefault("embed_daemon.idle_timeout", 600) // 10 minutes
	v.SetDefault("embed_daemon.lib_dir", filepath.Join(cortexDir, "lib"))
	v.SetDefault("embed_daemon.model_dir", filepath.Join(cortexDir, "models"))

	// Cache defaults
	v.SetDefault("cache.base_dir", filepath.Join(cortexDir, "cache"))
}
