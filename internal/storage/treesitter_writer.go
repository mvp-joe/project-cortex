package storage

import (
	"database/sql"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
)

// TreeSitterWriter handles writing tree-sitter extraction data to SQLite.
// Writes types, functions, and imports with their relationships.
type TreeSitterWriter struct {
	db *sql.DB
}

// Model types (Type, TypeField, Function, FunctionParameter, Import) are now defined in models.go

// NewTreeSitterWriter creates a TreeSitterWriter instance.
// DB must have schema already created via CreateSchema().
func NewTreeSitterWriter(db *sql.DB) *TreeSitterWriter {
	return &TreeSitterWriter{db: db}
}

// WriteTypesBatch writes multiple types and their fields in a single transaction.
// Uses INSERT OR REPLACE to handle updates.
func (w *TreeSitterWriter) WriteTypesBatch(types []*Type) error {
	if len(types) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Prepare statement for types
	typeSql, _, err := sq.Insert("types").
		Columns(
			"type_id", "file_path", "module_path", "name", "kind",
			"start_line", "end_line", "is_exported", "field_count", "method_count",
		).
		Values("", "", "", "", "", 0, 0, false, 0, 0).
		Options("OR REPLACE").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build type SQL: %w", err)
	}

	typeStmt, err := tx.Prepare(typeSql)
	if err != nil {
		return fmt.Errorf("failed to prepare type statement: %w", err)
	}
	defer typeStmt.Close()

	// Prepare statement for type fields
	fieldSql, _, err := sq.Insert("type_fields").
		Columns(
			"field_id", "type_id", "name", "field_type", "position",
			"is_method", "is_exported", "param_count", "return_count",
		).
		Values("", "", "", "", 0, false, false, nil, nil).
		Options("OR REPLACE").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build field SQL: %w", err)
	}

	fieldStmt, err := tx.Prepare(fieldSql)
	if err != nil {
		return fmt.Errorf("failed to prepare field statement: %w", err)
	}
	defer fieldStmt.Close()

	// Insert all types and their fields
	for _, t := range types {
		// Generate ID if not provided
		if t.ID == "" {
			t.ID = uuid.New().String()
		}

		// Insert type
		_, err := typeStmt.Exec(
			t.ID,
			t.FilePath,
			t.ModulePath,
			t.Name,
			t.Kind,
			t.StartLine,
			t.EndLine,
			t.IsExported,
			t.FieldCount,
			t.MethodCount,
		)
		if err != nil {
			return fmt.Errorf("failed to insert type %s: %w", t.Name, err)
		}

		// Insert fields if provided
		for _, field := range t.Fields {
			// Generate field ID if not provided
			if field.ID == "" {
				field.ID = uuid.New().String()
			}

			// Ensure TypeID matches parent
			field.TypeID = t.ID

			_, err := fieldStmt.Exec(
				field.ID,
				field.TypeID,
				field.Name,
				field.FieldType,
				field.Position,
				field.IsMethod,
				field.IsExported,
				field.ParamCount,  // Already *int
				field.ReturnCount, // Already *int
			)
			if err != nil {
				return fmt.Errorf("failed to insert field %s.%s: %w", t.Name, field.Name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit types batch: %w", err)
	}

	return nil
}

// WriteFunctionsBatch writes multiple functions and their parameters in a single transaction.
// Uses INSERT OR REPLACE to handle updates.
func (w *TreeSitterWriter) WriteFunctionsBatch(functions []*Function) error {
	if len(functions) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare statement for functions
	funcSql, _, err := sq.Insert("functions").
		Columns(
			"function_id", "file_path", "module_path", "name",
			"start_line", "end_line", "line_count",
			"is_exported", "is_method", "receiver_type_id", "receiver_type_name",
			"param_count", "return_count",
		).
		Values("", "", "", "", 0, 0, 0, false, false, nil, nil, 0, 0).
		Options("OR REPLACE").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build function SQL: %w", err)
	}

	funcStmt, err := tx.Prepare(funcSql)
	if err != nil {
		return fmt.Errorf("failed to prepare function statement: %w", err)
	}
	defer funcStmt.Close()

	// Prepare statement for parameters
	paramSql, _, err := sq.Insert("function_parameters").
		Columns(
			"param_id", "function_id", "name", "param_type",
			"position", "is_return", "is_variadic",
		).
		Values("", "", nil, "", 0, false, false).
		Options("OR REPLACE").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build parameter SQL: %w", err)
	}

	paramStmt, err := tx.Prepare(paramSql)
	if err != nil {
		return fmt.Errorf("failed to prepare parameter statement: %w", err)
	}
	defer paramStmt.Close()

	// Insert all functions and their parameters
	for _, f := range functions {
		// Generate ID if not provided
		if f.ID == "" {
			f.ID = uuid.New().String()
		}

		// Insert function
		_, err := funcStmt.Exec(
			f.ID,
			f.FilePath,
			f.ModulePath,
			f.Name,
			f.StartLine,
			f.EndLine,
			f.LineCount,
			f.IsExported,
			f.IsMethod,
			nullableString(f.ReceiverTypeID),
			nullableString(f.ReceiverTypeName),
			f.ParamCount,
			f.ReturnCount,
		)
		if err != nil {
			return fmt.Errorf("failed to insert function %s: %w", f.Name, err)
		}

		// Insert parameters if provided
		for _, param := range f.Parameters {
			// Generate param ID if not provided
			if param.ID == "" {
				param.ID = uuid.New().String()
			}

			// Ensure FunctionID matches parent
			param.FunctionID = f.ID

			_, err := paramStmt.Exec(
				param.ID,
				param.FunctionID,
				nullableString(param.Name),
				param.ParamType,
				param.Position,
				param.IsReturn,
				param.IsVariadic,
			)
			if err != nil {
				return fmt.Errorf("failed to insert parameter for %s: %w", f.Name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit functions batch: %w", err)
	}

	return nil
}

// WriteImportsBatch writes multiple imports in a single transaction.
// Uses INSERT OR REPLACE to handle updates.
func (w *TreeSitterWriter) WriteImportsBatch(imports []*Import) error {
	if len(imports) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare statement for imports
	importSql, _, err := sq.Insert("imports").
		Columns(
			"import_id", "file_path", "import_path",
			"is_standard_lib", "is_external", "is_relative", "import_line",
		).
		Values("", "", "", false, false, false, 0).
		Options("OR REPLACE").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build import SQL: %w", err)
	}

	importStmt, err := tx.Prepare(importSql)
	if err != nil {
		return fmt.Errorf("failed to prepare import statement: %w", err)
	}
	defer importStmt.Close()

	// Insert all imports
	for _, imp := range imports {
		// Generate ID if not provided
		if imp.ID == "" {
			imp.ID = uuid.New().String()
		}

		_, err := importStmt.Exec(
			imp.ID,
			imp.FilePath,
			imp.ImportPath,
			imp.IsStandardLib,
			imp.IsExternal,
			imp.IsRelative,
			imp.ImportLine,
		)
		if err != nil {
			return fmt.Errorf("failed to insert import %s: %w", imp.ImportPath, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit imports batch: %w", err)
	}

	return nil
}

// Close releases resources held by the writer.
// The underlying DB connection is NOT closed (caller owns it).
func (w *TreeSitterWriter) Close() error {
	// No resources to clean up currently (no prepared statements cached)
	// DB is owned by caller, not closed here
	return nil
}

// nullableString converts *string to sql.NullString for nullable columns.
// Returns nil if pointer is nil, otherwise returns the string value.
func nullableString(s *string) interface{} {
	if s == nil {
		return nil
	}
	return *s
}

// nullableIntPtr converts *int to interface{} for database insertion.
// Returns nil if pointer is nil, otherwise returns the int value.
func nullableIntPtr(i *int) interface{} {
	if i == nil {
		return nil
	}
	return *i
}

// nullableInt is defined in chunk_writer.go to avoid duplication
