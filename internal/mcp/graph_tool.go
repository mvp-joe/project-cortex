package mcp

import (
	"context"
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
		mcp.WithDescription("Query structural code relationships for refactoring, impact analysis, and dependency exploration. Operations: callers (who calls this function), callees (what does this function call), dependencies (packages this imports), dependents (packages importing this), type_usages (where is this type used)."),
		mcp.WithString("operation",
			mcp.Required(),
			mcp.Enum("callers", "callees", "dependencies", "dependents", "type_usages"),
			mcp.Description("Type of query: 'callers', 'callees', 'dependencies', 'dependents', or 'type_usages'")),
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
		argsMap, errResult := parseToolArguments(request)
		if errResult != nil {
			return errResult, nil
		}

		// Extract operation (required)
		operation, err := parseStringArg(argsMap, "operation", true)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Validate operation
		validOps := map[string]graph.QueryOperation{
			"callers":      graph.OperationCallers,
			"callees":      graph.OperationCallees,
			"dependencies": graph.OperationDependencies,
			"dependents":   graph.OperationDependents,
			"type_usages":  graph.OperationTypeUsages,
		}
		graphOp, valid := validOps[operation]
		if !valid {
			return mcp.NewToolResultError(fmt.Sprintf("invalid operation: %s (must be one of: callers, callees, dependencies, dependents, type_usages)", operation)), nil
		}

		// Extract target (required)
		target, err := parseStringArg(argsMap, "target", true)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		// Build query request with defaults
		req := &graph.QueryRequest{
			Operation:      graphOp,
			Target:         target,
			IncludeContext: parseBoolArg(argsMap, "include_context", true),
			ContextLines:   parseClampedInt(argsMap, "context_lines", 3, 0, 20),
			Depth:          parseClampedInt(argsMap, "depth", 1, 1, 10),
			MaxResults:     parseClampedInt(argsMap, "max_results", 100, 1, 500),
		}

		// Execute query
		response, err := querier.Query(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("graph query failed: %w", err)
		}

		// Marshal and return response
		return marshalToolResponse(response)
	}
}
