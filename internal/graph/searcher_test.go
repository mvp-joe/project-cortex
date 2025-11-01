package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for GraphSearcher:
// - Query callers returns direct callers at depth 1
// - Query callers returns transitive callers at depth > 1
// - Query callees returns direct callees at depth 1
// - Query dependencies returns package import relationships
// - Query dependents returns reverse package dependencies
// - Include context injects code snippets with line numbers
// - Reload updates graph from storage
// - MaxResults limits number of returned results
// - Empty queries handle no results gracefully

func setupTestGraph(t *testing.T) (Storage, string) {
	t.Helper()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Create test graph with function call relationships
	testData := &GraphData{
		Nodes: []Node{
			{ID: "main.main", Kind: NodeFunction, File: "main.go", StartLine: 5, EndLine: 10},
			{ID: "handler.ServeHTTP", Kind: NodeMethod, File: "handler.go", StartLine: 15, EndLine: 25},
			{ID: "service.Process", Kind: NodeFunction, File: "service.go", StartLine: 30, EndLine: 40},
			{ID: "repo.GetData", Kind: NodeFunction, File: "repo.go", StartLine: 45, EndLine: 55},
			{ID: "internal/mcp", Kind: NodePackage, File: "internal/mcp/server.go", StartLine: 1, EndLine: 100},
			{ID: "internal/graph", Kind: NodePackage, File: "internal/graph/types.go", StartLine: 1, EndLine: 50},
		},
		Edges: []Edge{
			// Call chain: main -> handler -> service -> repo
			{From: "main.main", To: "handler.ServeHTTP", Type: EdgeCalls, Location: &Location{File: "main.go", Line: 7}},
			{From: "handler.ServeHTTP", To: "service.Process", Type: EdgeCalls, Location: &Location{File: "handler.go", Line: 20}},
			{From: "service.Process", To: "repo.GetData", Type: EdgeCalls, Location: &Location{File: "service.go", Line: 35}},
			// Package imports
			{From: "internal/mcp", To: "internal/graph", Type: EdgeImports, Location: &Location{File: "internal/mcp/server.go", Line: 5}},
		},
	}

	err = storage.Save(testData)
	require.NoError(t, err)

	// Create test source files for context injection
	mainGo := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(mainGo, []byte(`package main

import "handler"

func main() {
	h := handler.New()
	h.ServeHTTP()
}
`), 0644)
	require.NoError(t, err)

	return storage, tmpDir
}

func TestSearcher_QueryCallers(t *testing.T) {
	t.Parallel()

	storage, rootDir := setupTestGraph(t)
	searcher, err := NewSearcher(storage, rootDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	tests := []struct {
		name           string
		target         string
		depth          int
		expectedCount  int
		expectedIDs    []string
	}{
		{
			name:          "direct callers",
			target:        "handler.ServeHTTP",
			depth:         1,
			expectedCount: 1,
			expectedIDs:   []string{"main.main"},
		},
		{
			name:          "transitive callers depth 2",
			target:        "service.Process",
			depth:         2,
			expectedCount: 2,
			expectedIDs:   []string{"handler.ServeHTTP", "main.main"},
		},
		{
			name:          "no callers",
			target:        "main.main",
			depth:         1,
			expectedCount: 0,
			expectedIDs:   []string{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &QueryRequest{
				Operation:      OperationCallers,
				Target:         tt.target,
				Depth:          tt.depth,
				IncludeContext: false,
			}

			resp, err := searcher.Query(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, tt.expectedCount, resp.TotalFound)
			assert.Equal(t, tt.expectedCount, len(resp.Results))

			// Check IDs
			resultIDs := make([]string, len(resp.Results))
			for i, r := range resp.Results {
				resultIDs[i] = r.Node.ID
			}
			assert.ElementsMatch(t, tt.expectedIDs, resultIDs)
		})
	}
}

func TestSearcher_QueryCallees(t *testing.T) {
	t.Parallel()

	storage, rootDir := setupTestGraph(t)
	searcher, err := NewSearcher(storage, rootDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationCallees,
		Target:         "handler.ServeHTTP",
		Depth:          1,
		IncludeContext: false,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, 1, resp.TotalFound)
	assert.Equal(t, 1, len(resp.Results))
	assert.Equal(t, "service.Process", resp.Results[0].Node.ID)
}

func TestSearcher_QueryDependencies(t *testing.T) {
	t.Parallel()

	storage, rootDir := setupTestGraph(t)
	searcher, err := NewSearcher(storage, rootDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationDependencies,
		Target:         "internal/mcp",
		IncludeContext: false,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, 1, resp.TotalFound)
	assert.Equal(t, 1, len(resp.Results))
	assert.Equal(t, "internal/graph", resp.Results[0].Node.ID)
}

func TestSearcher_QueryDependents(t *testing.T) {
	t.Parallel()

	storage, rootDir := setupTestGraph(t)
	searcher, err := NewSearcher(storage, rootDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationDependents,
		Target:         "internal/graph",
		IncludeContext: false,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, 1, resp.TotalFound)
	assert.Equal(t, 1, len(resp.Results))
	assert.Equal(t, "internal/mcp", resp.Results[0].Node.ID)
}

func TestSearcher_IncludeContext(t *testing.T) {
	t.Parallel()

	storage, rootDir := setupTestGraph(t)
	searcher, err := NewSearcher(storage, rootDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationCallers,
		Target:         "handler.ServeHTTP",
		Depth:          1,
		IncludeContext: true,
		ContextLines:   2,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should have context for main.main
	require.Len(t, resp.Results, 1)
	assert.NotEmpty(t, resp.Results[0].Context, "expected code context")
	assert.Contains(t, resp.Results[0].Context, "func main()", "expected function definition in context")
}

func TestSearcher_Reload(t *testing.T) {
	t.Parallel()

	storage, rootDir := setupTestGraph(t)
	searcher, err := NewSearcher(storage, rootDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	// Initial query should work
	req := &QueryRequest{
		Operation:      OperationCallers,
		Target:         "handler.ServeHTTP",
		Depth:          1,
		IncludeContext: false,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 1, resp.TotalFound)

	// Update graph data
	newData := &GraphData{
		Nodes: []Node{
			{ID: "newFunc", Kind: NodeFunction, File: "new.go", StartLine: 1, EndLine: 10},
		},
		Edges: []Edge{},
	}
	err = storage.Save(newData)
	require.NoError(t, err)

	// Reload
	err = searcher.Reload(ctx)
	require.NoError(t, err)

	// Old query should now return 0 results
	resp, err = searcher.Query(ctx, req)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.TotalFound)
}

func TestSearcher_MaxResults(t *testing.T) {
	t.Parallel()

	storage, rootDir := setupTestGraph(t)
	searcher, err := NewSearcher(storage, rootDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationCallers,
		Target:         "service.Process",
		Depth:          10, // Deep traversal
		IncludeContext: false,
		MaxResults:     1, // Limit to 1 result
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.LessOrEqual(t, len(resp.Results), 1, "should respect max results")
	if resp.TotalFound > 1 {
		assert.True(t, resp.Truncated, "should indicate truncation")
	}
}

// TestSearcher_QueryTypeUsages tests type usage queries
func TestSearcher_QueryTypeUsages(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Create test graph with type usage edges
	testData := &GraphData{
		Nodes: []Node{
			{ID: "testpkg.Config", Kind: NodeStruct, File: "types.go", StartLine: 5, EndLine: 10},
			{ID: "testpkg.Server", Kind: NodeStruct, File: "server.go", StartLine: 15, EndLine: 25},
			{ID: "testpkg.Handler", Kind: NodeStruct, File: "handler.go", StartLine: 30, EndLine: 35},
			{ID: "testpkg.NewServer", Kind: NodeFunction, File: "server.go", StartLine: 40, EndLine: 50},
			{ID: "testpkg.NewHandler", Kind: NodeFunction, File: "handler.go", StartLine: 55, EndLine: 65},
			{ID: "testpkg.Process", Kind: NodeFunction, File: "service.go", StartLine: 70, EndLine: 80},
		},
		Edges: []Edge{
			// Server uses Config as a field
			{From: "testpkg.Server", To: "testpkg.Config", Type: EdgeUsesType, Location: &Location{File: "server.go", Line: 17}},
			// Handler uses Config as a field
			{From: "testpkg.Handler", To: "testpkg.Config", Type: EdgeUsesType, Location: &Location{File: "handler.go", Line: 32}},
			// NewServer uses Config as a parameter
			{From: "testpkg.NewServer", To: "testpkg.Config", Type: EdgeUsesType, Location: &Location{File: "server.go", Line: 40}},
			// NewServer returns Server
			{From: "testpkg.NewServer", To: "testpkg.Server", Type: EdgeUsesType, Location: &Location{File: "server.go", Line: 40}},
			// NewHandler uses Config as a parameter
			{From: "testpkg.NewHandler", To: "testpkg.Config", Type: EdgeUsesType, Location: &Location{File: "handler.go", Line: 55}},
			// Process uses Server as a parameter
			{From: "testpkg.Process", To: "testpkg.Server", Type: EdgeUsesType, Location: &Location{File: "service.go", Line: 70}},
		},
	}

	err = storage.Save(testData)
	require.NoError(t, err)

	// Create test source files for context injection
	typesGo := filepath.Join(tmpDir, "types.go")
	err = os.WriteFile(typesGo, []byte(`package testpkg

type Config struct {
	Name string
	Port int
}
`), 0644)
	require.NoError(t, err)

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	tests := []struct {
		name          string
		target        string
		expectedCount int
		expectedIDs   []string
	}{
		{
			name:          "find all usages of Config type",
			target:        "testpkg.Config",
			expectedCount: 4,
			expectedIDs:   []string{"testpkg.Server", "testpkg.Handler", "testpkg.NewServer", "testpkg.NewHandler"},
		},
		{
			name:          "find all usages of Server type",
			target:        "testpkg.Server",
			expectedCount: 2,
			expectedIDs:   []string{"testpkg.NewServer", "testpkg.Process"},
		},
		{
			name:          "no usages for Handler type",
			target:        "testpkg.Handler",
			expectedCount: 0,
			expectedIDs:   []string{},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := &QueryRequest{
				Operation:      OperationTypeUsages,
				Target:         tt.target,
				IncludeContext: false,
			}

			resp, err := searcher.Query(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, resp)

			assert.Equal(t, tt.expectedCount, resp.TotalFound, "unexpected total found")
			assert.Equal(t, tt.expectedCount, len(resp.Results), "unexpected result count")

			// Check IDs
			resultIDs := make([]string, len(resp.Results))
			for i, r := range resp.Results {
				resultIDs[i] = r.Node.ID
				assert.Equal(t, 1, r.Depth, "type usages should always be depth 1")
			}
			assert.ElementsMatch(t, tt.expectedIDs, resultIDs, "unexpected result IDs")
		})
	}
}

// TestSearcher_QueryTypeUsages_WithContext tests type usage queries with context injection
func TestSearcher_QueryTypeUsages_WithContext(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Create test graph
	testData := &GraphData{
		Nodes: []Node{
			{ID: "testpkg.Config", Kind: NodeStruct, File: "types.go", StartLine: 3, EndLine: 6},
			{ID: "testpkg.Server", Kind: NodeStruct, File: "server.go", StartLine: 3, EndLine: 5},
		},
		Edges: []Edge{
			{From: "testpkg.Server", To: "testpkg.Config", Type: EdgeUsesType, Location: &Location{File: "server.go", Line: 4}},
		},
	}

	err = storage.Save(testData)
	require.NoError(t, err)

	// Create test source files
	typesGo := filepath.Join(tmpDir, "types.go")
	err = os.WriteFile(typesGo, []byte(`package testpkg

type Config struct {
	Name string
	Port int
}
`), 0644)
	require.NoError(t, err)

	serverGo := filepath.Join(tmpDir, "server.go")
	err = os.WriteFile(serverGo, []byte(`package testpkg

type Server struct {
	config *Config
}
`), 0644)
	require.NoError(t, err)

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationTypeUsages,
		Target:         "testpkg.Config",
		IncludeContext: true,
		ContextLines:   2,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, 1, resp.TotalFound)
	assert.Equal(t, 1, len(resp.Results))

	// Verify context was included
	result := resp.Results[0]
	assert.Equal(t, "testpkg.Server", result.Node.ID)
	assert.NotEmpty(t, result.Context, "context should be included")
	assert.Contains(t, result.Context, "config *Config", "context should contain the type usage")
}
