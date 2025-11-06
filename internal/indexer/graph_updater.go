package indexer

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/mvp-joe/project-cortex/internal/graph"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// GraphUpdater coordinates incremental graph updates based on file changes.
// Orchestrates: extraction → deletion → insertion → inference.
type GraphUpdater struct {
	db         *sql.DB
	extractor  graph.Extractor
	inferencer *storage.InterfaceInferencer
	rootDir    string
}

// NewGraphUpdater creates a new graph update coordinator.
// rootDir should be the absolute path to the project root.
func NewGraphUpdater(db *sql.DB, rootDir string) *GraphUpdater {
	return &GraphUpdater{
		db:         db,
		extractor:  graph.NewExtractor(rootDir),
		inferencer: storage.NewInterfaceInferencer(db),
		rootDir:    rootDir,
	}
}

// Update performs incremental graph updates based on file changes.
//
// Algorithm:
//  1. Process deletions (CASCADE handles related data)
//  2. Process additions/modifications (extract → delete old → insert new)
//  3. Re-infer interface implementations if types changed
//
// Returns error for logging. Failures should not block indexing since
// graph data is supplementary to core search functionality.
func (g *GraphUpdater) Update(ctx context.Context, changes *ChangeSet) error {
	hasTypeChanges := false

	// 1. Process deletions (CASCADE handles related data)
	for _, file := range changes.Deleted {
		if err := g.deleteCodeStructure(ctx, file); err != nil {
			return fmt.Errorf("delete %s: %w", file, err)
		}
		hasTypeChanges = true // Deleted types affect inference
	}

	// 2. Process additions and modifications
	changedFiles := append(changes.Added, changes.Modified...)
	for _, file := range changedFiles {
		// Only process Go files (graph extraction is Go-only currently)
		if !strings.HasSuffix(file, ".go") {
			continue
		}

		absPath := filepath.Join(g.rootDir, file)

		// Extract data from tree-sitter
		data, err := g.extractor.ExtractCodeStructure(absPath)
		if err != nil {
			return fmt.Errorf("extract %s: %w", file, err)
		}

		// Check if this file has type definitions
		if len(data.Types) > 0 {
			hasTypeChanges = true
		}

		// Delete old data for this file
		if err := g.deleteCodeStructure(ctx, file); err != nil {
			return fmt.Errorf("delete %s: %w", file, err)
		}

		// Insert new data
		if err := g.insertCodeStructure(ctx, file, data); err != nil {
			return fmt.Errorf("insert %s: %w", file, err)
		}
	}

	// 3. Re-infer interface implementations if types changed
	if hasTypeChanges {
		log.Printf("Type definitions changed, re-inferring interface implementations...")
		start := time.Now()

		if err := g.inferencer.InferImplementations(ctx); err != nil {
			return fmt.Errorf("interface inference: %w", err)
		}

		log.Printf("✓ Interface inference complete (%v)", time.Since(start))
	}

	return nil
}

// deleteCodeStructure removes all code structure data for a file.
// Foreign key CASCADE handles related data in child tables.
//
// Deleted tables:
//  - types (CASCADE to type_fields, type_relationships)
//  - functions (CASCADE to function_parameters, function_calls)
//  - imports
func (g *GraphUpdater) deleteCodeStructure(ctx context.Context, file string) error {
	// Delete from types (CASCADE to type_fields, type_relationships via from_type_id/to_type_id)
	_, err := g.db.ExecContext(ctx, "DELETE FROM types WHERE file_path = ?", file)
	if err != nil {
		return fmt.Errorf("delete types: %w", err)
	}

	// Delete from functions (CASCADE to function_parameters, function_calls via caller_function_id)
	_, err = g.db.ExecContext(ctx, "DELETE FROM functions WHERE file_path = ?", file)
	if err != nil {
		return fmt.Errorf("delete functions: %w", err)
	}

	// Delete imports
	_, err = g.db.ExecContext(ctx, "DELETE FROM imports WHERE file_path = ?", file)
	if err != nil {
		return fmt.Errorf("delete imports: %w", err)
	}

	return nil
}

// insertCodeStructure writes extracted code structure data to SQL tables.
// Uses a transaction to ensure atomicity.
func (g *GraphUpdater) insertCodeStructure(ctx context.Context, file string, data *graph.CodeStructure) error {
	tx, err := g.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Ensure file record exists (required by FK constraints)
	if err := g.ensureFileRecord(tx, file); err != nil {
		return fmt.Errorf("ensure file record: %w", err)
	}

	// Insert types and type_fields
	if err := g.insertTypes(tx, data.Types, data.TypeFields); err != nil {
		return fmt.Errorf("insert types: %w", err)
	}

	// Insert functions and function_parameters
	if err := g.insertFunctions(tx, data.Functions, data.FunctionParams); err != nil {
		return fmt.Errorf("insert functions: %w", err)
	}

	// Insert function_calls
	if err := g.insertFunctionCalls(tx, data.FunctionCalls); err != nil {
		return fmt.Errorf("insert calls: %w", err)
	}

	// Insert imports
	if err := g.insertImports(tx, data.Imports); err != nil {
		return fmt.Errorf("insert imports: %w", err)
	}

	return tx.Commit()
}

// insertTypes writes types and type_fields to SQL.
func (g *GraphUpdater) insertTypes(tx *sql.Tx, types []graph.Type, fields []graph.TypeField) error {
	if len(types) == 0 {
		return nil
	}

	// Insert types
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
			return fmt.Errorf("insert type %s: %w", t.ID, err)
		}
	}

	// Insert type_fields
	if len(fields) == 0 {
		return nil
	}

	for _, field := range fields {
		_, err := sq.Insert("type_fields").
			Columns(
				"field_id", "type_id", "name", "field_type", "position",
				"is_method", "is_exported", "param_count", "return_count",
			).
			Values(
				field.ID, field.TypeID, field.Name, field.FieldType, field.Position,
				boolToInt(field.IsMethod), boolToInt(field.IsExported),
				field.ParamCount, field.ReturnCount,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("insert type field %s: %w", field.ID, err)
		}
	}

	return nil
}

// insertFunctions writes functions and function_parameters to SQL.
func (g *GraphUpdater) insertFunctions(tx *sql.Tx, functions []graph.Function, params []graph.FunctionParameter) error {
	if len(functions) == 0 {
		return nil
	}

	// Insert functions
	for _, fn := range functions {
		_, err := sq.Insert("functions").
			Columns(
				"function_id", "file_path", "module_path", "name",
				"start_line", "end_line", "line_count",
				"is_exported", "is_method", "receiver_type_id", "receiver_type_name",
				"param_count", "return_count", "cyclomatic_complexity",
			).
			Values(
				fn.ID, fn.FilePath, fn.ModulePath, fn.Name,
				fn.StartLine, fn.EndLine, fn.LineCount,
				boolToInt(fn.IsExported), boolToInt(fn.IsMethod),
				fn.ReceiverTypeID, fn.ReceiverTypeName,
				fn.ParamCount, fn.ReturnCount, fn.CyclomaticComplexity,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("insert function %s: %w", fn.ID, err)
		}
	}

	// Insert function_parameters
	if len(params) == 0 {
		return nil
	}

	for _, param := range params {
		_, err := sq.Insert("function_parameters").
			Columns(
				"param_id", "function_id", "name", "param_type",
				"position", "is_return", "is_variadic",
			).
			Values(
				param.ID, param.FunctionID, param.Name, param.ParamType,
				param.Position, boolToInt(param.IsReturn), boolToInt(param.IsVariadic),
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("insert function parameter %s: %w", param.ID, err)
		}
	}

	return nil
}

// insertFunctionCalls writes function_calls to SQL.
func (g *GraphUpdater) insertFunctionCalls(tx *sql.Tx, calls []graph.FunctionCall) error {
	if len(calls) == 0 {
		return nil
	}

	for _, call := range calls {
		// Generate ID if not set
		callID := call.ID
		if callID == "" {
			callID = uuid.New().String()
		}

		_, err := sq.Insert("function_calls").
			Columns(
				"call_id", "caller_function_id", "callee_function_id", "callee_name",
				"source_file_path", "call_line", "call_column",
			).
			Values(
				callID, call.CallerFunctionID, call.CalleeFunctionID, call.CalleeName,
				call.SourceFilePath, call.CallLine, call.CallColumn,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("insert function call %s: %w", callID, err)
		}
	}

	return nil
}

// insertImports writes imports to SQL.
func (g *GraphUpdater) insertImports(tx *sql.Tx, imports []graph.Import) error {
	if len(imports) == 0 {
		return nil
	}

	for _, imp := range imports {
		// Generate ID if not set (format: {file_path}::{import_path})
		impID := imp.ID
		if impID == "" {
			impID = fmt.Sprintf("%s::%s", imp.FilePath, imp.ImportPath)
		}

		_, err := sq.Insert("imports").
			Columns(
				"import_id", "file_path", "import_path",
				"is_standard_lib", "is_external", "is_relative", "import_line",
			).
			Values(
				impID, imp.FilePath, imp.ImportPath,
				boolToInt(imp.IsStandardLib), boolToInt(imp.IsExternal),
				boolToInt(imp.IsRelative), imp.ImportLine,
			).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("insert import %s: %w", impID, err)
		}
	}

	return nil
}

// ensureFileRecord ensures a file record exists in the files table.
// Required because types and functions have FK constraints to files.
// If the record already exists, this is a no-op (INSERT OR IGNORE).
func (g *GraphUpdater) ensureFileRecord(tx *sql.Tx, filePath string) error {
	// Extract module path from file path using existing helper
	modulePath := extractModulePath(g.rootDir, filePath)

	// Determine language from extension
	language := "go" // Graph extraction currently only supports Go

	// Use raw SQL for INSERT OR IGNORE (Squirrel doesn't support it well)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO files (
			file_path, language, module_path, is_test,
			line_count_total, line_count_code, line_count_comment, line_count_blank,
			size_bytes, file_hash, last_modified, indexed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, filePath, language, modulePath, boolToInt(isTestFile(filePath)),
		0, 0, 0, 0, 0, "", now, now)
	return err
}

// boolToInt converts a boolean to SQLite integer (0 or 1).
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
