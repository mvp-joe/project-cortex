package pattern

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseAstGrepOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       []byte
		wantMatches int
		wantErr     bool
		validate    func(t *testing.T, result *AstGrepResult)
	}{
		{
			name:        "empty output",
			input:       nil,
			wantMatches: 0,
			wantErr:     false,
		},
		{
			name:        "empty json array",
			input:       []byte("[]"),
			wantMatches: 0,
			wantErr:     false,
		},
		{
			name: "single match without metavars",
			input: []byte(`[
				{
					"file": "internal/mcp/server.go",
					"text": "defer conn.Close()",
					"range": {
						"start": {"line": 42, "column": 1},
						"end": {"line": 42, "column": 19}
					},
					"metaVariables": {"single": {}}
				}
			]`),
			wantMatches: 1,
			wantErr:     false,
			validate: func(t *testing.T, result *AstGrepResult) {
				assert.Equal(t, "internal/mcp/server.go", result.Matches[0].File)
				assert.Equal(t, 42, result.Matches[0].Range.Start.Line)
				assert.Equal(t, 42, result.Matches[0].Range.End.Line)
				assert.Equal(t, "defer conn.Close()", result.Matches[0].Text)
				assert.Empty(t, result.Matches[0].MetaVariables.Single)
			},
		},
		{
			name: "single match with metavars",
			input: []byte(`[
				{
					"file": "internal/mcp/server.go",
					"text": "defer conn.Close()",
					"range": {
						"start": {"line": 42, "column": 1},
						"end": {"line": 42, "column": 19}
					},
					"metaVariables": {
						"single": {
							"FUNC": {
								"text": "conn.Close",
								"range": {
									"start": {"line": 42, "column": 7},
									"end": {"line": 42, "column": 17}
								}
							}
						}
					}
				}
			]`),
			wantMatches: 1,
			wantErr:     false,
			validate: func(t *testing.T, result *AstGrepResult) {
				assert.Equal(t, "internal/mcp/server.go", result.Matches[0].File)
				assert.Equal(t, "defer conn.Close()", result.Matches[0].Text)
				require.Len(t, result.Matches[0].MetaVariables.Single, 1)
				assert.Equal(t, "conn.Close", result.Matches[0].MetaVariables.Single["FUNC"].Text)
			},
		},
		{
			name: "multiple matches",
			input: []byte(`[
				{
					"file": "internal/mcp/server.go",
					"text": "defer conn.Close()",
					"range": {
						"start": {"line": 42, "column": 1},
						"end": {"line": 42, "column": 19}
					},
					"metaVariables": {"single": {}}
				},
				{
					"file": "internal/mcp/client.go",
					"text": "defer cleanup()",
					"range": {
						"start": {"line": 15, "column": 1},
						"end": {"line": 15, "column": 16}
					},
					"metaVariables": {"single": {}}
				}
			]`),
			wantMatches: 2,
			wantErr:     false,
			validate: func(t *testing.T, result *AstGrepResult) {
				assert.Equal(t, "internal/mcp/server.go", result.Matches[0].File)
				assert.Equal(t, "internal/mcp/client.go", result.Matches[1].File)
			},
		},
		{
			name: "match with multiple metavars",
			input: []byte(`[
				{
					"file": "test.go",
					"text": "func example(a int, b string) error {\n\treturn nil\n}",
					"range": {
						"start": {"line": 10, "column": 1},
						"end": {"line": 12, "column": 2}
					},
					"metaVariables": {
						"single": {
							"NAME": {
								"text": "example",
								"range": {"start": {"line": 10, "column": 6}, "end": {"line": 10, "column": 13}}
							},
							"PARAMS": {
								"text": "a int, b string",
								"range": {"start": {"line": 10, "column": 14}, "end": {"line": 10, "column": 29}}
							},
							"BODY": {
								"text": "return nil",
								"range": {"start": {"line": 11, "column": 2}, "end": {"line": 11, "column": 12}}
							}
						}
					}
				}
			]`),
			wantMatches: 1,
			wantErr:     false,
			validate: func(t *testing.T, result *AstGrepResult) {
				require.Len(t, result.Matches[0].MetaVariables.Single, 3)
				assert.Equal(t, "example", result.Matches[0].MetaVariables.Single["NAME"].Text)
				assert.Equal(t, "a int, b string", result.Matches[0].MetaVariables.Single["PARAMS"].Text)
				assert.Equal(t, "return nil", result.Matches[0].MetaVariables.Single["BODY"].Text)
			},
		},
		{
			name:        "invalid json",
			input:       []byte(`{invalid json`),
			wantMatches: 0,
			wantErr:     true,
		},
		{
			name:        "json object instead of array",
			input:       []byte(`{"matches": []}`),
			wantMatches: 0,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := parseAstGrepOutput(tt.input)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Len(t, result.Matches, tt.wantMatches)

			if tt.validate != nil {
				tt.validate(t, result)
			}
		})
	}
}

func TestTransformToResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		result   *AstGrepResult
		req      *PatternRequest
		tookMs   int64
		validate func(t *testing.T, response *PatternResponse)
	}{
		{
			name: "empty result",
			result: &AstGrepResult{
				Matches: []AstGrepMatch{},
			},
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
			},
			tookMs: 100,
			validate: func(t *testing.T, response *PatternResponse) {
				assert.Empty(t, response.Matches)
				assert.Equal(t, 0, response.Total)
				assert.Equal(t, int64(100), response.Metadata.TookMs)
				assert.Equal(t, "test", response.Metadata.Pattern)
				assert.Equal(t, "go", response.Metadata.Language)
				assert.Equal(t, "smart", response.Metadata.Strictness) // default
			},
		},
		{
			name: "single match without metavars",
			result: &AstGrepResult{
				Matches: []AstGrepMatch{
					{
						File: "test.go",
						Text: "defer conn.Close()",
						Range: AstGrepRange{
							Start: AstGrepPosition{Line: 42, Column: 1},
							End:   AstGrepPosition{Line: 42, Column: 19},
						},
						MetaVariables: AstGrepMetaVars{Single: map[string]AstGrepMetaVar{}},
					},
				},
			},
			req: &PatternRequest{
				Pattern:    "defer $FUNC()",
				Language:   "go",
				Strictness: "relaxed",
			},
			tookMs: 250,
			validate: func(t *testing.T, response *PatternResponse) {
				require.Len(t, response.Matches, 1)
				assert.Equal(t, 1, response.Total)

				match := response.Matches[0]
				assert.Equal(t, "test.go", match.FilePath)
				assert.Equal(t, 42, match.StartLine)
				assert.Equal(t, 42, match.EndLine)
				assert.Equal(t, "defer conn.Close()", match.MatchText)
				assert.Equal(t, "defer conn.Close()", match.Context)
				assert.Empty(t, match.Metavars)

				assert.Equal(t, int64(250), response.Metadata.TookMs)
				assert.Equal(t, "defer $FUNC()", response.Metadata.Pattern)
				assert.Equal(t, "go", response.Metadata.Language)
				assert.Equal(t, "relaxed", response.Metadata.Strictness)
			},
		},
		{
			name: "single match with metavars",
			result: &AstGrepResult{
				Matches: []AstGrepMatch{
					{
						File: "test.go",
						Text: "defer conn.Close()",
						Range: AstGrepRange{
							Start: AstGrepPosition{Line: 42, Column: 1},
							End:   AstGrepPosition{Line: 42, Column: 19},
						},
						MetaVariables: AstGrepMetaVars{
							Single: map[string]AstGrepMetaVar{
								"FUNC": {
									Text: "conn.Close",
									Range: AstGrepRange{
										Start: AstGrepPosition{Line: 42, Column: 7},
										End:   AstGrepPosition{Line: 42, Column: 17},
									},
								},
							},
						},
					},
				},
			},
			req: &PatternRequest{
				Pattern:  "defer $FUNC()",
				Language: "go",
			},
			tookMs: 150,
			validate: func(t *testing.T, response *PatternResponse) {
				require.Len(t, response.Matches, 1)

				match := response.Matches[0]
				require.Len(t, match.Metavars, 1)
				assert.Equal(t, "conn.Close", match.Metavars["FUNC"])
			},
		},
		{
			name: "multiple matches with multiple metavars",
			result: &AstGrepResult{
				Matches: []AstGrepMatch{
					{
						File: "file1.go",
						Text: "func example(a int) error {}",
						Range: AstGrepRange{
							Start: AstGrepPosition{Line: 10, Column: 1},
							End:   AstGrepPosition{Line: 12, Column: 2},
						},
						MetaVariables: AstGrepMetaVars{
							Single: map[string]AstGrepMetaVar{
								"NAME":   {Text: "example"},
								"PARAMS": {Text: "a int"},
							},
						},
					},
					{
						File: "file2.go",
						Text: "func test(b string) error {}",
						Range: AstGrepRange{
							Start: AstGrepPosition{Line: 20, Column: 1},
							End:   AstGrepPosition{Line: 22, Column: 2},
						},
						MetaVariables: AstGrepMetaVars{
							Single: map[string]AstGrepMetaVar{
								"NAME":   {Text: "test"},
								"PARAMS": {Text: "b string"},
							},
						},
					},
				},
			},
			req: &PatternRequest{
				Pattern:  "func $NAME($PARAMS) error { $$$BODY }",
				Language: "go",
			},
			tookMs: 300,
			validate: func(t *testing.T, response *PatternResponse) {
				require.Len(t, response.Matches, 2)
				assert.Equal(t, 2, response.Total)

				// First match
				assert.Equal(t, "file1.go", response.Matches[0].FilePath)
				assert.Equal(t, "example", response.Matches[0].Metavars["NAME"])
				assert.Equal(t, "a int", response.Matches[0].Metavars["PARAMS"])

				// Second match
				assert.Equal(t, "file2.go", response.Matches[1].FilePath)
				assert.Equal(t, "test", response.Matches[1].Metavars["NAME"])
				assert.Equal(t, "b string", response.Matches[1].Metavars["PARAMS"])
			},
		},
		{
			name: "default strictness when not specified",
			result: &AstGrepResult{
				Matches: []AstGrepMatch{},
			},
			req: &PatternRequest{
				Pattern:  "test",
				Language: "go",
				// Strictness not set
			},
			tookMs: 100,
			validate: func(t *testing.T, response *PatternResponse) {
				assert.Equal(t, "smart", response.Metadata.Strictness)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			response := transformToResponse(tt.result, tt.req, tt.tookMs)

			require.NotNil(t, response)
			tt.validate(t, response)
		})
	}
}

func TestApplyLimit(t *testing.T) {
	t.Parallel()

	// Helper to create a response with N matches
	createResponse := func(n int) *PatternResponse {
		matches := make([]PatternMatch, n)
		for i := 0; i < n; i++ {
			matches[i] = PatternMatch{
				FilePath:  "test.go",
				StartLine: i + 1,
				EndLine:   i + 1,
				MatchText: "match",
			}
		}
		return &PatternResponse{
			Matches: matches,
			Total:   n,
			Metadata: PatternMetadata{
				TookMs:     100,
				Pattern:    "test",
				Language:   "go",
				Strictness: "smart",
			},
		}
	}

	// Helper to create request with limit
	createRequest := func(limit int) *PatternRequest {
		return &PatternRequest{
			Pattern:  "test",
			Language: "go",
			Limit:    &limit,
		}
	}

	tests := []struct {
		name           string
		response       *PatternResponse
		req            *PatternRequest
		wantMatchCount int
		wantTotal      int
	}{
		{
			name:           "under limit - no change",
			response:       createResponse(10),
			req:            createRequest(50),
			wantMatchCount: 10,
			wantTotal:      10,
		},
		{
			name:           "at limit - no change",
			response:       createResponse(50),
			req:            createRequest(50),
			wantMatchCount: 50,
			wantTotal:      50,
		},
		{
			name:           "over limit - truncate matches",
			response:       createResponse(150),
			req:            createRequest(50),
			wantMatchCount: 50,
			wantTotal:      150, // Total preserves original count
		},
		{
			name:           "default limit when not specified",
			response:       createResponse(75),
			req:            &PatternRequest{Pattern: "test", Language: "go"}, // No limit set
			wantMatchCount: 50,                                                // DefaultLimit
			wantTotal:      75,
		},
		{
			name:           "zero matches",
			response:       createResponse(0),
			req:            createRequest(50),
			wantMatchCount: 0,
			wantTotal:      0,
		},
		{
			name:           "exactly one over limit",
			response:       createResponse(51),
			req:            createRequest(50),
			wantMatchCount: 50,
			wantTotal:      51,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := applyLimit(tt.response, tt.req)

			require.NotNil(t, result)
			assert.Len(t, result.Matches, tt.wantMatchCount)
			assert.Equal(t, tt.wantTotal, result.Total)

			// Verify metadata is preserved
			assert.Equal(t, tt.response.Metadata, result.Metadata)

			// Verify matches are first N (not random)
			if len(result.Matches) > 0 {
				for i, match := range result.Matches {
					assert.Equal(t, i+1, match.StartLine, "matches should be first N in order")
				}
			}
		})
	}
}

func TestApplyLimit_PreservesOrder(t *testing.T) {
	t.Parallel()

	// Create response with identifiable matches
	matches := make([]PatternMatch, 100)
	for i := 0; i < 100; i++ {
		matches[i] = PatternMatch{
			FilePath:  "test.go",
			StartLine: i + 1,
			EndLine:   i + 1,
			MatchText: "match",
		}
	}

	response := &PatternResponse{
		Matches: matches,
		Total:   100,
		Metadata: PatternMetadata{
			TookMs:     100,
			Pattern:    "test",
			Language:   "go",
			Strictness: "smart",
		},
	}

	limit := 10
	req := &PatternRequest{
		Pattern:  "test",
		Language: "go",
		Limit:    &limit,
	}

	result := applyLimit(response, req)

	// Verify we get the FIRST 10 matches, not random
	require.Len(t, result.Matches, 10)
	for i := 0; i < 10; i++ {
		assert.Equal(t, i+1, result.Matches[i].StartLine)
	}
}

func TestExecutionTimeout(t *testing.T) {
	// This is a placeholder test to document timeout behavior
	// Real timeout testing is in integration tests (requires real binary execution)
	t.Skip("Timeout testing requires integration tests with real ast-grep execution")
}
