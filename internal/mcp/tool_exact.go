package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	mcputils "github.com/mvp-joe/project-cortex/internal/mcp-utils"
)

// AddCortexExactTool registers the cortex_exact tool with an MCP server.
// This function is composable - it can be combined with other tool registrations.
func AddCortexExactTool(s *server.MCPServer, searcher ExactSearcher) {
	tool := mcp.NewTool(
		"cortex_exact",
		mcp.WithDescription(`Full-text keyword search using FTS5 query syntax.

Supports:
- Phrase search: Use double quotes for exact phrases: "sync.RWMutex" or "error handling"
- Boolean operators: AND, OR, NOT
- Prefix wildcards: handler* (matches handler, handlers, handleRequest)
- Filters: language and file_path (applied via SQL, not FTS query)

Examples:
- "sync.RWMutex" - Find exact identifier (use quotes for dotted names)
- handler AND http - Boolean AND
- authentication NOT test - Exclude test files
- handle* - Prefix matching`),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("FTS5 query string. Use double quotes for phrases: \"sync.RWMutex\"")),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return (1-100, default: 15)")),
		mcp.WithString("language",
			mcp.Description("Filter by language (e.g., 'go', 'typescript', 'python')")),
		mcp.WithString("file_path",
			mcp.Description("Filter by file path using SQL LIKE syntax (e.g., 'internal/%', '%_test.go')")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)

	s.AddTool(tool, createExactSearchHandler(searcher))
}

// createExactSearchHandler creates the handler function for cortex_exact tool.
func createExactSearchHandler(searcher ExactSearcher) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		startTime := time.Now()

		// Parse and bind arguments using mcputils
		var req CortexExactRequest
		if err := mcputils.CoerceBindArguments(request, &req); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid arguments: %v", err)), nil
		}

		// Validate required fields
		if req.Query == "" {
			return mcp.NewToolResultError("query is required"), nil
		}

		// Apply defaults
		if req.Limit == 0 {
			req.Limit = 15
		}

		// Clamp limit to valid range
		if req.Limit < 1 {
			req.Limit = 1
		}
		if req.Limit > 100 {
			req.Limit = 100
		}

		// Build search options
		options := &ExactSearchOptions{
			Limit:    req.Limit,
			Language: req.Language,
			FilePath: req.FilePath,
		}

		// Execute search
		results, err := searcher.Search(ctx, req.Query, options)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}

		// Build response
		response := &CortexExactResponse{
			Query:         req.Query,
			Results:       results,
			TotalFound:    len(results),
			TotalReturned: len(results),
			Metadata: ExactResponseMetadata{
				TookMs: int(time.Since(startTime).Milliseconds()),
				Source: "exact",
			},
		}

		// Marshal and return response
		return marshalToolResponse(response)
	}
}

// CortexExactRequest represents the JSON request schema for the cortex_exact MCP tool.
type CortexExactRequest struct {
	Query    string `json:"query" jsonschema:"required,description=FTS5 query string"`
	Limit    int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=15"`
	Language string `json:"language,omitempty" jsonschema:"description=Filter by language (e.g. 'go' 'typescript' 'python')"`
	FilePath string `json:"file_path,omitempty" jsonschema:"description=Filter by file path using SQL LIKE syntax (e.g. 'internal/%' '%_test.go')"`
}

// CortexExactResponse represents the JSON response schema for the cortex_exact MCP tool.
type CortexExactResponse struct {
	Query         string                `json:"query"`
	Results       []*ExactSearchResult  `json:"results"`
	TotalFound    int                   `json:"total_found"`
	TotalReturned int                   `json:"total_returned"`
	Metadata      ExactResponseMetadata `json:"metadata"`
}

// ExactResponseMetadata contains timing and source information.
type ExactResponseMetadata struct {
	TookMs int    `json:"took_ms"`
	Source string `json:"source"` // "exact"
}
