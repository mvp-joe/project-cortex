package config

import (
	"fmt"

	"github.com/mvp-joe/project-cortex/internal/embed"
)

// Config represents the complete cortex configuration.
// It can be loaded from .cortex/config.yml with environment variable overrides.
type Config struct {
	Embedding EmbeddingConfig `yaml:"embedding" mapstructure:"embedding"`
	Paths     PathsConfig     `yaml:"paths" mapstructure:"paths"`
	Chunking  ChunkingConfig  `yaml:"chunking" mapstructure:"chunking"`
	Storage   StorageConfig   `yaml:"storage" mapstructure:"storage"`
}

// EmbeddingConfig configures the embedding provider.
type EmbeddingConfig struct {
	Provider   string `yaml:"provider" mapstructure:"provider"`     // "local" or "openai"
	Model      string `yaml:"model" mapstructure:"model"`           // e.g., "BAAI/bge-small-en-v1.5"
	Dimensions int    `yaml:"dimensions" mapstructure:"dimensions"` // embedding vector dimensions
	Endpoint   string `yaml:"endpoint" mapstructure:"endpoint"`     // embedding service endpoint URL
}

// PathsConfig defines which files to index and which to ignore.
type PathsConfig struct {
	Code   []string `yaml:"code" mapstructure:"code"`     // glob patterns for code files
	Docs   []string `yaml:"docs" mapstructure:"docs"`     // glob patterns for documentation
	Ignore []string `yaml:"ignore" mapstructure:"ignore"` // glob patterns to ignore
}

// ChunkingConfig defines how content is chunked for indexing.
type ChunkingConfig struct {
	Strategies    []string `yaml:"strategies" mapstructure:"strategies"`           // e.g., ["symbols", "definitions", "data"]
	DocChunkSize  int      `yaml:"doc_chunk_size" mapstructure:"doc_chunk_size"`   // max tokens per doc chunk
	CodeChunkSize int      `yaml:"code_chunk_size" mapstructure:"code_chunk_size"` // max characters per code chunk
	Overlap       int      `yaml:"overlap" mapstructure:"overlap"`                 // token overlap between chunks
}

// StorageConfig defines cache and storage behavior.
// Note: SQLite is now the only supported storage backend.
type StorageConfig struct {
	CacheLocation      string  `yaml:"cache_location" mapstructure:"cache_location"`             // Override default ~/.cortex/cache
	BranchCacheEnabled bool    `yaml:"branch_cache_enabled" mapstructure:"branch_cache_enabled"` // Enable branch optimization
	CacheMaxAgeDays    int     `yaml:"cache_max_age_days" mapstructure:"cache_max_age_days"`     // Delete branches older than this
	CacheMaxSizeMB     float64 `yaml:"cache_max_size_mb" mapstructure:"cache_max_size_mb"`       // Max cache size per project
}

// Default returns a configuration with sensible defaults.
func Default() *Config {
	return &Config{
		Embedding: EmbeddingConfig{
			Provider:   "local",
			Model:      "BAAI/bge-small-en-v1.5",
			Dimensions: 384,
			Endpoint:   fmt.Sprintf("http://%s:%d/embed", embed.DefaultEmbedServerHost, embed.DefaultEmbedServerPort),
		},
		Paths: PathsConfig{
			Code: []string{
				"**/*.go",
				"**/*.ts",
				"**/*.tsx",
				"**/*.js",
				"**/*.jsx",
				"**/*.py",
				"**/*.rs",
				"**/*.c",
				"**/*.cpp",
				"**/*.cc",
				"**/*.h",
				"**/*.hpp",
				"**/*.php",
				"**/*.rb",
				"**/*.java",
			},
			Docs: []string{
				"**/*.md",
				"**/*.rst",
			},
			Ignore: []string{
				"node_modules/**",
				"vendor/**",
				".git/**",
				"dist/**",
				"build/**",
				"target/**",
				"__pycache__/**",
				"*.test",
				"*.pyc",
			},
		},
		Chunking: ChunkingConfig{
			Strategies:    []string{"symbols", "definitions", "data"},
			DocChunkSize:  800,
			CodeChunkSize: 2000,
			Overlap:       100,
		},
		Storage: StorageConfig{
			CacheLocation:      "", // Empty means use default ~/.cortex/cache
			BranchCacheEnabled: true,
			CacheMaxAgeDays:    30,
			CacheMaxSizeMB:     500,
		},
	}
}
