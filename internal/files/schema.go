package files

// TableSchema defines the columns available in a database table.
type TableSchema struct {
	Name    string
	Columns map[string]bool // Map for O(1) lookup
}

// NewTableSchema creates a new table schema.
func NewTableSchema(name string, columns ...string) TableSchema {
	colMap := make(map[string]bool, len(columns))
	for _, col := range columns {
		colMap[col] = true
	}
	return TableSchema{
		Name:    name,
		Columns: colMap,
	}
}

// HasColumn checks if a column exists in this table.
func (ts TableSchema) HasColumn(column string) bool {
	return ts.Columns[column]
}

// SchemaRegistry holds the schema definitions for all queryable tables.
// It's built from the actual SQLite schema in internal/storage/schema.go.
type SchemaRegistry struct {
	tables map[string]TableSchema
}

// NewSchemaRegistry creates a registry with all table schemas from the database.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{
		tables: map[string]TableSchema{
			"files": NewTableSchema("files",
				"file_path",
				"language",
				"module_path",
				"is_test",
				"line_count_total",
				"line_count_code",
				"line_count_comment",
				"line_count_blank",
				"size_bytes",
				"file_hash",
				"last_modified",
				"indexed_at",
			),
			"types": NewTableSchema("types",
				"type_id",
				"file_path",
				"module_path",
				"name",
				"kind",
				"start_line",
				"end_line",
				"is_exported",
				"field_count",
				"method_count",
			),
			"type_fields": NewTableSchema("type_fields",
				"field_id",
				"type_id",
				"name",
				"field_type",
				"position",
				"is_method",
				"is_exported",
				"param_count",
				"return_count",
			),
			"functions": NewTableSchema("functions",
				"function_id",
				"file_path",
				"module_path",
				"name",
				"start_line",
				"end_line",
				"line_count",
				"is_exported",
				"is_method",
				"receiver_type_id",
				"receiver_type_name",
				"param_count",
				"return_count",
				"cyclomatic_complexity",
			),
			"function_parameters": NewTableSchema("function_parameters",
				"param_id",
				"function_id",
				"name",
				"param_type",
				"position",
				"is_return",
				"is_variadic",
			),
			"type_relationships": NewTableSchema("type_relationships",
				"relationship_id",
				"from_type_id",
				"to_type_id",
				"relationship_type",
				"source_file_path",
				"source_line",
			),
			"function_calls": NewTableSchema("function_calls",
				"call_id",
				"caller_function_id",
				"callee_function_id",
				"callee_name",
				"source_file_path",
				"call_line",
				"call_column",
			),
			"imports": NewTableSchema("imports",
				"import_id",
				"file_path",
				"import_path",
				"is_standard_lib",
				"is_external",
				"is_relative",
				"import_line",
			),
			"chunks": NewTableSchema("chunks",
				"chunk_id",
				"file_path",
				"chunk_type",
				"title",
				"text",
				"embedding",
				"start_line",
				"end_line",
				"created_at",
				"updated_at",
			),
			"cache_metadata": NewTableSchema("cache_metadata",
				"key",
				"value",
				"updated_at",
			),
		},
	}
}

// GetTable retrieves a table schema by name.
func (sr *SchemaRegistry) GetTable(name string) (TableSchema, bool) {
	table, ok := sr.tables[name]
	return table, ok
}

// HasTable checks if a table exists.
func (sr *SchemaRegistry) HasTable(name string) bool {
	_, ok := sr.tables[name]
	return ok
}

// ValidateTableAndColumn checks if a table exists and has the specified column.
func (sr *SchemaRegistry) ValidateTableAndColumn(table, column string) error {
	tableSchema, ok := sr.GetTable(table)
	if !ok {
		return &ValidationError{
			Field:   "table",
			Value:   table,
			Message: "unknown table",
			Hint:    "Valid tables: files, types, type_fields, functions, function_parameters, type_relationships, function_calls, imports, chunks, cache_metadata",
		}
	}

	if !tableSchema.HasColumn(column) {
		return &ValidationError{
			Field:   "field",
			Value:   column,
			Message: "unknown column in table " + table,
			Hint:    "Check the table schema for valid columns",
		}
	}

	return nil
}
