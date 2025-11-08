package graph

import "context"

// QueryOperation represents the type of graph query to perform.
type QueryOperation string

const (
	OperationImplementations QueryOperation = "implementations"
	OperationCallers         QueryOperation = "callers"
	OperationCallees         QueryOperation = "callees"
	OperationDependencies    QueryOperation = "dependencies"
	OperationDependents      QueryOperation = "dependents"
	OperationTypeUsages      QueryOperation = "type_usages"
	OperationPath            QueryOperation = "path"
	OperationImpact          QueryOperation = "impact"
)

// Query defaults and limits
const (
	DefaultDepth        = 1
	DefaultMaxResults   = 100
	DefaultMaxPerLevel  = 50
	DefaultContextLines = 3
	MaxDepth            = 10
	MaxContextLines     = 20
)

// QueryRequest represents a graph query request.
type QueryRequest struct {
	Operation       QueryOperation // Type of query
	Target          string         // Target identifier to query
	To              string         // For path operation: destination node
	IncludeContext  bool           // Whether to include code context
	ContextLines    int            // Number of context lines around the code (default: 3)
	Depth           int            // Traversal depth (default: 1)
	MaxResults      int            // Maximum number of results (default: 100)
	MaxPerLevel     int            // Maximum results per depth level (default: 50)
	Scope           string         // SQL LIKE pattern to filter results by file path (e.g., "internal/%", "%_test.go") (not supported for path operation)
	ExcludePatterns []string       // SQL LIKE patterns to exclude from results (e.g., "%_test.go", "vendor/%") (not supported for path operation)
}

// QueryResponse represents the response to a graph query.
type QueryResponse struct {
	Operation     string         `json:"operation"`
	Target        string         `json:"target"`
	Results       []QueryResult  `json:"results"`
	TotalFound    int            `json:"total_found"`
	TotalReturned int            `json:"total_returned"`
	Truncated     bool           `json:"truncated"`
	TruncatedAt   int            `json:"truncated_at_depth,omitempty"`
	Suggestion    string         `json:"suggestion,omitempty"`
	Summary       *ImpactSummary `json:"summary,omitempty"` // For impact operation
	Metadata      ResponseMeta   `json:"metadata"`
}

// QueryResult represents a single result from a graph query.
type QueryResult struct {
	Node       *Node  `json:"node"`
	Context    string `json:"context,omitempty"`     // Code snippet if IncludeContext=true
	Depth      int    `json:"depth,omitempty"`       // Depth in traversal (for recursive queries)
	ImpactType string `json:"impact_type,omitempty"` // For impact operation: "implementation", "direct_caller", "transitive"
	Severity   string `json:"severity,omitempty"`    // For impact operation: "must_update", "review_needed"
}

// ImpactSummary provides aggregate statistics for impact analysis.
type ImpactSummary struct {
	Implementations   int `json:"implementations"`
	DirectCallers     int `json:"direct_callers"`
	TransitiveCallers int `json:"transitive_callers"`
	ExternalPackages  int `json:"external_packages"`
}

// ResponseMeta contains metadata about the query execution.
type ResponseMeta struct {
	TookMs int    `json:"took_ms"`
	Source string `json:"source"` // Always "graph"
}

// Searcher provides graph query capabilities with reverse indexes.
type Searcher interface {
	// Query executes a graph query and returns results.
	Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

	// Reload reloads the graph from storage.
	Reload(ctx context.Context) error

	// Close releases resources.
	Close() error
}
