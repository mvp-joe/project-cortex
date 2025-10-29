package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for GraphExtractor:
// - Extract nodes and edges from simple function with call
// - Extract method nodes from structs with pointer receivers
// - Extract package import edges
// - Extract package path correctly from file paths
// - Handle various Go code patterns (functions, methods, calls)

func TestExtractor_ExtractFile(t *testing.T) {
	t.Parallel()

	// Create temp directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name          string
		source        string
		expectedNodes int
		expectedEdges int
		checkNode     func(*testing.T, []Node)
		checkEdge     func(*testing.T, []Edge)
	}{
		{
			name: "simple function with call",
			source: `package main

func foo() {
	bar()
}

func bar() {
}
`,
			expectedNodes: 3, // package + 2 functions
			expectedEdges: 1, // 1 call (no imports for main)
			checkNode: func(t *testing.T, nodes []Node) {
				// Check function nodes exist
				var foundFoo, foundBar bool
				for _, node := range nodes {
					if node.ID == "main.foo" {
						foundFoo = true
						assert.Equal(t, NodeFunction, node.Kind)
					}
					if node.ID == "main.bar" {
						foundBar = true
						assert.Equal(t, NodeFunction, node.Kind)
					}
				}
				assert.True(t, foundFoo, "expected to find main.foo")
				assert.True(t, foundBar, "expected to find main.bar")
			},
			checkEdge: func(t *testing.T, edges []Edge) {
				// Check call edge exists
				var foundCall bool
				for _, edge := range edges {
					if edge.From == "main.foo" && edge.To == "main.bar" && edge.Type == EdgeCalls {
						foundCall = true
					}
				}
				assert.True(t, foundCall, "expected call edge from foo to bar")
			},
		},
		{
			name: "method on struct",
			source: `package test

type Handler struct{}

func (h *Handler) ServeHTTP() {
	h.process()
}

func (h *Handler) process() {
}
`,
			expectedNodes: 4, // package + struct + 2 methods
			expectedEdges: 0, // 0 imports
			checkNode: func(t *testing.T, nodes []Node) {
				var foundServeHTTP bool
				for _, node := range nodes {
					if node.ID == "test.Handler.ServeHTTP" {
						foundServeHTTP = true
						assert.Equal(t, NodeMethod, node.Kind)
					}
				}
				assert.True(t, foundServeHTTP, "expected to find test.Handler.ServeHTTP")
			},
			checkEdge: func(t *testing.T, edges []Edge) {
				var foundCall bool
				for _, edge := range edges {
					t.Logf("Edge: From=%s, To=%s, Type=%s", edge.From, edge.To, edge.Type)
					if edge.From == "test.Handler.ServeHTTP" && edge.To == "h.process" && edge.Type == EdgeCalls {
						foundCall = true
					}
				}
				assert.True(t, foundCall, "expected call edge from ServeHTTP to process")
			},
		},
		{
			name: "package imports",
			source: `package test

import (
	"fmt"
	"os"
)

func main() {
	fmt.Println("hello")
	os.Exit(0)
}
`,
			expectedNodes: 2, // package + 1 function
			expectedEdges: 2, // 2 imports (fmt, os)
			checkEdge: func(t *testing.T, edges []Edge) {
				var foundFmtImport, foundOsImport bool
				for _, edge := range edges {
					if edge.Type == EdgeImports {
						if edge.To == "fmt" {
							foundFmtImport = true
						}
						if edge.To == "os" {
							foundOsImport = true
						}
					}
				}
				assert.True(t, foundFmtImport, "expected import edge to fmt")
				assert.True(t, foundOsImport, "expected import edge to os")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Write test file
			testFile := filepath.Join(tmpDir, tt.name+".go")
			err := os.WriteFile(testFile, []byte(tt.source), 0644)
			require.NoError(t, err)

			// Extract
			extractor := NewExtractor(tmpDir)
			result, err := extractor.ExtractFile(testFile)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Check counts
			assert.GreaterOrEqual(t, len(result.Nodes), tt.expectedNodes, "unexpected node count")
			assert.GreaterOrEqual(t, len(result.Edges), tt.expectedEdges, "unexpected edge count")

			// Run custom checks
			if tt.checkNode != nil {
				tt.checkNode(t, result.Nodes)
			}
			if tt.checkEdge != nil {
				tt.checkEdge(t, result.Edges)
			}
		})
	}
}

func TestExtractor_PackagePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		relPath  string
		expected string
	}{
		{"main.go", "main"},
		{"internal/graph/extractor.go", "internal/graph"},
		{"cmd/cortex/main.go", "cmd/cortex"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.relPath, func(t *testing.T) {
			t.Parallel()
			result := extractPackagePath(tt.relPath)
			assert.Equal(t, tt.expected, result)
		})
	}
}
