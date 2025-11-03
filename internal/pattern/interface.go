package pattern

import "context"

// PatternSearcher defines the interface for AST pattern search functionality.
// Implementations must handle binary management, command execution, and result parsing.
//
// This interface enables:
// - Dependency injection for testing (mock implementations)
// - MCP tool integration without concrete type dependencies
// - Future alternative pattern search backends
type PatternSearcher interface {
	// Search executes a pattern search and returns structured results.
	//
	// Parameters:
	// - ctx: Context for cancellation and timeout control
	// - req: Pattern search request with pattern, language, and filters
	// - projectRoot: Absolute path to project root for file path resolution
	//
	// Returns:
	// - PatternResponse: Structured matches with metadata
	// - error: Validation errors, execution failures, or timeouts
	//
	// Error types:
	// - User errors (invalid pattern, unsupported language): Should be shown to LLM
	// - System errors (binary unavailable, execution failed): Internal failures
	Search(ctx context.Context, req *PatternRequest, projectRoot string) (*PatternResponse, error)
}
