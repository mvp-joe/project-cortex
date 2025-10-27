package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Config System:
// - Default() returns valid configuration with all expected defaults
// - LoadConfig() uses defaults when no config file exists
// - LoadConfig() loads from .cortex/config.yml when present
// - LoadConfig() loads from .cortex/config.yaml when present
// - LoadConfig() merges config file with defaults
// - Environment variables override config file values
// - Environment variables override defaults when no config file exists
// - LoadConfig() returns error for malformed YAML
// - LoadConfig() returns error for invalid configuration values
// - Validate() accepts valid configuration
// - Validate() rejects invalid provider
// - Validate() rejects negative/zero dimensions
// - Validate() rejects empty model
// - Validate() rejects empty endpoint
// - Validate() rejects negative/zero chunk sizes
// - Validate() rejects negative overlap
// - Validate() rejects overlap >= doc_chunk_size
// - Validate() rejects empty strategies list
// - Validate() rejects unknown strategy names
// - Validate() returns multiple errors for multiple invalid fields

func TestDefault_ReturnsValidConfiguration(t *testing.T) {
	t.Parallel()

	// Test: Default() returns valid configuration
	cfg := Default()

	require.NotNil(t, cfg)

	// Verify embedding defaults
	assert.Equal(t, "local", cfg.Embedding.Provider)
	assert.Equal(t, "BAAI/bge-small-en-v1.5", cfg.Embedding.Model)
	assert.Equal(t, 384, cfg.Embedding.Dimensions)
	assert.Equal(t, fmt.Sprintf("http://%s:%d/embed", embed.DefaultEmbedServerHost, embed.DefaultEmbedServerPort), cfg.Embedding.Endpoint)

	// Verify chunking defaults
	assert.Equal(t, []string{"symbols", "definitions", "data"}, cfg.Chunking.Strategies)
	assert.Equal(t, 800, cfg.Chunking.DocChunkSize)
	assert.Equal(t, 2000, cfg.Chunking.CodeChunkSize)
	assert.Equal(t, 100, cfg.Chunking.Overlap)

	// Verify paths have reasonable defaults
	assert.NotEmpty(t, cfg.Paths.Code)
	assert.NotEmpty(t, cfg.Paths.Docs)
	assert.NotEmpty(t, cfg.Paths.Ignore)

	// Verify default config passes validation
	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestLoadConfig_UsesDefaultsWhenNoConfigFile(t *testing.T) {
	t.Parallel()

	// Test: Load from directory with no config file returns defaults
	tempDir := t.TempDir()

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Should match defaults
	expected := Default()
	assert.Equal(t, expected.Embedding.Provider, cfg.Embedding.Provider)
	assert.Equal(t, expected.Embedding.Model, cfg.Embedding.Model)
	assert.Equal(t, expected.Embedding.Dimensions, cfg.Embedding.Dimensions)
}

func TestLoadConfig_LoadsFromConfigYml(t *testing.T) {
	t.Parallel()

	// Test: Load from .cortex/config.yml
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	configContent := `
embedding:
  provider: openai
  model: text-embedding-3-small
  dimensions: 1536
  endpoint: https://api.openai.com/v1/embeddings

paths:
  code:
    - "**/*.go"
    - "**/*.py"
  docs:
    - "**/*.md"
  ignore:
    - "vendor/**"

chunking:
  strategies: ["symbols"]
  doc_chunk_size: 1000
  code_chunk_size: 3000
  overlap: 200
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify loaded values
	assert.Equal(t, "openai", cfg.Embedding.Provider)
	assert.Equal(t, "text-embedding-3-small", cfg.Embedding.Model)
	assert.Equal(t, 1536, cfg.Embedding.Dimensions)
	assert.Equal(t, "https://api.openai.com/v1/embeddings", cfg.Embedding.Endpoint)

	assert.Equal(t, []string{"**/*.go", "**/*.py"}, cfg.Paths.Code)
	assert.Equal(t, []string{"**/*.md"}, cfg.Paths.Docs)
	assert.Equal(t, []string{"vendor/**"}, cfg.Paths.Ignore)

	assert.Equal(t, []string{"symbols"}, cfg.Chunking.Strategies)
	assert.Equal(t, 1000, cfg.Chunking.DocChunkSize)
	assert.Equal(t, 3000, cfg.Chunking.CodeChunkSize)
	assert.Equal(t, 200, cfg.Chunking.Overlap)
}

func TestLoadConfig_LoadsFromConfigYaml(t *testing.T) {
	t.Parallel()

	// Test: Load from .cortex/config.yaml (alternative extension)
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	configContent := `
embedding:
  provider: local
  model: custom-model
  dimensions: 512
  endpoint: http://localhost:9000/embed
`

	configPath := filepath.Join(cortexDir, "config.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify loaded values
	assert.Equal(t, "local", cfg.Embedding.Provider)
	assert.Equal(t, "custom-model", cfg.Embedding.Model)
	assert.Equal(t, 512, cfg.Embedding.Dimensions)
	assert.Equal(t, "http://localhost:9000/embed", cfg.Embedding.Endpoint)
}

func TestLoadConfig_MergesConfigWithDefaults(t *testing.T) {
	t.Parallel()

	// Test: Partial config file merges with defaults
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	// Only override embedding provider, rest should come from defaults
	configContent := `
embedding:
  provider: openai
  model: custom-model
  dimensions: 1536
  endpoint: https://api.openai.com/v1
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	require.NoError(t, err)

	// Should have custom embedding config
	assert.Equal(t, "openai", cfg.Embedding.Provider)
	assert.Equal(t, "custom-model", cfg.Embedding.Model)

	// Should have default chunking config
	assert.Equal(t, 800, cfg.Chunking.DocChunkSize)
	assert.Equal(t, 2000, cfg.Chunking.CodeChunkSize)
}

func TestLoadConfig_EnvironmentVariablesOverrideConfigFile(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Environment variables take precedence over config file
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	configContent := `
embedding:
  provider: local
  model: file-model
  dimensions: 384
  endpoint: http://localhost:8121/embed
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0644))

	// Set environment variables
	t.Setenv("CORTEX_EMBEDDING_PROVIDER", "openai")
	t.Setenv("CORTEX_EMBEDDING_MODEL", "env-model")
	t.Setenv("CORTEX_EMBEDDING_DIMENSIONS", "1536")

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	require.NoError(t, err)

	// Environment variables should win
	assert.Equal(t, "openai", cfg.Embedding.Provider)
	assert.Equal(t, "env-model", cfg.Embedding.Model)
	assert.Equal(t, 1536, cfg.Embedding.Dimensions)

	// Endpoint not overridden, should come from config file
	assert.Equal(t, "http://localhost:8121/embed", cfg.Embedding.Endpoint)
}

func TestLoadConfig_EnvironmentVariablesOverrideDefaults(t *testing.T) {
	// Note: Cannot use t.Parallel() with t.Setenv()

	// Test: Environment variables override defaults when no config file
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	// Set environment variables
	t.Setenv("CORTEX_EMBEDDING_PROVIDER", "openai")
	t.Setenv("CORTEX_EMBEDDING_ENDPOINT", "https://custom.endpoint/embed")
	t.Setenv("CORTEX_CHUNKING_DOC_CHUNK_SIZE", "1500")

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	require.NoError(t, err)

	// Environment variables should override defaults
	assert.Equal(t, "openai", cfg.Embedding.Provider)
	assert.Equal(t, "https://custom.endpoint/embed", cfg.Embedding.Endpoint)
	assert.Equal(t, 1500, cfg.Chunking.DocChunkSize)

	// Non-overridden values should be defaults
	assert.Equal(t, "BAAI/bge-small-en-v1.5", cfg.Embedding.Model)
	assert.Equal(t, 2000, cfg.Chunking.CodeChunkSize)
}

func TestLoadConfig_ReturnsErrorForMalformedYaml(t *testing.T) {
	t.Parallel()

	// Test: Malformed YAML returns error
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	malformedContent := `
embedding:
  provider: local
  model: "unclosed quote
  dimensions: not-a-number
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(malformedContent), 0644))

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	assert.Error(t, err)
	assert.Nil(t, cfg)
}

func TestLoadConfig_ReturnsErrorForInvalidValues(t *testing.T) {
	t.Parallel()

	// Test: Invalid configuration values fail validation
	tempDir := t.TempDir()
	cortexDir := filepath.Join(tempDir, ".cortex")
	require.NoError(t, os.MkdirAll(cortexDir, 0755))

	invalidContent := `
embedding:
  provider: invalid-provider
  model: test-model
  dimensions: -10
  endpoint: http://localhost:8121
`

	configPath := filepath.Join(cortexDir, "config.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(invalidContent), 0644))

	loader := NewLoader(tempDir)
	cfg, err := loader.Load()

	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "invalid")
}

func TestValidate_AcceptsValidConfiguration(t *testing.T) {
	t.Parallel()

	// Test: Valid configuration passes validation
	cfg := &Config{
		Embedding: EmbeddingConfig{
			Provider:   "local",
			Model:      "test-model",
			Dimensions: 384,
			Endpoint:   "http://localhost:8121",
		},
		Paths: PathsConfig{
			Code:   []string{"**/*.go"},
			Docs:   []string{"**/*.md"},
			Ignore: []string{"node_modules/**"},
		},
		Chunking: ChunkingConfig{
			Strategies:    []string{"symbols", "definitions"},
			DocChunkSize:  800,
			CodeChunkSize: 2000,
			Overlap:       100,
		},
	}

	err := Validate(cfg)
	assert.NoError(t, err)
}

func TestValidate_RejectsInvalidProvider(t *testing.T) {
	t.Parallel()

	// Test: Invalid provider fails validation
	cfg := Default()
	cfg.Embedding.Provider = "unsupported"

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidProvider)
}

func TestValidate_RejectsNegativeDimensions(t *testing.T) {
	t.Parallel()

	// Test: Negative dimensions fails validation
	cfg := Default()
	cfg.Embedding.Dimensions = -10

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidDimensions)
}

func TestValidate_RejectsZeroDimensions(t *testing.T) {
	t.Parallel()

	// Test: Zero dimensions fails validation
	cfg := Default()
	cfg.Embedding.Dimensions = 0

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidDimensions)
}

func TestValidate_RejectsEmptyModel(t *testing.T) {
	t.Parallel()

	// Test: Empty model fails validation
	cfg := Default()
	cfg.Embedding.Model = ""

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyModel)
}

func TestValidate_RejectsEmptyEndpoint(t *testing.T) {
	t.Parallel()

	// Test: Empty endpoint fails validation
	cfg := Default()
	cfg.Embedding.Endpoint = ""

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyEndpoint)
}

func TestValidate_RejectsNegativeDocChunkSize(t *testing.T) {
	t.Parallel()

	// Test: Negative doc chunk size fails validation
	cfg := Default()
	cfg.Chunking.DocChunkSize = -100

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidChunkSize)
}

func TestValidate_RejectsZeroCodeChunkSize(t *testing.T) {
	t.Parallel()

	// Test: Zero code chunk size fails validation
	cfg := Default()
	cfg.Chunking.CodeChunkSize = 0

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidChunkSize)
}

func TestValidate_RejectsNegativeOverlap(t *testing.T) {
	t.Parallel()

	// Test: Negative overlap fails validation
	cfg := Default()
	cfg.Chunking.Overlap = -50

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOverlap)
}

func TestValidate_RejectsOverlapGreaterThanChunkSize(t *testing.T) {
	t.Parallel()

	// Test: Overlap >= doc_chunk_size fails validation
	cfg := Default()
	cfg.Chunking.Overlap = 1000
	cfg.Chunking.DocChunkSize = 800

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidOverlap)
}

func TestValidate_RejectsEmptyStrategies(t *testing.T) {
	t.Parallel()

	// Test: Empty strategies list fails validation
	cfg := Default()
	cfg.Chunking.Strategies = []string{}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrEmptyStrategy)
}

func TestValidate_RejectsUnknownStrategy(t *testing.T) {
	t.Parallel()

	// Test: Unknown strategy name fails validation
	cfg := Default()
	cfg.Chunking.Strategies = []string{"symbols", "unknown-strategy"}

	err := Validate(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown-strategy")
}

func TestValidate_ReturnsMultipleErrorsForMultipleInvalidFields(t *testing.T) {
	t.Parallel()

	// Test: Multiple validation errors are all reported
	cfg := &Config{
		Embedding: EmbeddingConfig{
			Provider:   "invalid",
			Model:      "",
			Dimensions: -1,
			Endpoint:   "",
		},
		Chunking: ChunkingConfig{
			Strategies:    []string{},
			DocChunkSize:  -100,
			CodeChunkSize: 0,
			Overlap:       -50,
		},
	}

	err := Validate(cfg)
	assert.Error(t, err)

	// Error message should contain multiple issues
	errMsg := err.Error()
	assert.Contains(t, errMsg, "provider")
	assert.Contains(t, errMsg, "model")
	assert.Contains(t, errMsg, "dimensions")
	assert.Contains(t, errMsg, "endpoint")
	assert.Contains(t, errMsg, "strategies")
}
