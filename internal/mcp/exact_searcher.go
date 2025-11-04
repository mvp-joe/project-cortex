package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search/query"
)

// ExactSearcher defines the interface for full-text keyword search.
type ExactSearcher interface {
	// Search executes a keyword search using FTS query syntax.
	// Supports field scoping, boolean operators, phrase search, wildcards, and fuzzy matching.
	// Options parameter may be nil (defaults will be applied).
	Search(ctx context.Context, queryStr string, options *ExactSearchOptions) ([]*ExactSearchResult, error)

	// UpdateIncremental applies incremental updates to the search index.
	// Uses batch operations for optimal performance.
	UpdateIncremental(ctx context.Context, added, updated []*ContextChunk, deleted []string) error

	// Close releases resources held by the searcher.
	Close() error
}

// ExactSearchResult represents a single keyword search result with highlighting.
type ExactSearchResult struct {
	Chunk      *ContextChunk `json:"chunk"`
	Score      float64       `json:"score"`      // Match quality (0-1)
	Highlights []string      `json:"highlights"` // Matching snippets with <em> tags
}

// exactSearcher implements ExactSearcher using bleve full-text search.
type exactSearcher struct {
	index bleve.Index
	mu    sync.RWMutex // Protects index during updates
}

// NewExactSearcher creates a new ExactSearcher backed by an in-memory bleve index.
// It indexes all chunks from the provided ChunkSet.
func NewExactSearcher(ctx context.Context, chunkSet *ChunkSet) (ExactSearcher, error) {
	// Create in-memory index with custom mapping
	indexMapping := buildBleveMapping()
	index, err := bleve.NewMemOnly(indexMapping)
	if err != nil {
		return nil, fmt.Errorf("failed to create bleve index: %w", err)
	}

	// Batch index all chunks (optimal for initial load)
	if err := indexChunks(ctx, index, chunkSet.All()); err != nil {
		index.Close()
		return nil, fmt.Errorf("failed to index chunks: %w", err)
	}

	return &exactSearcher{
		index: index,
	}, nil
}

// buildBleveMapping creates the index mapping for chunk documents.
// All fields are indexed and stored for native filtering and retrieval.
func buildBleveMapping() *mapping.IndexMappingImpl {
	indexMapping := bleve.NewIndexMapping()

	// Text field (primary search target) - standard analyzer
	textMapping := bleve.NewTextFieldMapping()
	textMapping.Analyzer = "standard"
	textMapping.Store = true              // Store for highlighting
	textMapping.Index = true              // Searchable
	textMapping.IncludeTermVectors = true // Enable phrase search

	// Chunk type field (filterable) - keyword analyzer for exact matching
	chunkTypeMapping := bleve.NewTextFieldMapping()
	chunkTypeMapping.Analyzer = "keyword"
	chunkTypeMapping.Store = true
	chunkTypeMapping.Index = true

	// Tags field (filterable, array) - keyword analyzer
	tagsMapping := bleve.NewTextFieldMapping()
	tagsMapping.Analyzer = "keyword"
	tagsMapping.Store = true
	tagsMapping.Index = true

	// File path field (filterable) - standard analyzer for partial matching
	filePathMapping := bleve.NewTextFieldMapping()
	filePathMapping.Analyzer = "standard"
	filePathMapping.Store = true
	filePathMapping.Index = true

	// Title field (searchable) - standard analyzer
	titleMapping := bleve.NewTextFieldMapping()
	titleMapping.Analyzer = "standard"
	titleMapping.Store = true
	titleMapping.Index = true

	// ID field (stored but not analyzed) - keyword for exact match only
	idMapping := bleve.NewTextFieldMapping()
	idMapping.Analyzer = "keyword"
	idMapping.Store = true
	idMapping.Index = false // Don't index, just store

	// Document mapping
	docMapping := bleve.NewDocumentMapping()
	docMapping.AddFieldMappingsAt("id", idMapping)
	docMapping.AddFieldMappingsAt("text", textMapping)
	docMapping.AddFieldMappingsAt("chunk_type", chunkTypeMapping)
	docMapping.AddFieldMappingsAt("tags", tagsMapping)
	docMapping.AddFieldMappingsAt("file_path", filePathMapping)
	docMapping.AddFieldMappingsAt("title", titleMapping)

	indexMapping.DefaultMapping = docMapping
	return indexMapping
}

// indexChunks adds chunks to the bleve index in batches.
func indexChunks(ctx context.Context, index bleve.Index, chunks []*ContextChunk) error {
	const batchSize = 1000

	batch := index.NewBatch()
	for i, chunk := range chunks {
		// Check cancellation periodically
		if i%batchSize == 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}

		doc := chunkToDocument(chunk)
		if err := batch.Index(chunk.ID, doc); err != nil {
			return fmt.Errorf("failed to add chunk %s to batch: %w", chunk.ID, err)
		}

		// Execute batch every 1000 docs (optimal size)
		if batch.Size() >= batchSize {
			if err := index.Batch(batch); err != nil {
				return fmt.Errorf("failed to execute batch: %w", err)
			}
			batch = index.NewBatch()
		}
	}

	// Execute remaining
	if batch.Size() > 0 {
		if err := index.Batch(batch); err != nil {
			return fmt.Errorf("failed to execute final batch: %w", err)
		}
	}

	return nil
}

// chunkToDocument converts a ContextChunk to a bleve document.
func chunkToDocument(chunk *ContextChunk) map[string]interface{} {
	// Extract file_path from metadata
	filePath, _ := chunk.Metadata["file_path"].(string)

	return map[string]interface{}{
		"id":         chunk.ID,
		"text":       chunk.Text,
		"chunk_type": chunk.ChunkType,
		"tags":       chunk.Tags,
		"file_path":  filePath,
		"title":      chunk.Title,
	}
}

// Search executes a keyword search using bleve QueryStringQuery syntax.
func (s *exactSearcher) Search(ctx context.Context, queryStr string, options *ExactSearchOptions) ([]*ExactSearchResult, error) {
	// Apply defaults if options not provided
	if options == nil {
		options = DefaultExactSearchOptions()
	}

	limit := options.Limit
	if limit <= 0 || limit > 100 {
		limit = 15
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build query with filters
	var queries []query.Query
	queries = append(queries, bleve.NewQueryStringQuery(queryStr))

	// Add language filter if specified
	if options.Language != "" {
		langQuery := bleve.NewMatchQuery(options.Language)
		langQuery.SetField("tags")
		queries = append(queries, langQuery)
	}

	// Add file path filter if specified (use wildcard query for LIKE-style patterns)
	if options.FilePath != "" {
		pathQuery := bleve.NewWildcardQuery(options.FilePath)
		pathQuery.SetField("file_path")
		queries = append(queries, pathQuery)
	}

	// Combine with conjunction (AND)
	var finalQuery query.Query
	if len(queries) == 1 {
		// Single query - use it directly
		finalQuery = queries[0]
	} else {
		// Multiple queries - combine with AND
		finalQuery = bleve.NewConjunctionQuery(queries...)
	}

	// Execute search with highlighting
	searchRequest := bleve.NewSearchRequestOptions(finalQuery, limit, 0, false)
	highlightStyle := "html" // Use HTML style with <em> tags
	searchRequest.Highlight = bleve.NewHighlight()
	searchRequest.Highlight.Style = &highlightStyle
	searchRequest.Highlight.Fields = []string{"text"} // Only highlight text field

	// Request stored fields for chunk reconstruction
	searchRequest.Fields = []string{"id", "text", "chunk_type", "tags", "file_path", "title"}

	searchResult, err := s.index.Search(searchRequest)
	if err != nil {
		return nil, fmt.Errorf("bleve search failed: %w", err)
	}

	// Convert results (NO POST-FILTERING - bleve did it natively)
	results := make([]*ExactSearchResult, 0, len(searchResult.Hits))
	for _, hit := range searchResult.Hits {
		// Retrieve stored fields from bleve (avoids ChunkManager lookup)
		id, _ := hit.Fields["id"].(string)
		text, _ := hit.Fields["text"].(string)
		chunkType, _ := hit.Fields["chunk_type"].(string)
		filePath, _ := hit.Fields["file_path"].(string)
		title, _ := hit.Fields["title"].(string)

		// Reconstruct minimal chunk for response
		chunk := &ContextChunk{
			ID:        id,
			Text:      text,
			ChunkType: chunkType,
			Title:     title,
			Metadata: map[string]interface{}{
				"file_path": filePath,
			},
		}

		// Tags may be []interface{} from bleve
		if tagsRaw, ok := hit.Fields["tags"].([]interface{}); ok {
			tags := make([]string, len(tagsRaw))
			for i, t := range tagsRaw {
				tags[i], _ = t.(string)
			}
			chunk.Tags = tags
		}

		// Extract highlights
		highlights := extractHighlights(hit.Fragments)

		results = append(results, &ExactSearchResult{
			Chunk:      chunk,
			Score:      hit.Score,
			Highlights: highlights,
		})
	}

	return results, nil
}

// extractHighlights extracts highlighted snippets from bleve fragments.
// Limits to 3 highlights per result to avoid overwhelming the LLM.
func extractHighlights(fragments map[string][]string) []string {
	var highlights []string

	// Bleve returns fragments as map[field][]snippets
	for _, snippets := range fragments {
		highlights = append(highlights, snippets...)
	}

	// Limit to 3 highlights per result
	if len(highlights) > 3 {
		highlights = highlights[:3]
	}

	return highlights
}

// UpdateIncremental applies incremental updates to the bleve index.
// Uses batch operations for optimal performance.
func (s *exactSearcher) UpdateIncremental(ctx context.Context, added, updated []*ContextChunk, deleted []string) error {
	// Build batch (optimal: 100-1000 ops)
	batch := s.index.NewBatch()

	// 1. Delete removed chunks
	for _, id := range deleted {
		batch.Delete(id)
	}

	// 2. Add new + updated chunks (Index() handles both)
	allChanges := append(added, updated...)
	for _, chunk := range allChanges {
		doc := chunkToDocument(chunk)
		if err := batch.Index(chunk.ID, doc); err != nil {
			return fmt.Errorf("failed to add chunk %s to batch: %w", chunk.ID, err)
		}
	}

	// 3. Execute batch (no lock needed during build, lock during execution)
	// Note: We hold read lock during Search(), so use write lock here
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.index.Batch(batch); err != nil {
		return fmt.Errorf("failed to execute batch: %w", err)
	}

	return nil
}

// Close releases resources held by the searcher.
func (s *exactSearcher) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.index != nil {
		return s.index.Close()
	}
	return nil
}
