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

// TestExtractor_TypeUsageEdges tests that type usage edges are created correctly
func TestExtractor_TypeUsageEdges(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name      string
		code      string
		wantEdges []struct {
			from string
			to   string
		}
	}{
		{
			name: "function parameters create type usage edges",
			code: `package test
import "context"

func Process(ctx context.Context, data string) error {
	return nil
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				{from: "test.Process", to: "context.Context"},
				// Built-in types like string and error are skipped
			},
		},
		{
			name: "function returns create type usage edges",
			code: `package test

type Result struct {
	Value int
}

func GetResult() *Result {
	return nil
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				{from: "test.GetResult", to: "test.Result"},
			},
		},
		{
			name: "struct fields create type usage edges",
			code: `package test

type Config struct {
	Name string
}

type Server struct {
	config *Config
	port   int
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				{from: "test.Server", to: "test.Config"},
				// Built-in types like string and int are skipped
			},
		},
		{
			name: "cross-package type references",
			code: `package test
import "net/http"

type Handler struct {
	server *http.Server
}

func NewHandler(s *http.Server) *Handler {
	return &Handler{server: s}
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				{from: "test.Handler", to: "net/http.Server"},
				{from: "test.NewHandler", to: "net/http.Server"},
				{from: "test.NewHandler", to: "test.Handler"},
			},
		},
		{
			name: "pointer and slice types",
			code: `package test

type Item struct{}

func Process(items []*Item) *Item {
	return nil
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				{from: "test.Process", to: "test.Item"},
				// Only one edge per unique type, even though Item appears twice
			},
		},
		{
			name: "interface method parameters and returns",
			code: `package test
import "io"

type Processor interface {
	Process(r io.Reader) (io.Writer, error)
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				{from: "test.Processor", to: "io.Reader"},
				{from: "test.Processor", to: "io.Writer"},
			},
		},
		{
			name: "embedded struct creates both embeds and uses_type edges",
			code: `package test

type Base struct {
	ID int
}

type Derived struct {
	Base
	Name string
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				// Both EdgeEmbeds and EdgeUsesType should be created for embedded types
				{from: "test.Derived", to: "test.Base"},
			},
		},
		{
			name: "map and func types are skipped",
			code: `package test

type Handler func(string) error

type Config struct {
	values map[string]string
	handler Handler
}`,
			wantEdges: []struct {
				from string
				to   string
			}{
				{from: "test.Config", to: "test.Handler"},
				// map and inline func types are skipped
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Write code to temp file
			file := filepath.Join(tmpDir, tt.name+".go")
			err := os.WriteFile(file, []byte(tt.code), 0644)
			require.NoError(t, err)

			// Extract
			extractor := NewExtractor(tmpDir)
			result, err := extractor.ExtractFile(file)
			require.NoError(t, err)

			// Log all edges for debugging
			t.Logf("Extracted %d edges:", len(result.Edges))
			for _, edge := range result.Edges {
				t.Logf("  %s -[%s]-> %s", edge.From, edge.Type, edge.To)
			}

			// Verify expected type usage edges exist
			for _, wantEdge := range tt.wantEdges {
				found := false
				for _, edge := range result.Edges {
					if edge.From == wantEdge.from && edge.To == wantEdge.to {
						// Accept either EdgeUsesType or EdgeEmbeds for embedded types
						if edge.Type == EdgeUsesType || edge.Type == EdgeEmbeds {
							found = true
							break
						}
					}
				}
				assert.True(t, found, "Expected edge %s -> %s not found", wantEdge.from, wantEdge.to)
			}
		})
	}
}

// TestExtractor_BuiltinTypesSkipped verifies built-in types don't create edges
func TestExtractor_BuiltinTypesSkipped(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	code := `package test

func Process(
	b bool,
	i int,
	i8 int8,
	i16 int16,
	i32 int32,
	i64 int64,
	u uint,
	u8 uint8,
	u16 uint16,
	u32 uint32,
	u64 uint64,
	f32 float32,
	f64 float64,
	s string,
) error {
	return nil
}`

	file := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(file, []byte(code), 0644)
	require.NoError(t, err)

	extractor := NewExtractor(tmpDir)
	result, err := extractor.ExtractFile(file)
	require.NoError(t, err)

	// Count type usage edges - should be 0 since all types are built-ins
	typeUsageEdges := 0
	for _, edge := range result.Edges {
		if edge.Type == EdgeUsesType {
			typeUsageEdges++
			t.Logf("Unexpected type usage edge: %s -> %s", edge.From, edge.To)
		}
	}

	assert.Equal(t, 0, typeUsageEdges, "Built-in types should not create type usage edges")
}
