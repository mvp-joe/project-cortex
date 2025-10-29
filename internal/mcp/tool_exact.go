package mcp

import (
	"context"
	"encoding/json"
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
		mcp.WithDescription(`Full-text keyword search using bleve query syntax.

Supports:
- Field scoping: text:provider, tags:go, chunk_type:definitions, file_path:auth
- Boolean operators: AND, OR, NOT, +required, -excluded
- Phrase search: "error handling"
- Wildcards: Prov* (prefix matching)
- Fuzzy: Provdier~1 (edit distance)
- Combinations: text:handler AND tags:go AND -file_path:test

Default: Searches 'text' field only to avoid path/metadata noise.

Examples:
- text:Provider - Find "Provider" in code/docs
- text:handler AND tags:go - Go handlers only
- text:"error handling" - Exact phrase
- (text:handler OR text:controller) AND -tags:test - Exclude tests`),
		mcp.WithString("query",
			mcp.Required(),
			mcp.Description("Bleve query string with field scoping and boolean operators")),
		mcp.WithNumber("limit",
			mcp.Description("Maximum number of results to return (1-100, default: 15)")),
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
		var args CortexExactRequest
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

		// Execute search
		results, err := searcher.Search(ctx, args.Query, args.Limit)
		if err != nil {
			return nil, fmt.Errorf("search failed: %w", err)
		}

		// Build response
		response := &CortexExactResponse{
			Query:         args.Query,
			Results:       results,
			TotalFound:    len(results),
			TotalReturned: len(results),
			Metadata: ExactResponseMetadata{
				TookMs: int(time.Since(startTime).Milliseconds()),
				Source: "exact",
			},
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

// CortexExactRequest represents the JSON request schema for the cortex_exact MCP tool.
type CortexExactRequest struct {
	Query string `json:"query" jsonschema:"required,description=Bleve query string"`
	Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=15"`
}

// CortexExactResponse represents the JSON response schema for the cortex_exact MCP tool.
type CortexExactResponse struct {
	Query         string                 `json:"query"`
	Results       []*ExactSearchResult   `json:"results"`
	TotalFound    int                    `json:"total_found"`
	TotalReturned int                    `json:"total_returned"`
	Metadata      ExactResponseMetadata  `json:"metadata"`
}

// ExactResponseMetadata contains timing and source information.
type ExactResponseMetadata struct {
	TookMs int    `json:"took_ms"`
	Source string `json:"source"` // "exact"
}
