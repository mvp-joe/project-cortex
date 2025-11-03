package files

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTableSchema_HasColumn(t *testing.T) {
	t.Parallel()

	schema := NewTableSchema("test_table", "col1", "col2", "col3")

	assert.True(t, schema.HasColumn("col1"))
	assert.True(t, schema.HasColumn("col2"))
	assert.True(t, schema.HasColumn("col3"))
	assert.False(t, schema.HasColumn("col4"))
	assert.False(t, schema.HasColumn(""))
}

func TestSchemaRegistry_GetTable(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()

	// Test existing table
	filesTable, ok := registry.GetTable("files")
	assert.True(t, ok)
	assert.Equal(t, "files", filesTable.Name)
	assert.True(t, filesTable.HasColumn("file_path"))
	assert.True(t, filesTable.HasColumn("language"))

	// Test non-existent table
	_, ok = registry.GetTable("nonexistent")
	assert.False(t, ok)
}

func TestSchemaRegistry_HasTable(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()

	// All expected tables should exist
	expectedTables := []string{
		"files",
		"types",
		"type_fields",
		"functions",
		"function_parameters",
		"type_relationships",
		"function_calls",
		"imports",
		"chunks",
		"cache_metadata",
	}

	for _, table := range expectedTables {
		assert.True(t, registry.HasTable(table), "table %s should exist", table)
	}

	// Non-existent tables
	assert.False(t, registry.HasTable("nonexistent"))
	assert.False(t, registry.HasTable(""))
}

func TestSchemaRegistry_FilesTable(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()
	table, ok := registry.GetTable("files")
	require.True(t, ok)

	// Verify all expected columns exist
	expectedColumns := []string{
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
	}

	for _, col := range expectedColumns {
		assert.True(t, table.HasColumn(col), "files table should have column %s", col)
	}

	// Verify non-existent columns
	assert.False(t, table.HasColumn("nonexistent"))
}

func TestSchemaRegistry_TypesTable(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()
	table, ok := registry.GetTable("types")
	require.True(t, ok)

	expectedColumns := []string{
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
	}

	for _, col := range expectedColumns {
		assert.True(t, table.HasColumn(col), "types table should have column %s", col)
	}
}

func TestSchemaRegistry_FunctionsTable(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()
	table, ok := registry.GetTable("functions")
	require.True(t, ok)

	expectedColumns := []string{
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
	}

	for _, col := range expectedColumns {
		assert.True(t, table.HasColumn(col), "functions table should have column %s", col)
	}
}

func TestSchemaRegistry_ImportsTable(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()
	table, ok := registry.GetTable("imports")
	require.True(t, ok)

	expectedColumns := []string{
		"import_id",
		"file_path",
		"import_path",
		"is_standard_lib",
		"is_external",
		"is_relative",
		"import_line",
	}

	for _, col := range expectedColumns {
		assert.True(t, table.HasColumn(col), "imports table should have column %s", col)
	}
}

func TestSchemaRegistry_ValidateTableAndColumn_ValidCases(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()

	testCases := []struct {
		table  string
		column string
	}{
		{"files", "file_path"},
		{"files", "language"},
		{"types", "type_id"},
		{"types", "name"},
		{"functions", "function_id"},
		{"functions", "is_exported"},
		{"imports", "import_path"},
		{"chunks", "chunk_id"},
	}

	for _, tc := range testCases {
		err := registry.ValidateTableAndColumn(tc.table, tc.column)
		assert.NoError(t, err, "table=%s, column=%s should be valid", tc.table, tc.column)
	}
}

func TestSchemaRegistry_ValidateTableAndColumn_InvalidTable(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()

	err := registry.ValidateTableAndColumn("nonexistent", "any_column")
	require.Error(t, err)

	validationErr, ok := err.(*ValidationError)
	require.True(t, ok)
	assert.Equal(t, "table", validationErr.Field)
	assert.Equal(t, "nonexistent", validationErr.Value)
	assert.Contains(t, validationErr.Message, "unknown table")
}

func TestSchemaRegistry_ValidateTableAndColumn_InvalidColumn(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()

	err := registry.ValidateTableAndColumn("files", "nonexistent_column")
	require.Error(t, err)

	validationErr, ok := err.(*ValidationError)
	require.True(t, ok)
	assert.Equal(t, "field", validationErr.Field)
	assert.Equal(t, "nonexistent_column", validationErr.Value)
	assert.Contains(t, validationErr.Message, "unknown column")
}

func TestSchemaRegistry_AllTables(t *testing.T) {
	t.Parallel()

	registry := NewSchemaRegistry()

	// Verify all 10 tables exist
	tables := []string{
		"files",
		"types",
		"type_fields",
		"functions",
		"function_parameters",
		"type_relationships",
		"function_calls",
		"imports",
		"chunks",
		"cache_metadata",
	}

	for _, tableName := range tables {
		_, ok := registry.GetTable(tableName)
		assert.True(t, ok, "table %s should exist in registry", tableName)
	}
}
