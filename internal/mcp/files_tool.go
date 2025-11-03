package mcp

import (
	"database/sql"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/mvp-joe/project-cortex/internal/files"
)

// AddCortexFilesTool registers the cortex_files tool with an MCP server.
// This function follows the composable tool registration pattern used by other Cortex tools.
// The tool provides SQL-like querying capabilities over the files database for quantitative
// code analysis questions.
func AddCortexFilesTool(s *server.MCPServer, db *sql.DB) {
	tool := mcp.NewTool(
		"cortex_files",
		mcp.WithDescription(`Query code statistics and metadata using SQL-like JSON queries. Use for quantitative questions about project structure, module sizes, test coverage, and aggregations.

Supports:
- SELECT operations with field filtering
- WHERE clauses with comparison operators (=, !=, >, >=, <, <=, LIKE, IN, BETWEEN)
- JOIN operations across tables (files, types, functions, imports, modules, chunks)
- GROUP BY with aggregations (COUNT, SUM, AVG, MIN, MAX)
- ORDER BY with ASC/DESC sorting
- LIMIT and OFFSET for pagination

Example queries:
- Count files by language: {"from": "files", "aggregations": [{"function": "COUNT", "alias": "count"}], "groupBy": ["language"]}
- Find large files: {"from": "files", "fields": ["file_path", "line_count_total"], "where": {"field": "line_count_total", "operator": ">", "value": 500}}
- Module statistics: {"from": "modules", "fields": ["module_path", "file_count", "line_count_total"], "orderBy": [{"field": "file_count", "direction": "DESC"}]}`),
		mcp.WithString("operation",
			mcp.Required(),
			mcp.Description("Operation type: 'query' for custom queries")),
		mcp.WithObject("query",
			mcp.Required(),
			mcp.Description(`Query definition with structure:
{
  "fields": ["field1", "field2"],          // SELECT fields (optional, defaults to all)
  "from": "table_name",                    // FROM clause (required)
  "where": {...},                          // WHERE clause (optional)
  "joins": [...],                          // JOIN clauses (optional)
  "groupBy": ["field"],                    // GROUP BY fields (optional)
  "having": {...},                         // HAVING clause (optional)
  "orderBy": [{"field": "x", "direction": "ASC"}], // ORDER BY (optional)
  "limit": 100,                            // LIMIT (optional)
  "offset": 0,                             // OFFSET (optional)
  "aggregations": [{"function": "COUNT", "field": "x", "alias": "count"}] // Aggregations (optional)
}

Available tables: files, types, functions, imports, modules, chunks`)),
		mcp.WithReadOnlyHintAnnotation(true),
		mcp.WithDestructiveHintAnnotation(false),
	)

	// Create handler using the files package handler factory
	handler := files.CreateFilesToolHandler(db)

	// Register tool with server
	s.AddTool(tool, handler)
}
