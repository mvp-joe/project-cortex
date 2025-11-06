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
// DEPRECATED: Use CodeStructure instead (Phase 2 of graph refactor)
type FileGraphData struct {
	FilePath string
	Nodes    []Node
	Edges    []Edge
}

// CodeStructure represents schema-aligned code structure extracted from a file.
// Replaces FileGraphData with domain structs that map directly to SQL tables.
type CodeStructure struct {
	Functions      []Function           // Maps to functions table
	Types          []Type               // Maps to types table
	TypeFields     []TypeField          // Maps to type_fields table
	FunctionParams []FunctionParameter  // Maps to function_parameters table
	FunctionCalls  []FunctionCall       // Maps to function_calls table
	Imports        []Import             // Maps to imports table
}

// Domain model structs (schema-aligned)
// These mirror storage.* structs but are defined here to avoid import cycles.

// Type represents a type definition (struct, interface, class, enum).
type Type struct {
	ID          string      // type_id: {file_path}::{name} or UUID
	FilePath    string      // file_path: relative path from repo root
	ModulePath  string      // module_path: package/module name
	Name        string      // name: type name
	Kind        string      // kind: interface, struct, class, enum
	StartLine   int         // start_line: start line number
	EndLine     int         // end_line: end line number
	IsExported  bool        // is_exported: uppercase first letter (Go)
	FieldCount  int         // field_count: denormalized count
	MethodCount int         // method_count: denormalized count
	Fields      []TypeField // Joined: type fields (is_method=0)
	Methods     []TypeField // Joined: type methods (is_method=1)
}

// TypeField represents a struct field or interface method.
type TypeField struct {
	ID          string  // field_id: UUID or {type_id}::{name}
	TypeID      string  // type_id: FK to types
	Name        string  // name: field/method name
	FieldType   string  // field_type: string, int, *User, etc.
	Position    int     // position: 0-indexed position in type
	IsMethod    bool    // is_method: interface method vs struct field
	IsExported  bool    // is_exported: uppercase first letter (Go)
	ParamCount  *int    // param_count: for methods (nullable)
	ReturnCount *int    // return_count: for methods (nullable)
}

// Function represents a function or method definition in code.
type Function struct {
	ID                   string               // function_id: {file_path}::{name} or UUID
	FilePath             string               // file_path: relative path from repo root
	ModulePath           string               // module_path: package/module name
	Name                 string               // name: function name
	StartLine            int                  // start_line: start line number
	EndLine              int                  // end_line: end line number
	LineCount            int                  // line_count: end_line - start_line
	IsExported           bool                 // is_exported: uppercase first letter (Go)
	IsMethod             bool                 // is_method: has receiver
	ReceiverTypeID       *string              // receiver_type_id: FK to types (nullable)
	ReceiverTypeName     *string              // receiver_type_name: denormalized (nullable)
	ParamCount           int                  // param_count: number of parameters
	ReturnCount          int                  // return_count: number of return values
	CyclomaticComplexity *int                 // cyclomatic_complexity: optional metric (nullable)
	Parameters           []FunctionParameter  // Joined: function parameters (is_return=0)
	ReturnValues         []FunctionParameter  // Joined: return values (is_return=1)
}

// FunctionParameter represents a function parameter or return value.
type FunctionParameter struct {
	ID         string  // param_id: UUID or {function_id}::param{N}
	FunctionID string  // function_id: FK to functions
	Name       *string // name: parameter name (nullable for unnamed returns)
	ParamType  string  // param_type: string, *User, error, etc.
	Position   int     // position: 0-indexed
	IsReturn   bool    // is_return: parameter vs return value
	IsVariadic bool    // is_variadic: ...args
}

// Import represents an import/dependency declaration.
type Import struct {
	ID            string // import_id: UUID or {file_path}::{import_path}
	FilePath      string // file_path: relative path from repo root
	ImportPath    string // import_path: github.com/user/pkg, ./local, etc.
	IsStandardLib bool   // is_standard_lib: part of language stdlib
	IsExternal    bool   // is_external: third-party dependency
	IsRelative    bool   // is_relative: ./pkg, ../other
	ImportLine    int    // import_line: line number
}

// FunctionCall represents a function call relationship.
type FunctionCall struct {
	ID               string  // call_id: UUID
	CallerFunctionID string  // caller_function_id: who is calling
	CalleeFunctionID *string // callee_function_id: what is being called (nullable if external)
	CalleeName       string  // callee_name: function name (for external calls)
	SourceFilePath   string  // source_file_path: where call occurs
	CallLine         int     // call_line: line number
	CallColumn       *int    // call_column: optional column number (nullable)
}
