package graph

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPhase2_InterfaceExtraction verifies that interfaces and structs are extracted correctly.
func TestPhase2_InterfaceExtraction(t *testing.T) {
	t.Parallel()

	// Create temporary test files
	tmpDir := t.TempDir()

	// Write interface file
	interfaceCode := `package testpkg

import "context"

type Processor interface {
	Process(ctx context.Context, data string) error
	Close() error
}
`
	interfaceFile := filepath.Join(tmpDir, "interface.go")
	require.NoError(t, os.WriteFile(interfaceFile, []byte(interfaceCode), 0644))

	// Write struct file
	structCode := `package testpkg

import "context"

type MyProcessor struct {
	name string
}

func (p *MyProcessor) Process(ctx context.Context, data string) error {
	return nil
}

func (p *MyProcessor) Close() error {
	return nil
}
`
	structFile := filepath.Join(tmpDir, "struct.go")
	require.NoError(t, os.WriteFile(structFile, []byte(structCode), 0644))

	// Build graph
	builder := NewBuilder(tmpDir)
	graph, err := builder.BuildFull(context.Background(), []string{interfaceFile, structFile})
	require.NoError(t, err)

	// Verify interface node extracted
	var ifaceNode *Node
	for i := range graph.Nodes {
		if graph.Nodes[i].Kind == NodeInterface && graph.Nodes[i].ID == "testpkg.Processor" {
			ifaceNode = &graph.Nodes[i]
			break
		}
	}
	require.NotNil(t, ifaceNode, "Interface node should be extracted")
	assert.Len(t, ifaceNode.Methods, 2, "Interface should have 2 methods")

	// Verify struct node extracted
	var structNode *Node
	for i := range graph.Nodes {
		if graph.Nodes[i].Kind == NodeStruct && graph.Nodes[i].ID == "testpkg.MyProcessor" {
			structNode = &graph.Nodes[i]
			break
		}
	}
	require.NotNil(t, structNode, "Struct node should be extracted")
	assert.Len(t, structNode.Methods, 2, "Struct should have 2 methods")

	// Verify implementation edge created
	var implEdge *Edge
	for i := range graph.Edges {
		if graph.Edges[i].Type == EdgeImplements &&
			graph.Edges[i].From == "testpkg.MyProcessor" &&
			graph.Edges[i].To == "testpkg.Processor" {
			implEdge = &graph.Edges[i]
			break
		}
	}
	require.NotNil(t, implEdge, "Implementation edge should be created")
}

// TestPhase2_EmbeddedInterfaces verifies that embedded interfaces are resolved correctly.
func TestPhase2_EmbeddedInterfaces(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	code := `package testpkg

type Reader interface {
	Read() error
}

type Writer interface {
	Write() error
}

type ReadWriter interface {
	Reader
	Writer
}

type MyReadWriter struct{}

func (rw *MyReadWriter) Read() error {
	return nil
}

func (rw *MyReadWriter) Write() error {
	return nil
}
`
	testFile := filepath.Join(tmpDir, "embedded.go")
	require.NoError(t, os.WriteFile(testFile, []byte(code), 0644))

	// Build graph
	builder := NewBuilder(tmpDir)
	graph, err := builder.BuildFull(context.Background(), []string{testFile})
	require.NoError(t, err)

	// Verify ReadWriter has resolved methods
	var readWriterNode *Node
	for i := range graph.Nodes {
		if graph.Nodes[i].ID == "testpkg.ReadWriter" {
			readWriterNode = &graph.Nodes[i]
			break
		}
	}
	require.NotNil(t, readWriterNode)
	assert.Len(t, readWriterNode.EmbeddedTypes, 2, "ReadWriter should embed 2 interfaces")
	assert.Len(t, readWriterNode.ResolvedMethods, 2, "ReadWriter should have 2 resolved methods")

	// Verify implementation edge created
	var implEdge *Edge
	for i := range graph.Edges {
		if graph.Edges[i].Type == EdgeImplements &&
			graph.Edges[i].From == "testpkg.MyReadWriter" &&
			graph.Edges[i].To == "testpkg.ReadWriter" {
			implEdge = &graph.Edges[i]
			break
		}
	}
	require.NotNil(t, implEdge, "MyReadWriter should implement ReadWriter")
}

// TestPhase2_QueryImplementations verifies the implementations query operation.
func TestPhase2_QueryImplementations(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	code := `package testpkg

type Logger interface {
	Log(msg string)
}

type ConsoleLogger struct{}

func (c *ConsoleLogger) Log(msg string) {}

type FileLogger struct{}

func (f *FileLogger) Log(msg string) {}

type Other struct{}
`
	testFile := filepath.Join(tmpDir, "logger.go")
	require.NoError(t, os.WriteFile(testFile, []byte(code), 0644))

	// Build graph
	builder := NewBuilder(tmpDir)
	graphData, err := builder.BuildFull(context.Background(), []string{testFile})
	require.NoError(t, err)

	// Save and load with searcher
	storage, err := NewStorage(tmpDir)
	require.NoError(t, err)
	require.NoError(t, storage.Save(graphData))

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	// Query implementations
	resp, err := searcher.Query(context.Background(), &QueryRequest{
		Operation:      OperationImplementations,
		Target:         "testpkg.Logger",
		IncludeContext: false,
	})
	require.NoError(t, err)

	// Should find 2 implementations
	assert.Equal(t, 2, resp.TotalFound)
	assert.Equal(t, 2, resp.TotalReturned)

	// Verify implementor IDs
	implementors := make(map[string]bool)
	for _, result := range resp.Results {
		implementors[result.Node.ID] = true
	}

	assert.True(t, implementors["testpkg.ConsoleLogger"])
	assert.True(t, implementors["testpkg.FileLogger"])
	assert.False(t, implementors["testpkg.Other"])
}

// TestPhase2_QueryPath verifies the path query operation.
func TestPhase2_QueryPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	code := `package testpkg

func A() {
	B()
}

func B() {
	C()
}

func C() {
	D()
}

func D() {}
`
	testFile := filepath.Join(tmpDir, "chain.go")
	require.NoError(t, os.WriteFile(testFile, []byte(code), 0644))

	// Build graph
	builder := NewBuilder(tmpDir)
	graphData, err := builder.BuildFull(context.Background(), []string{testFile})
	require.NoError(t, err)

	// Save and load with searcher
	storage, err := NewStorage(tmpDir)
	require.NoError(t, err)
	require.NoError(t, storage.Save(graphData))

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	// Query path from A to D
	resp, err := searcher.Query(context.Background(), &QueryRequest{
		Operation:      OperationPath,
		Target:         "testpkg.A",
		To:             "testpkg.D",
		IncludeContext: false,
	})
	require.NoError(t, err)

	// Should find path: A -> B -> C -> D
	assert.Equal(t, 4, resp.TotalFound)
	assert.Equal(t, 4, resp.TotalReturned)

	// Verify path order
	expectedPath := []string{"testpkg.A", "testpkg.B", "testpkg.C", "testpkg.D"}
	for i, result := range resp.Results {
		assert.Equal(t, expectedPath[i], result.Node.ID)
		assert.Equal(t, i, result.Depth)
	}
}

// TestPhase2_QueryImpact verifies the impact query operation.
func TestPhase2_QueryImpact(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	code := `package testpkg

type Service interface {
	DoWork()
}

type ServiceImpl struct{}

func (s *ServiceImpl) DoWork() {}

func Caller1() {
	var s Service
	s.DoWork()
}

func Caller2() {
	var s Service
	s.DoWork()
}

func TransitiveCaller() {
	Caller1()
}
`
	testFile := filepath.Join(tmpDir, "impact.go")
	require.NoError(t, os.WriteFile(testFile, []byte(code), 0644))

	// Build graph
	builder := NewBuilder(tmpDir)
	graphData, err := builder.BuildFull(context.Background(), []string{testFile})
	require.NoError(t, err)

	// Save and load with searcher
	storage, err := NewStorage(tmpDir)
	require.NoError(t, err)
	require.NoError(t, storage.Save(graphData))

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	// Query impact of changing Service interface
	resp, err := searcher.Query(context.Background(), &QueryRequest{
		Operation:      OperationImpact,
		Target:         "testpkg.Service",
		IncludeContext: false,
		MaxResults:     100,
	})
	require.NoError(t, err)

	// Should find implementations
	assert.NotNil(t, resp.Summary)
	assert.Equal(t, 1, resp.Summary.Implementations)

	// Verify impact types in results
	impactTypes := make(map[string]int)
	for _, result := range resp.Results {
		impactTypes[result.ImpactType]++
	}

	assert.Greater(t, impactTypes["implementation"], 0, "Should have implementation impacts")
}

// TestPhase2_Filtering verifies scope and exclude_patterns filtering.
func TestPhase2_Filtering(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create subdirectories
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "internal"), 0755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, "external"), 0755))

	// Internal file
	internalCode := `package internal

import "shared"

func InternalFunc() {
	shared.SharedFunc()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "internal", "internal.go"), []byte(internalCode), 0644))

	// External file
	externalCode := `package external

import "shared"

func ExternalFunc() {
	shared.SharedFunc()
}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "external", "external.go"), []byte(externalCode), 0644))

	// Shared file
	sharedCode := `package shared

func SharedFunc() {}
`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "shared.go"), []byte(sharedCode), 0644))

	// Build graph
	builder := NewBuilder(tmpDir)
	files := []string{
		filepath.Join(tmpDir, "internal", "internal.go"),
		filepath.Join(tmpDir, "external", "external.go"),
		filepath.Join(tmpDir, "shared.go"),
	}
	graphData, err := builder.BuildFull(context.Background(), files)
	require.NoError(t, err)

	// Save and load with searcher
	storage, err := NewStorage(tmpDir)
	require.NoError(t, err)
	require.NoError(t, storage.Save(graphData))

	searcher, err := NewSearcher(storage, tmpDir)
	require.NoError(t, err)
	defer searcher.Close()

	// Query callers with scope filter
	resp, err := searcher.Query(context.Background(), &QueryRequest{
		Operation:      OperationCallers,
		Target:         "shared.SharedFunc",
		Scope:          "internal/*",
		IncludeContext: false,
	})
	require.NoError(t, err)

	// Should only include internal caller
	assert.Equal(t, 1, resp.TotalReturned)
	if len(resp.Results) > 0 {
		assert.Equal(t, "internal.InternalFunc", resp.Results[0].Node.ID)
	}

	// Query with exclude pattern
	resp2, err := searcher.Query(context.Background(), &QueryRequest{
		Operation:       OperationCallers,
		Target:          "shared.SharedFunc",
		ExcludePatterns: []string{"external/*"},
		IncludeContext:  false,
	})
	require.NoError(t, err)

	// Should exclude external caller
	assert.Equal(t, 1, resp2.TotalReturned)
	if len(resp2.Results) > 0 {
		assert.Equal(t, "internal.InternalFunc", resp2.Results[0].Node.ID)
	}
}
