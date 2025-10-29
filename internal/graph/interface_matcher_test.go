package graph

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveEmbeddings_Simple(t *testing.T) {
	t.Parallel()

	// Create Reader interface with Close method
	reader := Node{
		ID:   "pkg.Reader",
		Kind: NodeInterface,
		Methods: []MethodSignature{
			{
				Name: "Read",
				Parameters: []Parameter{
					{Name: "p", Type: TypeRef{Name: "byte", IsSlice: true}},
				},
				Returns: []Parameter{
					{Type: TypeRef{Name: "int"}},
					{Type: TypeRef{Name: "error"}},
				},
			},
		},
	}

	// Create ReadCloser that embeds Reader
	readCloser := Node{
		ID:            "pkg.ReadCloser",
		Kind:          NodeInterface,
		EmbeddedTypes: []string{"pkg.Reader"},
		Methods: []MethodSignature{
			{
				Name:       "Close",
				Parameters: []Parameter{},
				Returns: []Parameter{
					{Type: TypeRef{Name: "error"}},
				},
			},
		},
	}

	nodes := []Node{reader, readCloser}
	matcher := NewInterfaceMatcher(nodes)
	matcher.ResolveEmbeddings()

	// ReadCloser should have both Close and Read
	resolved := matcher.nodes["pkg.ReadCloser"]
	require.NotNil(t, resolved)
	assert.Len(t, resolved.ResolvedMethods, 2)

	methodNames := make(map[string]bool)
	for _, m := range resolved.ResolvedMethods {
		methodNames[m.Name] = true
	}

	assert.True(t, methodNames["Read"])
	assert.True(t, methodNames["Close"])
}

func TestResolveEmbeddings_Transitive(t *testing.T) {
	t.Parallel()

	// A -> Read()
	reader := Node{
		ID:   "pkg.Reader",
		Kind: NodeInterface,
		Methods: []MethodSignature{
			{Name: "Read"},
		},
	}

	// B -> Close() + embeds A
	readCloser := Node{
		ID:            "pkg.ReadCloser",
		Kind:          NodeInterface,
		EmbeddedTypes: []string{"pkg.Reader"},
		Methods: []MethodSignature{
			{Name: "Close"},
		},
	}

	// C -> Seek() + embeds B (should get Read, Close, Seek)
	readSeekCloser := Node{
		ID:            "pkg.ReadSeekCloser",
		Kind:          NodeInterface,
		EmbeddedTypes: []string{"pkg.ReadCloser"},
		Methods: []MethodSignature{
			{Name: "Seek"},
		},
	}

	nodes := []Node{reader, readCloser, readSeekCloser}
	matcher := NewInterfaceMatcher(nodes)
	matcher.ResolveEmbeddings()

	// ReadSeekCloser should have all three methods
	resolved := matcher.nodes["pkg.ReadSeekCloser"]
	require.NotNil(t, resolved)
	assert.Len(t, resolved.ResolvedMethods, 3)

	methodNames := make(map[string]bool)
	for _, m := range resolved.ResolvedMethods {
		methodNames[m.Name] = true
	}

	assert.True(t, methodNames["Read"])
	assert.True(t, methodNames["Close"])
	assert.True(t, methodNames["Seek"])
}

func TestImplementsInterface_Exact(t *testing.T) {
	t.Parallel()

	// Interface with one method
	iface := Node{
		ID:   "pkg.Closer",
		Kind: NodeInterface,
		ResolvedMethods: []MethodSignature{
			{
				Name:       "Close",
				Parameters: []Parameter{},
				Returns: []Parameter{
					{Type: TypeRef{Name: "error"}},
				},
			},
		},
	}

	// Struct that implements it
	strct := Node{
		ID:   "pkg.MyCloser",
		Kind: NodeStruct,
		Methods: []MethodSignature{
			{
				Name:       "Close",
				Parameters: []Parameter{},
				Returns: []Parameter{
					{Type: TypeRef{Name: "error"}},
				},
			},
		},
	}

	matcher := NewInterfaceMatcher([]Node{iface, strct})
	assert.True(t, matcher.implementsInterface(&strct, &iface))
}

func TestImplementsInterface_MissingMethod(t *testing.T) {
	t.Parallel()

	// Interface with two methods
	iface := Node{
		ID:   "pkg.ReadCloser",
		Kind: NodeInterface,
		ResolvedMethods: []MethodSignature{
			{Name: "Read"},
			{Name: "Close"},
		},
	}

	// Struct that only has Read
	strct := Node{
		ID:   "pkg.MyReader",
		Kind: NodeStruct,
		Methods: []MethodSignature{
			{Name: "Read"},
		},
	}

	matcher := NewInterfaceMatcher([]Node{iface, strct})
	assert.False(t, matcher.implementsInterface(&strct, &iface))
}

func TestImplementsInterface_WrongSignature(t *testing.T) {
	t.Parallel()

	// Interface expecting ([]byte) (int, error)
	iface := Node{
		ID:   "pkg.Reader",
		Kind: NodeInterface,
		ResolvedMethods: []MethodSignature{
			{
				Name: "Read",
				Parameters: []Parameter{
					{Name: "p", Type: TypeRef{Name: "byte", IsSlice: true}},
				},
				Returns: []Parameter{
					{Type: TypeRef{Name: "int"}},
					{Type: TypeRef{Name: "error"}},
				},
			},
		},
	}

	// Struct with wrong signature: (string) error
	strct := Node{
		ID:   "pkg.BadReader",
		Kind: NodeStruct,
		Methods: []MethodSignature{
			{
				Name: "Read",
				Parameters: []Parameter{
					{Name: "s", Type: TypeRef{Name: "string"}},
				},
				Returns: []Parameter{
					{Type: TypeRef{Name: "error"}},
				},
			},
		},
	}

	matcher := NewInterfaceMatcher([]Node{iface, strct})
	assert.False(t, matcher.implementsInterface(&strct, &iface))
}

func TestImplementsInterface_EmptyInterface(t *testing.T) {
	t.Parallel()

	// Empty interface: interface{}
	iface := Node{
		ID:              "pkg.Any",
		Kind:            NodeInterface,
		ResolvedMethods: []MethodSignature{},
	}

	// Any struct implements empty interface
	strct := Node{
		ID:      "pkg.MyStruct",
		Kind:    NodeStruct,
		Methods: []MethodSignature{},
	}

	matcher := NewInterfaceMatcher([]Node{iface, strct})
	assert.True(t, matcher.implementsInterface(&strct, &iface))
}

func TestTypeRefsEqual_Pointer(t *testing.T) {
	t.Parallel()

	matcher := NewInterfaceMatcher([]Node{})

	a := TypeRef{Name: "Context", Package: "context", IsPointer: true}
	b := TypeRef{Name: "Context", Package: "context", IsPointer: true}
	c := TypeRef{Name: "Context", Package: "context", IsPointer: false}

	assert.True(t, matcher.typeRefsEqual(a, b))
	assert.False(t, matcher.typeRefsEqual(a, c))
}

func TestTypeRefsEqual_Slice(t *testing.T) {
	t.Parallel()

	matcher := NewInterfaceMatcher([]Node{})

	a := TypeRef{Name: "byte", IsSlice: true}
	b := TypeRef{Name: "byte", IsSlice: true}
	c := TypeRef{Name: "byte", IsSlice: false}

	assert.True(t, matcher.typeRefsEqual(a, b))
	assert.False(t, matcher.typeRefsEqual(a, c))
}

func TestTypeRefsEqual_Package(t *testing.T) {
	t.Parallel()

	matcher := NewInterfaceMatcher([]Node{})

	a := TypeRef{Name: "Context", Package: "context"}
	b := TypeRef{Name: "Context", Package: "context"}
	c := TypeRef{Name: "Context", Package: "other"}

	assert.True(t, matcher.typeRefsEqual(a, b))
	assert.False(t, matcher.typeRefsEqual(a, c))
}

func TestInferImplementations_Multiple(t *testing.T) {
	t.Parallel()

	// Interface
	iface := Node{
		ID:   "pkg.Printer",
		Kind: NodeInterface,
		Methods: []MethodSignature{
			{
				Name: "Print",
				Parameters: []Parameter{
					{Name: "s", Type: TypeRef{Name: "string"}},
				},
			},
		},
	}

	// Two structs that implement it
	strct1 := Node{
		ID:   "pkg.ConsolePrinter",
		Kind: NodeStruct,
		File: "console.go",
		Methods: []MethodSignature{
			{
				Name: "Print",
				Parameters: []Parameter{
					{Name: "s", Type: TypeRef{Name: "string"}},
				},
			},
		},
	}

	strct2 := Node{
		ID:   "pkg.FilePrinter",
		Kind: NodeStruct,
		File: "file.go",
		Methods: []MethodSignature{
			{
				Name: "Print",
				Parameters: []Parameter{
					{Name: "s", Type: TypeRef{Name: "string"}},
				},
			},
		},
	}

	// One struct that doesn't
	strct3 := Node{
		ID:      "pkg.Other",
		Kind:    NodeStruct,
		File:    "other.go",
		Methods: []MethodSignature{},
	}

	nodes := []Node{iface, strct1, strct2, strct3}
	matcher := NewInterfaceMatcher(nodes)
	matcher.ResolveEmbeddings()

	edges := matcher.InferImplementations()

	// Should have 2 implementation edges
	assert.Len(t, edges, 2)

	// Check edge details
	for _, edge := range edges {
		assert.Equal(t, EdgeImplements, edge.Type)
		assert.Equal(t, "pkg.Printer", edge.To)
		assert.Contains(t, []string{"pkg.ConsolePrinter", "pkg.FilePrinter"}, edge.From)
	}
}

func TestInferImplementations_WithEmbedding(t *testing.T) {
	t.Parallel()

	// Base interface
	reader := Node{
		ID:   "pkg.Reader",
		Kind: NodeInterface,
		Methods: []MethodSignature{
			{Name: "Read"},
		},
	}

	// Extended interface that embeds Reader
	readCloser := Node{
		ID:            "pkg.ReadCloser",
		Kind:          NodeInterface,
		EmbeddedTypes: []string{"pkg.Reader"},
		Methods: []MethodSignature{
			{Name: "Close"},
		},
	}

	// Struct that implements ReadCloser (has both Read and Close)
	myReadCloser := Node{
		ID:   "pkg.MyReadCloser",
		Kind: NodeStruct,
		File: "impl.go",
		Methods: []MethodSignature{
			{Name: "Read"},
			{Name: "Close"},
		},
	}

	nodes := []Node{reader, readCloser, myReadCloser}
	matcher := NewInterfaceMatcher(nodes)
	matcher.ResolveEmbeddings()

	edges := matcher.InferImplementations()

	// Should implement both Reader and ReadCloser
	assert.Len(t, edges, 2)

	edgeMap := make(map[string]bool)
	for _, edge := range edges {
		edgeMap[edge.To] = true
	}

	assert.True(t, edgeMap["pkg.Reader"])
	assert.True(t, edgeMap["pkg.ReadCloser"])
}
