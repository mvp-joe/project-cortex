package pattern

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

const (
	// ExecutionTimeout is the maximum time allowed for ast-grep execution
	ExecutionTimeout = 30 * time.Second
)

// ExecutePattern executes a pattern search using ast-grep and returns structured results.
// This is the main entry point for pattern search functionality.
//
// Process:
// 1. Ensure ast-grep binary is installed
// 2. Validate request parameters
// 3. Build safe command arguments
// 4. Execute with 30s timeout
// 5. Parse JSON output
// 6. Transform to PatternResponse format
// 7. Apply result limiting
//
// Errors:
// - Binary not available: Installation or verification failed
// - Invalid request: Validation error (wrong language, bad paths, etc.)
// - Timeout: Pattern search exceeded 30s
// - Execution failed: ast-grep returned error
// - Parse failed: Invalid JSON output from ast-grep
func ExecutePattern(ctx context.Context, provider *AstGrepProvider, req *PatternRequest, projectRoot string) (*PatternResponse, error) {
	// 1. Ensure binary installed
	if err := provider.ensureBinaryInstalled(ctx); err != nil {
		return nil, fmt.Errorf("binary not available: %w", err)
	}

	// 2. Validate request
	if err := ValidateRequest(req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}

	// 3. Build command
	args, err := BuildCommand(req, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("command build failed: %w", err)
	}

	// 4. Execute with timeout
	execCtx, cancel := context.WithTimeout(ctx, ExecutionTimeout)
	defer cancel()

	cmd := exec.CommandContext(execCtx, provider.binaryPath, args...)
	cmd.Dir = projectRoot

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	startTime := time.Now()
	err = cmd.Run()
	tookMs := time.Since(startTime).Milliseconds()

	// 5. Handle errors
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("pattern search timed out (30s)")
		}
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("ast-grep error: %s", stderr.String())
		}
		return nil, fmt.Errorf("execution failed: %w", err)
	}

	// 6. Parse JSON output
	result, err := parseAstGrepOutput(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to parse output: %w", err)
	}

	// 7. Transform to response format
	response := transformToResponse(result, req, tookMs)

	// 8. Apply limit
	response = applyLimit(response, req)

	return response, nil
}

// parseAstGrepOutput parses ast-grep's JSON compact format output.
//
// ast-grep JSON compact format (v0.29.0+):
// [
//   {
//     "text": "defer conn.Close()",
//     "range": {"start": {"line": 2, "column": 1}, "end": {"line": 2, "column": 19}},
//     "file": "test.go",
//     "metaVariables": {"single": {"FUNC": {"text": "conn.Close", "range": {...}}}}
//   }
// ]
//
// Note: ast-grep returns an array directly, not wrapped in an object.
func parseAstGrepOutput(data []byte) (*AstGrepResult, error) {
	// Empty output means no matches (not an error)
	if len(data) == 0 {
		return &AstGrepResult{Matches: []AstGrepMatch{}}, nil
	}

	// ast-grep --json=compact returns an array directly
	var matches []AstGrepMatch
	if err := json.Unmarshal(data, &matches); err != nil {
		return nil, fmt.Errorf("invalid JSON output: %w", err)
	}

	return &AstGrepResult{Matches: matches}, nil
}

// transformToResponse converts ast-grep's raw output to our PatternResponse format.
//
// Transformations:
// - Extract metavariables as simple map[string]string (just the text, not full node)
// - Extract start/end line from range
// - Set context to matched text (ast-grep -C provides this in the text field)
// - Populate metadata with query parameters and timing
func transformToResponse(result *AstGrepResult, req *PatternRequest, tookMs int64) *PatternResponse {
	matches := make([]PatternMatch, len(result.Matches))

	for i, match := range result.Matches {
		// Extract metavariable text values (ignore line numbers)
		metavars := make(map[string]string)
		for name, metavar := range match.MetaVariables.Single {
			metavars[name] = metavar.Text
		}

		matches[i] = PatternMatch{
			FilePath:  match.File,
			StartLine: match.Range.Start.Line,
			EndLine:   match.Range.End.Line,
			MatchText: match.Text,
			Context:   match.Text, // ast-grep -C includes context in text field
			Metavars:  metavars,
		}
	}

	return &PatternResponse{
		Matches: matches,
		Total:   len(matches),
		Metadata: PatternMetadata{
			TookMs:     tookMs,
			Pattern:    req.Pattern,
			Language:   req.Language,
			Strictness: GetStrictness(req),
		},
	}
}

// applyLimit applies the result limit from the request.
// ast-grep doesn't have a --limit flag, so we post-process results.
//
// Behavior:
// - If matches <= limit: Return all matches
// - If matches > limit: Return first N matches, but Total reflects original count
//
// This allows LLMs to see "found 150 results, showing first 50" and refine queries.
func applyLimit(response *PatternResponse, req *PatternRequest) *PatternResponse {
	limit := GetLimit(req)

	// No limiting needed
	if len(response.Matches) <= limit {
		return response
	}

	// Apply limit but preserve total count
	return &PatternResponse{
		Matches:  response.Matches[:limit],
		Total:    response.Total, // Keep original total
		Metadata: response.Metadata,
	}
}
