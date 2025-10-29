package graph

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dominikbraun/graph"
	"github.com/maypok86/otter"
)

// QueryOperation represents the type of graph query to perform.
type QueryOperation string

const (
	OperationImplementations QueryOperation = "implementations"
	OperationCallers         QueryOperation = "callers"
	OperationCallees         QueryOperation = "callees"
	OperationDependencies    QueryOperation = "dependencies"
	OperationDependents      QueryOperation = "dependents"
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
	MaxFileCacheWeight  = 50 * 1024 * 1024 // 50MB
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
	Scope           string         // Glob pattern to filter results by file path (not supported for path operation)
	ExcludePatterns []string       // Glob patterns to exclude from results (not supported for path operation)
}

// QueryResponse represents the response to a graph query.
type QueryResponse struct {
	Operation     string          `json:"operation"`
	Target        string          `json:"target"`
	Results       []QueryResult   `json:"results"`
	TotalFound    int             `json:"total_found"`
	TotalReturned int             `json:"total_returned"`
	Truncated     bool            `json:"truncated"`
	TruncatedAt   int             `json:"truncated_at_depth,omitempty"`
	Suggestion    string          `json:"suggestion,omitempty"`
	Summary       *ImpactSummary  `json:"summary,omitempty"` // For impact operation
	Metadata      ResponseMeta    `json:"metadata"`
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
	Implementations    int `json:"implementations"`
	DirectCallers      int `json:"direct_callers"`
	TransitiveCallers  int `json:"transitive_callers"`
	ExternalPackages   int `json:"external_packages"`
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

// searcher implements Searcher with in-memory graph and reverse indexes.
type searcher struct {
	storage Storage
	rootDir string
	mu      sync.RWMutex // Protects graph and indexes

	// In-memory graph
	graph graph.Graph[string, *Node]

	// Reverse indexes for O(1) lookups
	implementations map[string][]string // interface -> [implementors]
	callers         map[string][]string // function -> [callers]
	callees         map[string][]string // function -> [callees]
	dependencies    map[string][]string // package -> [dependencies]
	dependents      map[string][]string // package -> [dependents]

	// File cache for context injection (weight-based LRU)
	fileCache otter.Cache[string, []string]
}

// resultWithDepth is an internal type for tracking depth in traversal.
type resultWithDepth struct {
	id    string
	depth int
}

// NewSearcher creates a new graph searcher.
func NewSearcher(storage Storage, rootDir string) (Searcher, error) {
	// Create file cache with weight-based eviction (50MB limit)
	cache, err := otter.MustBuilder[string, []string](MaxFileCacheWeight).
		Cost(func(key string, value []string) uint32 {
			// Approximate memory cost: each line ~100 bytes
			return uint32(len(value) * 100)
		}).
		CollectStats().
		Build()
	if err != nil {
		return nil, fmt.Errorf("failed to create file cache: %w", err)
	}

	s := &searcher{
		storage:         storage,
		rootDir:         rootDir,
		implementations: make(map[string][]string),
		callers:         make(map[string][]string),
		callees:         make(map[string][]string),
		dependencies:    make(map[string][]string),
		dependents:      make(map[string][]string),
		fileCache:       cache,
	}

	// Initial load
	if err := s.Reload(context.Background()); err != nil {
		return nil, err
	}

	return s, nil
}

// Reload reloads the graph from storage and rebuilds indexes.
func (s *searcher) Reload(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Load graph data
	data, err := s.storage.Load()
	if err != nil {
		return fmt.Errorf("failed to load graph: %w", err)
	}

	if data == nil {
		// No graph yet, initialize empty
		data = &GraphData{
			Nodes: []Node{},
			Edges: []Edge{},
		}
	}

	// Build in-memory graph using dominikbraun/graph
	s.graph = graph.New(func(n *Node) string { return n.ID }, graph.Directed())

	// Add nodes
	for i := range data.Nodes {
		node := &data.Nodes[i]
		if err := s.graph.AddVertex(node); err != nil {
			return fmt.Errorf("failed to add node %s: %w", node.ID, err)
		}
	}

	// Clear reverse indexes
	s.implementations = make(map[string][]string)
	s.callers = make(map[string][]string)
	s.callees = make(map[string][]string)
	s.dependencies = make(map[string][]string)
	s.dependents = make(map[string][]string)

	// Add edges and build reverse indexes
	for _, edge := range data.Edges {
		// Add edge to graph (allow errors for missing nodes - could be references to external packages)
		_ = s.graph.AddEdge(edge.From, edge.To)

		// Build reverse indexes based on edge type
		switch edge.Type {
		case EdgeImplements:
			s.implementations[edge.To] = append(s.implementations[edge.To], edge.From)
		case EdgeCalls:
			s.callees[edge.From] = append(s.callees[edge.From], edge.To)
			s.callers[edge.To] = append(s.callers[edge.To], edge.From)
		case EdgeImports:
			s.dependencies[edge.From] = append(s.dependencies[edge.From], edge.To)
			s.dependents[edge.To] = append(s.dependents[edge.To], edge.From)
		}
	}

	// Clear file cache on reload
	s.fileCache.Clear()

	return nil
}

// Query executes a graph query.
func (s *searcher) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	startTime := getMillis()

	// Set defaults
	if req.Depth <= 0 {
		req.Depth = DefaultDepth
	}
	if req.MaxResults <= 0 {
		req.MaxResults = DefaultMaxResults
	}
	if req.MaxPerLevel <= 0 {
		req.MaxPerLevel = DefaultMaxPerLevel
	}
	if req.ContextLines <= 0 {
		req.ContextLines = DefaultContextLines
	}

	// Execute operation based on type
	switch req.Operation {
	case OperationImplementations:
		return s.queryImplementations(ctx, req, startTime)
	case OperationPath:
		return s.queryPath(ctx, req, startTime)
	case OperationImpact:
		return s.queryImpact(ctx, req, startTime)
	default:
		return s.queryTraversal(ctx, req, startTime)
	}
}

// queryTraversal handles callers, callees, dependencies, dependents operations.
func (s *searcher) queryTraversal(ctx context.Context, req *QueryRequest, startTime int64) (*QueryResponse, error) {
	// Execute operation and get results with depth
	var resultWithDepths []resultWithDepth
	var err error

	switch req.Operation {
	case OperationCallers:
		resultWithDepths, err = s.queryCallers(req.Target, req.Depth)
	case OperationCallees:
		resultWithDepths, err = s.queryCallees(req.Target, req.Depth)
	case OperationDependencies:
		// Dependencies are always depth 1
		for _, id := range s.dependencies[req.Target] {
			resultWithDepths = append(resultWithDepths, resultWithDepth{id: id, depth: 1})
		}
	case OperationDependents:
		// Dependents are always depth 1
		for _, id := range s.dependents[req.Target] {
			resultWithDepths = append(resultWithDepths, resultWithDepth{id: id, depth: 1})
		}
	default:
		return nil, fmt.Errorf("unsupported operation: %s", req.Operation)
	}

	if err != nil {
		return nil, err
	}

	// Apply filtering and build results
	results, truncated, truncatedAt := s.buildResults(resultWithDepths, req)

	response := &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(resultWithDepths),
		TotalReturned: len(results),
		Truncated:     truncated,
		TruncatedAt:   truncatedAt,
		Metadata: ResponseMeta{
			TookMs: int(getMillis() - startTime),
			Source: "graph",
		},
	}

	if truncated {
		response.Suggestion = "Results truncated. Use scope, exclude_patterns, or reduce depth to narrow results."
	}

	return response, nil
}

// buildResults converts resultWithDepth to QueryResult with filtering and truncation.
func (s *searcher) buildResults(resultWithDepths []resultWithDepth, req *QueryRequest) ([]QueryResult, bool, int) {
	results := []QueryResult{}
	seen := make(map[string]bool)
	levelCounts := make(map[int]int)
	truncated := false
	truncatedAt := -1

	for _, rd := range resultWithDepths {
		if seen[rd.id] {
			continue
		}

		// Check max per level
		if levelCounts[rd.depth] >= req.MaxPerLevel {
			truncated = true
			if truncatedAt < 0 {
				truncatedAt = rd.depth
			}
			continue
		}

		// Check max results
		if len(results) >= req.MaxResults {
			truncated = true
			break
		}

		seen[rd.id] = true

		// Get node from graph
		node, err := s.graph.Vertex(rd.id)
		if err != nil {
			// Node might not exist in graph (external reference)
			continue
		}

		// Apply filtering
		if !s.matchesFilters(node, req) {
			continue
		}

		result := QueryResult{
			Node:  node,
			Depth: rd.depth,
		}

		// Inject context if requested
		if req.IncludeContext {
			context, err := s.extractContext(node.File, node.StartLine, node.EndLine, req.ContextLines)
			if err == nil {
				result.Context = context
			}
		}

		results = append(results, result)
		levelCounts[rd.depth]++
	}

	return results, truncated, truncatedAt
}

// queryCallers finds all functions that call the target (recursive up to depth).
func (s *searcher) queryCallers(target string, depth int) ([]resultWithDepth, error) {
	results := []resultWithDepth{}
	visited := make(map[string]int) // id -> depth at which it was first visited

	var traverse func(id string, currentDepth int)
	traverse = func(id string, currentDepth int) {
		if currentDepth > depth {
			return
		}
		if prevDepth, seen := visited[id]; seen && prevDepth <= currentDepth {
			return // Already visited at same or shallower depth
		}
		visited[id] = currentDepth

		// Get callers from reverse index
		for _, caller := range s.callers[id] {
			results = append(results, resultWithDepth{id: caller, depth: currentDepth})
			if currentDepth < depth {
				traverse(caller, currentDepth+1)
			}
		}
	}

	traverse(target, 1)
	return results, nil
}

// queryCallees finds all functions called by the target (recursive up to depth).
func (s *searcher) queryCallees(target string, depth int) ([]resultWithDepth, error) {
	results := []resultWithDepth{}
	visited := make(map[string]int) // id -> depth at which it was first visited

	var traverse func(id string, currentDepth int)
	traverse = func(id string, currentDepth int) {
		if currentDepth > depth {
			return
		}
		if prevDepth, seen := visited[id]; seen && prevDepth <= currentDepth {
			return // Already visited at same or shallower depth
		}
		visited[id] = currentDepth

		// Get callees from reverse index
		for _, callee := range s.callees[id] {
			results = append(results, resultWithDepth{id: callee, depth: currentDepth})
			if currentDepth < depth {
				traverse(callee, currentDepth+1)
			}
		}
	}

	traverse(target, 1)
	return results, nil
}

// extractContext reads the file and extracts lines with context padding.
func (s *searcher) extractContext(file string, startLine, endLine, contextLines int) (string, error) {
	// Get file lines (with caching)
	lines, err := s.getFileLines(file)
	if err != nil {
		return "", err
	}

	// Calculate context window
	from := max(0, startLine-contextLines-1)
	to := min(len(lines), endLine+contextLines)

	// Extract snippet
	snippet := strings.Join(lines[from:to], "\n")

	// Add line number comment
	prefix := fmt.Sprintf("// Lines %d-%d\n", from+1, to)
	return prefix + snippet, nil
}

// getFileLines reads a file and caches its lines.
func (s *searcher) getFileLines(relPath string) ([]string, error) {
	// Check cache first (Otter is thread-safe)
	if lines, ok := s.fileCache.Get(relPath); ok {
		return lines, nil
	}

	// Read file
	fullPath := filepath.Join(s.rootDir, relPath)
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, err
	}

	// Split into lines
	lines := strings.Split(string(content), "\n")

	// Store in cache (Otter handles eviction automatically)
	s.fileCache.Set(relPath, lines)

	return lines, nil
}

// Close releases resources.
func (s *searcher) Close() error {
	s.fileCache.Close()
	return nil
}

// Helper functions

func getMillis() int64 {
	return time.Now().UnixMilli()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// queryImplementations finds all structs that implement a given interface.
func (s *searcher) queryImplementations(ctx context.Context, req *QueryRequest, startTime int64) (*QueryResponse, error) {
	// Get implementors from index
	implementorIDs := s.implementations[req.Target]

	// Convert to resultWithDepth
	resultWithDepths := []resultWithDepth{}
	for _, id := range implementorIDs {
		resultWithDepths = append(resultWithDepths, resultWithDepth{id: id, depth: 1})
	}

	// Apply filtering and build results
	results, truncated, truncatedAt := s.buildResults(resultWithDepths, req)

	response := &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(resultWithDepths),
		TotalReturned: len(results),
		Truncated:     truncated,
		TruncatedAt:   truncatedAt,
		Metadata: ResponseMeta{
			TookMs: int(getMillis() - startTime),
			Source: "graph",
		},
	}

	return response, nil
}

// queryPath finds the shortest call path from target to destination using BFS.
func (s *searcher) queryPath(ctx context.Context, req *QueryRequest, startTime int64) (*QueryResponse, error) {
	if req.To == "" {
		return nil, fmt.Errorf("'to' parameter required for path operation")
	}

	// Path operation doesn't support filtering
	if req.Scope != "" || len(req.ExcludePatterns) > 0 {
		return nil, fmt.Errorf("path operation does not support scope or exclude_patterns filters")
	}

	// Use dominikbraun/graph ShortestPath
	path, err := graph.ShortestPath(s.graph, req.Target, req.To)
	if err != nil {
		// No path found
		return &QueryResponse{
			Operation:     string(req.Operation),
			Target:        req.Target,
			Results:       []QueryResult{},
			TotalFound:    0,
			TotalReturned: 0,
			Truncated:     false,
			Metadata: ResponseMeta{
				TookMs: int(getMillis() - startTime),
				Source: "graph",
			},
		}, nil
	}

	// Convert path to results
	results := []QueryResult{}
	for i, nodeID := range path {
		node, err := s.graph.Vertex(nodeID)
		if err != nil {
			continue
		}

		result := QueryResult{
			Node:  node,
			Depth: i,
		}

		// Inject context if requested
		if req.IncludeContext {
			context, err := s.extractContext(node.File, node.StartLine, node.EndLine, req.ContextLines)
			if err == nil {
				result.Context = context
			}
		}

		results = append(results, result)
	}

	response := &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(results),
		TotalReturned: len(results),
		Truncated:     false,
		Metadata: ResponseMeta{
			TookMs: int(getMillis() - startTime),
			Source: "graph",
		},
	}

	return response, nil
}

// queryImpact analyzes the blast radius of changing a function/interface.
func (s *searcher) queryImpact(ctx context.Context, req *QueryRequest, startTime int64) (*QueryResponse, error) {
	results := []QueryResult{}
	summary := &ImpactSummary{}

	// Phase 1: Find implementations (if target is an interface)
	implementorIDs := s.implementations[req.Target]
	for _, id := range implementorIDs {
		node, err := s.graph.Vertex(id)
		if err != nil {
			continue
		}

		// Apply filtering
		if !s.matchesFilters(node, req) {
			continue
		}

		result := QueryResult{
			Node:       node,
			Depth:      1,
			ImpactType: "implementation",
			Severity:   "must_update",
		}

		// Inject context if requested
		if req.IncludeContext {
			context, err := s.extractContext(node.File, node.StartLine, node.EndLine, req.ContextLines)
			if err == nil {
				result.Context = context
			}
		}

		results = append(results, result)
		summary.Implementations++
	}

	// Phase 2: Find direct callers
	directCallers := s.callers[req.Target]
	for _, id := range directCallers {
		node, err := s.graph.Vertex(id)
		if err != nil {
			continue
		}

		// Apply filtering
		if !s.matchesFilters(node, req) {
			continue
		}

		result := QueryResult{
			Node:       node,
			Depth:      1,
			ImpactType: "direct_caller",
			Severity:   "review_needed",
		}

		// Inject context if requested
		if req.IncludeContext {
			context, err := s.extractContext(node.File, node.StartLine, node.EndLine, req.ContextLines)
			if err == nil {
				result.Context = context
			}
		}

		results = append(results, result)
		summary.DirectCallers++
	}

	// Phase 3: Find transitive callers (depth 2-3)
	transitiveCallerSet := make(map[string]bool)
	for _, directCaller := range directCallers {
		transitiveCallers, _ := s.queryCallers(directCaller, 2)
		for _, tc := range transitiveCallers {
			if tc.id != req.Target && !transitiveCallerSet[tc.id] {
				transitiveCallerSet[tc.id] = true
				summary.TransitiveCallers++

				// Only include first few transitive callers in results
				if len(results) < req.MaxResults {
					node, err := s.graph.Vertex(tc.id)
					if err != nil {
						continue
					}

					// Apply filtering
					if !s.matchesFilters(node, req) {
						continue
					}

					result := QueryResult{
						Node:       node,
						Depth:      tc.depth + 1,
						ImpactType: "transitive",
						Severity:   "review_needed",
					}

					// Inject context if requested
					if req.IncludeContext {
						context, err := s.extractContext(node.File, node.StartLine, node.EndLine, req.ContextLines)
						if err == nil {
							result.Context = context
						}
					}

					results = append(results, result)
				}
			}
		}
	}

	// Determine if results were truncated
	totalFound := summary.Implementations + summary.DirectCallers + summary.TransitiveCallers
	truncated := len(results) < totalFound

	response := &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    totalFound,
		TotalReturned: len(results),
		Truncated:     truncated,
		Summary:       summary,
		Metadata: ResponseMeta{
			TookMs: int(getMillis() - startTime),
			Source: "graph",
		},
	}

	if truncated {
		response.Suggestion = "Showing top impacted sites. Use scope or exclude_patterns to narrow results."
	}

	return response, nil
}

// matchesFilters checks if a node matches the filtering criteria.
func (s *searcher) matchesFilters(node *Node, req *QueryRequest) bool {
	// Scope filter (glob pattern matching)
	if req.Scope != "" {
		matched, err := filepath.Match(req.Scope, node.File)
		if err != nil || !matched {
			return false
		}
	}

	// Exclude patterns
	for _, pattern := range req.ExcludePatterns {
		matched, err := filepath.Match(pattern, node.File)
		if err == nil && matched {
			return false
		}
	}

	return true
}
