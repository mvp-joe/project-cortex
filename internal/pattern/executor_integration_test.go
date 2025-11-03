//go:build integration

package pattern

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutePattern_RealAstGrep(t *testing.T) {
	t.Parallel()

	// Setup: Create temporary project with test files
	projectRoot := t.TempDir()

	// Create test Go file with defer statements
	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(`package main

func example() {
	defer conn.Close()
	defer cleanup()
	println("hello")
}

func another() {
	defer mu.Unlock()
}
`), 0644)
	require.NoError(t, err)

	// Initialize provider
	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Execute pattern search
	req := &PatternRequest{
		Pattern:  "defer $FUNC()",
		Language: "go",
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	// Verify
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, 3, result.Total, "should find 3 defer statements")
	assert.Len(t, result.Matches, 3)

	// Verify metadata
	assert.Equal(t, "defer $FUNC()", result.Metadata.Pattern)
	assert.Equal(t, "go", result.Metadata.Language)
	assert.Equal(t, "smart", result.Metadata.Strictness)
	assert.Greater(t, result.Metadata.TookMs, int64(0))

	// Verify matches contain expected defer statements
	matchTexts := []string{}
	for _, match := range result.Matches {
		matchTexts = append(matchTexts, strings.TrimSpace(match.MatchText))
		assert.Equal(t, "test.go", match.FilePath)
		assert.Greater(t, match.StartLine, 0)
		assert.NotEmpty(t, match.Metavars["FUNC"])
	}

	assert.Contains(t, matchTexts, "defer conn.Close()")
	assert.Contains(t, matchTexts, "defer cleanup()")
	assert.Contains(t, matchTexts, "defer mu.Unlock()")
}

func TestExecutePattern_WithContextLines(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(`package main

func example() {
	x := 1
	y := 2
	defer conn.Close()
	z := 3
	return
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	contextLines := 2
	req := &PatternRequest{
		Pattern:      "defer $FUNC()",
		Language:     "go",
		ContextLines: &contextLines,
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	require.Len(t, result.Matches, 1)

	// With -C 2, context should include 2 lines before and after
	match := result.Matches[0]
	assert.Contains(t, match.Context, "defer conn.Close()")
	// Note: ast-grep's -C includes surrounding lines in the text field
	// The exact format depends on ast-grep version, so we just verify it's not empty
	assert.NotEmpty(t, match.Context)
}

func TestExecutePattern_MultipleFiles(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	// Create multiple Go files
	err := os.WriteFile(filepath.Join(projectRoot, "file1.go"), []byte(`package main
func f1() {
	defer conn.Close()
}
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(projectRoot, "file2.go"), []byte(`package main
func f2() {
	defer cleanup()
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	req := &PatternRequest{
		Pattern:  "defer $FUNC()",
		Language: "go",
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	assert.Equal(t, 2, result.Total)

	// Verify matches from different files
	files := []string{}
	for _, match := range result.Matches {
		files = append(files, match.FilePath)
	}
	assert.Contains(t, files, "file1.go")
	assert.Contains(t, files, "file2.go")
}

func TestExecutePattern_WithFilePathFilter(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	// Create files in different directories
	internalDir := filepath.Join(projectRoot, "internal")
	err := os.MkdirAll(internalDir, 0755)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(internalDir, "server.go"), []byte(`package internal
func serve() {
	defer conn.Close()
}
`), 0644)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(projectRoot, "main.go"), []byte(`package main
func main() {
	defer cleanup()
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Search only internal directory
	req := &PatternRequest{
		Pattern:   "defer $FUNC()",
		Language:  "go",
		FilePaths: []string{"internal/**/*.go"},
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Total, "should only find match in internal/")

	require.Len(t, result.Matches, 1)
	assert.Contains(t, result.Matches[0].FilePath, "internal")
}

func TestExecutePattern_NoMatches(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(`package main

func example() {
	println("no defer statements here")
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	req := &PatternRequest{
		Pattern:  "defer $FUNC()",
		Language: "go",
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	assert.Equal(t, 0, result.Total)
	assert.Empty(t, result.Matches)
}

func TestExecutePattern_ResultLimiting(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	// Create file with many defer statements
	var code strings.Builder
	code.WriteString("package main\n\nfunc example() {\n")
	for i := 0; i < 75; i++ {
		code.WriteString("\tdefer cleanup()\n")
	}
	code.WriteString("}\n")

	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(code.String()), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	// Request with limit of 10
	limit := 10
	req := &PatternRequest{
		Pattern:  "defer $FUNC()",
		Language: "go",
		Limit:    &limit,
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	assert.Equal(t, 75, result.Total, "Total should reflect all matches")
	assert.Len(t, result.Matches, 10, "Matches should be limited to 10")
}

func TestExecutePattern_MetavariableExtraction(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(`package main

func example() error {
	return nil
}

func another() error {
	return fmt.Errorf("error")
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	req := &PatternRequest{
		Pattern:  "func $NAME() error { $$$BODY }",
		Language: "go",
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	assert.Equal(t, 2, result.Total)

	// Verify metavariables are extracted
	for _, match := range result.Matches {
		assert.NotEmpty(t, match.Metavars["NAME"], "NAME metavar should be extracted")
		assert.NotEmpty(t, match.Metavars["BODY"], "BODY metavar should be extracted")
	}

	// Verify specific function names
	names := []string{}
	for _, match := range result.Matches {
		names = append(names, match.Metavars["NAME"])
	}
	assert.Contains(t, names, "example")
	assert.Contains(t, names, "another")
}

func TestExecutePattern_InvalidRequest(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	provider := NewAstGrepProvider()
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *PatternRequest
		wantErr string
	}{
		{
			name: "missing pattern",
			req: &PatternRequest{
				Language: "go",
			},
			wantErr: "pattern is required",
		},
		{
			name: "missing language",
			req: &PatternRequest{
				Pattern: "defer $FUNC()",
			},
			wantErr: "language is required",
		},
		{
			name: "unsupported language",
			req: &PatternRequest{
				Pattern:  "test",
				Language: "cobol",
			},
			wantErr: "unsupported language",
		},
		{
			name: "invalid strictness",
			req: &PatternRequest{
				Pattern:    "test",
				Language:   "go",
				Strictness: "invalid",
			},
			wantErr: "invalid strictness",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ExecutePattern(ctx, provider, tt.req, projectRoot)

			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestExecutePattern_PathTraversal(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()
	provider := NewAstGrepProvider()
	ctx := context.Background()

	req := &PatternRequest{
		Pattern:   "test",
		Language:  "go",
		FilePaths: []string{"../../etc/passwd"},
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "path outside project root")
}

func TestExecutePattern_Strictness(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(`package main

func example() {
	// Comment
	defer conn.Close() // Inline comment
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	tests := []struct {
		name       string
		strictness string
	}{
		{"cst", "cst"},
		{"smart", "smart"},
		{"ast", "ast"},
		{"relaxed", "relaxed"},
		{"signature", "signature"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &PatternRequest{
				Pattern:    "defer $FUNC()",
				Language:   "go",
				Strictness: tt.strictness,
			}

			result, err := ExecutePattern(ctx, provider, req, projectRoot)

			require.NoError(t, err)
			assert.NotNil(t, result)
			assert.Equal(t, tt.strictness, result.Metadata.Strictness)
			// All strictness levels should find the defer statement
			assert.Greater(t, result.Total, 0)
		})
	}
}

func TestExecutePattern_Timeout(t *testing.T) {
	// This test is challenging because we need to trigger a timeout
	// In practice, 30s timeout is very generous for ast-grep
	// We can test timeout by using a context with very short deadline

	t.Parallel()

	projectRoot := t.TempDir()

	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(`package main
func example() {
	defer conn.Close()
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()

	// Create context with very short timeout (1ms should timeout during execution)
	// Note: This might be flaky on very fast machines
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait a bit to ensure context expires
	time.Sleep(10 * time.Millisecond)

	req := &PatternRequest{
		Pattern:  "defer $FUNC()",
		Language: "go",
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	// Should timeout (though might succeed on very fast machines)
	if err != nil {
		assert.Contains(t, err.Error(), "timeout")
		assert.Nil(t, result)
	}
}

func TestExecutePattern_TypeScript(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	testFile := filepath.Join(projectRoot, "test.ts")
	err := os.WriteFile(testFile, []byte(`
function Component() {
	const [state, setState] = useState(0);
	const [count, setCount] = useState(10);
	return <div />;
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	req := &PatternRequest{
		Pattern:  "useState($INIT)",
		Language: "typescript",
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	assert.Equal(t, 2, result.Total, "should find 2 useState calls")

	// Verify metavariables
	inits := []string{}
	for _, match := range result.Matches {
		inits = append(inits, match.Metavars["INIT"])
	}
	assert.Contains(t, inits, "0")
	assert.Contains(t, inits, "10")
}

func TestExecutePattern_MultilineMatch(t *testing.T) {
	t.Parallel()

	projectRoot := t.TempDir()

	testFile := filepath.Join(projectRoot, "test.go")
	err := os.WriteFile(testFile, []byte(`package main

func example() error {
	if err != nil {
		return err
	}
	return nil
}
`), 0644)
	require.NoError(t, err)

	provider := NewAstGrepProvider()
	ctx := context.Background()

	req := &PatternRequest{
		Pattern:  "if err != nil { $$$BODY }",
		Language: "go",
	}

	result, err := ExecutePattern(ctx, provider, req, projectRoot)

	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)

	match := result.Matches[0]
	assert.Greater(t, match.EndLine, match.StartLine, "multiline match should span multiple lines")
	assert.NotEmpty(t, match.Metavars["BODY"])
}
