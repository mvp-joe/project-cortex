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
	"slices"
	"sync"
	"time"

	"github.com/philippgille/chromem-go"
)

const (
	// DefaultResultMultiplier controls over-fetching for post-filtering headroom.
	// We fetch 2x the requested limit to ensure enough results remain after post-filtering.
	DefaultResultMultiplier = 2
)

// chromemSearcher implements ContextSearcher using chromem-go as the vector database.
type chromemSearcher struct {
	config       *MCPServerConfig
	provider     EmbeddingProvider
	db           *chromem.DB
	collection   *chromem.Collection
	chunkManager *ChunkManager
	metrics      *ReloadMetrics
	mu           sync.RWMutex // Protects collection during reload
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

	// Create ChunkManager for shared chunk loading
	chunkManager := NewChunkManager(config.ChunksDir)

	searcher := &chromemSearcher{
		config:       config,
		provider:     provider,
		db:           db,
		chunkManager: chunkManager,
		metrics:      NewReloadMetrics(),
	}

	// Initial load of chunks (use Reload to track metrics)
	if err := searcher.Reload(ctx); err != nil {
		return nil, fmt.Errorf("failed to load chunks: %w", err)
	}

	return searcher, nil
}

// newChromemSearcherWithChunkManager creates a chromemSearcher with a shared ChunkManager.
// This is used by SearcherCoordinator to avoid duplicate chunk loading.
func newChromemSearcherWithChunkManager(
	ctx context.Context,
	config *MCPServerConfig,
	provider EmbeddingProvider,
	chunkManager *ChunkManager,
	initialSet *ChunkSet,
) (*chromemSearcher, error) {
	if config == nil {
		config = DefaultMCPServerConfig()
	}
	if provider == nil {
		return nil, fmt.Errorf("embedding provider is required")
	}

	// Create chromem-go database
	db := chromem.NewDB()

	// Create collection
	collection, err := db.CreateCollection("cortex", nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}

	// Add initial chunks to collection
	for _, chunk := range initialSet.All() {
		metadata := make(map[string]string)
		if chunk.ChunkType != "" {
			metadata["chunk_type"] = chunk.ChunkType
		}
		for k, v := range chunk.Metadata {
			if str, ok := v.(string); ok {
				metadata[k] = str
			}
		}

		doc := chromem.Document{
			ID:        chunk.ID,
			Content:   chunk.Text,
			Embedding: chunk.Embedding,
			Metadata:  metadata,
		}

		if err := collection.AddDocument(ctx, doc); err != nil {
			return nil, fmt.Errorf("failed to add chunk %s: %w", chunk.ID, err)
		}
	}

	// Update ChunkManager with initial set
	chunkManager.Update(initialSet, time.Now())

	return &chromemSearcher{
		config:       config,
		provider:     provider,
		db:           db,
		collection:   collection,
		chunkManager: chunkManager,
		metrics:      NewReloadMetrics(),
	}, nil
}

// loadChunks loads chunks from disk and populates the chromem-go collection.
// This is called during initialization and reload.
func (s *chromemSearcher) loadChunks(ctx context.Context) error {
	// Load chunks via ChunkManager (shared loading)
	newSet, err := s.chunkManager.Load(ctx)
	if err != nil {
		return err
	}

	// Create a new collection (atomic replacement during reload)
	collection, err := s.db.CreateCollection("cortex", nil, nil)
	if err != nil {
		return fmt.Errorf("failed to create collection: %w", err)
	}

	// Add chunks to collection
	for _, chunk := range newSet.All() {
		// Convert chunk metadata to map[string]string for chromem-go
		// The indexer already stores tags as tag_0, tag_1, tag_2, etc. in Metadata
		metadata := make(map[string]string)

		// Add chunk_type
		if chunk.ChunkType != "" {
			metadata["chunk_type"] = chunk.ChunkType
		}

		// Copy all metadata fields from chunk (includes tag_0, tag_1, etc.)
		for k, v := range chunk.Metadata {
			// Convert interface{} to string
			if str, ok := v.(string); ok {
				metadata[k] = str
			}
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

	// Atomic swap (write lock for both collection and ChunkManager)
	s.mu.Lock()
	s.collection = collection
	s.mu.Unlock()

	// Update ChunkManager with new ChunkSet
	s.chunkManager.Update(newSet, time.Now())

	return nil
}

// buildWhereFilter constructs a WHERE filter map for chromem-go native filtering.
// Uses first chunk_type and first tag for native WHERE filtering.
// Additional values are handled by post-filtering.
func (s *chromemSearcher) buildWhereFilter(options *SearchOptions) map[string]string {
	whereFilter := make(map[string]string)

	// Add chunk type filtering if specified
	// Use FIRST chunk type for native WHERE filtering
	// Post-filter handles multiple chunk types
	if len(options.ChunkTypes) > 0 {
		whereFilter["chunk_type"] = options.ChunkTypes[0]
	}

	// Add tag filtering if specified
	// Use FIRST tag for native WHERE filtering
	// Post-filter handles additional tags (AND logic)
	if len(options.Tags) > 0 {
		whereFilter["tag_0"] = options.Tags[0]
	}

	return whereFilter
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

	// Build native WHERE filter (first tag, first chunk_type)
	whereFilter := s.buildWhereFilter(options)

	// Query chromem-go with 2x multiplier for post-filtering headroom
	nResults := options.Limit * DefaultResultMultiplier

	// Native chromem filtering via WHERE clause
	docs, err := collection.QueryEmbedding(
		ctx,
		queryEmbedding,
		nResults,
		whereFilter, // Native filter (first values)
		nil,         // WhereDocument unused
	)
	if err != nil {
		return nil, fmt.Errorf("vector search failed: %w", err)
	}

	// Post-filter for additional criteria and convert to SearchResults
	results := make([]*SearchResult, 0, options.Limit)
	for _, doc := range docs {
		// Post-filter: Multiple chunk types
		// Note: First chunk type already filtered by WHERE clause
		if len(options.ChunkTypes) > 1 {
			chunkType := doc.Metadata["chunk_type"]
			if !contains(options.ChunkTypes, chunkType) {
				continue
			}
		}

		// Post-filter: Additional tags (skip tag_0, already filtered by WHERE)
		// Must have ALL specified tags (AND logic)
		if len(options.Tags) > 1 {
			// Reconstruct tags from metadata (tag_0, tag_1, tag_2, ...)
			docTags := extractTagsFromMetadata(doc.Metadata)
			// Check additional tags (skip first tag, already filtered by WHERE)
			hasAllTags := true
			for _, requiredTag := range options.Tags[1:] {
				if !slices.Contains(docTags, requiredTag) {
					hasAllTags = false
					break
				}
			}
			if !hasAllTags {
				continue
			}
		}

		// Post-filter: Minimum score
		if options.MinScore > 0 && doc.Similarity < float32(options.MinScore) {
			continue
		}

		// Create search result (reconstruct chunk from chromem document)
		chunk := &ContextChunk{
			ID:        doc.ID,
			Text:      doc.Content,
			ChunkType: doc.Metadata["chunk_type"],
			Tags:      extractTagsFromMetadata(doc.Metadata),
			Metadata:  convertMetadata(doc.Metadata),
		}

		results = append(results, &SearchResult{
			Chunk:         chunk,
			CombinedScore: float64(doc.Similarity),
		})

		// Early exit: Stop once we have enough results
		if len(results) >= options.Limit {
			break
		}
	}

	return results, nil
}

// Reload reloads chunks from disk (for hot reload).
func (s *chromemSearcher) Reload(ctx context.Context) error {
	startTime := time.Now()
	err := s.loadChunks(ctx)
	duration := time.Since(startTime)

	// Get chunk count (0 if reload failed)
	chunkCount := 0
	if err == nil {
		s.mu.RLock()
		if s.collection != nil {
			chunkCount = s.collection.Count()
		}
		s.mu.RUnlock()
	}

	// Record metrics
	s.metrics.RecordReload(duration, err, chunkCount)

	return err
}

// GetMetrics returns current reload operation metrics.
func (s *chromemSearcher) GetMetrics() MetricsSnapshot {
	return s.metrics.GetMetrics()
}

// UpdateIncremental applies incremental updates to the chromem collection.
// This method is used by SearcherCoordinator for efficient reloads.
func (s *chromemSearcher) UpdateIncremental(ctx context.Context, added, updated []*ContextChunk, deleted []string) error {
	// NO LOCK - chromem operations are thread-safe
	// We only need to lock when swapping the collection reference

	s.mu.RLock()
	collection := s.collection
	s.mu.RUnlock()

	if collection == nil {
		return fmt.Errorf("collection not initialized")
	}

	// 1. Delete removed chunks
	for _, id := range deleted {
		// Log error but continue (chunk might not exist)
		if err := collection.Delete(ctx, nil, nil, id); err != nil {
			// Deletion errors are logged but don't fail the operation
			// The chunk might have already been deleted or never existed
		}
	}

	// 2. Update changed chunks (delete + add)
	for _, chunk := range updated {
		// Delete old version (ignore errors)
		collection.Delete(ctx, nil, nil, chunk.ID)

		// Add updated version
		metadata := make(map[string]string)
		if chunk.ChunkType != "" {
			metadata["chunk_type"] = chunk.ChunkType
		}
		for k, v := range chunk.Metadata {
			if str, ok := v.(string); ok {
				metadata[k] = str
			}
		}

		doc := chromem.Document{
			ID:        chunk.ID,
			Content:   chunk.Text,
			Embedding: chunk.Embedding,
			Metadata:  metadata,
		}
		if err := collection.AddDocument(ctx, doc); err != nil {
			return fmt.Errorf("failed to update chunk %s: %w", chunk.ID, err)
		}
	}

	// 3. Add new chunks
	for _, chunk := range added {
		metadata := make(map[string]string)
		if chunk.ChunkType != "" {
			metadata["chunk_type"] = chunk.ChunkType
		}
		for k, v := range chunk.Metadata {
			if str, ok := v.(string); ok {
				metadata[k] = str
			}
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

	return nil
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

// extractTagsFromMetadata extracts tags from indexed metadata keys (tag_0, tag_1, tag_2, ...).
func extractTagsFromMetadata(metadata map[string]string) []string {
	tags := make([]string, 0)
	// Tags are stored as tag_0, tag_1, tag_2, etc.
	// Extract them in order
	for i := 0; ; i++ {
		key := fmt.Sprintf("tag_%d", i)
		tag, exists := metadata[key]
		if !exists {
			break
		}
		tags = append(tags, tag)
	}
	return tags
}

func convertMetadata(meta map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range meta {
		// Skip already-extracted fields and tag_* keys
		if k == "chunk_type" {
			continue
		}
		// Skip tag_0, tag_1, tag_2, etc. (already extracted into Tags array)
		if len(k) >= 5 && k[:4] == "tag_" {
			continue
		}
		result[k] = v
	}
	return result
}
