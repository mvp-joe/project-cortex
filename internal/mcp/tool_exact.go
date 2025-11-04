package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

		// Parse arguments
		argsMap, errResult := parseToolArguments(request)
		if errResult != nil {
			return errResult, nil
		}

		// Extract query (required)
		query, err := parseStringArg(argsMap, "query", true)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Extract optional parameters
		limit := parseIntArg(argsMap, "limit", 15)
		language, _ := parseStringArg(argsMap, "language", false)
		filePath, _ := parseStringArg(argsMap, "file_path", false)

		// Build search options
		options := &ExactSearchOptions{
			Limit:    limit,
			Language: language,
			FilePath: filePath,
		}

		// Execute search
		results, err := searcher.Search(ctx, query, options)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}

		// Build response
		response := &CortexExactResponse{
			Query:         query,
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
	Query string `json:"query" jsonschema:"required,description=Bleve query string"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=15"`
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
