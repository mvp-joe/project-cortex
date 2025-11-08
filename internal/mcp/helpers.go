package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
)

// parseToolArguments validates and extracts the arguments map from an MCP tool request.
// Returns the arguments map or an error result if validation fails.
func parseToolArguments(request mcp.CallToolRequest) (map[string]interface{}, *mcp.CallToolResult) {
	argsMap, ok := request.Params.Arguments.(map[string]interface{})
	if !ok {
		return nil, mcp.NewToolResultError("invalid arguments format")
	}
	return argsMap, nil
}

// marshalToolResponse marshals a response object to JSON and returns it as an MCP tool result.
// This helper eliminates the repeated pattern of json.Marshal + error handling + NewToolResultText.
func marshalToolResponse(response interface{}) (*mcp.CallToolResult, error) {
	jsonData, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}
	return mcp.NewToolResultText(string(jsonData)), nil
}
