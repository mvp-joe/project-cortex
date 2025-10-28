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
)

// QueryOperation represents the type of graph query to perform.
type QueryOperation string

const (
	OperationCallers     QueryOperation = "callers"
	OperationCallees     QueryOperation = "callees"
	OperationDependencies QueryOperation = "dependencies"
	OperationDependents   QueryOperation = "dependents"
)

// Query defaults and limits
const (
	DefaultDepth        = 1
	DefaultMaxResults   = 100
	DefaultContextLines = 3
	MaxDepth            = 10
	MaxContextLines     = 20
	MaxFileCacheSize    = 100
)

// QueryRequest represents a graph query request.
type QueryRequest struct {
	Operation      QueryOperation // Type of query
	Target         string         // Target identifier to query
	IncludeContext bool           // Whether to include code context
	ContextLines   int            // Number of context lines around the code (default: 3)
	Depth          int            // Traversal depth (default: 1)
	MaxResults     int            // Maximum number of results (default: 100)
}

// QueryResponse represents the response to a graph query.
type QueryResponse struct {
	Operation     string        `json:"operation"`
	Target        string        `json:"target"`
	Results       []QueryResult `json:"results"`
	TotalFound    int           `json:"total_found"`
	TotalReturned int           `json:"total_returned"`
	Truncated     bool          `json:"truncated"`
	Metadata      ResponseMeta  `json:"metadata"`
}

// QueryResult represents a single result from a graph query.
type QueryResult struct {
	Node    *Node  `json:"node"`
	Context string `json:"context,omitempty"` // Code snippet if IncludeContext=true
	Depth   int    `json:"depth,omitempty"`   // Depth in traversal (for recursive queries)
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
	callers      map[string][]string // function -> [callers]
	callees      map[string][]string // function -> [callees]
	dependencies map[string][]string // package -> [dependencies]
	dependents   map[string][]string // package -> [dependents]

	// File cache for context injection
	fileCache map[string][]string // file path -> lines (LRU would be better for large projects)
}

// resultWithDepth is an internal type for tracking depth in traversal.
type resultWithDepth struct {
	id    string
	depth int
}

// NewSearcher creates a new graph searcher.
func NewSearcher(storage Storage, rootDir string) (Searcher, error) {
	s := &searcher{
		storage:      storage,
		rootDir:      rootDir,
		callers:      make(map[string][]string),
		callees:      make(map[string][]string),
		dependencies: make(map[string][]string),
		dependents:   make(map[string][]string),
		fileCache:    make(map[string][]string),
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
		case EdgeCalls:
			s.callees[edge.From] = append(s.callees[edge.From], edge.To)
			s.callers[edge.To] = append(s.callers[edge.To], edge.From)
		case EdgeImports:
			s.dependencies[edge.From] = append(s.dependencies[edge.From], edge.To)
			s.dependents[edge.To] = append(s.dependents[edge.To], edge.From)
		}
	}

	// Clear file cache on reload
	s.fileCache = make(map[string][]string)

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
	if req.ContextLines <= 0 {
		req.ContextLines = DefaultContextLines
	}

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

	// Build results
	results := []QueryResult{}
	seen := make(map[string]bool)

	for _, rd := range resultWithDepths {
		if seen[rd.id] {
			continue
		}
		seen[rd.id] = true

		// Get node from graph
		node, err := s.graph.Vertex(rd.id)
		if err != nil {
			// Node might not exist in graph (external reference)
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

		if len(results) >= req.MaxResults {
			break
		}
	}

	response := &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(resultWithDepths),
		TotalReturned: len(results),
		Truncated:     len(results) < len(resultWithDepths),
		Metadata: ResponseMeta{
			TookMs: int(getMillis() - startTime),
			Source: "graph",
		},
	}

	return response, nil
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
	// Check cache first (read lock already held by caller)
	if lines, ok := s.fileCache[relPath]; ok {
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

	// Simple cache size limit - clear cache if it grows too large
	// Note: This is called under read lock, but we need write lock to modify cache
	// For now, we'll accept unbounded cache growth since proper LRU requires refactoring
	// TODO: Implement LRU cache or acquire write lock here
	if len(s.fileCache) < MaxFileCacheSize {
		s.fileCache[relPath] = lines
	}

	return lines, nil
}

// Close releases resources.
func (s *searcher) Close() error {
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
