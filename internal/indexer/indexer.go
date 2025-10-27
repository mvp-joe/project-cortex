package indexer

import (
	"context"
	"fmt"

	"github.com/mvp-joe/project-cortex/internal/embed"
	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
)

// Implementation Plan:
// 1. Indexer interface - high-level API for indexing operations
// 2. Parser interface - language-specific code parsing
// 3. Chunker interface - documentation chunking
// 4. Formatter - converts extractions to natural language text
// 5. FileDiscovery - glob pattern matching with gitignore support
// 6. IncrementalTracker - checksum-based change detection
// 7. AtomicWriter - safe file writing with temp → rename
// 8. Main indexer implementation - orchestrates all components

// Indexer provides the main interface for indexing codebase content.
type Indexer interface {
	// Index processes all files in the codebase and generates chunk files.
	// Returns statistics about the indexing process.
	Index(ctx context.Context) (*ProcessingStats, error)

	// IndexIncremental processes only changed files based on checksums.
	// Returns statistics about the indexing process.
	IndexIncremental(ctx context.Context) (*ProcessingStats, error)

	// Watch starts watching for file changes and reindexes incrementally.
	// Blocks until context is cancelled.
	Watch(ctx context.Context) error

	// Close releases all resources held by the indexer.
	Close() error
}

// Parser extracts structured information from source code files.
type Parser interface {
	// ParseFile extracts code structure from a source file.
	// Returns CodeExtraction containing symbols, definitions, and data.
	ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error)

	// SupportsLanguage checks if this parser supports the given language.
	SupportsLanguage(language string) bool
}

// Chunker splits documentation files into semantic chunks.
type Chunker interface {
	// ChunkDocument splits a markdown file into semantic chunks.
	// Returns a slice of DocumentationChunk.
	ChunkDocument(ctx context.Context, filePath string, content string) ([]DocumentationChunk, error)
}

// Formatter converts code extractions and doc chunks into natural language text.
type Formatter interface {
	// FormatSymbols converts SymbolsData into natural language text.
	FormatSymbols(data *extraction.SymbolsData, language string) string

	// FormatDefinitions converts DefinitionsData into formatted code with line comments.
	FormatDefinitions(data *extraction.DefinitionsData, language string) string

	// FormatData converts DataData into formatted code with line comments.
	FormatData(data *extraction.DataData, language string) string

	// FormatDocumentation formats a documentation chunk (may add context).
	FormatDocumentation(chunk *DocumentationChunk) string
}

// Config contains configuration for the indexer.
type Config struct {
	// Root directory of the codebase to index
	RootDir string

	// Paths configuration
	CodePatterns   []string
	DocsPatterns   []string
	IgnorePatterns []string

	// Chunking configuration
	ChunkStrategies []string // ["symbols", "definitions", "data"]
	DocChunkSize    int      // tokens
	CodeChunkSize   int      // characters
	Overlap         int      // tokens

	// Output configuration
	OutputDir string // .cortex/chunks/

	// Embedding configuration
	EmbeddingProvider string
	EmbeddingModel    string
	EmbeddingDims     int
	EmbeddingEndpoint string
	EmbeddingBinary   string
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig(rootDir string) *Config {
	return &Config{
		RootDir: rootDir,
		CodePatterns: []string{
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
		DocsPatterns: []string{
			"**/*.md",
			"**/*.rst",
		},
		IgnorePatterns: []string{
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
		ChunkStrategies:   []string{"symbols", "definitions", "data"},
		DocChunkSize:      800,
		CodeChunkSize:     2000,
		Overlap:           100,
		OutputDir:         ".cortex/chunks",
		EmbeddingProvider: "local",
		EmbeddingModel:    "BAAI/bge-small-en-v1.5",
		EmbeddingDims:     384,
		EmbeddingEndpoint: fmt.Sprintf("http://%s:%d/embed", embed.DefaultEmbedServerHost, embed.DefaultEmbedServerPort),
		EmbeddingBinary:   "cortex-embed",
	}
}
