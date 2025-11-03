package storage

// Test Plan for Graph Reader:
// - ReadGraphData converts SQL types back to graph.GraphData
// - ReadGraphData includes metadata (node_count, edge_count)
// - ReadGraphData reconstructs nodes from types and functions tables
// - ReadGraphData reconstructs edges from relationships and calls tables
// - ReadTypes reads all type definitions
// - ReadTypes preserves all type fields (ID, name, kind, exported, counts)
// - ReadTypesByFile filters types by file path
// - ReadFunctions reads all functions (standalone and methods)
// - ReadFunctions preserves nullable receiver fields
// - ReadFunctions distinguishes between is_method true/false
// - ReadFunctionsByFile filters functions by file path
// - ReadCallGraph reads all function calls
// - ReadCallGraph handles internal calls (with callee_function_id)
// - ReadCallGraph handles external calls (nullable callee_function_id)
// - ReadCallGraph preserves call location (line, column)
// - ReadTypeRelationships reads implements and embeds relationships
// - ReadTypeRelationships preserves relationship type, from/to IDs, location
// - BuildCallGraph creates in-memory call graph structure
// - BuildCallGraph populates Callees and Callers arrays bidirectionally
// - BuildCallGraph creates CallGraphNode with function metadata
// - FindAllCallers traverses graph transitively up to depth limit
// - FindAllCallers finds all transitive callers (direct and indirect)
// - FindAllCallees traverses graph transitively down to depth limit
// - FindAllCallees finds all transitive callees (direct and indirect)
// - Round-trip: WriteGraphData → ReadGraphData preserves all data
// - Round-trip: Node conversion (graph.Node → SQL → graph.Node)
// - Round-trip: Edge conversion (graph.Edge → SQL → graph.Edge)

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvp-joe/project-cortex/internal/graph"
)

func TestGraphReader_ReadGraphData(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	// Setup: Write test data
	graphData := &graph.GraphData{
		Nodes: []graph.Node{
			{
				ID:        "embed.Provider",
				Kind:      graph.NodeInterface,
				File:      "internal/embed/provider.go",
				StartLine: 10,
				EndLine:   20,
			},
			{
				ID:        "localProvider",
				Kind:      graph.NodeStruct,
				File:      "internal/embed/local.go",
				StartLine: 25,
				EndLine:   35,
			},
			{
				ID:        "NewProvider",
				Kind:      graph.NodeFunction,
				File:      "internal/embed/factory.go",
				StartLine: 15,
				EndLine:   25,
			},
		},
		Edges: []graph.Edge{
			{
				From: "localProvider",
				To:   "embed.Provider",
				Type: graph.EdgeImplements,
				Location: &graph.Location{
					File: "internal/embed/local.go",
					Line: 25,
				},
			},
		},
	}

	err := writer.WriteGraphData(graphData)
	require.NoError(t, err)

	// Test: Read graph data
	readData, err := reader.ReadGraphData()
	require.NoError(t, err)
	assert.NotNil(t, readData)
	assert.Equal(t, 3, readData.Metadata.NodeCount)
	assert.Equal(t, 1, readData.Metadata.EdgeCount)
	assert.Len(t, readData.Nodes, 3)
	assert.Len(t, readData.Edges, 1)

	// Verify nodes (order-independent check)
	nodesByID := make(map[string]graph.Node)
	for _, node := range readData.Nodes {
		nodesByID[node.ID] = node
	}

	providerNode, exists := nodesByID["embed.Provider"]
	assert.True(t, exists)
	assert.Equal(t, graph.NodeInterface, providerNode.Kind)

	localProviderNode, exists := nodesByID["localProvider"]
	assert.True(t, exists)
	assert.Equal(t, graph.NodeStruct, localProviderNode.Kind)

	// Verify edges
	assert.Equal(t, "localProvider", readData.Edges[0].From)
	assert.Equal(t, "embed.Provider", readData.Edges[0].To)
	assert.Equal(t, graph.EdgeImplements, readData.Edges[0].Type)
}

func TestGraphReader_ReadTypes(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	types := []*Type{
		{
			ID:          "pkg.Handler",
			FilePath:    "pkg/handler.go",
			ModulePath:  "pkg",
			Name:        "Handler",
			Kind:        "struct",
			StartLine:   10,
			EndLine:     50,
			IsExported:  true,
			FieldCount:  3,
			MethodCount: 5,
		},
		{
			ID:          "pkg.Service",
			FilePath:    "pkg/service.go",
			ModulePath:  "pkg",
			Name:        "Service",
			Kind:        "interface",
			StartLine:   5,
			EndLine:     20,
			IsExported:  true,
			MethodCount: 2,
		},
	}

	err := writer.WriteTypes(types)
	require.NoError(t, err)

	// Test
	readTypes, err := reader.ReadTypes()
	require.NoError(t, err)
	assert.Len(t, readTypes, 2)

	// Verify first type
	assert.Equal(t, "pkg.Handler", readTypes[0].ID)
	assert.Equal(t, "Handler", readTypes[0].Name)
	assert.Equal(t, "struct", readTypes[0].Kind)
	assert.True(t, readTypes[0].IsExported)
	assert.Equal(t, 3, readTypes[0].FieldCount)
	assert.Equal(t, 5, readTypes[0].MethodCount)

	// Verify second type
	assert.Equal(t, "pkg.Service", readTypes[1].ID)
	assert.Equal(t, "Service", readTypes[1].Name)
	assert.Equal(t, "interface", readTypes[1].Kind)
}

func TestGraphReader_ReadTypesByFile(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	types := []*Type{
		{
			ID:         "pkg.Handler",
			FilePath:   "pkg/handler.go",
			ModulePath: "pkg",
			Name:       "Handler",
			Kind:       "struct",
			StartLine:  10,
			EndLine:    50,
			IsExported: true,
		},
		{
			ID:         "pkg.Request",
			FilePath:   "pkg/handler.go",
			ModulePath: "pkg",
			Name:       "Request",
			Kind:       "struct",
			StartLine:  60,
			EndLine:    80,
			IsExported: true,
		},
		{
			ID:         "pkg.Service",
			FilePath:   "pkg/service.go",
			ModulePath: "pkg",
			Name:       "Service",
			Kind:       "interface",
			StartLine:  5,
			EndLine:    20,
			IsExported: true,
		},
	}

	err := writer.WriteTypes(types)
	require.NoError(t, err)

	// Test
	readTypes, err := reader.ReadTypesByFile("pkg/handler.go")
	require.NoError(t, err)
	assert.Len(t, readTypes, 2) // Only Handler and Request
	assert.Equal(t, "Handler", readTypes[0].Name)
	assert.Equal(t, "Request", readTypes[1].Name)
}

func TestGraphReader_ReadFunctions(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	receiverType := "pkg.Handler"
	receiverName := "Handler"
	functions := []*Function{
		{
			ID:          "pkg.NewHandler",
			FilePath:    "pkg/handler.go",
			ModulePath:  "pkg",
			Name:        "NewHandler",
			StartLine:   10,
			EndLine:     30,
			IsExported:  true,
			IsMethod:    false,
			ParamCount:  1,
			ReturnCount: 1,
		},
		{
			ID:               "pkg.Handler.Process",
			FilePath:         "pkg/handler.go",
			ModulePath:       "pkg",
			Name:             "Process",
			StartLine:        40,
			EndLine:          100,
			IsExported:       true,
			IsMethod:         true,
			ReceiverTypeID:   &receiverType,
			ReceiverTypeName: &receiverName,
			ParamCount:       2,
			ReturnCount:      1,
		},
	}

	err := writer.WriteFunctions(functions)
	require.NoError(t, err)

	// Test
	readFunctions, err := reader.ReadFunctions()
	require.NoError(t, err)
	assert.Len(t, readFunctions, 2)

	// Verify standalone function
	assert.Equal(t, "pkg.NewHandler", readFunctions[0].ID)
	assert.Equal(t, "NewHandler", readFunctions[0].Name)
	assert.False(t, readFunctions[0].IsMethod)
	assert.Nil(t, readFunctions[0].ReceiverTypeID)

	// Verify method
	assert.Equal(t, "pkg.Handler.Process", readFunctions[1].ID)
	assert.Equal(t, "Process", readFunctions[1].Name)
	assert.True(t, readFunctions[1].IsMethod)
	assert.NotNil(t, readFunctions[1].ReceiverTypeID)
	assert.Equal(t, "pkg.Handler", *readFunctions[1].ReceiverTypeID)
}

func TestGraphReader_ReadFunctionsByFile(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	functions := []*Function{
		{
			ID:         "pkg.FuncA",
			FilePath:   "pkg/a.go",
			ModulePath: "pkg",
			Name:       "FuncA",
			StartLine:  10,
			EndLine:    20,
			IsExported: true,
		},
		{
			ID:         "pkg.FuncB",
			FilePath:   "pkg/b.go",
			ModulePath: "pkg",
			Name:       "FuncB",
			StartLine:  15,
			EndLine:    25,
			IsExported: true,
		},
	}

	err := writer.WriteFunctions(functions)
	require.NoError(t, err)

	// Test
	readFunctions, err := reader.ReadFunctionsByFile("pkg/a.go")
	require.NoError(t, err)
	assert.Len(t, readFunctions, 1)
	assert.Equal(t, "FuncA", readFunctions[0].Name)
}

func TestGraphReader_ReadCallGraph(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	callee1 := "pkg.FuncB"
	col1 := 15
	calls := []*FunctionCall{
		{
			ID:               "call-1",
			CallerFunctionID: "pkg.FuncA",
			CalleeFunctionID: &callee1,
			CalleeName:       "FuncB",
			SourceFilePath:   "pkg/a.go",
			CallLine:         25,
			CallColumn:       &col1,
		},
		{
			ID:               "call-2",
			CallerFunctionID: "pkg.FuncB",
			CalleeFunctionID: nil, // External call
			CalleeName:       "http.Get",
			SourceFilePath:   "pkg/b.go",
			CallLine:         40,
		},
	}

	err := writer.WriteCalls(calls)
	require.NoError(t, err)

	// Test
	readCalls, err := reader.ReadCallGraph()
	require.NoError(t, err)
	assert.Len(t, readCalls, 2)

	// Verify internal call
	assert.Equal(t, "pkg.FuncA", readCalls[0].CallerFunctionID)
	assert.NotNil(t, readCalls[0].CalleeFunctionID)
	assert.Equal(t, "pkg.FuncB", *readCalls[0].CalleeFunctionID)
	assert.Equal(t, "FuncB", readCalls[0].CalleeName)
	assert.NotNil(t, readCalls[0].CallColumn)
	assert.Equal(t, 15, *readCalls[0].CallColumn)

	// Verify external call
	assert.Equal(t, "pkg.FuncB", readCalls[1].CallerFunctionID)
	assert.Nil(t, readCalls[1].CalleeFunctionID)
	assert.Equal(t, "http.Get", readCalls[1].CalleeName)
}

func TestGraphReader_ReadTypeRelationships(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	relationships := []*TypeRelationship{
		{
			ID:               "rel-1",
			FromTypeID:       "pkg.Handler",
			ToTypeID:         "http.Handler",
			RelationshipType: "implements",
			SourceFilePath:   "pkg/handler.go",
			SourceLine:       15,
		},
		{
			ID:               "rel-2",
			FromTypeID:       "pkg.Config",
			ToTypeID:         "pkg.BaseConfig",
			RelationshipType: "embeds",
			SourceFilePath:   "pkg/config.go",
			SourceLine:       20,
		},
	}

	err := writer.WriteRelationships(relationships)
	require.NoError(t, err)

	// Test
	readRelationships, err := reader.ReadTypeRelationships()
	require.NoError(t, err)
	assert.Len(t, readRelationships, 2)

	// Verify relationships (order-independent check)
	relsByType := make(map[string]*TypeRelationship)
	for _, rel := range readRelationships {
		relsByType[rel.RelationshipType] = rel
	}

	// Verify implements relationship
	implRel, exists := relsByType["implements"]
	assert.True(t, exists)
	assert.Equal(t, "pkg.Handler", implRel.FromTypeID)
	assert.Equal(t, "http.Handler", implRel.ToTypeID)

	// Verify embeds relationship
	embedRel, exists := relsByType["embeds"]
	assert.True(t, exists)
	assert.Equal(t, "pkg.Config", embedRel.FromTypeID)
	assert.Equal(t, "pkg.BaseConfig", embedRel.ToTypeID)
}

func TestGraphReader_BuildCallGraph(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	// Setup: Create a simple call graph
	//   FuncA -> FuncB -> FuncC
	//         \-> FuncD
	functions := []*Function{
		{ID: "pkg.FuncA", FilePath: "pkg/a.go", ModulePath: "pkg", Name: "FuncA", StartLine: 10, EndLine: 20, IsExported: true},
		{ID: "pkg.FuncB", FilePath: "pkg/b.go", ModulePath: "pkg", Name: "FuncB", StartLine: 10, EndLine: 20, IsExported: true},
		{ID: "pkg.FuncC", FilePath: "pkg/c.go", ModulePath: "pkg", Name: "FuncC", StartLine: 10, EndLine: 20, IsExported: true},
		{ID: "pkg.FuncD", FilePath: "pkg/d.go", ModulePath: "pkg", Name: "FuncD", StartLine: 10, EndLine: 20, IsExported: true},
	}
	err := writer.WriteFunctions(functions)
	require.NoError(t, err)

	funcB := "pkg.FuncB"
	funcC := "pkg.FuncC"
	funcD := "pkg.FuncD"
	calls := []*FunctionCall{
		{ID: "call-1", CallerFunctionID: "pkg.FuncA", CalleeFunctionID: &funcB, CalleeName: "FuncB", SourceFilePath: "pkg/a.go", CallLine: 15},
		{ID: "call-2", CallerFunctionID: "pkg.FuncA", CalleeFunctionID: &funcD, CalleeName: "FuncD", SourceFilePath: "pkg/a.go", CallLine: 18},
		{ID: "call-3", CallerFunctionID: "pkg.FuncB", CalleeFunctionID: &funcC, CalleeName: "FuncC", SourceFilePath: "pkg/b.go", CallLine: 15},
	}
	err = writer.WriteCalls(calls)
	require.NoError(t, err)

	// Test: Build in-memory call graph
	callGraph, err := reader.BuildCallGraph()
	require.NoError(t, err)
	assert.Len(t, callGraph, 4)

	// Verify FuncA has 2 callees, 0 callers
	funcANode := callGraph["pkg.FuncA"]
	require.NotNil(t, funcANode)
	assert.Equal(t, "FuncA", funcANode.Name)
	assert.Len(t, funcANode.Callees, 2)
	assert.Len(t, funcANode.Callers, 0)

	// Verify FuncB has 1 callee (FuncC), 1 caller (FuncA)
	funcBNode := callGraph["pkg.FuncB"]
	require.NotNil(t, funcBNode)
	assert.Len(t, funcBNode.Callees, 1)
	assert.Len(t, funcBNode.Callers, 1)
	assert.Equal(t, "pkg.FuncC", funcBNode.Callees[0].To)
	assert.Equal(t, "pkg.FuncA", funcBNode.Callers[0].From)

	// Verify FuncC has 0 callees, 1 caller (FuncB)
	funcCNode := callGraph["pkg.FuncC"]
	require.NotNil(t, funcCNode)
	assert.Len(t, funcCNode.Callees, 0)
	assert.Len(t, funcCNode.Callers, 1)
}

func TestFindAllCallers(t *testing.T) {
	t.Parallel()

	// Build test call graph in memory
	//   FuncA -> FuncB -> FuncC
	callGraph := map[string]*CallGraphNode{
		"pkg.FuncA": {
			ID:       "pkg.FuncA",
			Name:     "FuncA",
			FilePath: "pkg/a.go",
			Callers:  []*CallGraphEdge{},
			Callees: []*CallGraphEdge{
				{From: "pkg.FuncA", To: "pkg.FuncB", Line: 15},
			},
		},
		"pkg.FuncB": {
			ID:       "pkg.FuncB",
			Name:     "FuncB",
			FilePath: "pkg/b.go",
			Callers: []*CallGraphEdge{
				{From: "pkg.FuncA", To: "pkg.FuncB", Line: 15},
			},
			Callees: []*CallGraphEdge{
				{From: "pkg.FuncB", To: "pkg.FuncC", Line: 20},
			},
		},
		"pkg.FuncC": {
			ID:       "pkg.FuncC",
			Name:     "FuncC",
			FilePath: "pkg/c.go",
			Callers: []*CallGraphEdge{
				{From: "pkg.FuncB", To: "pkg.FuncC", Line: 20},
			},
			Callees: []*CallGraphEdge{},
		},
	}

	// Test: Find all callers of FuncC
	callers := FindAllCallers(callGraph, "pkg.FuncC", 10)
	assert.Len(t, callers, 2) // FuncB and FuncA

	// Verify we found both transitive callers
	callerNames := make(map[string]bool)
	for _, caller := range callers {
		callerNames[caller.Name] = true
	}
	assert.True(t, callerNames["FuncA"])
	assert.True(t, callerNames["FuncB"])
}

func TestFindAllCallees(t *testing.T) {
	t.Parallel()

	// Build test call graph in memory
	//   FuncA -> FuncB -> FuncC
	callGraph := map[string]*CallGraphNode{
		"pkg.FuncA": {
			ID:       "pkg.FuncA",
			Name:     "FuncA",
			FilePath: "pkg/a.go",
			Callers:  []*CallGraphEdge{},
			Callees: []*CallGraphEdge{
				{From: "pkg.FuncA", To: "pkg.FuncB", Line: 15},
			},
		},
		"pkg.FuncB": {
			ID:       "pkg.FuncB",
			Name:     "FuncB",
			FilePath: "pkg/b.go",
			Callers: []*CallGraphEdge{
				{From: "pkg.FuncA", To: "pkg.FuncB", Line: 15},
			},
			Callees: []*CallGraphEdge{
				{From: "pkg.FuncB", To: "pkg.FuncC", Line: 20},
			},
		},
		"pkg.FuncC": {
			ID:       "pkg.FuncC",
			Name:     "FuncC",
			FilePath: "pkg/c.go",
			Callers: []*CallGraphEdge{
				{From: "pkg.FuncB", To: "pkg.FuncC", Line: 20},
			},
			Callees: []*CallGraphEdge{},
		},
	}

	// Test: Find all callees of FuncA
	callees := FindAllCallees(callGraph, "pkg.FuncA", 10)
	assert.Len(t, callees, 2) // FuncB and FuncC

	// Verify we found both transitive callees
	calleeNames := make(map[string]bool)
	for _, callee := range callees {
		calleeNames[callee.Name] = true
	}
	assert.True(t, calleeNames["FuncB"])
	assert.True(t, calleeNames["FuncC"])
}

func TestRoundTrip_GraphDataConversion(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	// Write original graph data
	originalData := &graph.GraphData{
		Metadata: graph.GraphMetadata{
			NodeCount: 2,
			EdgeCount: 1,
		},
		Nodes: []graph.Node{
			{
				ID:        "embed.Provider",
				Kind:      graph.NodeInterface,
				File:      "internal/embed/provider.go",
				StartLine: 10,
				EndLine:   20,
			},
			{
				ID:        "localProvider",
				Kind:      graph.NodeStruct,
				File:      "internal/embed/local.go",
				StartLine: 30,
				EndLine:   40,
			},
		},
		Edges: []graph.Edge{
			{
				From: "localProvider",
				To:   "embed.Provider",
				Type: graph.EdgeImplements,
				Location: &graph.Location{
					File: "internal/embed/local.go",
					Line: 30,
				},
			},
		},
	}

	err := writer.WriteGraphData(originalData)
	require.NoError(t, err)

	// Read back
	readData, err := reader.ReadGraphData()
	require.NoError(t, err)

	// Verify round-trip preserved data
	assert.Equal(t, originalData.Metadata.NodeCount, readData.Metadata.NodeCount)
	assert.Equal(t, originalData.Metadata.EdgeCount, readData.Metadata.EdgeCount)

	// Verify nodes preserved (order may differ, compare by ID)
	nodesByID := make(map[string]graph.Node)
	for _, node := range readData.Nodes {
		nodesByID[node.ID] = node
	}

	for _, originalNode := range originalData.Nodes {
		readNode, exists := nodesByID[originalNode.ID]
		assert.True(t, exists, "Node %s should exist", originalNode.ID)
		assert.Equal(t, originalNode.Kind, readNode.Kind)
		assert.Equal(t, originalNode.File, readNode.File)
		assert.Equal(t, originalNode.StartLine, readNode.StartLine)
		assert.Equal(t, originalNode.EndLine, readNode.EndLine)
	}
}
