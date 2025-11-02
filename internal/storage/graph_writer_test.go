package storage

// Test Plan for Graph Writer:
// - WriteGraphData converts graph.Node to SQL types (types and functions)
// - WriteGraphData converts graph.Edge to SQL types (relationships and calls)
// - WriteGraphData inserts types correctly (interfaces and structs)
// - WriteGraphData inserts functions correctly (standalone functions and methods)
// - WriteGraphData inserts type relationships correctly (implements and embeds)
// - WriteGraphData inserts call graph edges correctly (internal and external calls)
// - WriteTypes inserts type definitions with all fields
// - WriteTypes handles exported and unexported types
// - WriteTypes calculates field_count and method_count correctly
// - WriteFunctions inserts standalone functions
// - WriteFunctions inserts methods with receiver information
// - WriteFunctions handles nullable receiver fields for standalone functions
// - ClearExistingData removes old data before bulk write
// - WriteGraphData is idempotent (replaces existing data)
// - convertNodesToSQL extracts types from NodeInterface and NodeStruct
// - convertNodesToSQL extracts functions from NodeFunction and NodeMethod
// - convertNodesToSQL calculates method_count from MethodSignature array
// - convertNodesToSQL calculates param_count and return_count from signatures
// - convertNodesToSQL determines is_exported from identifier capitalization
// - convertEdgesToSQL creates relationships for EdgeImplements and EdgeEmbeds
// - convertEdgesToSQL creates calls for EdgeCalls
// - convertEdgesToSQL handles nullable callee_function_id for external calls
// - convertEdgesToSQL extracts call location (file, line, column)
// - Helper functions: extractTypeName, extractFunctionName, extractReceiverType
// - Helper functions: isExported, boolToInt

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvp-joe/project-cortex/internal/graph"
)

func TestGraphWriter_WriteGraphData(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	// Create test graph data
	graphData := &graph.GraphData{
		Nodes: []graph.Node{
			{
				ID:        "embed.Provider",
				Kind:      graph.NodeInterface,
				File:      "internal/embed/provider.go",
				StartLine: 10,
				EndLine:   15,
				Methods: []graph.MethodSignature{
					{
						Name: "Embed",
						Parameters: []graph.Parameter{
							{Name: "ctx", Type: graph.TypeRef{Name: "Context", Package: "context"}},
							{Name: "texts", Type: graph.TypeRef{Name: "string", IsSlice: true}},
						},
						Returns: []graph.Parameter{
							{Type: graph.TypeRef{Name: "float32", IsSlice: true}},
							{Type: graph.TypeRef{Name: "error"}},
						},
					},
				},
			},
			{
				ID:        "localProvider",
				Kind:      graph.NodeStruct,
				File:      "internal/embed/local.go",
				StartLine: 20,
				EndLine:   25,
			},
			{
				ID:        "localProvider.Embed",
				Kind:      graph.NodeMethod,
				File:      "internal/embed/local.go",
				StartLine: 30,
				EndLine:   50,
				Methods: []graph.MethodSignature{
					{
						Name: "Embed",
						Parameters: []graph.Parameter{
							{Name: "ctx", Type: graph.TypeRef{Name: "Context", Package: "context"}},
							{Name: "texts", Type: graph.TypeRef{Name: "string", IsSlice: true}},
						},
						Returns: []graph.Parameter{
							{Type: graph.TypeRef{Name: "float32", IsSlice: true}},
							{Type: graph.TypeRef{Name: "error"}},
						},
					},
				},
			},
		},
		Edges: []graph.Edge{
			{
				From: "localProvider",
				To:   "embed.Provider",
				Type: graph.EdgeImplements,
				Location: &graph.Location{
					File: "internal/embed/local.go",
					Line: 20,
				},
			},
			{
				From: "localProvider.Embed",
				To:   "http.Post",
				Type: graph.EdgeCalls,
				Location: &graph.Location{
					File: "internal/embed/local.go",
					Line: 35,
				},
			},
		},
	}

	// Write graph data
	err := writer.WriteGraphData(graphData)
	require.NoError(t, err)

	// Verify data was written
	types, err := reader.ReadTypes()
	require.NoError(t, err)
	assert.Len(t, types, 2) // Provider and localProvider

	functions, err := reader.ReadFunctions()
	require.NoError(t, err)
	assert.Len(t, functions, 1) // localProvider.Embed

	relationships, err := reader.ReadTypeRelationships()
	require.NoError(t, err)
	assert.Len(t, relationships, 1) // localProvider implements Provider

	calls, err := reader.ReadCallGraph()
	require.NoError(t, err)
	assert.Len(t, calls, 1) // localProvider.Embed calls http.Post
}

func TestGraphWriter_WriteTypes(t *testing.T) {
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
			EndLine:     20,
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
			EndLine:     15,
			IsExported:  true,
			MethodCount: 2,
		},
	}

	err := writer.WriteTypes(types)
	require.NoError(t, err)

	// Verify
	readTypes, err := reader.ReadTypes()
	require.NoError(t, err)
	assert.Len(t, readTypes, 2)
	assert.Equal(t, "Handler", readTypes[0].Name)
	assert.Equal(t, "Service", readTypes[1].Name)
}

func TestGraphWriter_WriteFunctions(t *testing.T) {
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
			StartLine:   30,
			EndLine:     40,
			IsExported:  true,
			IsMethod:    false,
			ParamCount:  1,
			ReturnCount: 1,
		},
		{
			ID:               "pkg.Handler.ServeHTTP",
			FilePath:         "pkg/handler.go",
			ModulePath:       "pkg",
			Name:             "ServeHTTP",
			StartLine:        50,
			EndLine:          100,
			IsExported:       true,
			IsMethod:         true,
			ReceiverTypeID:   &receiverType,
			ReceiverTypeName: &receiverName,
			ParamCount:       2,
			ReturnCount:      0,
		},
	}

	err := writer.WriteFunctions(functions)
	require.NoError(t, err)

	// Verify
	readFunctions, err := reader.ReadFunctions()
	require.NoError(t, err)
	assert.Len(t, readFunctions, 2)
	assert.Equal(t, "NewHandler", readFunctions[0].Name)
	assert.False(t, readFunctions[0].IsMethod)
	assert.Equal(t, "ServeHTTP", readFunctions[1].Name)
	assert.True(t, readFunctions[1].IsMethod)
}

func TestGraphWriter_ClearExistingData(t *testing.T) {
	t.Parallel()

	db, writer, reader := setupGraphTestDB(t)
	defer db.Close()

	// Write initial data
	graphData1 := &graph.GraphData{
		Nodes: []graph.Node{
			{
				ID:        "pkg.TypeA",
				Kind:      graph.NodeInterface,
				File:      "pkg/a.go",
				StartLine: 10,
				EndLine:   20,
			},
		},
		Edges: []graph.Edge{},
	}
	err := writer.WriteGraphData(graphData1)
	require.NoError(t, err)

	// Write new data (should clear old data)
	graphData2 := &graph.GraphData{
		Nodes: []graph.Node{
			{
				ID:        "pkg.TypeB",
				Kind:      graph.NodeStruct,
				File:      "pkg/b.go",
				StartLine: 5,
				EndLine:   15,
			},
		},
		Edges: []graph.Edge{},
	}
	err = writer.WriteGraphData(graphData2)
	require.NoError(t, err)

	// Verify only new data exists
	types, err := reader.ReadTypes()
	require.NoError(t, err)
	assert.Len(t, types, 1)
	assert.Equal(t, "TypeB", types[0].Name)
}

func TestConvertNodesToSQL(t *testing.T) {
	t.Parallel()

	nodes := []graph.Node{
		{
			ID:        "embed.Provider",
			Kind:      graph.NodeInterface,
			File:      "internal/embed/provider.go",
			StartLine: 10,
			EndLine:   20,
			Methods: []graph.MethodSignature{
				{Name: "Embed"},
				{Name: "Close"},
			},
		},
		{
			ID:        "localProvider",
			Kind:      graph.NodeStruct,
			File:      "internal/embed/local.go",
			StartLine: 30,
			EndLine:   40,
		},
		{
			ID:        "NewProvider",
			Kind:      graph.NodeFunction,
			File:      "internal/embed/factory.go",
			StartLine: 15,
			EndLine:   25,
			Methods: []graph.MethodSignature{
				{
					Parameters: []graph.Parameter{{Name: "config"}},
					Returns:    []graph.Parameter{{Type: graph.TypeRef{Name: "Provider"}}, {Type: graph.TypeRef{Name: "error"}}},
				},
			},
		},
		{
			ID:        "localProvider.Embed",
			Kind:      graph.NodeMethod,
			File:      "internal/embed/local.go",
			StartLine: 50,
			EndLine:   70,
		},
	}

	types, functions := convertNodesToSQL(nodes)

	// Verify types conversion
	assert.Len(t, types, 2) // Provider and localProvider
	assert.Equal(t, "Provider", types[0].Name)
	assert.Equal(t, "interface", types[0].Kind)
	assert.Equal(t, 2, types[0].MethodCount)
	assert.True(t, types[0].IsExported)

	assert.Equal(t, "localProvider", types[1].Name)
	assert.Equal(t, "struct", types[1].Kind)
	assert.False(t, types[1].IsExported)

	// Verify functions conversion
	assert.Len(t, functions, 2) // NewProvider and localProvider.Embed
	assert.Equal(t, "NewProvider", functions[0].Name)
	assert.False(t, functions[0].IsMethod)
	assert.Equal(t, 1, functions[0].ParamCount)
	assert.Equal(t, 2, functions[0].ReturnCount)

	assert.Equal(t, "Embed", functions[1].Name)
	assert.True(t, functions[1].IsMethod)
	assert.NotNil(t, functions[1].ReceiverTypeName)
	assert.Equal(t, "localProvider", *functions[1].ReceiverTypeName)
}

func TestConvertEdgesToSQL(t *testing.T) {
	t.Parallel()

	edges := []graph.Edge{
		{
			From: "localProvider",
			To:   "embed.Provider",
			Type: graph.EdgeImplements,
			Location: &graph.Location{
				File: "internal/embed/local.go",
				Line: 20,
			},
		},
		{
			From: "Config",
			To:   "BaseConfig",
			Type: graph.EdgeEmbeds,
			Location: &graph.Location{
				File: "internal/config/config.go",
				Line: 15,
			},
		},
		{
			From: "localProvider.Embed",
			To:   "http.Post",
			Type: graph.EdgeCalls,
			Location: &graph.Location{
				File:   "internal/embed/local.go",
				Line:   35,
				Column: 10,
			},
		},
		{
			From: "Handler.Process",
			To:   "pkg.DoWork",
			Type: graph.EdgeCalls,
			Location: &graph.Location{
				File: "internal/handler.go",
				Line: 50,
			},
		},
	}

	relationships, calls := convertEdgesToSQL(edges)

	// Verify relationships conversion
	assert.Len(t, relationships, 2) // implements and embeds
	assert.Equal(t, "implements", relationships[0].RelationshipType)
	assert.Equal(t, "localProvider", relationships[0].FromTypeID)
	assert.Equal(t, "embed.Provider", relationships[0].ToTypeID)

	assert.Equal(t, "embeds", relationships[1].RelationshipType)
	assert.Equal(t, "Config", relationships[1].FromTypeID)
	assert.Equal(t, "BaseConfig", relationships[1].ToTypeID)

	// Verify calls conversion
	assert.Len(t, calls, 2)
	assert.Equal(t, "localProvider.Embed", calls[0].CallerFunctionID)
	assert.Equal(t, "Post", calls[0].CalleeName)
	assert.NotNil(t, calls[0].CallColumn)
	assert.Equal(t, 10, *calls[0].CallColumn)

	assert.Equal(t, "Handler.Process", calls[1].CallerFunctionID)
	assert.Equal(t, "DoWork", calls[1].CalleeName)
	assert.Nil(t, calls[1].CallColumn)
}

func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("extractTypeName", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "Provider", extractTypeName("embed.Provider"))
		assert.Equal(t, "Handler", extractTypeName("pkg.Handler"))
		assert.Equal(t, "TypeName", extractTypeName("TypeName"))
	})

	t.Run("extractFunctionName", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "Embed", extractFunctionName("localProvider.Embed"))
		assert.Equal(t, "NewHandler", extractFunctionName("pkg.NewHandler"))
		assert.Equal(t, "DoWork", extractFunctionName("DoWork"))
	})

	t.Run("extractReceiverType", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, "localProvider", extractReceiverType("localProvider.Embed"))
		assert.Equal(t, "Handler", extractReceiverType("Handler.ServeHTTP"))
		assert.Equal(t, "", extractReceiverType("NewHandler"))
	})

	t.Run("isExported", func(t *testing.T) {
		t.Parallel()
		assert.True(t, isExported("Provider"))
		assert.True(t, isExported("Handler"))
		assert.False(t, isExported("localProvider"))
		assert.False(t, isExported("handler"))
	})

	t.Run("boolToInt", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 1, boolToInt(true))
		assert.Equal(t, 0, boolToInt(false))
	})
}
