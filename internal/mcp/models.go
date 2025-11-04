package mcp

// Implementation Plan:
// 1. ContextChunk - core data structure with custom JSON marshaling
// 2. SearchOptions - query parameters with defaults
// 3. SearchResult - search response with similarity score
// 4. MCPServerConfig - configuration for MCP server
// 5. Request/Response types for MCP tool interface

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mvp-joe/project-cortex/internal/embed"
)

// ContextChunk represents a searchable chunk of code or documentation.
// The Embedding field is excluded from JSON serialization to reduce payload size.
type ContextChunk struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Text      string                 `json:"text"`
	ChunkType string                 `json:"chunk_type,omitempty"`
	Embedding []float32              `json:"embedding,omitempty"` // Excluded via MarshalJSON
	Tags      []string               `json:"tags,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// MarshalJSON implements custom JSON marshaling that excludes the Embedding field.
// This reduces response payload size by ~60% for MCP tool responses.
func (c ContextChunk) MarshalJSON() ([]byte, error) {
	type Alias ContextChunk
	return json.Marshal(&struct {
		Embedding []float32 `json:"embedding,omitempty"`
		*Alias
	}{
		Embedding: nil, // Always exclude embedding
		Alias:     (*Alias)(&c),
	})
}

// SearchOptions contains parameters for context search queries.
type SearchOptions struct {
	// Limit specifies the maximum number of results to return (1-100)
	Limit int `json:"limit,omitempty"`

	// MinScore filters results below this similarity threshold (0.0-1.0)
	MinScore float64 `json:"min_score,omitempty"`

	// Tags filters results to only include chunks with ALL specified tags (AND logic)
	Tags []string `json:"tags,omitempty"`

	// ChunkTypes filters results by chunk type (documentation, symbols, definitions, data)
	ChunkTypes []string `json:"chunk_types,omitempty"`
}

// DefaultSearchOptions returns default search options (limit: 15, no filters).
func DefaultSearchOptions() *SearchOptions {
	return &SearchOptions{
		Limit:    15,
		MinScore: 0.0,
	}
}

// SearchResult represents a single search result with similarity score.
type SearchResult struct {
	Chunk         *ContextChunk `json:"chunk"`
	CombinedScore float64       `json:"combined_score"`
}

// MCPServerConfig contains configuration for the MCP server.
type MCPServerConfig struct {
	ProjectPath      string // Project root path (for SQLite cache lookup)
	EmbeddingService *EmbeddingServiceConfig
}

// EmbeddingServiceConfig contains embedding provider configuration.
type EmbeddingServiceConfig struct {
	BaseURL string
	// Note: Dimensions are read from chunk file metadata, not hardcoded
}

// DefaultMCPServerConfig returns default MCP server configuration.
func DefaultMCPServerConfig() *MCPServerConfig {
	return &MCPServerConfig{
		ProjectPath: ".", // Current directory by default
		EmbeddingService: &EmbeddingServiceConfig{
			BaseURL: fmt.Sprintf("http://%s:%d", embed.DefaultEmbedServerHost, embed.DefaultEmbedServerPort),
		},
	}
}

// CortexSearchRequest represents the JSON request schema for the cortex_search MCP tool.
type CortexSearchRequest struct {
	Query        string   `json:"query" jsonschema:"required,description=Natural language search query"`
	Limit        int      `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=15,description=Maximum number of results"`
	Tags         []string `json:"tags,omitempty" jsonschema:"description=Filter by tags (AND logic)"`
	ChunkTypes   []string `json:"chunk_types,omitempty" jsonschema:"description=Filter by chunk type (documentation|symbols|definitions|data)"`
	IncludeStats bool     `json:"include_stats,omitempty" jsonschema:"default=false,description=Include reload metrics in response"`
}

// CortexSearchResponse represents the JSON response schema for the cortex_search MCP tool.
type CortexSearchResponse struct {
	Results []*SearchResult  `json:"results"`
	Total   int              `json:"total"`
	Metrics *MetricsSnapshot `json:"metrics,omitempty"`
}
