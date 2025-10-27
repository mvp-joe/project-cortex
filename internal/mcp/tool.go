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
	"encoding/json"
	"fmt"

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
	)

	s.AddTool(tool, createCortexSearchHandler(searcher))
}

// createCortexSearchHandler creates the handler function for cortex_search tool.
func createCortexSearchHandler(searcher ContextSearcher) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse arguments
		var args CortexSearchRequest
		argsMap, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		// Extract query (required)
		query, ok := argsMap["query"].(string)
		if !ok || query == "" {
			return mcp.NewToolResultError("query parameter is required"), nil
		}
		args.Query = query

		// Extract limit (optional)
		if limit, ok := argsMap["limit"].(float64); ok {
			args.Limit = int(limit)
		} else {
			args.Limit = 15
		}

		// Extract tags (optional)
		if tags, ok := argsMap["tags"].([]interface{}); ok {
			args.Tags = make([]string, 0, len(tags))
			for _, tag := range tags {
				if tagStr, ok := tag.(string); ok {
					args.Tags = append(args.Tags, tagStr)
				}
			}
		}

		// Extract chunk_types (optional)
		if chunkTypes, ok := argsMap["chunk_types"].([]interface{}); ok {
			args.ChunkTypes = make([]string, 0, len(chunkTypes))
			for _, ct := range chunkTypes {
				if ctStr, ok := ct.(string); ok {
					args.ChunkTypes = append(args.ChunkTypes, ctStr)
				}
			}
		}

		// Build search options
		options := &SearchOptions{
			Limit:      args.Limit,
			Tags:       args.Tags,
			ChunkTypes: args.ChunkTypes,
		}

		// Execute search
		results, err := searcher.Query(ctx, args.Query, options)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}

		// Build response
		response := &CortexSearchResponse{
			Results: results,
			Total:   len(results),
		}

		// Marshal to JSON
		jsonData, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Return as text result (mcp-go convention)
		return mcp.NewToolResultText(string(jsonData)), nil
	}
}
