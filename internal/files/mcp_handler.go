package files

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	mcputils "github.com/mvp-joe/project-cortex/internal/mcp-utils"
)

// FilesToolRequest represents the request structure for the cortex_files MCP tool.
// Note: Query uses json.RawMessage to handle both map[string]interface{} and json.RawMessage inputs.
type FilesToolRequest struct {
	Operation string          `json:"operation"`
	Query     json.RawMessage `json:"query"`
}

// CreateFilesToolHandler creates an MCP tool handler for cortex_files queries.
// The handler accepts a database connection and returns a closure that processes MCP requests.
// This design allows the handler to be thread-safe by not sharing mutable state between invocations.
func CreateFilesToolHandler(db *sql.DB) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Validate arguments format early
		if _, ok := request.GetRawArguments().(map[string]interface{}); !ok {
			return mcp.NewToolResultError("invalid arguments format: expected object"), nil
		}

		// Parse and bind request arguments using CoerceBindArguments
		var req FilesToolRequest
		if err := mcputils.CoerceBindArguments(request, &req); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid arguments: %v", err)), nil
		}

		// Validate operation field is present and non-empty
		if req.Operation == "" {
			return mcp.NewToolResultError("operation field is required and must be a string"), nil
		}

		// Validate operation type (currently only "query" is supported)
		if req.Operation != "query" {
			return mcp.NewToolResultError(fmt.Sprintf("unsupported operation: %s (only 'query' is supported)", req.Operation)), nil
		}

		// Validate query field is present
		if len(req.Query) == 0 {
			return mcp.NewToolResultError("query field is required for 'query' operation"), nil
		}

		// Unmarshal query to QueryDefinition
		var queryDef QueryDefinition
		if err := json.Unmarshal(req.Query, &queryDef); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("invalid query definition: %v", err)), nil
		}

		// Create executor and execute query
		executor := NewExecutor(db)
		result, err := executor.Execute(&queryDef)
		if err != nil {
			// Database/execution errors are returned as errors, not error results
			// This allows mcp-go to handle them according to JSON-RPC spec
			return nil, fmt.Errorf("query execution failed: %w", err)
		}

		// Marshal result to JSON
		jsonData, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result: %w", err)
		}

		// Return as text result (mcp-go convention)
		return mcp.NewToolResultText(string(jsonData)), nil
	}
}
