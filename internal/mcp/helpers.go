package mcp

import (
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/philippgille/chromem-go"
)

// chunkToChromemMetadata converts a ContextChunk's metadata to chromem-go's metadata format.
// It extracts chunk_type and all string-valued metadata fields (including tag_0, tag_1, etc.).
func chunkToChromemMetadata(chunk *ContextChunk) map[string]string {
	metadata := make(map[string]string)
	if chunk.ChunkType != "" {
		metadata["chunk_type"] = chunk.ChunkType
	}
	for k, v := range chunk.Metadata {
		if str, ok := v.(string); ok {
			metadata[k] = str
		}
	}
	return metadata
}

// chunkToChromemDocument converts a ContextChunk to a chromem.Document.
// This is the standard conversion used when adding chunks to chromem-go collections.
func chunkToChromemDocument(chunk *ContextChunk) chromem.Document {
	return chromem.Document{
		ID:        chunk.ID,
		Content:   chunk.Text,
		Embedding: chunk.Embedding,
		Metadata:  chunkToChromemMetadata(chunk),
	}
}

// convertMetadata converts chromem-go metadata map to a generic metadata map.
// It filters out chunk_type (already extracted) and tag_* keys (already extracted into Tags array).
func convertMetadata(meta map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range meta {
		// Skip already-extracted fields and tag_* keys
		if k == "chunk_type" {
			continue
		}
		// Skip tag_0, tag_1, tag_2, etc. (already extracted into Tags array)
		if len(k) >= 5 && k[:4] == "tag_" {
			continue
		}
		result[k] = v
	}
	return result
}

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
