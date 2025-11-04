package graph

import "time"

// NodeKind represents the type of a code entity.
type NodeKind string

const (
	NodeInterface NodeKind = "interface"
	NodeStruct    NodeKind = "struct"
	NodeFunction  NodeKind = "function"
	NodeMethod    NodeKind = "method"
	NodePackage   NodeKind = "package"
)

// Node represents a code entity with its source location.
type Node struct {
	ID              string            `json:"id"`                         // Fully qualified identifier (e.g., "embed.Provider", "localProvider.Embed")
	Kind            NodeKind          `json:"kind"`                       // Type of node
	File            string            `json:"file"`                       // Relative file path
	StartLine       int               `json:"start_line"`                 // Start line number (1-indexed)
	EndLine         int               `json:"end_line"`                   // End line number (1-indexed)
	Methods         []MethodSignature `json:"methods,omitempty"`          // For interfaces and structs
	EmbeddedTypes   []string          `json:"embedded_types,omitempty"`   // For embedded interfaces/structs
	ResolvedMethods []MethodSignature `json:"resolved_methods,omitempty"` // Flattened method set after resolving embeddings
}

// MethodSignature represents a method's signature.
type MethodSignature struct {
	Name       string      `json:"name"`       // Method name (e.g., "Embed", "Close")
	Parameters []Parameter `json:"parameters"` // Function parameters
	Returns    []Parameter `json:"returns"`    // Return values
}

// Parameter represents a function parameter or return value.
type Parameter struct {
	Name string  `json:"name,omitempty"` // Parameter name (optional for returns)
	Type TypeRef `json:"type"`           // Type information
}

// TypeRef represents type information extracted from AST.
type TypeRef struct {
	Name      string `json:"name"`                 // Type name (e.g., "Context", "error", "int")
	Package   string `json:"package,omitempty"`    // Package path (e.g., "context", empty for built-ins)
	IsPointer bool   `json:"is_pointer,omitempty"` // *Type
	IsSlice   bool   `json:"is_slice,omitempty"`   // []Type
	IsMap     bool   `json:"is_map,omitempty"`     // map[K]V
}

// EdgeType represents the type of relationship between nodes.
type EdgeType string

const (
	EdgeImplements EdgeType = "implements" // struct -> interface
	EdgeEmbeds     EdgeType = "embeds"     // struct -> struct/interface (embedding)
	EdgeCalls      EdgeType = "calls"      // function -> function
	EdgeImports    EdgeType = "imports"    // package -> package
	EdgeUsesType   EdgeType = "uses_type"  // function/struct -> type (parameters, returns, fields)
)

// Edge represents a relationship between two code entities.
type Edge struct {
	From     string    `json:"from"`     // Source node ID
	To       string    `json:"to"`       // Target node ID
	Type     EdgeType  `json:"type"`     // Relationship type
	Location *Location `json:"location"` // Where relationship occurs
}

// Location represents the source location of a relationship.
type Location struct {
	File   string `json:"file"`   // File where relationship originates
	Line   int    `json:"line"`   // Line number
	Column int    `json:"column"` // Column number (optional, 0 if unknown)
}

// GraphData represents the complete code graph structure stored in JSON.
type GraphData struct {
	Metadata GraphMetadata `json:"_metadata"`
	Nodes    []Node        `json:"nodes"`
	Edges    []Edge        `json:"edges"`
}

// GraphMetadata contains metadata about the graph.
type GraphMetadata struct {
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	NodeCount   int       `json:"node_count"`
	EdgeCount   int       `json:"edge_count"`
}

// FileGraphData represents the graph data extracted from a single file.
// Used during incremental updates to track which nodes/edges belong to which files.
type FileGraphData struct {
	FilePath string
	Nodes    []Node
	Edges    []Edge
}
