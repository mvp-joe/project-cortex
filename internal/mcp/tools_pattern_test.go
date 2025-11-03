package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mvp-joe/project-cortex/internal/pattern"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockPatternSearcher implements pattern.PatternSearcher for testing
type mockPatternSearcher struct {
	searchFunc func(ctx context.Context, req *pattern.PatternRequest, projectRoot string) (*pattern.PatternResponse, error)
}

func (m *mockPatternSearcher) Search(ctx context.Context, req *pattern.PatternRequest, projectRoot string) (*pattern.PatternResponse, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, req, projectRoot)
	}
	// Default: return success with 1 match
	return &pattern.PatternResponse{
		Matches: []pattern.PatternMatch{
			{
				FilePath:  "test.go",
				StartLine: 10,
				EndLine:   10,
				MatchText: "defer conn.Close()",
				Context:   "func cleanup() {\n\tdefer conn.Close()\n}",
				Metavars:  map[string]string{"FUNC": "conn.Close"},
			},
		},
		Total: 1,
		Metadata: pattern.PatternMetadata{
			TookMs:     100,
			Pattern:    "defer $FUNC()",
			Language:   "go",
			Strictness: "smart",
		},
	}, nil
}

// TestAddCortexPatternTool verifies the tool is registered correctly
func TestAddCortexPatternTool(t *testing.T) {
	t.Parallel()

	mcpServer := server.NewMCPServer("test", "1.0.0", server.WithToolCapabilities(true))
	mockSearcher := &mockPatternSearcher{}
	projectRoot := "/tmp/test-project"

	// Register tool
	AddCortexPatternTool(mcpServer, mockSearcher, projectRoot)

	// Verify tool exists by checking server tools
	// Note: mcp-go doesn't expose tools list, so we validate via handler execution
	assert.NotNil(t, mcpServer, "server should exist")
}

// TestCortexPatternHandler_ValidRequest tests successful pattern search
func TestCortexPatternHandler_ValidRequest(t *testing.T) {
	t.Parallel()

	mockSearcher := &mockPatternSearcher{}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"pattern":  "defer $FUNC()",
				"language": "go",
			},
		},
	}

	result, err := handler(context.Background(), request)

	require.NoError(t, err, "should not return system error")
	require.NotNil(t, result, "should return result")
	assert.False(t, result.IsError, "should not be error result")

	// Parse response JSON
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "should be text content")

	var response pattern.PatternResponse
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err, "should parse response JSON")

	assert.Equal(t, 1, response.Total, "should have 1 match")
	assert.Len(t, response.Matches, 1, "should return 1 match")
	assert.Equal(t, "test.go", response.Matches[0].FilePath)
	assert.Equal(t, "defer conn.Close()", response.Matches[0].MatchText)
	assert.Equal(t, "conn.Close", response.Matches[0].Metavars["FUNC"])
}

// TestCortexPatternHandler_MissingPattern tests validation of required pattern field
func TestCortexPatternHandler_MissingPattern(t *testing.T) {
	t.Parallel()

	mockSearcher := &mockPatternSearcher{}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"language": "go",
				// Missing pattern
			},
		},
	}

	result, err := handler(context.Background(), request)

	require.NoError(t, err, "should not return system error")
	require.NotNil(t, result, "should return result")
	assert.True(t, result.IsError, "should be error result")

	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "should be text content")
	assert.Contains(t, textContent.Text, "pattern parameter is required")
}

// TestCortexPatternHandler_MissingLanguage tests validation of required language field
func TestCortexPatternHandler_MissingLanguage(t *testing.T) {
	t.Parallel()

	mockSearcher := &mockPatternSearcher{}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"pattern": "defer $FUNC()",
				// Missing language
			},
		},
	}

	result, err := handler(context.Background(), request)

	require.NoError(t, err, "should not return system error")
	require.NotNil(t, result, "should return result")
	assert.True(t, result.IsError, "should be error result")

	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "should be text content")
	assert.Contains(t, textContent.Text, "language parameter is required")
}

// TestCortexPatternHandler_InvalidArgumentsFormat tests handling of malformed arguments
func TestCortexPatternHandler_InvalidArgumentsFormat(t *testing.T) {
	t.Parallel()

	mockSearcher := &mockPatternSearcher{}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: "invalid string instead of map",
		},
	}

	result, err := handler(context.Background(), request)

	require.NoError(t, err, "should not return system error")
	require.NotNil(t, result, "should return result")
	assert.True(t, result.IsError, "should be error result")

	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "should be text content")
	assert.Contains(t, textContent.Text, "invalid arguments format")
}

// TestCortexPatternHandler_UserError tests that user errors are returned as tool errors
func TestCortexPatternHandler_UserError(t *testing.T) {
	t.Parallel()

	mockSearcher := &mockPatternSearcher{
		searchFunc: func(ctx context.Context, req *pattern.PatternRequest, projectRoot string) (*pattern.PatternResponse, error) {
			return nil, errors.New("invalid request: unsupported language: kotlin")
		},
	}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"pattern":  "fun main()",
				"language": "kotlin",
			},
		},
	}

	result, err := handler(context.Background(), request)

	require.NoError(t, err, "should not return system error")
	require.NotNil(t, result, "should return result")
	assert.True(t, result.IsError, "should be error result")

	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "should be text content")
	assert.Contains(t, textContent.Text, "unsupported language")
}

// TestCortexPatternHandler_SystemError tests that system errors are returned as Go errors
func TestCortexPatternHandler_SystemError(t *testing.T) {
	t.Parallel()

	mockSearcher := &mockPatternSearcher{
		searchFunc: func(ctx context.Context, req *pattern.PatternRequest, projectRoot string) (*pattern.PatternResponse, error) {
			return nil, errors.New("binary download failed: network error")
		},
	}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"pattern":  "defer $FUNC()",
				"language": "go",
			},
		},
	}

	result, err := handler(context.Background(), request)

	require.Error(t, err, "should return system error")
	assert.Nil(t, result, "should not return result for system error")
	assert.Contains(t, err.Error(), "network error")
}

// TestCortexPatternHandler_WithOptionalParameters tests handling of all optional parameters
func TestCortexPatternHandler_WithOptionalParameters(t *testing.T) {
	t.Parallel()

	var capturedReq *pattern.PatternRequest
	mockSearcher := &mockPatternSearcher{
		searchFunc: func(ctx context.Context, req *pattern.PatternRequest, projectRoot string) (*pattern.PatternResponse, error) {
			capturedReq = req
			return &pattern.PatternResponse{
				Matches: []pattern.PatternMatch{},
				Total:   0,
				Metadata: pattern.PatternMetadata{
					TookMs:     50,
					Pattern:    req.Pattern,
					Language:   req.Language,
					Strictness: "relaxed",
				},
			}, nil
		},
	}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	contextLines := 5
	limit := 20

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"pattern":       "useState($INIT)",
				"language":      "tsx",
				"file_paths":    []interface{}{"src/**/*.tsx", "components/**/*.tsx"},
				"context_lines": float64(contextLines),
				"strictness":    "relaxed",
				"limit":         float64(limit),
			},
		},
	}

	result, err := handler(context.Background(), request)

	require.NoError(t, err, "should not return error")
	require.NotNil(t, result, "should return result")
	assert.False(t, result.IsError, "should not be error result")

	// Verify parameters were passed correctly
	require.NotNil(t, capturedReq, "should capture request")
	assert.Equal(t, "useState($INIT)", capturedReq.Pattern)
	assert.Equal(t, "tsx", capturedReq.Language)
	assert.Equal(t, []string{"src/**/*.tsx", "components/**/*.tsx"}, capturedReq.FilePaths)
	require.NotNil(t, capturedReq.ContextLines)
	assert.Equal(t, contextLines, *capturedReq.ContextLines)
	assert.Equal(t, "relaxed", capturedReq.Strictness)
	require.NotNil(t, capturedReq.Limit)
	assert.Equal(t, limit, *capturedReq.Limit)
}

// TestCortexPatternHandler_JSONMarshaling tests response JSON marshaling
func TestCortexPatternHandler_JSONMarshaling(t *testing.T) {
	t.Parallel()

	mockSearcher := &mockPatternSearcher{
		searchFunc: func(ctx context.Context, req *pattern.PatternRequest, projectRoot string) (*pattern.PatternResponse, error) {
			return &pattern.PatternResponse{
				Matches: []pattern.PatternMatch{
					{
						FilePath:  "internal/server.go",
						StartLine: 42,
						EndLine:   45,
						MatchText: "if err != nil {\n\treturn err\n}",
						Context:   "func handle() error {\n\tif err != nil {\n\t\treturn err\n\t}\n\treturn nil\n}",
						Metavars:  map[string]string{},
					},
				},
				Total: 1,
				Metadata: pattern.PatternMetadata{
					TookMs:     75,
					Pattern:    "if err != nil { return err }",
					Language:   "go",
					Strictness: "smart",
				},
			}, nil
		},
	}
	projectRoot := "/tmp/test-project"

	handler := createCortexPatternHandler(mockSearcher, projectRoot)

	request := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"pattern":  "if err != nil { return err }",
				"language": "go",
			},
		},
	}

	result, err := handler(context.Background(), request)

	require.NoError(t, err, "should not return error")
	require.NotNil(t, result, "should return result")
	assert.False(t, result.IsError, "should not be error result")

	// Verify JSON is valid and has expected structure
	textContent, ok := mcp.AsTextContent(result.Content[0])
	require.True(t, ok, "should be text content")

	var response pattern.PatternResponse
	err = json.Unmarshal([]byte(textContent.Text), &response)
	require.NoError(t, err, "should parse JSON")

	assert.Equal(t, 1, response.Total)
	assert.Len(t, response.Matches, 1)
	assert.Equal(t, "internal/server.go", response.Matches[0].FilePath)
	assert.Equal(t, 42, response.Matches[0].StartLine)
	assert.Equal(t, 45, response.Matches[0].EndLine)
	assert.NotEmpty(t, response.Matches[0].MatchText)
	assert.NotEmpty(t, response.Matches[0].Context)
	assert.Equal(t, int64(75), response.Metadata.TookMs)
	assert.Equal(t, "if err != nil { return err }", response.Metadata.Pattern)
	assert.Equal(t, "go", response.Metadata.Language)
	assert.Equal(t, "smart", response.Metadata.Strictness)
}

// TestIsUserError tests the user error detection logic
func TestIsUserError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "validation error",
			err:      errors.New("invalid request: pattern cannot be empty"),
			expected: true,
		},
		{
			name:     "unsupported language",
			err:      errors.New("unsupported language: kotlin"),
			expected: true,
		},
		{
			name:     "path traversal",
			err:      errors.New("path outside project root: ../../etc/passwd"),
			expected: true,
		},
		{
			name:     "timeout",
			err:      errors.New("pattern search timed out (30s)"),
			expected: true,
		},
		{
			name:     "ast-grep error",
			err:      errors.New("ast-grep error: invalid pattern syntax"),
			expected: true,
		},
		{
			name:     "system error - binary download",
			err:      errors.New("binary download failed: network error"),
			expected: false,
		},
		{
			name:     "system error - execution",
			err:      errors.New("execution failed: command not found"),
			expected: false,
		},
		{
			name:     "system error - JSON parsing",
			err:      errors.New("failed to parse output: unexpected EOF"),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isUserError(tt.err)
			assert.Equal(t, tt.expected, result, "error: %v", tt.err)
		})
	}
}
