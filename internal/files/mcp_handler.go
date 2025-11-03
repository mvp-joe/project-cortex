package files

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// FilesToolRequest represents the request structure for the cortex_files MCP tool.
type FilesToolRequest struct {
	Operation string          `json:"operation"`
	Query     json.RawMessage `json:"query"`
}

// CreateFilesToolHandler creates an MCP tool handler for cortex_files queries.
// The handler accepts a database connection and returns a closure that processes MCP requests.
// This design allows the handler to be thread-safe by not sharing mutable state between invocations.
func CreateFilesToolHandler(db *sql.DB) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Parse request arguments from JSON string to map
		argsMap, ok := request.Params.Arguments.(map[string]interface{})
		if !ok {
			return mcp.NewToolResultError("invalid arguments format: expected object"), nil
		}

		// Extract operation field
		operation, ok := argsMap["operation"].(string)
		if !ok {
			return mcp.NewToolResultError("operation field is required and must be a string"), nil
		}

		// Validate operation type (currently only "query" is supported)
		if operation != "query" {
			return mcp.NewToolResultError(fmt.Sprintf("unsupported operation: %s (only 'query' is supported)", operation)), nil
		}

		// Extract query field
		queryData, ok := argsMap["query"]
		if !ok {
			return mcp.NewToolResultError("query field is required for 'query' operation"), nil
		}

		// Marshal query data back to JSON for unmarshal into QueryDefinition
		queryJSON, err := json.Marshal(queryData)
		if err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("failed to parse query field: %v", err)), nil
		}

		// Unmarshal to QueryDefinition
		var queryDef QueryDefinition
		if err := json.Unmarshal(queryJSON, &queryDef); err != nil {
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
