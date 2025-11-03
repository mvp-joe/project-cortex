package config

import (
	"github.com/mvp-joe/project-cortex/internal/indexer"
)

// ToIndexerConfig converts a Config to an indexer.Config.
// The rootDir parameter specifies the root directory of the codebase to index.
func (c *Config) ToIndexerConfig(rootDir string) *indexer.Config {
	return &indexer.Config{
		RootDir:           rootDir,
		CodePatterns:      c.Paths.Code,
		DocsPatterns:      c.Paths.Docs,
		IgnorePatterns:    c.Paths.Ignore,
		ChunkStrategies:   c.Chunking.Strategies,
		DocChunkSize:      c.Chunking.DocChunkSize,
		CodeChunkSize:     c.Chunking.CodeChunkSize,
		Overlap:           c.Chunking.Overlap,
		OutputDir:         ".cortex/chunks",
		StorageBackend:    c.Storage.Backend,
		EmbeddingProvider: c.Embedding.Provider,
		EmbeddingModel:    c.Embedding.Model,
		EmbeddingDims:     c.Embedding.Dimensions,
		EmbeddingEndpoint: c.Embedding.Endpoint,
		EmbeddingBinary:   "cortex-embed",
	}
}
