package mcp

// Implementation Plan:
// 1. chromemSearcher - unexported implementation of ContextSearcher
// 2. NewChromemSearcher - public constructor returning interface
// 3. Initialize chromem-go database with 384 dimensions
// 4. Load chunks and add to collection
// 5. Query with vector similarity search
// 6. Apply filters (chunk_types, tags) with AND logic
// 7. Reload support with atomic swap (RWMutex)
// 8. Thread-safe concurrent queries

import (
	"context"
	"fmt"
	"sync"

	"github.com/philippgille/chromem-go"
)

// chromemSearcher implements ContextSearcher using chromem-go as the vector database.
type chromemSearcher struct {
	config     *MCPServerConfig
	provider   EmbeddingProvider
	db         *chromem.DB
	collection *chromem.Collection
	mu         sync.RWMutex // Protects collection during reload
}

// NewChromemSearcher creates a new ContextSearcher backed by chromem-go.
// It loads chunks from the configured directory and initializes the vector database.
func NewChromemSearcher(ctx context.Context, config *MCPServerConfig, provider EmbeddingProvider) (ContextSearcher, error) {
	if config == nil {
		config = DefaultMCPServerConfig()
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}

	// Create chromem-go database
	db := chromem.NewDB()

	searcher := &chromemSearcher{
		config:   config,
		provider: provider,
		db:       db,
	}

	// Initial load of chunks
	if err := searcher.loadChunks(ctx); err != nil {
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}

	return searcher, nil
}

// loadChunks loads chunks from disk and populates the chromem-go collection.
// This is called during initialization and reload.
func (s *chromemSearcher) loadChunks(ctx context.Context) error {
	// Load chunks from JSON files
	chunks, err := LoadChunks(s.config.ChunksDir)
	if err != nil {
		return err
	}

	// Create a new collection (atomic replacement during reload)
	collection, err := s.db.CreateCollection("cortex", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	// Add chunks to collection
	for _, chunk := range chunks {
		// Convert metadata to map[string]string for chromem-go
		metadata := make(map[string]string)
		if chunk.ChunkType != "" {
			metadata["chunk_type"] = chunk.ChunkType
		}
		// Store tags as comma-separated string
		if len(chunk.Tags) > 0 {
			tagsStr := ""
			for i, tag := range chunk.Tags {
				if i > 0 {
					tagsStr += ","
				}
				tagsStr += tag
			}
			metadata["tags"] = tagsStr
		}

		doc := chromem.Document{
			ID:        chunk.ID,
			Content:   chunk.Text,
			Embedding: chunk.Embedding,
			Metadata:  metadata,
		}

		if err := collection.AddDocument(ctx, doc); err != nil {
			return fmt.Errorf("failed to add chunk %s: %w", chunk.ID, err)
		}
	}

	// Atomic swap (write lock)
	s.mu.Lock()
	s.collection = collection
	s.mu.Unlock()

	return nil
}

// Query executes a semantic search for the given query string.
func (s *chromemSearcher) Query(ctx context.Context, query string, options *SearchOptions) ([]*SearchResult, error) {
	if options == nil {
		options = DefaultSearchOptions()
	}

	// Validate and normalize options
	if options.Limit <= 0 || options.Limit > 100 {
		options.Limit = 15
	}

	// Generate query embedding (use "query" mode for search queries)
	embeddings, err := s.provider.Embed(ctx, []string{query}, "query")
	if err != nil {
		return nil, fmt.Errorf("failed to generate query embedding: %w", err)
	}
	if len(embeddings) == 0 {
		return nil, fmt.Errorf("no embedding returned for query")
	}
	queryEmbedding := embeddings[0]

	// Acquire read lock for query
	s.mu.RLock()
	collection := s.collection
	s.mu.RUnlock()

	if collection == nil {
		return nil, fmt.Errorf("collection not initialized")
	}

	// Query chromem-go with 2x multiplier for post-filtering headroom
	nResults := options.Limit * 2
	docs, err := collection.QueryEmbedding(ctx, queryEmbedding, nResults, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Convert to SearchResults and apply filters
	results := make([]*SearchResult, 0, len(docs))
	for _, doc := range docs {
		// Apply chunk type filter
		if len(options.ChunkTypes) > 0 {
			chunkType := doc.Metadata["chunk_type"]
			if !contains(options.ChunkTypes, chunkType) {
				continue
			}
		}

		// Apply tag filter (AND logic - must have all specified tags)
		if len(options.Tags) > 0 {
			docTags := splitTags(doc.Metadata["tags"])
			if !containsAll(docTags, options.Tags) {
				continue
			}
		}

		// Apply minimum score filter
		if doc.Similarity < float32(options.MinScore) {
			continue
		}

		// Create search result (reconstruct minimal chunk info)
		chunk := &ContextChunk{
			ID:        doc.ID,
			Text:      doc.Content,
			ChunkType: doc.Metadata["chunk_type"],
			Tags:      splitTags(doc.Metadata["tags"]),
			Metadata:  convertMetadata(doc.Metadata),
		}

		result := &SearchResult{
			Chunk:         chunk,
			CombinedScore: float64(doc.Similarity),
		}
		results = append(results, result)
	}

	// Limit final results
	if len(results) > options.Limit {
		results = results[:options.Limit]
	}

	return results, nil
}

// Reload reloads chunks from disk (for hot reload).
func (s *chromemSearcher) Reload(ctx context.Context) error {
	return s.loadChunks(ctx)
}

// Close releases resources.
func (s *chromemSearcher) Close() error {
	// chromem-go doesn't require explicit cleanup
	return nil
}

// Helper functions

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func containsAll(haystack []string, needles []string) bool {
	for _, needle := range needles {
		if !contains(haystack, needle) {
			return false
		}
	}
	return true
}

func splitTags(tagsStr string) []string {
	if tagsStr == "" {
		return nil
	}
	tags := make([]string, 0)
	current := ""
	for _, ch := range tagsStr {
		if ch == ',' {
			if current != "" {
				tags = append(tags, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		tags = append(tags, current)
	}
	return tags
}

func convertMetadata(meta map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range meta {
		if k != "tags" && k != "chunk_type" { // Skip already-extracted fields
			result[k] = v
		}
	}
	return result
}
