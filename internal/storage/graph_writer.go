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
	db     *sql.DB
	ownsDB bool // true if we opened the connection, false if shared
}

// NewGraphWriter creates a new GraphWriter for the specified database.
// The database schema must already be created via CreateSchema.
// Enables foreign keys for relational integrity.
//
// Deprecated: Use NewGraphWriterWithDB to share database connections.
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

	return &GraphWriter{db: db, ownsDB: true}, nil
}

// NewGraphWriterWithDB creates a GraphWriter using an existing database connection.
// The caller is responsible for managing the database lifecycle (schema, foreign keys, close).
// This is the preferred constructor when sharing a connection across multiple writers.
func NewGraphWriterWithDB(db *sql.DB) *GraphWriter {
	return &GraphWriter{db: db, ownsDB: false}
}

// Close closes the database connection if owned by this writer.
// If created via NewGraphWriterWithDB (shared connection), this is a no-op.
func (w *GraphWriter) Close() error {
	if !w.ownsDB {
		// Shared connection - caller owns it
		return nil
	}
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
	fmt.Printf("[GRAPH DEBUG] Converted %d nodes -> %d types, %d functions\n", len(graphData.Nodes), len(types), len(functions))

	// Build sets of valid IDs for FK constraint validation
	validTypeIDs := make(map[string]bool)
	for _, t := range types {
		validTypeIDs[t.ID] = true
	}
	validFunctionIDs := make(map[string]bool)
	for _, f := range functions {
		validFunctionIDs[f.ID] = true
	}

	// Write types first (functions may have FK to types via receiver_type_id)
	if err := writeTypes(tx, types); err != nil {
		return fmt.Errorf("failed to write types: %w", err)
	}
	fmt.Printf("[GRAPH DEBUG] Wrote %d types to database\n", len(types))

	// Write functions
	if err := writeFunctions(tx, functions); err != nil {
		return fmt.Errorf("failed to write functions: %w", err)
	}
	fmt.Printf("[GRAPH DEBUG] Wrote %d functions to database\n", len(functions))

	// Convert edges to relationships and calls (filtering invalid FKs)
	relationships, calls := convertEdgesToSQL(graphData.Edges, validTypeIDs, validFunctionIDs)
	fmt.Printf("[GRAPH DEBUG] Converted %d edges -> %d relationships, %d calls\n", len(graphData.Edges), len(relationships), len(calls))

	// Write relationships
	if err := writeRelationships(tx, relationships); err != nil {
		return fmt.Errorf("failed to write relationships: %w", err)
	}
	fmt.Printf("[GRAPH DEBUG] Wrote %d relationships to database\n", len(relationships))

	// Write calls
	if err := writeCalls(tx, calls); err != nil {
		return fmt.Errorf("failed to write calls: %w", err)
	}
	fmt.Printf("[GRAPH DEBUG] Wrote %d calls to database\n", len(calls))

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	fmt.Printf("[GRAPH DEBUG] Transaction committed successfully\n")

	return nil
}

// WriteTypes writes type records to the database.
func (w *GraphWriter) WriteTypes(types []*GraphType) error {
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
func (w *GraphWriter) WriteFunctions(functions []*GraphFunction) error {
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

// GraphType represents a code type (interface, struct, class, enum) for SQL storage.
type GraphType struct {
	ID          string
	FilePath    string
	ModulePath  string
	Name        string
	Kind        string // interface, struct, class, enum
	StartLine   int
	EndLine     int
	StartPos    int    // 0-indexed byte offset of node start
	EndPos      int    // 0-indexed byte offset of node end
	IsExported  bool
	FieldCount  int
	MethodCount int
}

// GraphFunction represents a function or method for SQL storage.
type GraphFunction struct {
	ID               string
	FilePath         string
	ModulePath       string
	Name             string
	StartLine        int
	EndLine          int
	StartPos         int     // 0-indexed byte offset of node start
	EndPos           int     // 0-indexed byte offset of node end
	IsExported       bool
	IsMethod         bool
	ReceiverTypeID   *string // nullable
	ReceiverTypeName *string // nullable
	ParamCount       int
	ReturnCount      int
}

// Model types (TypeRelationship, FunctionCall) are now defined in models.go

// convertNodesToSQL converts graph.Node slice to Type and Function slices.
func convertNodesToSQL(nodes []graph.Node) ([]*GraphType, []*GraphFunction) {
	var types []*GraphType
	var functions []*GraphFunction

	// Count node kinds for debugging
	kindCounts := make(map[graph.NodeKind]int)
	for _, node := range nodes {
		kindCounts[node.Kind]++
	}
	fmt.Printf("[GRAPH DEBUG] Node kinds: %+v\n", kindCounts)

	for _, node := range nodes {
		switch node.Kind {
		case graph.NodeInterface, graph.NodeStruct:
			// Extract module path from file path (simplified: use dirname)
			modulePath := extractModulePath(node.File)

			types = append(types, &GraphType{
				ID:          node.ID,
				FilePath:    node.File,
				ModulePath:  modulePath,
				Name:        extractTypeName(node.ID),
				Kind:        string(node.Kind),
				StartLine:   node.StartLine,
				EndLine:     node.EndLine,
				StartPos:    node.StartPos,
				EndPos:      node.EndPos,
				IsExported:  isExported(extractTypeName(node.ID)),
				FieldCount:  len(node.EmbeddedTypes), // Approximate
				MethodCount: len(node.Methods),
			})

		case graph.NodeFunction, graph.NodeMethod:
			modulePath := extractModulePath(node.File)
			isMethod := node.Kind == graph.NodeMethod

			fn := &GraphFunction{
				ID:         node.ID,
				FilePath:   node.File,
				ModulePath: modulePath,
				Name:       extractFunctionName(node.ID),
				StartLine:  node.StartLine,
				EndLine:    node.EndLine,
				StartPos:   node.StartPos,
				EndPos:     node.EndPos,
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
// Filters out edges that would violate FK constraints (references to non-existent types/functions).
func convertEdgesToSQL(edges []graph.Edge, validTypeIDs, validFunctionIDs map[string]bool) ([]*TypeRelationship, []*FunctionCall) {
	var relationships []*TypeRelationship
	var calls []*FunctionCall

	skippedRelationships := 0
	skippedCalls := 0

	for _, edge := range edges {
		switch edge.Type {
		case graph.EdgeImplements, graph.EdgeEmbeds:
			// Skip relationships where either type doesn't exist in our types table
			if !validTypeIDs[edge.From] || !validTypeIDs[edge.To] {
				skippedRelationships++
				continue
			}

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
			// Skip calls where the caller function doesn't exist
			if !validFunctionIDs[edge.From] {
				skippedCalls++
				continue
			}

			call := &FunctionCall{
				ID:               uuid.New().String(),
				CallerFunctionID: edge.From,
				CalleeName:       extractFunctionName(edge.To),
			}

			// Only set callee_function_id if it's an internal call (exists in our graph)
			// External calls have NULL callee_function_id but keep the name
			if !isExternalCall(edge.To) && validFunctionIDs[edge.To] {
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

	if skippedRelationships > 0 || skippedCalls > 0 {
		fmt.Printf("[GRAPH DEBUG] Skipped %d invalid relationships and %d invalid calls (FK violations)\n", skippedRelationships, skippedCalls)
	}

	return relationships, calls
}

// writeTypes writes types to the database within a transaction.
func writeTypes(tx *sql.Tx, types []*GraphType) error {
	if len(types) == 0 {
		return nil
	}

	for _, t := range types {
		_, err := sq.Insert("types").
			Columns(
				"type_id", "file_path", "module_path", "name", "kind",
				"start_line", "end_line", "start_pos", "end_pos",
				"is_exported", "field_count", "method_count",
			).
			Values(
				t.ID, t.FilePath, t.ModulePath, t.Name, t.Kind,
				t.StartLine, t.EndLine, t.StartPos, t.EndPos,
				boolToInt(t.IsExported), t.FieldCount, t.MethodCount,
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
func writeFunctions(tx *sql.Tx, functions []*GraphFunction) error {
	if len(functions) == 0 {
		return nil
	}

	for _, fn := range functions {
		lineCount := fn.EndLine - fn.StartLine
		_, err := sq.Insert("functions").
			Columns(
				"function_id", "file_path", "module_path", "name",
				"start_line", "end_line", "start_pos", "end_pos", "line_count",
				"is_exported", "is_method", "receiver_type_id", "receiver_type_name",
				"param_count", "return_count",
			).
			Values(
				fn.ID, fn.FilePath, fn.ModulePath, fn.Name,
				fn.StartLine, fn.EndLine, fn.StartPos, fn.EndPos, lineCount,
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
