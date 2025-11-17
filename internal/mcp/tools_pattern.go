package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	mcputils "github.com/mvp-joe/project-cortex/internal/mcp-utils"
	"github.com/mvp-joe/project-cortex/internal/pattern"
)

// AddCortexPatternTool registers the cortex_pattern tool with an MCP server.
// This function is composable - it can be combined with other tool registrations.
//
// The cortex_pattern tool enables structural AST pattern matching using ast-grep.
// Unlike text search (cortex_exact) or semantic search (cortex_search), it understands
// code structure for finding complex patterns, anti-patterns, and language-specific idioms.
func AddCortexPatternTool(s *server.MCPServer, searcher pattern.PatternSearcher, projectRoot string) {
	tool := mcp.NewTool(
		"cortex_pattern",
		mcp.WithDescription("Search code using structural AST patterns. Use for finding anti-patterns, code smells, language-specific idioms, and complex structural patterns that text search cannot handle."),
		mcp.WithString("pattern",
			mcp.Required(),
			mcp.Description("AST pattern with metavariables (e.g., 'defer $FUNC()' or 'useState($INIT)')")),
		mcp.WithString("language",
			mcp.Required(),
			mcp.Description("Target language: go, typescript, javascript, tsx, jsx, python, rust, c, cpp, java, php, ruby")),
		mcp.WithArray("file_paths",
			mcp.Description("Optional file/glob filters (e.g., ['internal/**/*.go'])")),
		mcp.WithNumber("context_lines",
			mcp.Description("Lines of context before/after match (0-10, default: 3)")),
		mcp.WithString("strictness",
			mcp.Description("Matching algorithm: cst, smart (default), ast, relaxed, signature")),
		mcp.WithNumber("limit",
			mcp.Description("Maximum results to return (1-100, default: 50)")),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)

	handler := createCortexPatternHandler(searcher, projectRoot)
	s.AddTool(tool, handler)
}

// createCortexPatternHandler creates the handler function for cortex_pattern tool.
func createCortexPatternHandler(searcher pattern.PatternSearcher, projectRoot string) func(context.Context, mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// Check if arguments are in the correct format (map)
		// GetRawArguments() returns the raw Arguments field before conversion
		if _, ok := request.GetRawArguments().(map[string]interface{}); !ok {
			return mcp.NewToolResultError("invalid arguments format"), nil
		}

		// Bind and validate arguments using CoerceBindArguments
		var req pattern.PatternRequest
		if err := mcputils.CoerceBindArguments(request, &req); err != nil {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid arguments: %v", err)), nil
		}

		// Validate required fields (to provide clear error messages)
		if req.Pattern == "" {
			return mcp.NewToolResultError("pattern parameter is required"), nil
		}
		if req.Language == "" {
			return mcp.NewToolResultError("language parameter is required"), nil
		}

		// Execute search (additional validation happens in searcher.Search via ValidateRequest)
		result, err := searcher.Search(ctx, &req, projectRoot)
		if err != nil {
			// Distinguish user errors from system errors
			if isUserError(err) {
				return mcp.NewToolResultError(err.Error()), nil
			}
			return nil, err
		}

		// Marshal and return response
		return marshalToolResponse(result)
	}
}

// isUserError determines if an error should be shown to the LLM (user error)
// vs treated as an internal system error.
//
// User errors:
// - Validation failures (invalid pattern, unsupported language)
// - Path traversal attempts (outside project root)
// - Timeouts (pattern search too slow)
// - Pattern syntax errors (ast-grep errors)
//
// System errors:
// - Binary download failures
// - Execution failures (non-timeout)
// - JSON parsing failures
func isUserError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	// User errors that should be shown to LLM
	return strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "unsupported") ||
		strings.Contains(msg, "outside project root") ||
		strings.Contains(msg, "timed out") ||
		strings.Contains(msg, "ast-grep error")
}
