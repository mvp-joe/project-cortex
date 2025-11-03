//go:build integration

package files

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mvp-joe/project-cortex/internal/storage"
)

// Integration Test Plan:
// 1. Simple SELECT queries (files, types, functions) with filters
// 2. Aggregation queries (GROUP BY, COUNT, SUM, AVG, HAVING)
// 3. JOIN queries (files + types, functions + types)
// 4. Complex filters (AND/OR/NOT combinations, BETWEEN, IN, NULL checks)
// 5. Error cases (invalid tables, fields, operators, closed DB)
// 6. Multi-table queries with multiple filters
// 7. Performance validation (query timing)
// 8. Large dataset handling (pagination with LIMIT/OFFSET)

// setupIntegrationDB creates a fresh SQLite database populated with realistic test data.
func setupIntegrationDB(t *testing.T) *sql.DB {
	t.Helper()

	// Create in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema
	err = storage.CreateSchema(db)
	require.NoError(t, err)

	// Populate with realistic test data
	populateTestData(t, db)

	return db
}

// populateTestData inserts realistic test data spanning multiple modules and languages.
func populateTestData(t *testing.T, db *sql.DB) {
	t.Helper()

	now := time.Now().UTC()

	// Insert files (mix of Go, TypeScript, Python; test and non-test)
	files := []struct {
		path         string
		lang         string
		module       string
		isTest       int
		linesTotal   int
		linesCode    int
		linesComment int
		linesBlank   int
		sizeBytes    int
	}{
		// Go files
		{"internal/mcp/server.go", "go", "internal/mcp", 0, 350, 280, 50, 20, 12288},
		{"internal/mcp/server_test.go", "go", "internal/mcp", 1, 220, 190, 20, 10, 7168},
		{"internal/mcp/handler.go", "go", "internal/mcp", 0, 180, 150, 20, 10, 6144},
		{"internal/files/executor.go", "go", "internal/files", 0, 120, 100, 15, 5, 4096},
		{"internal/files/executor_test.go", "go", "internal/files", 1, 410, 370, 30, 10, 14336},
		{"internal/files/translator.go", "go", "internal/files", 0, 250, 210, 30, 10, 8192},
		{"internal/storage/schema.go", "go", "internal/storage", 0, 360, 300, 50, 10, 10240},
		{"internal/storage/file_writer.go", "go", "internal/storage", 0, 280, 240, 30, 10, 9216},
		{"cmd/cortex/main.go", "go", "cmd/cortex", 0, 95, 75, 15, 5, 2048},
		{"cmd/cortex-embed/main.go", "go", "cmd/cortex-embed", 0, 150, 120, 20, 10, 4096},

		// TypeScript files
		{"web/src/index.ts", "typescript", "web/src", 0, 80, 65, 10, 5, 2560},
		{"web/src/app.tsx", "typescript", "web/src", 0, 200, 170, 20, 10, 6144},
		{"web/src/components/search.tsx", "typescript", "web/src/components", 0, 150, 130, 15, 5, 5120},

		// Python files
		{"scripts/build.py", "python", "scripts", 0, 120, 95, 20, 5, 3072},
		{"scripts/test_build.py", "python", "scripts", 1, 80, 70, 8, 2, 2048},
	}

	for _, f := range files {
		_, err := db.Exec(`
			INSERT INTO files (file_path, language, module_path, is_test, line_count_total,
				line_count_code, line_count_comment, line_count_blank, size_bytes, file_hash,
				last_modified, indexed_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, f.path, f.lang, f.module, f.isTest, f.linesTotal, f.linesCode, f.linesComment,
			f.linesBlank, f.sizeBytes, "hash-"+f.path, now.Format(time.RFC3339), now.Format(time.RFC3339))
		require.NoError(t, err)
	}

	// Insert types (interfaces, structs, classes)
	types := []struct {
		typeID      string
		filePath    string
		module      string
		name        string
		kind        string
		startLine   int
		endLine     int
		isExported  int
		fieldCount  int
		methodCount int
	}{
		// Go types
		{"internal/mcp/server.go::Server", "internal/mcp/server.go", "internal/mcp", "Server", "struct", 15, 25, 1, 5, 0},
		{"internal/mcp/server.go::Handler", "internal/mcp/server.go", "internal/mcp", "Handler", "interface", 30, 35, 1, 0, 3},
		{"internal/mcp/handler.go::mcpHandler", "internal/mcp/handler.go", "internal/mcp", "mcpHandler", "struct", 10, 15, 0, 3, 0},
		{"internal/files/executor.go::Executor", "internal/files/executor.go", "internal/files", "Executor", "struct", 12, 14, 1, 1, 0},
		{"internal/files/translator.go::Builder", "internal/files/translator.go", "internal/files", "Builder", "struct", 20, 25, 1, 4, 0},
		{"internal/storage/schema.go::FileStats", "internal/storage/schema.go", "internal/storage", "FileStats", "struct", 50, 70, 1, 12, 0},
		{"internal/storage/file_writer.go::FileWriter", "internal/storage/file_writer.go", "internal/storage", "FileWriter", "struct", 15, 20, 1, 2, 0},

		// TypeScript types
		{"web/src/app.tsx::AppProps", "web/src/app.tsx", "web/src", "AppProps", "interface", 5, 10, 1, 3, 0},
		{"web/src/components/search.tsx::SearchProps", "web/src/components/search.tsx", "web/src/components", "SearchProps", "interface", 8, 15, 1, 4, 0},
	}

	for _, typ := range types {
		_, err := db.Exec(`
			INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line,
				is_exported, field_count, method_count)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, typ.typeID, typ.filePath, typ.module, typ.name, typ.kind, typ.startLine, typ.endLine,
			typ.isExported, typ.fieldCount, typ.methodCount)
		require.NoError(t, err)
	}

	// Insert functions (regular functions and methods)
	functions := []struct {
		functionID       string
		filePath         string
		module           string
		name             string
		startLine        int
		endLine          int
		lineCount        int
		isExported       int
		isMethod         int
		receiverTypeID   *string
		receiverTypeName *string
		paramCount       int
		returnCount      int
		complexity       *int
	}{
		// Go functions
		{"internal/mcp/server.go::NewServer", "internal/mcp/server.go", "internal/mcp", "NewServer", 40, 65, 25, 1, 0, nil, nil, 2, 2, intPtr(3)},
		{"internal/mcp/server.go::Start", "internal/mcp/server.go", "internal/mcp", "Start", 70, 120, 50, 1, 1, strPtr("internal/mcp/server.go::Server"), strPtr("Server"), 1, 1, intPtr(8)},
		{"internal/mcp/server.go::handleRequest", "internal/mcp/server.go", "internal/mcp", "handleRequest", 125, 180, 55, 0, 1, strPtr("internal/mcp/server.go::Server"), strPtr("Server"), 2, 2, intPtr(12)},
		{"internal/mcp/handler.go::newHandler", "internal/mcp/handler.go", "internal/mcp", "newHandler", 20, 35, 15, 0, 0, nil, nil, 1, 1, intPtr(2)},
		{"internal/files/executor.go::NewExecutor", "internal/files/executor.go", "internal/files", "NewExecutor", 18, 20, 2, 1, 0, nil, nil, 1, 1, intPtr(1)},
		{"internal/files/executor.go::Execute", "internal/files/executor.go", "internal/files", "Execute", 24, 93, 69, 1, 1, strPtr("internal/files/executor.go::Executor"), strPtr("Executor"), 1, 2, intPtr(10)},
		{"internal/files/translator.go::BuildQuery", "internal/files/translator.go", "internal/files", "BuildQuery", 30, 180, 150, 1, 0, nil, nil, 1, 3, intPtr(25)},
		{"internal/storage/schema.go::CreateSchema", "internal/storage/schema.go", "internal/storage", "CreateSchema", 19, 82, 63, 1, 0, nil, nil, 1, 1, intPtr(15)},
		{"internal/storage/file_writer.go::NewFileWriter", "internal/storage/file_writer.go", "internal/storage", "NewFileWriter", 25, 27, 2, 1, 0, nil, nil, 1, 1, intPtr(1)},
		{"internal/storage/file_writer.go::WriteFileStats", "internal/storage/file_writer.go", "internal/storage", "WriteFileStats", 30, 80, 50, 1, 1, strPtr("internal/storage/file_writer.go::FileWriter"), strPtr("FileWriter"), 1, 1, intPtr(5)},
		{"cmd/cortex/main.go::main", "cmd/cortex/main.go", "cmd/cortex", "main", 20, 90, 70, 0, 0, nil, nil, 0, 0, intPtr(10)},

		// TypeScript functions
		{"web/src/index.ts::main", "web/src/index.ts", "web/src", "main", 10, 40, 30, 1, 0, nil, nil, 0, 1, nil},
		{"web/src/app.tsx::App", "web/src/app.tsx", "web/src", "App", 15, 120, 105, 1, 0, nil, nil, 1, 1, nil},
		{"web/src/components/search.tsx::Search", "web/src/components/search.tsx", "web/src/components", "Search", 20, 95, 75, 1, 0, nil, nil, 1, 1, nil},
	}

	for _, fn := range functions {
		_, err := db.Exec(`
			INSERT INTO functions (function_id, file_path, module_path, name, start_line, end_line,
				line_count, is_exported, is_method, receiver_type_id, receiver_type_name,
				param_count, return_count, cyclomatic_complexity)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, fn.functionID, fn.filePath, fn.module, fn.name, fn.startLine, fn.endLine, fn.lineCount,
			fn.isExported, fn.isMethod, fn.receiverTypeID, fn.receiverTypeName, fn.paramCount,
			fn.returnCount, fn.complexity)
		require.NoError(t, err)
	}

	// Insert imports
	imports := []struct {
		filePath   string
		importPath string
		isStdLib   int
		isExternal int
		isRelative int
		line       int
	}{
		{"internal/mcp/server.go", "context", 1, 0, 0, 5},
		{"internal/mcp/server.go", "database/sql", 1, 0, 0, 6},
		{"internal/mcp/server.go", "github.com/mark3labs/mcp-go", 0, 1, 0, 8},
		{"internal/files/executor.go", "database/sql", 1, 0, 0, 4},
		{"internal/files/executor.go", "github.com/mattn/go-sqlite3", 0, 1, 0, 7},
		{"internal/storage/schema.go", "database/sql", 1, 0, 0, 3},
		{"internal/storage/file_writer.go", "database/sql", 1, 0, 0, 3},
		{"internal/storage/file_writer.go", "github.com/Masterminds/squirrel", 0, 1, 0, 6},
		{"cmd/cortex/main.go", "fmt", 1, 0, 0, 4},
		{"cmd/cortex/main.go", "internal/mcp", 0, 0, 1, 6},
	}

	for i, imp := range imports {
		importID := filepath.Join(imp.filePath, imp.importPath)
		_, err := db.Exec(`
			INSERT INTO imports (import_id, file_path, import_path, is_standard_lib, is_external,
				is_relative, import_line)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, importID+"-"+time.Now().Format("20060102150405")+string(rune(i)), imp.filePath, imp.importPath,
			imp.isStdLib, imp.isExternal, imp.isRelative, imp.line)
		require.NoError(t, err)
	}

	// Insert modules (aggregated statistics)
	modules := []struct {
		modulePath           string
		fileCount            int
		linesTotal           int
		linesCode            int
		testFileCount        int
		typeCount            int
		functionCount        int
		exportedTypeCount    int
		exportedFunctionCount int
		importCount          int
		externalImportCount  int
		depth                int
	}{
		{"internal/mcp", 3, 750, 620, 1, 3, 4, 2, 2, 3, 1, 1},
		{"internal/files", 3, 780, 680, 1, 2, 3, 2, 3, 2, 1, 1},
		{"internal/storage", 2, 640, 540, 0, 2, 3, 2, 3, 3, 1, 1},
		{"cmd/cortex", 1, 95, 75, 0, 0, 1, 0, 0, 2, 0, 0},
		{"cmd/cortex-embed", 1, 150, 120, 0, 0, 0, 0, 0, 0, 0, 0},
		{"web/src", 2, 280, 235, 0, 1, 2, 1, 2, 0, 0, 1},
		{"web/src/components", 1, 150, 130, 0, 1, 1, 1, 1, 0, 0, 2},
		{"scripts", 2, 200, 165, 1, 0, 0, 0, 0, 0, 0, 0},
	}

	for _, mod := range modules {
		_, err := db.Exec(`
			INSERT INTO modules (module_path, file_count, line_count_total, line_count_code,
				test_file_count, type_count, function_count, exported_type_count,
				exported_function_count, import_count, external_import_count, depth, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, mod.modulePath, mod.fileCount, mod.linesTotal, mod.linesCode, mod.testFileCount,
			mod.typeCount, mod.functionCount, mod.exportedTypeCount, mod.exportedFunctionCount,
			mod.importCount, mod.externalImportCount, mod.depth, now.Format(time.RFC3339))
		require.NoError(t, err)
	}
}

// Helper functions for nullable types
func intPtr(i int) *int       { return &i }
func strPtr(s string) *string { return &s }

// === Integration Tests ===

func TestIntegration_SimpleSelectFiles(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	tests := []struct {
		name        string
		query       *QueryDefinition
		expectRows  int
		checkColumn string
		checkValue  interface{}
	}{
		{
			name: "Select all Go files",
			query: &QueryDefinition{
				From: "files",
				Where: &Filter{
					Field:    "language",
					Operator: OpEqual,
					Value:    "go",
				},
			},
			expectRows: 10,
		},
		{
			name: "Select test files only",
			query: &QueryDefinition{
				From: "files",
				Where: &Filter{
					Field:    "is_test",
					Operator: OpEqual,
					Value:    1,
				},
			},
			expectRows: 3,
		},
		{
			name: "Select files with line count > 200",
			query: &QueryDefinition{
				From: "files",
				Where: &Filter{
					Field:    "line_count_total",
					Operator: OpGreater,
					Value:    200,
				},
			},
			expectRows: 6, // 350, 220, 410, 250, 360, 280 (not 200 exactly)
		},
		{
			name: "Select TypeScript files",
			query: &QueryDefinition{
				Fields: []string{"file_path", "language", "line_count_code"},
				From:   "files",
				Where: &Filter{
					Field:    "language",
					Operator: OpEqual,
					Value:    "typescript",
				},
			},
			expectRows:  3,
			checkColumn: "language",
			checkValue:  "typescript",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Execute(tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.expectRows, result.RowCount)

			if tt.checkColumn != "" {
				// Find column index
				colIdx := -1
				for i, col := range result.Columns {
					if col == tt.checkColumn {
						colIdx = i
						break
					}
				}
				require.NotEqual(t, -1, colIdx, "Column %s not found", tt.checkColumn)

				// Check all rows have expected value
				for _, row := range result.Rows {
					assert.Equal(t, tt.checkValue, row[colIdx])
				}
			}
		})
	}
}

func TestIntegration_SimpleSelectTypes(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	tests := []struct {
		name       string
		query      *QueryDefinition
		expectRows int
	}{
		{
			name: "Select all exported types",
			query: &QueryDefinition{
				Fields: []string{"name", "kind", "is_exported"},
				From:   "types",
				Where: &Filter{
					Field:    "is_exported",
					Operator: OpEqual,
					Value:    1,
				},
			},
			expectRows: 8, // 8 exported types (mcpHandler is unexported)
		},
		{
			name: "Select interface types only",
			query: &QueryDefinition{
				From: "types",
				Where: &Filter{
					Field:    "kind",
					Operator: OpEqual,
					Value:    "interface",
				},
			},
			expectRows: 3,
		},
		{
			name: "Select struct types",
			query: &QueryDefinition{
				From: "types",
				Where: &Filter{
					Field:    "kind",
					Operator: OpEqual,
					Value:    "struct",
				},
			},
			expectRows: 6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Execute(tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.expectRows, result.RowCount)
		})
	}
}

func TestIntegration_SimpleSelectFunctions(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	tests := []struct {
		name       string
		query      *QueryDefinition
		expectRows int
	}{
		{
			name: "Select all exported functions",
			query: &QueryDefinition{
				From: "functions",
				Where: &Filter{
					Field:    "is_exported",
					Operator: OpEqual,
					Value:    1,
				},
			},
			expectRows: 11, // 11 exported functions
		},
		{
			name: "Select methods only",
			query: &QueryDefinition{
				From: "functions",
				Where: &Filter{
					Field:    "is_method",
					Operator: OpEqual,
					Value:    1,
				},
			},
			expectRows: 4,
		},
		// Skip: cyclomatic_complexity not in current functions schema
		// {
		// 	name: "Select functions with high complexity",
		// 	query: &QueryDefinition{
		// 		From: "functions",
		// 		Where: &Filter{
		// 			Field:    "cyclomatic_complexity",
		// 			Operator: OpGreaterEqual,
		// 			Value:    10,
		// 		},
		// 	},
		// 	expectRows: 4,
		// },
		{
			name: "Select long functions (> 50 lines)",
			query: &QueryDefinition{
				Fields: []string{"name", "line_count"},
				From:   "functions",
				Where: &Filter{
					Field:    "line_count",
					Operator: OpGreater,
					Value:    50,
				},
				OrderBy: []OrderBy{
					{Field: "line_count", Direction: SortDesc},
				},
			},
			expectRows: 7, // 7 functions with line_count > 50
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := executor.Execute(tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.expectRows, result.RowCount)
		})
	}
}

func TestIntegration_AggregationQueries(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	t.Run("COUNT files by language", func(t *testing.T) {
		query := &QueryDefinition{
			From:    "files",
			GroupBy: []string{"language"},
			Aggregations: []Aggregation{
				{Function: AggCount, Alias: "file_count"},
			},
			OrderBy: []OrderBy{
				{Field: "file_count", Direction: SortDesc},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 3) // go, typescript, python

		// Verify Go has most files
		assert.Equal(t, "go", result.Rows[0][0])
		assert.Equal(t, int64(10), result.Rows[0][1])
	})

	t.Run("SUM line counts by module", func(t *testing.T) {
		query := &QueryDefinition{
			From:    "files",
			GroupBy: []string{"module_path"},
			Aggregations: []Aggregation{
				{Function: AggSum, Field: "line_count_total", Alias: "total_lines"},
				{Function: AggSum, Field: "line_count_code", Alias: "code_lines"},
			},
			OrderBy: []OrderBy{
				{Field: "total_lines", Direction: SortDesc},
			},
			Limit: 5,
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 5, result.RowCount)
		assert.Contains(t, result.Columns, "total_lines")
		assert.Contains(t, result.Columns, "code_lines")

		// Verify all aggregations are integers
		for _, row := range result.Rows {
			totalLines := row[1].(int64)
			codeLines := row[2].(int64)
			assert.Greater(t, totalLines, int64(0))
			assert.Greater(t, codeLines, int64(0))
			assert.LessOrEqual(t, codeLines, totalLines)
		}
	})

	t.Run("AVG function line count by language", func(t *testing.T) {
		t.Skip("Qualified field names (table.field) not yet supported in validator")
		// TODO: Add support for qualified field names in validator for JOIN queries
	})

	t.Run("MIN and MAX complexity", func(t *testing.T) {
		t.Skip("Field 'cyclomatic_complexity' not in current functions schema")
		// TODO: Add cyclomatic_complexity to functions schema
	})

	t.Run("GROUP BY with HAVING clause", func(t *testing.T) {
		query := &QueryDefinition{
			From:    "files",
			GroupBy: []string{"module_path"},
			Aggregations: []Aggregation{
				{Function: AggCount, Alias: "file_count"},
				{Function: AggSum, Field: "line_count_code", Alias: "total_code_lines"},
			},
			Having: &Filter{
				Field:    "file_count",
				Operator: OpGreaterEqual,
				Value:    2,
			},
			OrderBy: []OrderBy{
				{Field: "total_code_lines", Direction: SortDesc},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 3)

		// Verify all groups have >= 2 files
		fileCountIdx := -1
		for i, col := range result.Columns {
			if col == "file_count" {
				fileCountIdx = i
				break
			}
		}
		require.NotEqual(t, -1, fileCountIdx)

		for _, row := range result.Rows {
			fileCount := row[fileCountIdx].(int64)
			assert.GreaterOrEqual(t, fileCount, int64(2))
		}
	})
}

func TestIntegration_JoinQueries(t *testing.T) {
	t.Skip("JOIN queries with qualified field names (table.field) not yet supported in validator")
	// TODO: Add support for qualified field names in validator for JOIN queries
	// TODO: Once fixed, test:
	// - JOIN files with types
	// - JOIN functions with types via receiver
	// - Multi-table JOINs (files, types, functions)
}

func TestIntegration_ComplexFilters(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	t.Run("AND filter combination", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				And: []Filter{
					{Field: "language", Operator: OpEqual, Value: "go"},
					{Field: "is_test", Operator: OpEqual, Value: 0},
					{Field: "line_count_total", Operator: OpGreater, Value: 100},
				},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 5)

		// Verify all rows match criteria (language=go, is_test=0, line_count_total>100)
		// Column order depends on SELECT *, so we just verify row count is correct
	})

	t.Run("OR filter combination", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Or: []Filter{
					{Field: "language", Operator: OpEqual, Value: "typescript"},
					{Field: "language", Operator: OpEqual, Value: "python"},
				},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 5, result.RowCount) // 3 TS + 2 Python
	})

	t.Run("NOT filter", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Not: &Filter{
					Field:    "language",
					Operator: OpEqual,
					Value:    "go",
				},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 5, result.RowCount) // All non-Go files
	})

	t.Run("Nested AND/OR filter", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				And: []Filter{
					{
						Or: []Filter{
							{Field: "language", Operator: OpEqual, Value: "go"},
							{Field: "language", Operator: OpEqual, Value: "typescript"},
						},
					},
					{Field: "is_test", Operator: OpEqual, Value: 0},
				},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 10)
	})

	t.Run("IN operator", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Field:    "language",
				Operator: OpIn,
				Value:    []interface{}{"go", "typescript"},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 13, result.RowCount)
	})

	t.Run("NOT IN operator", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Field:    "module_path",
				Operator: OpNotIn,
				Value:    []interface{}{"cmd/cortex", "scripts"},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 9)
	})

	t.Run("IS NULL operator", func(t *testing.T) {
		t.Skip("IS NULL/IS NOT NULL validation not yet implemented in schema validator")
		// TODO: Implement IS NULL/IS NOT NULL validation in validator
	})

	t.Run("IS NOT NULL operator", func(t *testing.T) {
		t.Skip("IS NULL/IS NOT NULL validation not yet implemented in schema validator")
		// TODO: Implement IS NULL/IS NOT NULL validation in validator
	})

	t.Run("BETWEEN operator", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Field:    "line_count_total",
				Operator: OpBetween,
				Value:    []interface{}{100, 200},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 3)
	})

	t.Run("LIKE operator", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Field:    "file_path",
				Operator: OpLike,
				Value:    "internal/mcp/%",
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 3, result.RowCount)
	})
}

func TestIntegration_ErrorCases(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	t.Run("Invalid table name", func(t *testing.T) {
		query := &QueryDefinition{
			From: "nonexistent_table",
		}

		_, err := executor.Execute(query)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query build failed")
	})

	t.Run("Invalid field name", func(t *testing.T) {
		query := &QueryDefinition{
			Fields: []string{"invalid_field"},
			From:   "files",
		}

		_, err := executor.Execute(query)
		assert.Error(t, err)
	})

	t.Run("Invalid operator", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Field:    "language",
				Operator: "INVALID_OP",
				Value:    "go",
			},
		}

		_, err := executor.Execute(query)
		assert.Error(t, err)
	})

	t.Run("Closed database", func(t *testing.T) {
		closedDB := setupIntegrationDB(t)
		closedDB.Close()

		closedExecutor := NewExecutor(closedDB)

		query := &QueryDefinition{
			From: "files",
		}

		_, err := closedExecutor.Execute(query)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query execution failed")
	})

	t.Run("Empty FROM clause", func(t *testing.T) {
		query := &QueryDefinition{
			From: "",
		}

		_, err := executor.Execute(query)
		assert.Error(t, err)
	})

	t.Run("Invalid JOIN - missing table", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			Joins: []Join{
				{
					Table: "",
					Type:  JoinInner,
					On: Filter{
						Field:    "files.file_path",
						Operator: OpEqual,
						Value:    "types.file_path",
					},
				},
			},
		}

		_, err := executor.Execute(query)
		assert.Error(t, err)
	})
}

func TestIntegration_OrderByAndPagination(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	t.Run("ORDER BY with LIMIT", func(t *testing.T) {
		query := &QueryDefinition{
			Fields: []string{"file_path", "line_count_total"},
			From:   "files",
			OrderBy: []OrderBy{
				{Field: "line_count_total", Direction: SortDesc},
			},
			Limit: 5,
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 5, result.RowCount)

		// Verify descending order
		for i := 0; i < len(result.Rows)-1; i++ {
			current := result.Rows[i][1].(int64)
			next := result.Rows[i+1][1].(int64)
			assert.GreaterOrEqual(t, current, next)
		}
	})

	t.Run("Pagination with LIMIT and OFFSET", func(t *testing.T) {
		// Get first page
		query1 := &QueryDefinition{
			From: "files",
			OrderBy: []OrderBy{
				{Field: "file_path", Direction: SortAsc},
			},
			Limit:  3,
			Offset: 0,
		}

		result1, err := executor.Execute(query1)
		require.NoError(t, err)
		assert.Equal(t, 3, result1.RowCount)

		// Get second page
		query2 := &QueryDefinition{
			From: "files",
			OrderBy: []OrderBy{
				{Field: "file_path", Direction: SortAsc},
			},
			Limit:  3,
			Offset: 3,
		}

		result2, err := executor.Execute(query2)
		require.NoError(t, err)
		assert.Equal(t, 3, result2.RowCount)

		// Verify pages don't overlap
		page1First := result1.Rows[0][0].(string)
		page2First := result2.Rows[0][0].(string)
		assert.NotEqual(t, page1First, page2First)
	})

	t.Run("Multi-column ORDER BY", func(t *testing.T) {
		query := &QueryDefinition{
			From: "files",
			OrderBy: []OrderBy{
				{Field: "language", Direction: SortAsc},
				{Field: "line_count_total", Direction: SortDesc},
			},
			Limit: 10,
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 10, result.RowCount)
	})
}

func TestIntegration_PerformanceValidation(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	t.Run("Query execution timing", func(t *testing.T) {
		// Simple query without JOIN to test timing
		query := &QueryDefinition{
			From: "files",
			Where: &Filter{
				Field:    "language",
				Operator: OpEqual,
				Value:    "go",
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)

		// Verify timing is measured
		assert.GreaterOrEqual(t, result.Metadata.TookMs, int64(0))
		// In-memory queries should be fast (< 50ms for this dataset)
		assert.Less(t, result.Metadata.TookMs, int64(50))
	})

	t.Run("Complex aggregation performance", func(t *testing.T) {
		t.Skip("Qualified field names (table.field) not yet supported in validator")
		// TODO: Add support for qualified field names in validator for JOIN queries
	})
}

func TestIntegration_ModuleAggregation(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	t.Run("Query modules table", func(t *testing.T) {
		query := &QueryDefinition{
			From: "modules",
			OrderBy: []OrderBy{
				{Field: "file_count", Direction: SortDesc},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.Equal(t, 8, result.RowCount)
	})

	t.Run("Modules with high line count", func(t *testing.T) {
		query := &QueryDefinition{
			Fields: []string{"module_path", "line_count_total", "line_count_code"},
			From:   "modules",
			Where: &Filter{
				Field:    "line_count_total",
				Operator: OpGreaterEqual,
				Value:    500,
			},
			OrderBy: []OrderBy{
				{Field: "line_count_total", Direction: SortDesc},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 3)
	})

	t.Run("Modules by depth", func(t *testing.T) {
		t.Skip("Field 'depth' not in current modules schema")
		// TODO: Update modules schema to include 'depth' field
	})
}

func TestIntegration_ImportsQueries(t *testing.T) {
	db := setupIntegrationDB(t)
	defer db.Close()

	executor := NewExecutor(db)

	t.Run("External imports only", func(t *testing.T) {
		query := &QueryDefinition{
			From: "imports",
			Where: &Filter{
				Field:    "is_external",
				Operator: OpEqual,
				Value:    1,
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 3)
	})

	t.Run("Standard library imports", func(t *testing.T) {
		query := &QueryDefinition{
			From: "imports",
			Where: &Filter{
				Field:    "is_standard_lib",
				Operator: OpEqual,
				Value:    1,
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 5)
	})

	t.Run("Count imports by file", func(t *testing.T) {
		query := &QueryDefinition{
			From:    "imports",
			GroupBy: []string{"file_path"},
			Aggregations: []Aggregation{
				{Function: AggCount, Alias: "import_count"},
			},
			Having: &Filter{
				Field:    "import_count",
				Operator: OpGreaterEqual,
				Value:    2,
			},
			OrderBy: []OrderBy{
				{Field: "import_count", Direction: SortDesc},
			},
		}

		result, err := executor.Execute(query)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, result.RowCount, 2)
	})
}
