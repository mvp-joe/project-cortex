package config

// Config represents the complete cortex configuration.
// It can be loaded from .cortex/config.yml with environment variable overrides.
type Config struct {
	Embedding EmbeddingConfig `yaml:"embedding" mapstructure:"embedding"`
	Paths     PathsConfig     `yaml:"paths" mapstructure:"paths"`
	Chunking  ChunkingConfig  `yaml:"chunking" mapstructure:"chunking"`
}

// EmbeddingConfig configures the embedding provider.
type EmbeddingConfig struct {
	Provider   string `yaml:"provider" mapstructure:"provider"`     // "local" or "openai"
	Model      string `yaml:"model" mapstructure:"model"`           // e.g., "BAAI/bge-small-en-v1.5"
	Dimensions int    `yaml:"dimensions" mapstructure:"dimensions"` // embedding vector dimensions
	Endpoint   string `yaml:"endpoint" mapstructure:"endpoint"`     // e.g., "http://localhost:8121/embed"
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

// Default returns a configuration with sensible defaults.
func Default() *Config {
	return &Config{
		Embedding: EmbeddingConfig{
			Provider:   "local",
			Model:      "BAAI/bge-small-en-v1.5",
			Dimensions: 384,
			Endpoint:   "http://localhost:8121/embed",
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
	}
}
