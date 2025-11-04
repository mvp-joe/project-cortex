package config

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrInvalidProvider indicates an unsupported embedding provider
	ErrInvalidProvider = errors.New("invalid embedding provider")

	// ErrInvalidDimensions indicates invalid embedding dimensions
	ErrInvalidDimensions = errors.New("invalid embedding dimensions")

	// ErrInvalidChunkSize indicates invalid chunk size configuration
	ErrInvalidChunkSize = errors.New("invalid chunk size")

	// ErrInvalidOverlap indicates invalid overlap configuration
	ErrInvalidOverlap = errors.New("invalid overlap")

	// ErrEmptyEndpoint indicates missing embedding endpoint
	ErrEmptyEndpoint = errors.New("empty embedding endpoint")

	// ErrEmptyModel indicates missing embedding model
	ErrEmptyModel = errors.New("empty embedding model")

	// ErrEmptyStrategy indicates missing chunking strategies
	ErrEmptyStrategy = errors.New("empty chunking strategies")

	// ErrInvalidBackend indicates an unsupported storage backend
	ErrInvalidBackend = errors.New("invalid storage backend")

	// ErrDeprecatedBackend indicates a deprecated storage backend
	ErrDeprecatedBackend = errors.New("deprecated storage backend")

	// ErrInvalidCacheSettings indicates invalid cache configuration
	ErrInvalidCacheSettings = errors.New("invalid cache settings")
)

// Validate checks that the configuration is valid and complete.
func Validate(cfg *Config) error {
	var errs []error

	// Validate embedding configuration
	if err := validateEmbedding(&cfg.Embedding); err != nil {
		errs = append(errs, err)
	}

	// Validate paths configuration
	if err := validatePaths(&cfg.Paths); err != nil {
		errs = append(errs, err)
	}

	// Validate chunking configuration
	if err := validateChunking(&cfg.Chunking); err != nil {
		errs = append(errs, err)
	}

	// Validate storage configuration
	if err := validateStorage(&cfg.Storage); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return joinErrors(errs)
	}

	return nil
}

func validateEmbedding(cfg *EmbeddingConfig) error {
	var errs []error

	// Validate provider
	provider := strings.ToLower(cfg.Provider)
	if provider != "local" && provider != "openai" {
		errs = append(errs, fmt.Errorf("%w: must be 'local' or 'openai', got '%s'", ErrInvalidProvider, cfg.Provider))
	}

	// Validate model
	if strings.TrimSpace(cfg.Model) == "" {
		errs = append(errs, fmt.Errorf("%w: model is required", ErrEmptyModel))
	}

	// Validate dimensions
	if cfg.Dimensions <= 0 {
		errs = append(errs, fmt.Errorf("%w: dimensions must be positive, got %d", ErrInvalidDimensions, cfg.Dimensions))
	}

	// Validate endpoint
	if strings.TrimSpace(cfg.Endpoint) == "" {
		errs = append(errs, fmt.Errorf("%w: endpoint is required", ErrEmptyEndpoint))
	}

	if len(errs) > 0 {
		return joinErrors(errs)
	}

	return nil
}

func validatePaths(cfg *PathsConfig) error {
	// Paths can be empty - validation is lenient here
	// The indexer will handle empty patterns gracefully
	return nil
}

func validateChunking(cfg *ChunkingConfig) error {
	var errs []error

	// Validate strategies
	if len(cfg.Strategies) == 0 {
		errs = append(errs, fmt.Errorf("%w: at least one strategy required", ErrEmptyStrategy))
	}

	// Validate known strategies
	validStrategies := map[string]bool{
		"symbols":     true,
		"definitions": true,
		"data":        true,
	}

	for _, strategy := range cfg.Strategies {
		if !validStrategies[strategy] {
			errs = append(errs, fmt.Errorf("unknown chunking strategy: %s (valid: symbols, definitions, data)", strategy))
		}
	}

	// Validate doc chunk size
	if cfg.DocChunkSize <= 0 {
		errs = append(errs, fmt.Errorf("%w: doc_chunk_size must be positive, got %d", ErrInvalidChunkSize, cfg.DocChunkSize))
	}

	// Validate code chunk size
	if cfg.CodeChunkSize <= 0 {
		errs = append(errs, fmt.Errorf("%w: code_chunk_size must be positive, got %d", ErrInvalidChunkSize, cfg.CodeChunkSize))
	}

	// Validate overlap
	if cfg.Overlap < 0 {
		errs = append(errs, fmt.Errorf("%w: overlap cannot be negative, got %d", ErrInvalidOverlap, cfg.Overlap))
	}

	// Warn if overlap is too large (but don't fail validation) - only check if DocChunkSize is positive
	if cfg.DocChunkSize > 0 && cfg.Overlap >= cfg.DocChunkSize {
		errs = append(errs, fmt.Errorf("%w: overlap (%d) should be less than doc_chunk_size (%d)", ErrInvalidOverlap, cfg.Overlap, cfg.DocChunkSize))
	}

	if len(errs) > 0 {
		return joinErrors(errs)
	}

	return nil
}

func validateStorage(cfg *StorageConfig) error {
	var errs []error

	// SQLite is the only supported backend now - no validation needed

	// Validate cache max age (negative is invalid, zero means no age-based eviction)
	if cfg.CacheMaxAgeDays < 0 {
		errs = append(errs, fmt.Errorf("%w: cache_max_age_days cannot be negative, got %d", ErrInvalidCacheSettings, cfg.CacheMaxAgeDays))
	}

	// Validate cache max size (negative is invalid, zero means no size-based eviction)
	if cfg.CacheMaxSizeMB < 0 {
		errs = append(errs, fmt.Errorf("%w: cache_max_size_mb cannot be negative, got %.2f", ErrInvalidCacheSettings, cfg.CacheMaxSizeMB))
	}

	if len(errs) > 0 {
		return joinErrors(errs)
	}

	return nil
}

// joinErrors combines multiple errors into a single error with clear formatting.
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}

	if len(errs) == 1 {
		return errs[0]
	}

	var msgs []string
	for _, err := range errs {
		msgs = append(msgs, err.Error())
	}

	return fmt.Errorf("validation failed:\n  - %s", strings.Join(msgs, "\n  - "))
}
