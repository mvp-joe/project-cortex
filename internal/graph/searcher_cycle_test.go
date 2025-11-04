package graph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSearcher_QueryCallers_WithCycle tests that cycle detection works correctly
func TestSearcher_QueryCallers_WithCycle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Create a circular call graph: A -> B -> C -> A
	testData := &GraphData{
		Nodes: []Node{
			{ID: "pkg.A", Kind: NodeFunction, File: "a.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.B", Kind: NodeFunction, File: "b.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.C", Kind: NodeFunction, File: "c.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.D", Kind: NodeFunction, File: "d.go", StartLine: 1, EndLine: 10},
		},
		Edges: []Edge{
			{From: "pkg.A", To: "pkg.B", Type: EdgeCalls, Location: &Location{File: "a.go", Line: 5}},
			{From: "pkg.B", To: "pkg.C", Type: EdgeCalls, Location: &Location{File: "b.go", Line: 5}},
			{From: "pkg.C", To: "pkg.A", Type: EdgeCalls, Location: &Location{File: "c.go", Line: 5}}, // Cycle!
			{From: "pkg.D", To: "pkg.C", Type: EdgeCalls, Location: &Location{File: "d.go", Line: 5}},
		},
	}

	err = storage.Save(testData)
	require.NoError(t, err)

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	t.Run("cycle detection at depth 5", func(t *testing.T) {
		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "pkg.A",
			Depth:          5, // Deep enough to expose cycles
			IncludeContext: false,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		// Should find C (direct caller), B and D (depth 2), and A (through cycle at depth 3)
		// But should NOT infinitely loop - each node visited only once
		assert.Equal(t, 4, resp.TotalFound, "should find 4 callers: C (depth 1), B (depth 2), D (depth 2), A (depth 3)")

		// Verify each node appears exactly once
		nodeCount := make(map[string]int)
		for _, result := range resp.Results {
			nodeCount[result.Node.ID]++
		}

		assert.Equal(t, 1, nodeCount["pkg.A"], "pkg.A should appear exactly once (cycle detected)")
		assert.Equal(t, 1, nodeCount["pkg.B"], "pkg.B should appear exactly once")
		assert.Equal(t, 1, nodeCount["pkg.C"], "pkg.C should appear exactly once")
		assert.Equal(t, 1, nodeCount["pkg.D"], "pkg.D should appear exactly once")
	})

	t.Run("starting from D", func(t *testing.T) {
		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "pkg.D",
			Depth:          3,
			IncludeContext: false,
		}

		resp, err := searcher.Query(ctx, req)
		require.NoError(t, err)

		// D has no callers in this graph
		assert.Equal(t, 0, resp.TotalFound, "D should have no callers")
	})
}

// TestSearcher_QueryCallees_WithCycle tests callees with cycles
func TestSearcher_QueryCallees_WithCycle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Create a circular call graph: A -> B -> C -> A
	testData := &GraphData{
		Nodes: []Node{
			{ID: "pkg.A", Kind: NodeFunction, File: "a.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.B", Kind: NodeFunction, File: "b.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.C", Kind: NodeFunction, File: "c.go", StartLine: 1, EndLine: 10},
		},
		Edges: []Edge{
			{From: "pkg.A", To: "pkg.B", Type: EdgeCalls, Location: &Location{File: "a.go", Line: 5}},
			{From: "pkg.B", To: "pkg.C", Type: EdgeCalls, Location: &Location{File: "b.go", Line: 5}},
			{From: "pkg.C", To: "pkg.A", Type: EdgeCalls, Location: &Location{File: "c.go", Line: 5}}, // Cycle!
		},
	}

	err = storage.Save(testData)
	require.NoError(t, err)

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationCallees,
		Target:         "pkg.A",
		Depth:          5, // Deep enough to expose cycles
		IncludeContext: false,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should find B (direct callee) and C, A (through cycle)
	// But should NOT infinitely loop
	assert.Equal(t, 3, resp.TotalFound, "should find 3 callees: B (depth 1), C (depth 2), A (depth 3)")

	// Verify each node appears exactly once
	nodeCount := make(map[string]int)
	for _, result := range resp.Results {
		nodeCount[result.Node.ID]++
	}

	assert.Equal(t, 1, nodeCount["pkg.A"], "pkg.A should appear exactly once (cycle detected)")
	assert.Equal(t, 1, nodeCount["pkg.B"], "pkg.B should appear exactly once")
	assert.Equal(t, 1, nodeCount["pkg.C"], "pkg.C should appear exactly once")
}

// TestSearcher_ComplexCycle tests a more complex cycle scenario
func TestSearcher_ComplexCycle(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Create a diamond with cycle:
	//     A
	//    / \
	//   B   C
	//    \ /
	//     D
	//     |
	//     A (cycle back)
	testData := &GraphData{
		Nodes: []Node{
			{ID: "pkg.A", Kind: NodeFunction, File: "a.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.B", Kind: NodeFunction, File: "b.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.C", Kind: NodeFunction, File: "c.go", StartLine: 1, EndLine: 10},
			{ID: "pkg.D", Kind: NodeFunction, File: "d.go", StartLine: 1, EndLine: 10},
		},
		Edges: []Edge{
			{From: "pkg.A", To: "pkg.B", Type: EdgeCalls},
			{From: "pkg.A", To: "pkg.C", Type: EdgeCalls},
			{From: "pkg.B", To: "pkg.D", Type: EdgeCalls},
			{From: "pkg.C", To: "pkg.D", Type: EdgeCalls},
			{From: "pkg.D", To: "pkg.A", Type: EdgeCalls}, // Cycle back to A
		},
	}

	err = storage.Save(testData)
	require.NoError(t, err)

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	ctx := context.Background()

	req := &QueryRequest{
		Operation:      OperationCallees,
		Target:         "pkg.A",
		Depth:          10, // Very deep to stress test cycle detection
		IncludeContext: false,
	}

	resp, err := searcher.Query(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should find all 4 nodes (A calls B and C, both call D, D calls A)
	assert.Equal(t, 4, resp.TotalFound, "should find all 4 nodes in the graph")

	// Verify no duplicates
	nodeCount := make(map[string]int)
	for _, result := range resp.Results {
		nodeCount[result.Node.ID]++
	}

	assert.Equal(t, 1, nodeCount["pkg.A"], "pkg.A should appear exactly once")
	assert.Equal(t, 1, nodeCount["pkg.B"], "pkg.B should appear exactly once")
	assert.Equal(t, 1, nodeCount["pkg.C"], "pkg.C should appear exactly once")
	assert.Equal(t, 1, nodeCount["pkg.D"], "pkg.D should appear exactly once")
}
