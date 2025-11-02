package storage

import (
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/mvp-joe/project-cortex/internal/graph"
)

// GraphWriter writes graph data (types, functions, relationships, calls) to SQLite.
// Provides both bulk write (complete graph) and granular writes (types only, functions only, etc.).
type GraphWriter struct {
	db *sql.DB
}

// NewGraphWriter creates a new GraphWriter for the specified database.
// The database schema must already be created via CreateSchema.
// Enables foreign keys for relational integrity.
func NewGraphWriter(dbPath string) (*GraphWriter, error) {
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	return &GraphWriter{db: db}, nil
}

// Close closes the database connection.
func (w *GraphWriter) Close() error {
	if w.db != nil {
		return w.db.Close()
	}
	return nil
}

// WriteGraphData writes complete graph data to SQLite in a single transaction.
// Clears existing graph data before writing to ensure clean state.
//
// Steps:
//  1. Begin transaction
//  2. Clear existing graph data (cascade deletes handle child records)
//  3. Convert GraphData.Nodes to Type and Function rows
//  4. Convert GraphData.Edges to TypeRelationship and FunctionCall rows
//  5. Commit transaction
//
// Returns error if conversion or write fails (rolls back transaction).
func (w *GraphWriter) WriteGraphData(graphData *graph.GraphData) error {
	if graphData == nil {
		return fmt.Errorf("graphData cannot be nil")
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Clear existing graph data (in reverse dependency order to avoid FK violations)
	clearTables := []string{
		"function_calls",
		"type_relationships",
		"function_parameters",
		"type_fields",
		"functions",
		"types",
	}
	for _, table := range clearTables {
		if _, err := sq.Delete(table).RunWith(tx).Exec(); err != nil {
			return fmt.Errorf("failed to clear existing data (%s): %w", table, err)
		}
	}

	// Convert nodes to types and functions
	types, functions := convertNodesToSQL(graphData.Nodes)

	// Write types first (functions may have FK to types via receiver_type_id)
	if err := writeTypes(tx, types); err != nil {
		return fmt.Errorf("failed to write types: %w", err)
	}

	// Write functions
	if err := writeFunctions(tx, functions); err != nil {
		return fmt.Errorf("failed to write functions: %w", err)
	}

	// Convert edges to relationships and calls
	relationships, calls := convertEdgesToSQL(graphData.Edges)

	// Write relationships
	if err := writeRelationships(tx, relationships); err != nil {
		return fmt.Errorf("failed to write relationships: %w", err)
	}

	// Write calls
	if err := writeCalls(tx, calls); err != nil {
		return fmt.Errorf("failed to write calls: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// WriteTypes writes type records to the database.
func (w *GraphWriter) WriteTypes(types []*Type) error {
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := writeTypes(tx, types); err != nil {
		return err
	}

	return tx.Commit()
}

// WriteFunctions writes function records to the database.
func (w *GraphWriter) WriteFunctions(functions []*Function) error {
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := writeFunctions(tx, functions); err != nil {
		return err
	}

	return tx.Commit()
}

// WriteRelationships writes type relationship records to the database.
func (w *GraphWriter) WriteRelationships(relationships []*TypeRelationship) error {
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := writeRelationships(tx, relationships); err != nil {
		return err
	}

	return tx.Commit()
}

// WriteCalls writes function call records to the database.
func (w *GraphWriter) WriteCalls(calls []*FunctionCall) error {
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if err := writeCalls(tx, calls); err != nil {
		return err
	}

	return tx.Commit()
}

// Type represents a code type (interface, struct, class, enum) for SQL storage.
type Type struct {
	ID         string
	FilePath   string
	ModulePath string
	Name       string
	Kind       string // interface, struct, class, enum
	StartLine  int
	EndLine    int
	IsExported bool
	FieldCount int
	MethodCount int
}

// Function represents a function or method for SQL storage.
type Function struct {
	ID              string
	FilePath        string
	ModulePath      string
	Name            string
	StartLine       int
	EndLine         int
	IsExported      bool
	IsMethod        bool
	ReceiverTypeID  *string // nullable
	ReceiverTypeName *string // nullable
	ParamCount      int
	ReturnCount     int
}

// TypeRelationship represents a type relationship edge (implements, embeds).
type TypeRelationship struct {
	ID               string
	FromTypeID       string
	ToTypeID         string
	RelationshipType string // implements, embeds
	SourceFilePath   string
	SourceLine       int
}

// FunctionCall represents a function call edge.
type FunctionCall struct {
	ID               string
	CallerFunctionID string
	CalleeFunctionID *string // nullable for external calls
	CalleeName       string
	SourceFilePath   string
	CallLine         int
	CallColumn       *int // nullable
}

// convertNodesToSQL converts graph.Node slice to Type and Function slices.
func convertNodesToSQL(nodes []graph.Node) ([]*Type, []*Function) {
	var types []*Type
	var functions []*Function

	for _, node := range nodes {
		switch node.Kind {
		case graph.NodeInterface, graph.NodeStruct:
			// Extract module path from file path (simplified: use dirname)
			modulePath := extractModulePath(node.File)

			types = append(types, &Type{
				ID:         node.ID,
				FilePath:   node.File,
				ModulePath: modulePath,
				Name:       extractTypeName(node.ID),
				Kind:       string(node.Kind),
				StartLine:  node.StartLine,
				EndLine:    node.EndLine,
				IsExported: isExported(extractTypeName(node.ID)),
				FieldCount: len(node.EmbeddedTypes), // Approximate
				MethodCount: len(node.Methods),
			})

		case graph.NodeFunction, graph.NodeMethod:
			modulePath := extractModulePath(node.File)
			isMethod := node.Kind == graph.NodeMethod

			fn := &Function{
				ID:         node.ID,
				FilePath:   node.File,
				ModulePath: modulePath,
				Name:       extractFunctionName(node.ID),
				StartLine:  node.StartLine,
				EndLine:    node.EndLine,
				IsExported: isExported(extractFunctionName(node.ID)),
				IsMethod:   isMethod,
			}

			// For methods, extract receiver type from ID (e.g., "localProvider.Embed" -> "localProvider")
			if isMethod {
				receiverType := extractReceiverType(node.ID)
				fn.ReceiverTypeName = &receiverType
				// ReceiverTypeID would need to be resolved from types table (deferred to caller)
			}

			// Count parameters and returns from first method signature if available
			if len(node.Methods) > 0 {
				fn.ParamCount = len(node.Methods[0].Parameters)
				fn.ReturnCount = len(node.Methods[0].Returns)
			}

			functions = append(functions, fn)
		}
	}

	return types, functions
}

// convertEdgesToSQL converts graph.Edge slice to TypeRelationship and FunctionCall slices.
func convertEdgesToSQL(edges []graph.Edge) ([]*TypeRelationship, []*FunctionCall) {
	var relationships []*TypeRelationship
	var calls []*FunctionCall

	for _, edge := range edges {
		switch edge.Type {
		case graph.EdgeImplements, graph.EdgeEmbeds:
			rel := &TypeRelationship{
				ID:               uuid.New().String(),
				FromTypeID:       edge.From,
				ToTypeID:         edge.To,
				RelationshipType: string(edge.Type),
			}
			if edge.Location != nil {
				rel.SourceFilePath = edge.Location.File
				rel.SourceLine = edge.Location.Line
			}
			relationships = append(relationships, rel)

		case graph.EdgeCalls:
			call := &FunctionCall{
				ID:               uuid.New().String(),
				CallerFunctionID: edge.From,
				CalleeName:       extractFunctionName(edge.To),
			}

			// Only set callee_function_id if it's an internal call (exists in our graph)
			// External calls have NULL callee_function_id but keep the name
			if !isExternalCall(edge.To) {
				calleeFnID := edge.To
				call.CalleeFunctionID = &calleeFnID
			}

			if edge.Location != nil {
				call.SourceFilePath = edge.Location.File
				call.CallLine = edge.Location.Line
				if edge.Location.Column > 0 {
					col := edge.Location.Column
					call.CallColumn = &col
				}
			}
			calls = append(calls, call)
		}
	}

	return relationships, calls
}

// writeTypes writes types to the database within a transaction.
func writeTypes(tx *sql.Tx, types []*Type) error {
	if len(types) == 0 {
		return nil
	}

	for _, t := range types {
		_, err := sq.Insert("types").
			Columns(
				"type_id", "file_path", "module_path", "name", "kind",
				"start_line", "end_line", "is_exported", "field_count", "method_count",
			).
			Values(
				t.ID, t.FilePath, t.ModulePath, t.Name, t.Kind,
				t.StartLine, t.EndLine, boolToInt(t.IsExported),
				t.FieldCount, t.MethodCount,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to insert type %s: %w", t.ID, err)
		}
	}

	return nil
}

// writeFunctions writes functions to the database within a transaction.
func writeFunctions(tx *sql.Tx, functions []*Function) error {
	if len(functions) == 0 {
		return nil
	}

	for _, fn := range functions {
		lineCount := fn.EndLine - fn.StartLine
		_, err := sq.Insert("functions").
			Columns(
				"function_id", "file_path", "module_path", "name",
				"start_line", "end_line", "line_count",
				"is_exported", "is_method", "receiver_type_id", "receiver_type_name",
				"param_count", "return_count",
			).
			Values(
				fn.ID, fn.FilePath, fn.ModulePath, fn.Name,
				fn.StartLine, fn.EndLine, lineCount,
				boolToInt(fn.IsExported), boolToInt(fn.IsMethod),
				fn.ReceiverTypeID, fn.ReceiverTypeName,
				fn.ParamCount, fn.ReturnCount,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to insert function %s: %w", fn.ID, err)
		}
	}

	return nil
}

// writeRelationships writes type relationships to the database within a transaction.
func writeRelationships(tx *sql.Tx, relationships []*TypeRelationship) error {
	if len(relationships) == 0 {
		return nil
	}

	for _, rel := range relationships {
		_, err := sq.Insert("type_relationships").
			Columns(
				"relationship_id", "from_type_id", "to_type_id",
				"relationship_type", "source_file_path", "source_line",
			).
			Values(
				rel.ID, rel.FromTypeID, rel.ToTypeID,
				rel.RelationshipType, rel.SourceFilePath, rel.SourceLine,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to insert relationship %s: %w", rel.ID, err)
		}
	}

	return nil
}

// writeCalls writes function calls to the database within a transaction.
func writeCalls(tx *sql.Tx, calls []*FunctionCall) error {
	if len(calls) == 0 {
		return nil
	}

	for _, call := range calls {
		_, err := sq.Insert("function_calls").
			Columns(
				"call_id", "caller_function_id", "callee_function_id", "callee_name",
				"source_file_path", "call_line", "call_column",
			).
			Values(
				call.ID, call.CallerFunctionID, call.CalleeFunctionID, call.CalleeName,
				call.SourceFilePath, call.CallLine, call.CallColumn,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to insert call %s: %w", call.ID, err)
		}
	}

	return nil
}

// Helper functions for ID parsing and conversion

func extractModulePath(filePath string) string {
	// Simplified: use directory of file
	// In real implementation, would resolve to Go package or TS module
	if len(filePath) == 0 {
		return ""
	}
	// For now, just return "internal/<dir>" pattern or the directory
	return "extracted_module" // TODO: Implement proper module resolution
}

func extractTypeName(id string) string {
	// ID format: "package.TypeName" or just "TypeName"
	// Extract after last dot
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '.' {
			return id[i+1:]
		}
	}
	return id
}

func extractFunctionName(id string) string {
	// ID format: "package.FunctionName" or "receiver.MethodName"
	// Extract after last dot
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '.' {
			return id[i+1:]
		}
	}
	return id
}

func extractReceiverType(id string) string {
	// ID format: "receiverType.MethodName"
	// Extract before last dot
	for i := len(id) - 1; i >= 0; i-- {
		if id[i] == '.' {
			return id[:i]
		}
	}
	return ""
}

func isExported(name string) bool {
	if len(name) == 0 {
		return false
	}
	// In Go, uppercase first letter means exported
	return name[0] >= 'A' && name[0] <= 'Z'
}

func isExternalCall(targetID string) bool {
	// Simple heuristic: if ID doesn't match our internal pattern, it's external
	// Real implementation would check against known function IDs in the graph
	// For now, assume standard library or third-party if it contains certain patterns
	return len(targetID) == 0 || targetID == "unknown"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
