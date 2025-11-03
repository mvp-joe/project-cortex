package pattern

// PatternRequest represents an MCP cortex_pattern request
type PatternRequest struct {
	Pattern      string   `json:"pattern"`        // Required: AST pattern with metavariables
	Language     string   `json:"language"`       // Required: Target language
	FilePaths    []string `json:"file_paths"`     // Optional: File/glob filters
	ContextLines *int     `json:"context_lines"`  // Optional: Lines before/after match (0-10, default: 3)
	Strictness   string   `json:"strictness"`     // Optional: Matching algorithm (default: "smart")
	Limit        *int     `json:"limit"`          // Optional: Max results (1-100, default: 50)
}

// PatternMatch represents a single pattern match result
type PatternMatch struct {
	FilePath  string            `json:"file_path"`  // Relative to project root
	StartLine int               `json:"start_line"` // 1-indexed
	EndLine   int               `json:"end_line"`   // 1-indexed
	MatchText string            `json:"match_text"` // The matched code
	Context   string            `json:"context"`    // Surrounding lines (from -C flag)
	Metavars  map[string]string `json:"metavars"`   // Extracted metavariables
}

// PatternResponse represents the cortex_pattern tool response
type PatternResponse struct {
	Matches  []PatternMatch    `json:"matches"`
	Total    int               `json:"total"` // Total found (may be > len(Matches) if limited)
	Metadata PatternMetadata   `json:"metadata"`
}

// PatternMetadata contains query execution metadata
type PatternMetadata struct {
	TookMs     int64  `json:"took_ms"`
	Pattern    string `json:"pattern"`
	Language   string `json:"language"`
	Strictness string `json:"strictness"`
}

// AstGrepResult represents the raw JSON output from ast-grep --json=compact
// Note: ast-grep returns an array directly, not wrapped in an object
type AstGrepResult struct {
	Matches []AstGrepMatch
}

// AstGrepMatch represents a single match from ast-grep JSON output
// Actual format from ast-grep v0.29.0:
// {
//   "text": "matched code",
//   "range": {"start": {"line": 2, "column": 1}, "end": {"line": 2, "column": 19}},
//   "file": "test.go",
//   "metaVariables": {"single": {"FUNC": {"text": "conn.Close", "range": {...}}}}
// }
type AstGrepMatch struct {
	File          string             `json:"file"`
	Text          string             `json:"text"`
	Range         AstGrepRange       `json:"range"`
	MetaVariables AstGrepMetaVars    `json:"metaVariables"`
}

// AstGrepRange represents line/column position
type AstGrepRange struct {
	Start AstGrepPosition `json:"start"`
	End   AstGrepPosition `json:"end"`
}

// AstGrepPosition represents a line/column position
type AstGrepPosition struct {
	Line   int `json:"line"` // 1-indexed
	Column int `json:"column"`
}

// AstGrepMetaVars contains captured metavariables
type AstGrepMetaVars struct {
	Single map[string]AstGrepMetaVar `json:"single"`
	// Multi and Transformed exist but we don't need them
}

// AstGrepMetaVar represents a captured metavariable
type AstGrepMetaVar struct {
	Text  string       `json:"text"`
	Range AstGrepRange `json:"range"`
}
