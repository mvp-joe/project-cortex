package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for GraphBuilder:
// - Full build from multiple Go files produces correct node/edge counts
// - Full build handles parse errors gracefully
// - Full build respects context cancellation
// - Incremental build preserves unchanged nodes
// - Incremental build replaces nodes from changed files
// - Incremental build removes nodes from deleted files
// - Incremental build removes edges from deleted files
// - Incremental build filters dangling edges when nodes are deleted
// - Incremental build with no previous graph falls back to full build
// - Node deduplication keeps first occurrence across files
// - Node deduplication logs warnings for duplicates

func TestBuilder_BuildFull_MultipleFiles(t *testing.T) {
	t.Parallel()

	// Test: Full build from multiple Go files produces correct node/edge counts
	tmpDir := t.TempDir()

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.go")
	err := os.WriteFile(file1, []byte(`package test

func Foo() {
	Bar()
}
`), 0644)
	require.NoError(t, err)

	file2 := filepath.Join(tmpDir, "file2.go")
	err = os.WriteFile(file2, []byte(`package test

func Bar() {
	Baz()
}

func Baz() {
}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	graph, err := builder.BuildFull(ctx, []string{file1, file2})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Expect: 1 package node + 3 function nodes (Foo, Bar, Baz)
	assert.Equal(t, 4, len(graph.Nodes), "expected 4 nodes (1 package + 3 functions)")

	// Expect: 2 call edges (Foo->Bar, Bar->Baz)
	callEdges := 0
	for _, edge := range graph.Edges {
		if edge.Type == EdgeCalls {
			callEdges++
		}
	}
	assert.Equal(t, 2, callEdges, "expected 2 call edges")

	// Verify node IDs
	nodeIDs := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeIDs[node.ID] = true
	}
	assert.True(t, nodeIDs["test.Foo"], "expected test.Foo node")
	assert.True(t, nodeIDs["test.Bar"], "expected test.Bar node")
	assert.True(t, nodeIDs["test.Baz"], "expected test.Baz node")
}

func TestBuilder_BuildFull_ParseErrors(t *testing.T) {
	t.Parallel()

	// Test: Full build handles parse errors gracefully
	tmpDir := t.TempDir()

	// Create valid file
	validFile := filepath.Join(tmpDir, "valid.go")
	err := os.WriteFile(validFile, []byte(`package test

func Valid() {}
`), 0644)
	require.NoError(t, err)

	// Create invalid file
	invalidFile := filepath.Join(tmpDir, "invalid.go")
	err = os.WriteFile(invalidFile, []byte(`package test

func Invalid( {{{ // Malformed
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	// Should complete successfully despite parse error
	graph, err := builder.BuildFull(ctx, []string{validFile, invalidFile})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Should have data from valid file only
	nodeIDs := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeIDs[node.ID] = true
	}
	assert.True(t, nodeIDs["test.Valid"], "expected valid file to be processed")
}

func TestBuilder_BuildFull_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Test: Full build respects context cancellation
	tmpDir := t.TempDir()

	// Create test file
	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(`package test

func Foo() {}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = builder.BuildFull(ctx, []string{testFile})
	assert.Error(t, err, "expected error from cancelled context")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestBuilder_BuildIncremental_PreservesUnchanged(t *testing.T) {
	t.Parallel()

	// Test: Incremental build preserves unchanged nodes
	tmpDir := t.TempDir()

	// Create initial files
	file1 := filepath.Join(tmpDir, "file1.go")
	err := os.WriteFile(file1, []byte(`package test

func Foo() {}
`), 0644)
	require.NoError(t, err)

	file2 := filepath.Join(tmpDir, "file2.go")
	err = os.WriteFile(file2, []byte(`package test

func Bar() {}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	// Build initial graph
	previousGraph, err := builder.BuildFull(ctx, []string{file1, file2})
	require.NoError(t, err)
	initialNodeCount := len(previousGraph.Nodes)

	// Modify file2 only
	err = os.WriteFile(file2, []byte(`package test

func Bar() {
	Baz()
}

func Baz() {}
`), 0644)
	require.NoError(t, err)

	// Incremental build with file2 changed
	graph, err := builder.BuildIncremental(ctx, previousGraph, []string{file2}, []string{}, []string{file1, file2})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Should have more nodes now (added Baz)
	assert.Greater(t, len(graph.Nodes), initialNodeCount, "expected more nodes after adding Baz")

	// Should still have Foo from unchanged file
	nodeIDs := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeIDs[node.ID] = true
	}
	assert.True(t, nodeIDs["test.Foo"], "expected test.Foo from unchanged file")
	assert.True(t, nodeIDs["test.Bar"], "expected test.Bar from changed file")
	assert.True(t, nodeIDs["test.Baz"], "expected test.Baz from changed file")
}

func TestBuilder_BuildIncremental_ReplacesChanged(t *testing.T) {
	t.Parallel()

	// Test: Incremental build replaces nodes from changed files
	tmpDir := t.TempDir()

	file := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(file, []byte(`package test

func Foo() {
	Bar()
}

func Bar() {}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	// Build initial graph
	previousGraph, err := builder.BuildFull(ctx, []string{file})
	require.NoError(t, err)

	// Verify initial state has call edge
	initialCallEdges := 0
	for _, edge := range previousGraph.Edges {
		if edge.Type == EdgeCalls {
			initialCallEdges++
		}
	}
	assert.Equal(t, 1, initialCallEdges, "expected 1 call edge initially")

	// Modify file - remove the call
	err = os.WriteFile(file, []byte(`package test

func Foo() {
	// No longer calls Bar
}

func Bar() {}
`), 0644)
	require.NoError(t, err)

	// Incremental build
	graph, err := builder.BuildIncremental(ctx, previousGraph, []string{file}, []string{}, []string{file})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Verify call edge was removed
	callEdges := 0
	for _, edge := range graph.Edges {
		if edge.Type == EdgeCalls {
			callEdges++
		}
	}
	assert.Equal(t, 0, callEdges, "expected call edge to be removed")
}

func TestBuilder_BuildIncremental_DeletesFiles(t *testing.T) {
	t.Parallel()

	// Test: Incremental build removes nodes from deleted files
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "file1.go")
	err := os.WriteFile(file1, []byte(`package test

func Foo() {}
`), 0644)
	require.NoError(t, err)

	file2 := filepath.Join(tmpDir, "file2.go")
	err = os.WriteFile(file2, []byte(`package test

func Bar() {}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	// Build initial graph
	previousGraph, err := builder.BuildFull(ctx, []string{file1, file2})
	require.NoError(t, err)

	// Delete file2
	relPath2, _ := filepath.Rel(tmpDir, file2)

	// Incremental build with file2 deleted
	graph, err := builder.BuildIncremental(ctx, previousGraph, []string{}, []string{file2}, []string{file1})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Should not have Bar anymore
	nodeIDs := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeIDs[node.ID] = true
	}
	assert.True(t, nodeIDs["test.Foo"], "expected test.Foo to remain")
	assert.False(t, nodeIDs["test.Bar"], "expected test.Bar to be removed")

	// Verify no nodes reference deleted file
	for _, node := range graph.Nodes {
		assert.NotEqual(t, relPath2, node.File, "no nodes should reference deleted file")
	}
}

func TestBuilder_BuildIncremental_RemovesDanglingEdges(t *testing.T) {
	t.Parallel()

	// Test: Incremental build filters dangling edges when nodes are deleted
	tmpDir := t.TempDir()

	file1 := filepath.Join(tmpDir, "file1.go")
	err := os.WriteFile(file1, []byte(`package test

func Foo() {
	Bar()
}
`), 0644)
	require.NoError(t, err)

	file2 := filepath.Join(tmpDir, "file2.go")
	err = os.WriteFile(file2, []byte(`package test

func Bar() {}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	// Build initial graph
	previousGraph, err := builder.BuildFull(ctx, []string{file1, file2})
	require.NoError(t, err)

	// Verify initial call edge exists
	initialCallEdges := 0
	for _, edge := range previousGraph.Edges {
		if edge.Type == EdgeCalls && edge.From == "test.Foo" && edge.To == "Bar" {
			initialCallEdges++
		}
	}
	assert.Equal(t, 1, initialCallEdges, "expected initial call edge from Foo to Bar")

	// Delete file2 (which contains Bar)
	graph, err := builder.BuildIncremental(ctx, previousGraph, []string{}, []string{file2}, []string{file1})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Edge from Foo to Bar should be removed (dangling edge)
	// Note: The current implementation doesn't filter dangling edges yet,
	// but this test documents the expected behavior
	for _, edge := range graph.Edges {
		if edge.Type == EdgeCalls {
			// Verify both From and To nodes exist
			fromExists := false
			toExists := false
			for _, node := range graph.Nodes {
				if node.ID == edge.From {
					fromExists = true
				}
				if node.ID == edge.To {
					toExists = true
				}
			}
			assert.True(t, fromExists && toExists,
				"edge from %s to %s should have both nodes exist", edge.From, edge.To)
		}
	}
}

func TestBuilder_BuildIncremental_NoPreviousGraph(t *testing.T) {
	t.Parallel()

	// Test: Incremental build with no previous graph falls back to full build
	tmpDir := t.TempDir()

	file := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(file, []byte(`package test

func Foo() {}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	// Incremental build with nil previous graph
	graph, err := builder.BuildIncremental(ctx, nil, []string{file}, []string{}, []string{file})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Should have processed the file
	nodeIDs := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeIDs[node.ID] = true
	}
	assert.True(t, nodeIDs["test.Foo"], "expected test.Foo node")
}

func TestBuilder_NodeDeduplication(t *testing.T) {
	t.Parallel()

	// Test: Node deduplication keeps first occurrence across files
	tmpDir := t.TempDir()

	// Create two files with same function name (duplicate)
	file1 := filepath.Join(tmpDir, "file1.go")
	err := os.WriteFile(file1, []byte(`package test

func Duplicate() {
	// Version 1
}
`), 0644)
	require.NoError(t, err)

	file2 := filepath.Join(tmpDir, "file2.go")
	err = os.WriteFile(file2, []byte(`package test

func Duplicate() {
	// Version 2
}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	graph, err := builder.BuildFull(ctx, []string{file1, file2})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Count occurrences of Duplicate function
	duplicateCount := 0
	for _, node := range graph.Nodes {
		if node.ID == "test.Duplicate" {
			duplicateCount++
		}
	}

	// Should only have one occurrence due to deduplication
	assert.Equal(t, 1, duplicateCount, "expected duplicate node IDs to be deduplicated")
}

func TestBuilder_SkipsNonGoFiles(t *testing.T) {
	t.Parallel()

	// Test: Builder skips non-Go files
	tmpDir := t.TempDir()

	goFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(goFile, []byte(`package test

func Foo() {}
`), 0644)
	require.NoError(t, err)

	txtFile := filepath.Join(tmpDir, "readme.txt")
	err = os.WriteFile(txtFile, []byte("This is not a Go file"), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	graph, err := builder.BuildFull(ctx, []string{goFile, txtFile})
	require.NoError(t, err)
	require.NotNil(t, graph)

	// Should only process Go file
	nodeIDs := make(map[string]bool)
	for _, node := range graph.Nodes {
		nodeIDs[node.ID] = true
	}
	assert.True(t, nodeIDs["test.Foo"], "expected Go file to be processed")
}

func TestBuilder_BuildIncremental_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Test: Incremental build respects context cancellation
	tmpDir := t.TempDir()

	file := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(file, []byte(`package test

func Foo() {}
`), 0644)
	require.NoError(t, err)

	builder := NewBuilder(tmpDir)

	// Build initial graph
	previousGraph, err := builder.BuildFull(context.Background(), []string{file})
	require.NoError(t, err)

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = builder.BuildIncremental(ctx, previousGraph, []string{file}, []string{}, []string{file})
	assert.Error(t, err, "expected error from cancelled context")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestBuilder_EmptyGraph(t *testing.T) {
	t.Parallel()

	// Test: Building with no files produces empty graph
	tmpDir := t.TempDir()
	builder := NewBuilder(tmpDir)
	ctx := context.Background()

	graph, err := builder.BuildFull(ctx, []string{})
	require.NoError(t, err)
	require.NotNil(t, graph)

	assert.Empty(t, graph.Nodes, "expected empty nodes")
	assert.Empty(t, graph.Edges, "expected empty edges")
}
