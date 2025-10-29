package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mvp-joe/project-cortex/internal/graph"
)

// GraphQuerier is the interface for graph query operations.
type GraphQuerier interface {
	Query(ctx context.Context, req *graph.QueryRequest) (*graph.QueryResponse, error)
	Close() error
}

// CortexGraphRequest represents the MCP tool request parameters.
type CortexGraphRequest struct {
	Operation      string `json:"operation"`       // "callers", "callees", "dependencies", "dependents"
	Target         string `json:"target"`          // Target identifier to query
	IncludeContext bool   `json:"include_context"` // Whether to include code snippets
	ContextLines   int    `json:"context_lines"`   // Number of context lines (default: 3)
	Depth          int    `json:"depth"`           // Traversal depth (default: 1)
	MaxResults     int    `json:"max_results"`     // Maximum results (default: 100)
}

// AddCortexGraphTool registers the cortex_graph tool with an MCP server.
func AddCortexGraphTool(s *server.MCPServer, querier GraphQuerier) {
	tool := mcp.NewTool(
		"cortex_graph",
		mcp.WithDescription("Query structural code relationships for refactoring, impact analysis, and dependency exploration. Supports operations: callers (who calls this function), callees (what does this function call), dependencies (what packages does this import), dependents (what packages import this)."),
		mcp.WithString("operation",
			mcp.Required(),
			mcp.Description("Type of query: 'callers', 'callees', 'dependencies', or 'dependents'")),
		mcp.WithString("target",
			mcp.Required(),
			mcp.Description("Target identifier (e.g., 'embed.Provider', 'localProvider.Embed', 'internal/mcp')")),
		mcp.WithBoolean("include_context",
			mcp.Description("Include code snippets in results (default: true)")),
		mcp.WithNumber("context_lines",
			mcp.Description("Number of context lines around code (default: 3, max: 20)")),
		mcp.WithNumber("depth",
			mcp.Description("Traversal depth for recursive queries (default: 1, max: 10)")),
		mcp.WithNumber("max_results",
			mcp.Description("Maximum number of results to return (default: 100, max: 500)")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)

	s.AddTool(tool, createCortexGraphHandler(querier))
}

// createCortexGraphHandler creates the handler function for cortex_graph tool.
func createCortexGraphHandler(querier GraphQuerier) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse arguments
		argsMap, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		// Extract operation (required)
		operation, ok := argsMap["operation"].(string)
		if !ok || operation == "" {
			return mcp.NewToolResultError("operation parameter is required"), nil
		}

		// Validate operation
		validOps := map[string]graph.QueryOperation{
			"callers":      graph.OperationCallers,
			"callees":      graph.OperationCallees,
			"dependencies": graph.OperationDependencies,
			"dependents":   graph.OperationDependents,
		}
		graphOp, valid := validOps[operation]
		if !valid {
			return mcp.NewToolResultError(fmt.Sprintf("invalid operation: %s (must be one of: callers, callees, dependencies, dependents)", operation)), nil
		}

		// Extract target (required)
		target, ok := argsMap["target"].(string)
		if !ok || target == "" {
			return mcp.NewToolResultError("target parameter is required"), nil
		}

		// Build query request
		req := &graph.QueryRequest{
			Operation:      graphOp,
			Target:         target,
			IncludeContext: true, // default
			ContextLines:   3,    // default
			Depth:          1,    // default
			MaxResults:     100,  // default
		}

		// Extract optional parameters
		if includeContext, ok := argsMap["include_context"].(bool); ok {
			req.IncludeContext = includeContext
		}

		if contextLines, ok := argsMap["context_lines"].(float64); ok {
			lines := int(contextLines)
			if lines < 0 {
				lines = 0
			} else if lines > 20 {
				lines = 20
			}
			req.ContextLines = lines
		}

		if depth, ok := argsMap["depth"].(float64); ok {
			d := int(depth)
			if d < 1 {
				d = 1
			} else if d > 10 {
				d = 10
			}
			req.Depth = d
		}

		if maxResults, ok := argsMap["max_results"].(float64); ok {
			max := int(maxResults)
			if max < 1 {
				max = 1
			} else if max > 500 {
				max = 500
			}
			req.MaxResults = max
		}

		// Execute query
		response, err := querier.Query(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("graph query failed: %w", err)
		}

		// Marshal response to JSON
		jsonData, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response: %w", err)
		}

		// Return as text result
		return mcp.NewToolResultText(string(jsonData)), nil
	}
}
