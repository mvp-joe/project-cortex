package storage

// Domain models that mirror SQL tables in schema.go.
// These are lightweight data transfer structs, NOT ORM models.
// Consolidated from treesitter_writer.go and graph_writer.go.

// Type represents a type definition (struct, interface, class, enum).
// Maps to the types table + joined data from type_fields.
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
// Maps to the type_fields table.
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
// Maps to the functions table + joined data from function_parameters.
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
// Maps to the function_parameters table.
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
// Maps to the imports table.
type Import struct {
	ID            string // import_id: UUID or {file_path}::{import_path}
	FilePath      string // file_path: relative path from repo root
	ImportPath    string // import_path: github.com/user/pkg, ./local, etc.
	IsStandardLib bool   // is_standard_lib: part of language stdlib
	IsExternal    bool   // is_external: third-party dependency
	IsRelative    bool   // is_relative: ./pkg, ../other
	ImportLine    int    // import_line: line number
}

// TypeRelationship represents a relationship between two types.
// Maps to the type_relationships table.
type TypeRelationship struct {
	ID               string // relationship_id: UUID
	FromTypeID       string // from_type_id: source type
	ToTypeID         string // to_type_id: target type
	RelationshipType string // relationship_type: implements, embeds, extends
	SourceFilePath   string // source_file_path: where relationship is declared
	SourceLine       int    // source_line: line number
}

// FunctionCall represents a function call relationship.
// Maps to the function_calls table.
type FunctionCall struct {
	ID               string  // call_id: UUID
	CallerFunctionID string  // caller_function_id: who is calling
	CalleeFunctionID *string // callee_function_id: what is being called (nullable if external)
	CalleeName       string  // callee_name: function name (for external calls)
	SourceFilePath   string  // source_file_path: where call occurs
	CallLine         int     // call_line: line number
	CallColumn       *int    // call_column: optional column number (nullable)
}
