package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"
)

// Loader provides configuration loading capabilities.
type Loader interface {
	// Load loads configuration from file and environment variables.
	// Priority: defaults → config file → environment variables (env wins)
	Load() (*Config, error)
}

type loader struct {
	rootDir string
}

// NewLoader creates a new configuration loader for the given root directory.
func NewLoader(rootDir string) Loader {
	return &loader{
		rootDir: rootDir,
	}
}

// Load loads configuration with the following priority (highest to lowest):
// 1. Environment variables (CORTEX_*)
// 2. Config file (.cortex/config.yml or .cortex/config.yaml)
// 3. Default values
func (l *loader) Load() (*Config, error) {
	// Configure viper
	v := viper.New()

	// Set up config file search
	configDir := filepath.Join(l.rootDir, ".cortex")
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(configDir)

	// Enable environment variable overrides
	v.SetEnvPrefix("CORTEX")
	v.AutomaticEnv()
	// Replace . with _ in env var names (e.g., CORTEX_EMBEDDING_PROVIDER)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Bind environment variables to config keys
	// Embedding configuration
	v.BindEnv("embedding.provider")
	v.BindEnv("embedding.model")
	v.BindEnv("embedding.dimensions")
	v.BindEnv("embedding.endpoint")

	// Chunking configuration
	v.BindEnv("chunking.doc_chunk_size")
	v.BindEnv("chunking.code_chunk_size")
	v.BindEnv("chunking.overlap")

	// Storage configuration
	v.BindEnv("storage.backend")
	v.BindEnv("storage.cache_location")
	v.BindEnv("storage.branch_cache_enabled")
	v.BindEnv("storage.cache_max_age_days")
	v.BindEnv("storage.cache_max_size_mb")

	// Set defaults in viper
	setDefaults(v)

	// Try to read config file
	if err := v.ReadInConfig(); err != nil {
		// Config file not found is acceptable - we'll use defaults + env vars
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			// Some other error occurred while reading the config file
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	// Unmarshal into config struct
	cfg := &Config{}
	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate the configuration
	if err := Validate(cfg); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return cfg, nil
}

// setDefaults configures viper with default values.
func setDefaults(v *viper.Viper) {
	defaults := Default()

	// Embedding defaults
	v.SetDefault("embedding.provider", defaults.Embedding.Provider)
	v.SetDefault("embedding.model", defaults.Embedding.Model)
	v.SetDefault("embedding.dimensions", defaults.Embedding.Dimensions)
	v.SetDefault("embedding.endpoint", defaults.Embedding.Endpoint)

	// Paths defaults
	v.SetDefault("paths.code", defaults.Paths.Code)
	v.SetDefault("paths.docs", defaults.Paths.Docs)
	v.SetDefault("paths.ignore", defaults.Paths.Ignore)

	// Chunking defaults
	v.SetDefault("chunking.strategies", defaults.Chunking.Strategies)
	v.SetDefault("chunking.doc_chunk_size", defaults.Chunking.DocChunkSize)
	v.SetDefault("chunking.code_chunk_size", defaults.Chunking.CodeChunkSize)
	v.SetDefault("chunking.overlap", defaults.Chunking.Overlap)

	// Storage defaults (SQLite is the only backend now)
	v.SetDefault("storage.cache_location", defaults.Storage.CacheLocation)
	v.SetDefault("storage.branch_cache_enabled", defaults.Storage.BranchCacheEnabled)
	v.SetDefault("storage.cache_max_age_days", defaults.Storage.CacheMaxAgeDays)
	v.SetDefault("storage.cache_max_size_mb", defaults.Storage.CacheMaxSizeMB)
}

// LoadConfig is a convenience function that creates a loader and loads config.
// It uses the current working directory as the root.
func LoadConfig() (*Config, error) {
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}
	return NewLoader(wd).Load()
}

// LoadConfigFromDir loads configuration from a specific directory.
func LoadConfigFromDir(rootDir string) (*Config, error) {
	return NewLoader(rootDir).Load()
}
