package graph

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Benchmark for queryCallers to compare old vs new implementation
func BenchmarkQueryCallers(b *testing.B) {
	tmpDir := b.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(b, err)

	// Create a large call graph for benchmarking
	nodes := make([]Node, 0, 100)
	edges := make([]Edge, 0, 200)

	// Create a deep call chain: func0 -> func1 -> func2 -> ... -> func99
	for i := 0; i < 100; i++ {
		nodes = append(nodes, Node{
			ID:        "pkg.func" + string(rune('0'+i)),
			Kind:      NodeFunction,
			File:      "bench.go",
			StartLine: i * 10,
			EndLine:   i*10 + 5,
		})

		if i > 0 {
			// func(i-1) calls func(i)
			edges = append(edges, Edge{
				From: "pkg.func" + string(rune('0'+i-1)),
				To:   "pkg.func" + string(rune('0'+i)),
				Type: EdgeCalls,
				Location: &Location{
					File: "bench.go",
					Line: i * 10,
				},
			})
		}
	}

	testData := &GraphData{
		Nodes: nodes,
		Edges: edges,
	}

	err = storage.Save(testData)
	require.NoError(b, err)

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(b, err)
	defer searcher.Close()

	ctx := context.Background()

	b.Run("depth=1", func(b *testing.B) {
		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "pkg.func50",
			Depth:          1,
			IncludeContext: false,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := searcher.Query(ctx, req)
			require.NoError(b, err)
		}
	})

	b.Run("depth=5", func(b *testing.B) {
		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "pkg.func50",
			Depth:          5,
			IncludeContext: false,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := searcher.Query(ctx, req)
			require.NoError(b, err)
		}
	})

	b.Run("depth=10", func(b *testing.B) {
		req := &QueryRequest{
			Operation:      OperationCallers,
			Target:         "pkg.func50",
			Depth:          10,
			IncludeContext: false,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := searcher.Query(ctx, req)
			require.NoError(b, err)
		}
	})
}

// Benchmark for queryCallees
func BenchmarkQueryCallees(b *testing.B) {
	tmpDir := b.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(b, err)

	// Create a large call graph
	nodes := make([]Node, 0, 100)
	edges := make([]Edge, 0, 200)

	for i := 0; i < 100; i++ {
		nodes = append(nodes, Node{
			ID:        "pkg.func" + string(rune('0'+i)),
			Kind:      NodeFunction,
			File:      "bench.go",
			StartLine: i * 10,
			EndLine:   i*10 + 5,
		})

		if i > 0 {
			edges = append(edges, Edge{
				From: "pkg.func" + string(rune('0'+i-1)),
				To:   "pkg.func" + string(rune('0'+i)),
				Type: EdgeCalls,
				Location: &Location{
					File: "bench.go",
					Line: i * 10,
				},
			})
		}
	}

	testData := &GraphData{
		Nodes: nodes,
		Edges: edges,
	}

	err = storage.Save(testData)
	require.NoError(b, err)

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(b, err)
	defer searcher.Close()

	ctx := context.Background()

	b.Run("depth=1", func(b *testing.B) {
		req := &QueryRequest{
			Operation:      OperationCallees,
			Target:         "pkg.func0",
			Depth:          1,
			IncludeContext: false,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := searcher.Query(ctx, req)
			require.NoError(b, err)
		}
	})

	b.Run("depth=5", func(b *testing.B) {
		req := &QueryRequest{
			Operation:      OperationCallees,
			Target:         "pkg.func0",
			Depth:          5,
			IncludeContext: false,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := searcher.Query(ctx, req)
			require.NoError(b, err)
		}
	})

	b.Run("depth=10", func(b *testing.B) {
		req := &QueryRequest{
			Operation:      OperationCallees,
			Target:         "pkg.func0",
			Depth:          10,
			IncludeContext: false,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := searcher.Query(ctx, req)
			require.NoError(b, err)
		}
	})
}
