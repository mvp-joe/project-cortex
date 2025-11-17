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
	mcputils "github.com/mvp-joe/project-cortex/internal/mcp-utils"
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

		// Parse and bind arguments using mcputils
		var req CortexSearchRequest
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
		options := &SearchOptions{
			Limit:      req.Limit,
			Tags:       req.Tags,
			ChunkTypes: req.ChunkTypes,
		}

		// Execute search
		results, err := searcher.Query(ctx, req.Query, options)
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
		if req.IncludeStats {
			metrics := searcher.GetMetrics()
			response.Metrics = &metrics
		}

		// Marshal and return response
		return marshalToolResponse(response)
	}
}
