package mcp

// Implementation Plan:
// 1. AddCortexSearchTool - composable tool registration function
// 2. createCortexSearchHandler - handler factory that captures searcher
// 3. Parse CortexSearchRequest from MCP arguments
// 4. Execute searcher.Query with options
// 5. Build CortexSearchResponse
// 6. Return as JSON text (mcp-go convention)

import (
	"context"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// AddCortexSearchTool registers the cortex_search tool with an MCP server.
// This function is composable - it can be combined with other tool registrations.
func AddCortexSearchTool(s *server.MCPServer, searcher ContextSearcher) {
	tool := mcp.NewTool(
		"cortex_search",
		mcp.WithDescription("Search for relevant context in the project codebase and documentation using semantic search. Returns code chunks, documentation, symbols, and definitions ranked by relevance."),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Natural language search query (e.g., 'authentication implementation', 'error handling patterns')")),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return (1-100, default: 15)")),
		mcp.WithArray("tags",
			mcp.Description("Filter results by tags - must have ALL specified tags (AND logic). Examples: ['go', 'code'], ['documentation', 'architecture']")),
		mcp.WithArray("chunk_types",
			mcp.Description("Filter by chunk types. Options: 'documentation' (README, guides, docs), 'symbols' (code overview), 'definitions' (function signatures), 'data' (constants, configs). Leave empty to search all types.")),
		mcp.WithBoolean("include_stats",
			mcp.Description("Include reload metrics in response (default: false). Shows reload health, chunk count, and error statistics.")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)

	s.AddTool(tool, createCortexSearchHandler(searcher))
}

// createCortexSearchHandler creates the handler function for cortex_search tool.
func createCortexSearchHandler(searcher ContextSearcher) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
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
		tags := parseArrayArg(argsMap, "tags")
		chunkTypes := parseArrayArg(argsMap, "chunk_types")
		includeStats := parseBoolArg(argsMap, "include_stats", false)

		// Build search options
		options := &SearchOptions{
			Limit:      limit,
			Tags:       tags,
			ChunkTypes: chunkTypes,
		}

		// Execute search
		results, err := searcher.Query(ctx, query, options)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}

		// Build response
		response := &CortexSearchResponse{
			Results: results,
			Total:   len(results),
			Metadata: SearchResponseMetadata{
				TookMs: int(time.Since(startTime).Milliseconds()),
				Source: "search",
			},
		}

		// Include metrics if requested
		if includeStats {
			metrics := searcher.GetMetrics()
			response.Metrics = &metrics
		}

		// Marshal and return response
		return marshalToolResponse(response)
	}
}
