package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for GraphExtractor:
// - Extract domain structs (Functions, Types, Imports, etc.) from Go files
// - Extract functions with parameters and return values
// - Extract methods on structs with receivers
// - Extract types (structs, interfaces) with fields/methods
// - Extract function calls with caller/callee relationships
// - Extract imports with proper categorization (stdlib, external, relative)
// - Handle various Go code patterns (functions, methods, calls, embedded types)

func TestExtractor_ExtractCodeStructure(t *testing.T) {
	t.Parallel()

	// Create temp directory for test files
	tmpDir := t.TempDir()

	tests := []struct {
		name  string
		source        string
		check func(*testing.T, *CodeStructure)
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
			check: func(t *testing.T, result *CodeStructure) {
				// Should have 2 functions
				require.Len(t, result.Functions, 2)

				// Check functions exist
				var foundFoo, foundBar bool
				for _, fn := range result.Functions {
					if fn.Name == "foo" {
						foundFoo = true
						assert.False(t, fn.IsMethod)
						assert.True(t, fn.IsExported == false) // lowercase
					}
					if fn.Name == "bar" {
						foundBar = true
						assert.False(t, fn.IsMethod)
					}
				}
				assert.True(t, foundFoo, "expected to find foo function")
				assert.True(t, foundBar, "expected to find bar function")

				// Should have 1 function call (foo -> bar)
				require.Len(t, result.FunctionCalls, 1)
				assert.Equal(t, "main.bar", result.FunctionCalls[0].CalleeName)
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
			check: func(t *testing.T, result *CodeStructure) {
				// Should have 1 type (Handler struct)
				require.Len(t, result.Types, 1)
				assert.Equal(t, "Handler", result.Types[0].Name)
				assert.Equal(t, "struct", result.Types[0].Kind)
				assert.Equal(t, 2, result.Types[0].MethodCount)

				// Should have 2 methods
				require.Len(t, result.Functions, 2)
				for _, fn := range result.Functions {
					assert.True(t, fn.IsMethod, "expected method, got function")
					assert.NotNil(t, fn.ReceiverTypeID)
					assert.NotNil(t, fn.ReceiverTypeName)
					assert.Equal(t, "Handler", *fn.ReceiverTypeName)
				}

				// Should have 1 function call (ServeHTTP -> process)
				require.Len(t, result.FunctionCalls, 1)
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
			check: func(t *testing.T, result *CodeStructure) {
				// Should have 2 imports
				require.Len(t, result.Imports, 2)

				var foundFmt, foundOs bool
				for _, imp := range result.Imports {
					if imp.ImportPath == "fmt" {
						foundFmt = true
						assert.True(t, imp.IsStandardLib)
						assert.False(t, imp.IsExternal)
						assert.False(t, imp.IsRelative)
					}
					if imp.ImportPath == "os" {
						foundOs = true
						assert.True(t, imp.IsStandardLib)
					}
				}
				assert.True(t, foundFmt, "expected fmt import")
				assert.True(t, foundOs, "expected os import")

				// Should have 1 function (main)
				require.Len(t, result.Functions, 1)
				assert.Equal(t, "main", result.Functions[0].Name)
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

			// Extract using new method
			extractor := NewExtractor(tmpDir)
			result, err := extractor.ExtractCodeStructure(testFile)
			require.NoError(t, err)
			require.NotNil(t, result)

			// Run custom checks
			if tt.check != nil {
				tt.check(t, result)
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

// TestExtractor_BytePositionCapture verifies that byte positions are captured correctly
func TestExtractor_BytePositionCapture(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Test source with known byte positions
	// We'll verify positions match expected offsets
	source := `package test

type Handler struct {
	name string
}

func (h *Handler) Process() {
	h.helper()
}

func (h *Handler) helper() {
}
`

	testFile := filepath.Join(tmpDir, "test.go")
	err := os.WriteFile(testFile, []byte(source), 0644)
	require.NoError(t, err)

	// Extract
	extractor := NewExtractor(tmpDir)
	result, err := extractor.ExtractCodeStructure(testFile)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify type has byte positions
	require.Len(t, result.Types, 1)
	handlerType := result.Types[0]
	assert.Equal(t, "Handler", handlerType.Name)
	assert.Greater(t, handlerType.StartPos, 0, "StartPos should be > 0")
	assert.Greater(t, handlerType.EndPos, handlerType.StartPos, "EndPos should be > StartPos")

	// Verify the captured positions actually point to the type definition
	typeCode := source[handlerType.StartPos:handlerType.EndPos]
	assert.Contains(t, typeCode, "type Handler struct", "Byte positions should extract correct code")

	// Verify functions have byte positions
	require.Len(t, result.Functions, 2)
	for _, fn := range result.Functions {
		assert.Greater(t, fn.StartPos, 0, "Function %s StartPos should be > 0", fn.Name)
		assert.Greater(t, fn.EndPos, fn.StartPos, "Function %s EndPos should be > StartPos", fn.Name)

		// Verify the captured positions actually point to the function
		funcCode := source[fn.StartPos:fn.EndPos]
		assert.Contains(t, funcCode, "func", "Function byte positions should extract correct code")
		assert.Contains(t, funcCode, fn.Name, "Function code should contain function name")
	}
}

// TestExtractor_BytePositions_EdgeCases tests edge cases for byte position capture
func TestExtractor_BytePositions_EdgeCases(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	tests := []struct {
		name   string
		source string
		check  func(*testing.T, *CodeStructure, string)
	}{
		{
			name: "nested function (closure)",
			source: `package test

func outer() {
	inner := func() {
		// closure
	}
	inner()
}
`,
			check: func(t *testing.T, result *CodeStructure, source string) {
				// Should extract outer function
				require.Len(t, result.Functions, 1)
				fn := result.Functions[0]
				assert.Equal(t, "outer", fn.Name)
				assert.Greater(t, fn.StartPos, 0)
				assert.Greater(t, fn.EndPos, fn.StartPos)

				// Verify positions capture full function including closure
				funcCode := source[fn.StartPos:fn.EndPos]
				assert.Contains(t, funcCode, "func outer")
				assert.Contains(t, funcCode, "inner := func()")
			},
		},
		{
			name: "type alias",
			source: `package test

type Status string
`,
			check: func(t *testing.T, result *CodeStructure, source string) {
				require.Len(t, result.Types, 1)
				typ := result.Types[0]
				assert.Equal(t, "Status", typ.Name)
				assert.Equal(t, "alias", typ.Kind)
				assert.Greater(t, typ.StartPos, 0)
				assert.Greater(t, typ.EndPos, typ.StartPos)

				typeCode := source[typ.StartPos:typ.EndPos]
				assert.Contains(t, typeCode, "type Status")
			},
		},
		{
			name: "embedded type",
			source: `package test

type Base struct {
	ID int
}

type Derived struct {
	Base
	Name string
}
`,
			check: func(t *testing.T, result *CodeStructure, source string) {
				require.Len(t, result.Types, 2)

				// Check Base
				var base, derived *Type
				for i := range result.Types {
					if result.Types[i].Name == "Base" {
						base = &result.Types[i]
					} else if result.Types[i].Name == "Derived" {
						derived = &result.Types[i]
					}
				}
				require.NotNil(t, base)
				require.NotNil(t, derived)

				assert.Greater(t, base.StartPos, 0)
				assert.Greater(t, base.EndPos, base.StartPos)
				baseCode := source[base.StartPos:base.EndPos]
				assert.Contains(t, baseCode, "type Base struct")

				assert.Greater(t, derived.StartPos, 0)
				assert.Greater(t, derived.EndPos, derived.StartPos)
				derivedCode := source[derived.StartPos:derived.EndPos]
				assert.Contains(t, derivedCode, "type Derived struct")
				assert.Contains(t, derivedCode, "Base") // Embedded field
			},
		},
		{
			name: "method on pointer receiver",
			source: `package test

type Server struct{}

func (s *Server) Start() error {
	return nil
}
`,
			check: func(t *testing.T, result *CodeStructure, source string) {
				require.Len(t, result.Functions, 1)
				method := result.Functions[0]
				assert.Equal(t, "Start", method.Name)
				assert.True(t, method.IsMethod)
				assert.Greater(t, method.StartPos, 0)
				assert.Greater(t, method.EndPos, method.StartPos)

				methodCode := source[method.StartPos:method.EndPos]
				assert.Contains(t, methodCode, "func (s *Server) Start()")
			},
		},
		{
			name: "multiline function declaration",
			source: `package test

func ComplexFunction(
	param1 string,
	param2 int,
	param3 bool,
) (
	result string,
	err error,
) {
	return "", nil
}
`,
			check: func(t *testing.T, result *CodeStructure, source string) {
				require.Len(t, result.Functions, 1)
				fn := result.Functions[0]
				assert.Equal(t, "ComplexFunction", fn.Name)
				assert.Greater(t, fn.StartPos, 0)
				assert.Greater(t, fn.EndPos, fn.StartPos)

				// Verify end position includes closing brace
				funcCode := source[fn.StartPos:fn.EndPos]
				assert.Contains(t, funcCode, "func ComplexFunction")
				assert.Contains(t, funcCode, "return \"\", nil")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			testFile := filepath.Join(tmpDir, tt.name+".go")
			err := os.WriteFile(testFile, []byte(tt.source), 0644)
			require.NoError(t, err)

			extractor := NewExtractor(tmpDir)
			result, err := extractor.ExtractCodeStructure(testFile)
			require.NoError(t, err)
			require.NotNil(t, result)

			if tt.check != nil {
				tt.check(t, result, tt.source)
			}
		})
	}
}

// TestExtractor_TypeUsageEdges tests that type usage edges are created correctly
// DEPRECATED: Tests old ExtractFile method. Will be removed in Phase 4.
func TestExtractor_TypeUsageEdges(t *testing.T) {
	t.Skip("Deprecated: old ExtractFile method - use TestExtractor_ExtractCodeStructure instead")
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

// TestExtractor_VerifyBytePositionsExtractCorrectCode verifies byte positions extract correct code
func TestExtractor_VerifyBytePositionsExtractCorrectCode(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	source := `package test

type Handler struct {
	name string
}

func (h *Handler) Process() {
	h.helper()
}

func (h *Handler) helper() {
}
`

	testFile := filepath.Join(tmpDir, "verify_positions.go")
	err := os.WriteFile(testFile, []byte(source), 0644)
	require.NoError(t, err)

	// Extract
	extractor := NewExtractor(tmpDir)
	result, err := extractor.ExtractCodeStructure(testFile)
	require.NoError(t, err)

	// Verify type positions extract correct code
	require.Len(t, result.Types, 1)
	typ := result.Types[0]
	require.Greater(t, typ.StartPos, 0)
	require.Greater(t, typ.EndPos, typ.StartPos)

	typeCode := source[typ.StartPos:typ.EndPos]
	t.Logf("Type code (%d-%d):\n%s", typ.StartPos, typ.EndPos, typeCode)
	assert.Contains(t, typeCode, "type Handler struct")
	assert.Contains(t, typeCode, "name string")
	assert.Equal(t, "Handler", typ.Name)

	// Verify function positions extract correct code
	require.Len(t, result.Functions, 2)
	for _, fn := range result.Functions {
		require.Greater(t, fn.StartPos, 0)
		require.Greater(t, fn.EndPos, fn.StartPos)

		fnCode := source[fn.StartPos:fn.EndPos]
		t.Logf("Function %s (%d-%d):\n%s", fn.Name, fn.StartPos, fn.EndPos, fnCode)
		assert.Contains(t, fnCode, "func")
		assert.Contains(t, fnCode, fn.Name)

		// Verify code matches expected patterns
		if fn.Name == "Process" {
			assert.Contains(t, fnCode, "h.helper()")
		} else if fn.Name == "helper" {
			assert.Contains(t, fnCode, "func (h *Handler) helper()")
		}
	}
}

// TestExtractor_BuiltinTypesSkipped verifies built-in types don't create edges
// DEPRECATED: Tests old ExtractFile method. Will be removed in Phase 4.
func TestExtractor_BuiltinTypesSkipped(t *testing.T) {
	t.Skip("Deprecated: old ExtractFile method - use TestExtractor_ExtractCodeStructure instead")
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
