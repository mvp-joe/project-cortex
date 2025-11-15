---
status: archived
started_at: 2025-10-29T00:00:00Z
completed_at: 2025-11-02T00:00:00Z
dependencies: [sqlite-cache-storage]
---

# cortex_files Specification

## Purpose

The `cortex_files` tool provides fast, queryable access to code statistics and metadata via direct SQL queries to the unified SQLite cache. It enables LLMs to answer quantitative questions about project structure ("Which modules have the most code?", "What's the test coverage ratio?") without resorting to slow bash command chains or manual file analysis.

## Core Concept

**Input**: Codebase files (parsed during indexing)

**Process**: Extract metadata (lines, types, functions, imports) → Store in 5-table SQLite schema → Load into memory → Accept type-safe JSON queries

**Output**: SQL query results with file stats, aggregations, and cross-table joins

## Technology Stack

- **Language**: Go 1.25+
- **Database**: SQLite 3 (unified cache, via `github.com/mattn/go-sqlite3`)
- **Query Builder**: Squirrel (`github.com/Masterminds/squirrel`)
- **Storage**: Unified SQLite cache at `~/.cortex/cache/{key}/branches/{branch}.db`
- **Parser**: Tree-sitter (reuse existing parsing from indexer)

## Problem Statement

### Current LLM Workflow (Slow)

LLMs answering project sizing questions today:

1. Try `find . -name "*.go" | wc -l` (count files)
2. Try `find . -name "*.go" -exec wc -l {} +` (count lines, parse output)
3. Try bash loops with `grep`, `awk`, `sed` (complex parsing)
4. Sometimes even write Python scripts for aggregation

**Time**: 20-30 seconds of trial-and-error
**Accuracy**: Often wrong (parsing errors, missing edge cases)

### With cortex_files (Fast)

```json
{
  "from": "files",
  "aggregations": [
    {"function": "COUNT", "alias": "file_count"},
    {"function": "SUM", "field": "lines_total", "alias": "total_lines"}
  ],
  "groupBy": ["language"],
  "orderBy": [{"field": "total_lines", "direction": "DESC"}]
}
```

**Time**: <5ms
**Accuracy**: 100% (parsed from AST)

## Database Schema

### Unified SQLite Cache (10 Tables)

**Important:** The `cortex_files` tool queries the **unified SQLite cache** defined in the [SQLite Cache Storage Specification](2025-10-30_sqlite-cache-storage.md). It does NOT maintain a separate database.

**Cache location:** `~/.cortex/cache/{cache-key}/branches/{branch}.db`

The unified schema contains 10 tables:

1. **files** - File metadata, line counts, hashes (used by cortex_files)
2. **types** - Interfaces, structs, classes (used by cortex_files)
3. **type_fields** - Struct fields, interface methods (extended graph data)
4. **functions** - Standalone and method functions (used by cortex_files)
5. **function_parameters** - Parameters and return values (extended graph data)
6. **type_relationships** - Implements, embeds edges (used by cortex_graph)
7. **function_calls** - Call graph edges (used by cortex_graph)
8. **imports** - Import declarations (used by cortex_files)
9. **chunks** - Semantic search chunks with embeddings (used by cortex_search/cortex_exact)
10. **cache_metadata** - Cache configuration and stats

**Tables used by cortex_files:**

The `cortex_files` tool primarily queries these tables:
- `files` - Base file statistics (includes `module_path` column for aggregation)
- `types` - Type definitions and counts
- `functions` - Function/method metadata
- `imports` - Import/dependency relationships

**Full schema:** See [SQLite Cache Storage Specification § Data Model](2025-10-30_sqlite-cache-storage.md#data-model) for complete table definitions and foreign key relationships.

### Key Differences from Original cortex_files Design

**What changed:**
- ❌ **Old:** Separate 5-table database in `.cortex/stats.ndjson` (NDJSON format)
- ✅ **New:** Queries unified 10-table SQLite cache (shared with cortex_search, cortex_exact, cortex_graph)
- ❌ **Old:** In-memory SQLite loaded from NDJSON on MCP server startup
- ✅ **New:** Queries on-disk SQLite cache directly (no loading, instant queries)
- ❌ **Old:** Independent data extraction and storage
- ✅ **New:** Data populated by indexer during normal indexing (no separate extraction)

**Benefits of unified schema:**
- Single source of truth for all cache data
- Cross-tool queries possible (JOIN chunks with files, types with functions)
- Branch isolation (each branch has separate .db file)
- Automatic updates (daemon watches source files, re-indexes on change)
- Transactional consistency (SQLite ACID guarantees)

### Schema Compatibility

The original 5-table design is a **subset** of the unified 10-table schema. All original queries work unchanged:

**Original query (still works):**
```sql
SELECT module_path, SUM(lines_total) as total_lines
FROM files
GROUP BY module_path
ORDER BY total_lines DESC;
```

**New capability (cross-index JOIN):**
```sql
SELECT f.file_path, f.lines_total, COUNT(c.chunk_id) as chunk_count
FROM files f
LEFT JOIN chunks c ON f.file_path = c.file_path
GROUP BY f.file_path
ORDER BY chunk_count DESC;
```

## JSON Query Interface

### Simplified Schema (SQL-like)

Based on DesignForge pattern, simplified for SQL familiarity:

```typescript
/**
 * Comparison operators (SQL syntax)
 */
export type ComparisonOperator =
  | "="           // Equals
  | "!="          // Not equals
  | ">"           // Greater than
  | ">="          // Greater than or equal
  | "<"           // Less than
  | "<="          // Less than or equal
  | "LIKE"        // Pattern matching
  | "NOT LIKE"    // Negated pattern
  | "IN"          // Set membership
  | "NOT IN"      // Not in set
  | "IS NULL"     // NULL check
  | "IS NOT NULL" // NOT NULL check
  | "BETWEEN";    // Range (value must be [min, max])

/**
 * Field filter (e.g., lines_total > 500)
 */
export interface FieldFilter {
  field: string;              // Column name
  operator: ComparisonOperator;
  value?: any;                // Not needed for IS NULL/IS NOT NULL
}

/**
 * Logical operators
 */
export interface AndFilter {
  and: Filter[];
}

export interface OrFilter {
  or: Filter[];
}

export interface NotFilter {
  not: Filter;
}

/**
 * Recursive filter type
 */
export type Filter =
  | FieldFilter
  | AndFilter
  | OrFilter
  | NotFilter;

/**
 * JOIN definition
 */
export interface Join {
  table: string;              // Table to join
  type: "INNER" | "LEFT" | "RIGHT" | "FULL";
  on: Filter;                 // Join condition
}

/**
 * Aggregation function
 */
export interface Aggregation {
  function: "COUNT" | "SUM" | "AVG" | "MIN" | "MAX";
  field?: string;             // Not needed for COUNT(*)
  alias: string;              // Result column name
  distinct?: boolean;         // DISTINCT flag
}

/**
 * ORDER BY clause
 */
export interface OrderBy {
  field: string;
  direction: "ASC" | "DESC";
}

/**
 * Complete query definition
 */
export interface QueryDefinition {
  fields?: string[];          // SELECT columns (default: all)
  from: string;               // Table name (required)
  where?: Filter;             // WHERE clause
  joins?: Join[];             // JOIN clauses
  groupBy?: string[];         // GROUP BY columns
  having?: Filter;            // HAVING clause
  orderBy?: OrderBy[];        // ORDER BY clauses
  limit?: number;             // LIMIT (1-1000)
  offset?: number;            // OFFSET
  aggregations?: Aggregation[]; // Aggregations
}
```

### Example Queries

**Simple SELECT:**
```json
{
  "from": "files",
  "where": {
    "field": "language",
    "operator": "=",
    "value": "go"
  },
  "orderBy": [{"field": "lines_total", "direction": "DESC"}],
  "limit": 10
}
```

**Aggregation with GROUP BY:**
```json
{
  "from": "files",
  "aggregations": [
    {"function": "COUNT", "alias": "file_count"},
    {"function": "SUM", "field": "lines_total", "alias": "total_lines"}
  ],
  "groupBy": ["language"],
  "orderBy": [{"field": "total_lines", "direction": "DESC"}]
}
```

**Complex AND/OR:**
```json
{
  "from": "files",
  "where": {
    "and": [
      {"field": "is_test", "operator": "=", "value": false},
      {
        "or": [
          {"field": "lines_total", "operator": ">", "value": 500},
          {"field": "module_path", "operator": "LIKE", "value": "internal/%"}
        ]
      }
    ]
  }
}
```

**JOIN example:**
```json
{
  "fields": ["files.file_path", "types.name", "types.kind"],
  "from": "files",
  "joins": [
    {
      "table": "types",
      "type": "INNER",
      "on": {
        "field": "files.file_path",
        "operator": "=",
        "value": "types.file_path"
      }
    }
  ],
  "where": {"field": "types.is_exported", "operator": "=", "value": true},
  "limit": 50
}
```

## Query Translation with Squirrel

### Why Squirrel

**Benefits:**
- Type-safe SQL building (no string concatenation)
- Parameterized queries (SQL injection prevention built-in)
- Supports all features we need (WHERE, JOIN, GROUP BY, HAVING, subqueries)
- Works with any SQL driver (including SQLite)
- Mature library (2.5K+ stars, well-maintained)

**Complexity reduction:**
- Without Squirrel: Build custom JSON → SQL translator (1-1.5 weeks)
- With Squirrel: Parse JSON → Use Squirrel API (2-3 days)

### Translation Example

```go
import (
    sq "github.com/Masterminds/squirrel"
)

func buildQuery(qd *QueryDefinition) (string, []interface{}, error) {
    // Start with SELECT
    fields := qd.Fields
    if len(fields) == 0 {
        fields = []string{"*"}
    }
    builder := sq.Select(fields...).From(qd.From)

    // Add WHERE clause
    if qd.Where != nil {
        whereClause, err := buildFilter(qd.Where)
        if err != nil {
            return "", nil, err
        }
        builder = builder.Where(whereClause)
    }

    // Add JOINs
    for _, join := range qd.Joins {
        onClause, err := buildFilter(join.On)
        if err != nil {
            return "", nil, err
        }

        joinExpr := fmt.Sprintf("%s ON (?)", join.Table)
        switch join.Type {
        case "INNER":
            builder = builder.InnerJoin(joinExpr, onClause)
        case "LEFT":
            builder = builder.LeftJoin(joinExpr, onClause)
        case "RIGHT":
            builder = builder.RightJoin(joinExpr, onClause)
        }
    }

    // Add aggregations (if any)
    if len(qd.Aggregations) > 0 {
        // Replace fields with aggregations
        aggFields := make([]string, len(qd.Aggregations))
        for i, agg := range qd.Aggregations {
            aggFields[i] = buildAggregation(agg)
        }
        builder = sq.Select(append(qd.GroupBy, aggFields...)...).From(qd.From)
    }

    // Add GROUP BY
    if len(qd.GroupBy) > 0 {
        builder = builder.GroupBy(qd.GroupBy...)
    }

    // Add HAVING
    if qd.Having != nil {
        havingClause, err := buildFilter(qd.Having)
        if err != nil {
            return "", nil, err
        }
        builder = builder.Having(havingClause)
    }

    // Add ORDER BY
    for _, order := range qd.OrderBy {
        builder = builder.OrderBy(order.Field + " " + order.Direction)
    }

    // Add LIMIT/OFFSET
    if qd.Limit > 0 {
        builder = builder.Limit(uint64(qd.Limit))
    }
    if qd.Offset > 0 {
        builder = builder.Offset(uint64(qd.Offset))
    }

    // Generate SQL with placeholders
    return builder.PlaceholderFormat(sq.Question).ToSql()
}

func buildFilter(filter Filter) (sq.Sqlizer, error) {
    if ff, ok := filter.(FieldFilter); ok {
        switch ff.Operator {
        case "=":
            return sq.Eq{ff.Field: ff.Value}, nil
        case "!=":
            return sq.NotEq{ff.Field: ff.Value}, nil
        case ">":
            return sq.Gt{ff.Field: ff.Value}, nil
        case ">=":
            return sq.GtOrEq{ff.Field: ff.Value}, nil
        case "<":
            return sq.Lt{ff.Field: ff.Value}, nil
        case "<=":
            return sq.LtOrEq{ff.Field: ff.Value}, nil
        case "LIKE":
            return sq.Like{ff.Field: ff.Value}, nil
        case "NOT LIKE":
            return sq.NotLike{ff.Field: ff.Value}, nil
        case "IN":
            return sq.Eq{ff.Field: ff.Value}, nil  // Squirrel auto-detects IN
        case "NOT IN":
            return sq.NotEq{ff.Field: ff.Value}, nil
        case "IS NULL":
            return sq.Eq{ff.Field: nil}, nil
        case "IS NOT NULL":
            return sq.NotEq{ff.Field: nil}, nil
        case "BETWEEN":
            vals, ok := ff.Value.([]interface{})
            if !ok || len(vals) != 2 {
                return nil, fmt.Errorf("BETWEEN requires array of 2 values")
            }
            return sq.And{
                sq.GtOrEq{ff.Field: vals[0]},
                sq.LtOrEq{ff.Field: vals[1]},
            }, nil
        }
    }

    if af, ok := filter.(AndFilter); ok {
        var ands []sq.Sqlizer
        for _, f := range af.And {
            clause, err := buildFilter(f)
            if err != nil {
                return nil, err
            }
            ands = append(ands, clause)
        }
        return sq.And(ands), nil
    }

    if of, ok := filter.(OrFilter); ok {
        var ors []sq.Sqlizer
        for _, f := range of.Or {
            clause, err := buildFilter(f)
            if err != nil {
                return nil, err
            }
            ors = append(ors, clause)
        }
        return sq.Or(ors), nil
    }

    if nf, ok := filter.(NotFilter); ok {
        clause, err := buildFilter(nf.Not)
        if err != nil {
            return nil, err
        }
        return sq.Expr("NOT (?)", clause), nil
    }

    return nil, fmt.Errorf("unknown filter type")
}

func buildAggregation(agg Aggregation) string {
    var expr string
    switch agg.Function {
    case "COUNT":
        if agg.Field == "" {
            expr = "COUNT(*)"
        } else if agg.Distinct {
            expr = fmt.Sprintf("COUNT(DISTINCT %s)", agg.Field)
        } else {
            expr = fmt.Sprintf("COUNT(%s)", agg.Field)
        }
    case "SUM":
        expr = fmt.Sprintf("SUM(%s)", agg.Field)
    case "AVG":
        expr = fmt.Sprintf("AVG(%s)", agg.Field)
    case "MIN":
        expr = fmt.Sprintf("MIN(%s)", agg.Field)
    case "MAX":
        expr = fmt.Sprintf("MAX(%s)", agg.Field)
    }
    return fmt.Sprintf("%s AS %s", expr, agg.Alias)
}
```

### Validation

**Before translation, validate:**

```go
var validTables = map[string]bool{
    "files": true,
    "types": true,
    "functions": true,
    "imports": true,
}

var tableSchema = map[string]map[string]bool{
    "files": {
        "file_path": true, "language": true, "is_test": true,
        "module_path": true, "lines_total": true, "lines_code": true,
        "lines_comment": true, "lines_blank": true, "size_bytes": true,
        "last_modified": true, "type_count": true, "function_count": true,
        "import_count": true,
    },
    "types": {
        "type_id": true, "name": true, "kind": true, "file_path": true,
        "module_path": true, "start_line": true, "end_line": true,
        "is_exported": true, "field_count": true, "method_count": true,
    },
    // ... other tables
}

func validateQuery(qd *QueryDefinition) error {
    // Validate table exists
    if !validTables[qd.From] {
        return fmt.Errorf("invalid table: %s", qd.From)
    }

    // Validate fields exist in schema
    schema := tableSchema[qd.From]
    for _, field := range qd.Fields {
        // Handle qualified fields (e.g., "files.file_path")
        parts := strings.Split(field, ".")
        fieldName := parts[len(parts)-1]

        if fieldName != "*" && !schema[fieldName] {
            return fmt.Errorf("invalid field '%s' for table '%s'", fieldName, qd.From)
        }
    }

    // Validate WHERE filters
    if qd.Where != nil {
        if err := validateFilter(qd.Where, qd.From); err != nil {
            return err
        }
    }

    // Validate JOINs
    for _, join := range qd.Joins {
        if !validTables[join.Table] {
            return fmt.Errorf("invalid join table: %s", join.Table)
        }
        if err := validateFilter(join.On, qd.From); err != nil {
            return err
        }
    }

    // Validate GROUP BY
    for _, field := range qd.GroupBy {
        if !schema[field] {
            return fmt.Errorf("invalid group by field: %s", field)
        }
    }

    // Validate ORDER BY
    for _, order := range qd.OrderBy {
        if !schema[order.Field] && !isAggregationAlias(order.Field, qd.Aggregations) {
            return fmt.Errorf("invalid order by field: %s", order.Field)
        }
    }

    return nil
}

func validateFilter(filter Filter, tableName string) error {
    schema := tableSchema[tableName]

    if ff, ok := filter.(FieldFilter); ok {
        // Extract field name (handle qualified fields)
        parts := strings.Split(ff.Field, ".")
        fieldName := parts[len(parts)-1]

        if !schema[fieldName] {
            return fmt.Errorf("invalid field: %s", ff.Field)
        }
    }

    if af, ok := filter.(AndFilter); ok {
        for _, f := range af.And {
            if err := validateFilter(f, tableName); err != nil {
                return err
            }
        }
    }

    if of, ok := filter.(OrFilter); ok {
        for _, f := range of.Or {
            if err := validateFilter(f, tableName); err != nil {
                return err
            }
        }
    }

    if nf, ok := filter.(NotFilter); ok {
        return validateFilter(nf.Not, tableName)
    }

    return nil
}
```

## Data Extraction

### One-Pass Extraction Strategy

**Integrate with existing indexer parsing** (don't re-parse):

```go
// In indexer/parser/go_parser.go (existing file)

type FileStats struct {
    // Existing chunk data
    Symbols     *ContextChunk
    Definitions []*ContextChunk
    Data        []*ContextChunk

    // NEW: Statistics for cortex_files
    Stats FileMetadata
}

type FileMetadata struct {
    FilePath    string
    Language    string
    IsTest      bool
    ModulePath  string
    LinesTotal  int
    LinesCode   int
    LinesComment int
    LinesBlank  int
    SizeBytes   int
    LastModified time.Time

    Types     []TypeMetadata
    Functions []FunctionMetadata
    Imports   []ImportMetadata
}

type TypeMetadata struct {
    Name         string
    Kind         string  // struct, interface, class, enum
    StartLine    int
    EndLine      int
    IsExported   bool
    FieldCount   int
    MethodCount  int
}

type FunctionMetadata struct {
    Name         string
    StartLine    int
    EndLine      int
    IsExported   bool
    IsMethod     bool
    ReceiverType string
    ParamCount   int
    ReturnCount  int
    Lines        int
}

type ImportMetadata struct {
    ImportedModule string
    IsStandardLib  bool
    IsExternal     bool
    IsRelative     bool
}
```

**During tree-sitter traversal:**

```go
func (p *GoParser) ParseFile(ctx context.Context, filePath string) (*FileStats, error) {
    // Existing parsing for chunks
    chunks := p.extractChunks(tree)

    // NEW: Extract statistics
    stats := &FileMetadata{
        FilePath:   filePath,
        Language:   "go",
        ModulePath: p.detectModulePath(filePath),
        // ... line counts (from tree traversal)
    }

    // Extract types
    p.walkTree(tree.RootNode(), func(node *sitter.Node) {
        if node.Type() == "type_declaration" {
            stats.Types = append(stats.Types, p.extractTypeMetadata(node))
        }
        if node.Type() == "function_declaration" {
            stats.Functions = append(stats.Functions, p.extractFunctionMetadata(node))
        }
        if node.Type() == "import_declaration" {
            stats.Imports = append(stats.Imports, p.extractImportMetadata(node))
        }
    })

    return &FileStats{
        Symbols:     chunks.Symbols,
        Definitions: chunks.Definitions,
        Data:        chunks.Data,
        Stats:       stats,  // NEW
    }, nil
}
```

**Cost analysis:**
- Existing parsing: ~50ms per file (already paid)
- Additional stats extraction: ~5-10ms per file (minimal overhead)
- Total overhead: ~10-20% increase in indexing time

**Worth it:** Eliminates need for separate stats extraction pass.

### Module Path Detection

```go
func detectModulePath(filePath string) string {
    // For Go: Read go.mod, parse module path from package
    // For TypeScript: Read package.json, use directory structure
    // For Python: Detect __init__.py, use directory path

    // Example for Go:
    parts := strings.Split(filePath, "/")
    // internal/mcp/server.go → internal/mcp
    if len(parts) > 1 {
        return strings.Join(parts[:len(parts)-1], "/")
    }
    return "."
}
```

### Test Classification

```go
func isTestFile(filePath string, language string) bool {
    switch language {
    case "go":
        return strings.HasSuffix(filePath, "_test.go")
    case "typescript", "javascript":
        return strings.Contains(filePath, ".test.") ||
               strings.Contains(filePath, ".spec.") ||
               strings.Contains(filePath, "__tests__/")
    case "python":
        return strings.HasPrefix(filepath.Base(filePath), "test_") ||
               strings.HasSuffix(filePath, "_test.py")
    case "rust":
        // Rust uses #[test] annotations, harder to detect from filename
        return strings.Contains(filePath, "/tests/")
    }
    return false
}
```

## Data Persistence

### Unified SQLite Cache

**Important:** The `cortex_files` tool does NOT maintain separate storage. All data is populated by the indexer and stored in the unified SQLite cache.

**Cache location:** `~/.cortex/cache/{cache-key}/branches/{branch}.db`

**Data flow:**

```
Indexer Run
    │
    ├─> Parse source files (tree-sitter)
    │
    ├─> Extract chunks (symbols, definitions, data)
    │
    ├─> Extract graph data (types, functions, relationships)
    │
    ├─> Extract file stats (line counts, module paths)
    │
    └─> Write to unified SQLite cache (single transaction)
            │
            ├─> files table (includes module_path for aggregation)
            ├─> types table
            ├─> functions table
            ├─> imports table
            └─> chunks table
```

**No separate NDJSON files:** The original design used `.cortex/stats.ndjson`. This is replaced by direct SQLite storage.

**MCP Server Access:**

In daemon mode, the MCP server loads the unified cache and `cortex_files` queries it directly:

```go
func (d *Daemon) handleCortexFilesQuery(query *FilesQuery) (*FilesResult, error) {
    // Query unified cache (already loaded)
    rows, err := d.cacheDB.Query(query.ToSQL())
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    // Parse results
    return parseFilesResults(rows)
}
```

In embedded mode, the MCP server loads the cache on startup:

```go
func (s *Server) startup() error {
    // Load .cortex/settings.local.json to find cache location
    settings := loadSettings()

    // Open unified cache
    s.cacheDB, err = sql.Open("sqlite3", settings.CachePath)
    if err != nil {
        return err
    }

    // cortex_files queries cacheDB directly (no separate loading)
    return nil
}
```

## Incremental Updates

### File Change Detection

**Integrated with auto-daemon file watcher:**

In daemon mode, the auto-daemon watches project source files and automatically re-indexes on changes. The `cortex_files` data is updated as part of the normal indexing flow:

```go
// In auto-daemon (file watcher)

func (d *Daemon) handleFileChange(path string) {
    // 1. Re-index changed file (extracts chunks + graph data + file stats)
    chunks, graphData, fileStats := d.indexer.ProcessFile(path)

    // 2. Update unified SQLite cache (single transaction updates all tables)
    tx, _ := d.cacheDB.Begin()
    d.updateFileInCache(tx, path, chunks, graphData, fileStats)
    tx.Commit()

    // 3. Rebuild in-memory indexes (chromem-go, bleve)
    d.rebuildIndexes()

    log.Printf("File %s updated in cache", path)
}
```

**No separate stats update:** File statistics are extracted during normal indexing and updated atomically with chunks and graph data.

### Database Update Strategy

**For file changes:**

```go
func (db *StatsDB) UpdateFile(stats *FileMetadata) error {
    tx, _ := db.Begin()
    defer tx.Rollback()

    // Delete existing entries
    tx.Exec("DELETE FROM types WHERE file_path = ?", stats.FilePath)
    tx.Exec("DELETE FROM functions WHERE file_path = ?", stats.FilePath)
    tx.Exec("DELETE FROM imports WHERE file_path = ?", stats.FilePath)

    // Upsert file
    tx.Exec(`
        INSERT OR REPLACE INTO files (file_path, language, is_test, ...)
        VALUES (?, ?, ?, ...)
    `, stats.FilePath, stats.Language, stats.IsTest, ...)

    // Insert new entries
    for _, typ := range stats.Types {
        insertType(tx, stats.FilePath, stats.ModulePath, &typ)
    }
    for _, fn := range stats.Functions {
        insertFunction(tx, stats.FilePath, stats.ModulePath, &fn)
    }
    for _, imp := range stats.Imports {
        insertImport(tx, stats.FilePath, &imp)
    }

    return tx.Commit()
}
```

## Module Statistics via SQL GROUP BY

Module-level statistics are computed on-demand using SQL aggregation queries. Since the `files` table includes `module_path`, simple GROUP BY queries provide instant results without maintaining a separate table.

### Example: All modules with statistics
```sql
SELECT
    module_path,
    COUNT(*) as file_count,
    SUM(line_count_total) as total_lines,
    SUM(line_count_code) as code_lines,
    SUM(CASE WHEN is_test THEN 1 ELSE 0 END) as test_file_count
FROM files
GROUP BY module_path
ORDER BY code_lines DESC
```

### Example: Top 10 largest modules
```sql
SELECT module_path, SUM(line_count_code) as total_code
FROM files
GROUP BY module_path
ORDER BY total_code DESC
LIMIT 10
```

### Example: Specific module stats
```sql
SELECT
    module_path,
    COUNT(*) as file_count,
    AVG(line_count_total) as avg_lines_per_file
FROM files
WHERE module_path = 'internal/storage'
GROUP BY module_path
```

**Performance:** GROUP BY queries on the files table complete in <1ms for typical projects (thousands of files). No pre-aggregation or materialized views needed.

**Benefits:**
- No separate table to maintain
- Always up-to-date (no staleness issues)
- Simpler architecture (less code, fewer bugs)
- Standard SQL patterns that all developers understand

### Periodic Persistence

**In daemon mode, write to disk periodically:**

```go
func (d *Daemon) statsFlushLoop() {
    ticker := time.NewTicker(60 * time.Second)
    defer ticker.Stop()

    changeCount := 0
    for {
        select {
        case <-ticker.C:
            if changeCount > 0 {
                d.flushStatsToDisk()
                changeCount = 0
            }
        case <-d.fileChange:
            changeCount++
            if changeCount >= 100 {
                d.flushStatsToDisk()
                changeCount = 0
            }
        case <-d.shutdown:
            d.flushStatsToDisk()  // Final flush
            return
        }
    }
}

func (d *Daemon) flushStatsToDisk() {
    // Export in-memory SQLite to NDJSON
    rows, _ := d.statsDB.Query("SELECT * FROM files")
    var allStats []*FileMetadata
    for rows.Next() {
        var stat FileMetadata
        // Scan row into stat
        allStats = append(allStats, &stat)
    }
    writeStats(allStats)
}
```

**Write triggers:**
- Every 60 seconds (periodic)
- Every 100 file changes (batched)
- On daemon shutdown (graceful)

## MCP Tool Interface

### Request Schema

```json
{
  "operation": "query",
  "query": {
    "from": "files",
    "where": {
      "field": "language",
      "operator": "=",
      "value": "go"
    },
    "orderBy": [{"field": "lines_total", "direction": "DESC"}],
    "limit": 10
  }
}
```

**Parameters:**

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `operation` | string | ✅ Yes | Operation type: `"query"` or pre-defined |
| `query` | QueryDefinition | ✅ Yes (for "query" operation) | JSON query definition |

**Pre-defined operations** (future enhancement):

```json
// Top files by lines
{"operation": "top_files", "limit": 10, "order_by": "lines"}

// Module stats
{"operation": "module_stats", "module_path": "internal/mcp"}

// Test coverage ratio
{"operation": "test_coverage"}
```

### Response Schema

```json
{
  "columns": ["file_path", "language", "lines_total"],
  "rows": [
    ["internal/mcp/server.go", "go", 245],
    ["internal/mcp/tool.go", "go", 156]
  ],
  "row_count": 2,
  "metadata": {
    "took_ms": 3,
    "query": "SELECT file_path, language, lines_total FROM files WHERE language = ? ORDER BY lines_total DESC LIMIT 10",
    "source": "stats"
  }
}
```

**Response fields:**

```go
type QueryResponse struct {
    Columns  []string        `json:"columns"`    // Column names
    Rows     [][]interface{} `json:"rows"`       // Row data (heterogeneous)
    RowCount int             `json:"row_count"`  // Number of rows returned
    Metadata struct {
        TookMs int    `json:"took_ms"`   // Query execution time
        Query  string `json:"query"`     // Generated SQL (for debugging)
        Source string `json:"source"`    // Always "stats"
    } `json:"metadata"`
}
```

### MCP Tool Registration

```go
func AddCortexFilesTool(s *server.MCPServer, db *StatsDB) {
    tool := mcp.NewTool(
        "cortex_files",
        mcp.WithDescription("Query code statistics and metadata using SQL-like queries. Use for quantitative questions about project structure, module sizes, test coverage, and aggregations."),
        mcp.WithString("operation",
            mcp.Required(),
            mcp.Description("Operation type: 'query' for custom queries")),
        mcp.WithObject("query",
            mcp.Description("Query definition (required for 'query' operation)")),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
    )

    handler := createCortexFilesHandler(db)
    s.AddTool(tool, handler)
}

func createCortexFilesHandler(db *StatsDB) mcp.ToolHandler {
    return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        var req struct {
            Operation string           `json:"operation"`
            Query     *QueryDefinition `json:"query"`
        }

        if err := json.Unmarshal([]byte(request.Params.Arguments), &req); err != nil {
            return mcp.NewToolResultError(fmt.Sprintf("Invalid request: %v", err)), nil
        }

        // Validate query
        if err := validateQuery(req.Query); err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }

        // Build SQL
        sql, args, err := buildQuery(req.Query)
        if err != nil {
            return nil, fmt.Errorf("failed to build query: %w", err)
        }

        // Execute
        start := time.Now()
        rows, err := db.Query(sql, args...)
        if err != nil {
            return nil, fmt.Errorf("query execution failed: %w", err)
        }
        defer rows.Close()

        // Format response
        result := &QueryResponse{
            Metadata: struct {
                TookMs int    `json:"took_ms"`
                Query  string `json:"query"`
                Source string `json:"source"`
            }{
                TookMs: int(time.Since(start).Milliseconds()),
                Query:  sql,
                Source: "stats",
            },
        }

        // Read columns
        result.Columns, _ = rows.Columns()

        // Read rows
        for rows.Next() {
            // Dynamic scanning based on column count
            values := make([]interface{}, len(result.Columns))
            valuePtrs := make([]interface{}, len(result.Columns))
            for i := range values {
                valuePtrs[i] = &values[i]
            }

            rows.Scan(valuePtrs...)
            result.Rows = append(result.Rows, values)
        }
        result.RowCount = len(result.Rows)

        // Return as JSON
        jsonBytes, _ := json.Marshal(result)
        return mcp.NewToolResultText(string(jsonBytes)), nil
    }
}
```

## Performance Characteristics

### Query Performance

**Simple queries (single table, WHERE, ORDER BY):**
- Execution time: <1ms
- Example: "Find all Go files with >500 lines"

**Aggregation queries (GROUP BY, COUNT/SUM/AVG):**
- Execution time: <5ms
- Example: "Total lines by language"

**JOIN queries (2-3 tables):**
- Execution time: <10ms
- Example: "Files with exported types"

**Complex queries (multiple JOINs, subqueries):**
- Execution time: <25ms
- Example: "Modules with most external dependencies"

**Factors:**
- In-memory SQLite (no disk I/O)
- Proper indexes on common filter columns
- Small dataset (10K files = ~50K total rows across all tables)

### Memory Footprint

**Per file:**
- Files table: ~200 bytes/row
- Types table: ~100 bytes/type × ~5 types/file = 500 bytes
- Functions table: ~100 bytes/function × ~10 functions/file = 1000 bytes
- Imports table: ~80 bytes/import × ~10 imports/file = 800 bytes
- Total: ~2.5 KB per file

**For 10K files:**
- Files: 10K × 200 bytes = 2 MB
- Types: 50K × 100 bytes = 5 MB
- Functions: 100K × 100 bytes = 10 MB
- Imports: 100K × 80 bytes = 8 MB
- Indexes: ~5 MB
- **Total: ~30 MB**

**Acceptable:** In-memory SQLite uses ~30MB for 10K files. Most projects are smaller.

### Startup Time

**Loading from SQLite cache:**
- Read file: ~10ms (streaming)
- Parse JSON: ~50ms (10K lines)
- Insert into SQLite: ~100ms (batched transactions)
- Build indexes: ~20ms
- **Total: ~180ms**

**Cold start overhead:** Acceptable for daemon startup. No module aggregation needed (computed on-demand via GROUP BY).

## Integration with Other Tools

### Complementary Roles

**cortex_files** answers "where" and "how much":
- "Which modules have the most code?"
- "What's the largest file in internal/?"
- "How many exported types in internal/mcp?"

**cortex_search** answers "what" and "why":
- "How does authentication work?"
- "Why was this implemented this way?"

**cortex_graph** answers "who" and "dependencies":
- "Who calls this function?"
- "What does this package depend on?"

**cortex_exact** answers "where is" (exact matches):
- "Where is `sync.RWMutex` used?"

**cortex_pattern** answers "structural patterns":
- "Find all defer statements"

### Hybrid Workflows

**Workflow 1: Stats → Semantic exploration**
```
1. cortex_files: "Which modules have >10 files and >5000 lines?"
   → Returns: internal/mcp, internal/indexer
2. cortex_search: "How does the indexer work?"
   → Returns: Semantic explanation with code chunks
```

**Workflow 2: Stats → Relationship traversal**
```
1. cortex_files: "Functions with >10 parameters"
   → Returns: list of complex functions
2. cortex_graph: "Who calls these functions?"
   → Returns: callers and call chains
```

**Workflow 3: Stats → Structural analysis**
```
1. cortex_files: "Files in internal/mcp with >200 lines"
   → Returns: server.go, tool.go
2. cortex_pattern: "Find all defer statements in these files"
   → Returns: structural matches
```

## Testing Strategy

### Unit Tests

**Query building:**
```go
func TestBuildQuery_SimpleWhere(t *testing.T)
func TestBuildQuery_Aggregation(t *testing.T)
func TestBuildQuery_Join(t *testing.T)
func TestBuildQuery_ComplexFilter(t *testing.T)
```

**Validation:**
```go
func TestValidateQuery_InvalidTable(t *testing.T)
func TestValidateQuery_InvalidField(t *testing.T)
func TestValidateQuery_InvalidOperator(t *testing.T)
```

**Filter translation:**
```go
func TestBuildFilter_Equals(t *testing.T)
func TestBuildFilter_AndOr(t *testing.T)
func TestBuildFilter_Between(t *testing.T)
func TestBuildFilter_IsNull(t *testing.T)
```

### Integration Tests

**With real SQLite:**

```go
//go:build integration

func TestQueryExecution_TopFilesByLines(t *testing.T) {
    db := setupTestDB(t)
    defer db.Close()

    query := &QueryDefinition{
        From: "files",
        OrderBy: []OrderBy{
            {Field: "lines_total", Direction: "DESC"},
        },
        Limit: 5,
    }

    result, err := executeQuery(db, query)
    require.NoError(t, err)
    assert.Equal(t, 5, len(result.Rows))
}

func TestQueryExecution_AggregationByLanguage(t *testing.T) {
    db := setupTestDB(t)

    query := &QueryDefinition{
        From: "files",
        Aggregations: []Aggregation{
            {Function: "COUNT", Alias: "count"},
            {Function: "SUM", Field: "lines_total", Alias: "total"},
        },
        GroupBy: []string{"language"},
    }

    result, err := executeQuery(db, query)
    require.NoError(t, err)
    assert.Contains(t, result.Columns, "language")
    assert.Contains(t, result.Columns, "count")
    assert.Contains(t, result.Columns, "total")
}

func TestIncrementalUpdate(t *testing.T) {
    db := setupTestDB(t)

    // Initial state
    countBefore := queryFileCount(db)

    // Update file
    newStats := &FileMetadata{
        FilePath:   "internal/new.go",
        Language:   "go",
        LinesTotal: 150,
        // ...
    }
    db.UpdateFile(newStats)

    // Verify update
    countAfter := queryFileCount(db)
    assert.Equal(t, countBefore+1, countAfter)

    // Verify module updated
    module := queryModule(db, "internal")
    assert.Equal(t, countBefore+1, module.FileCount)
}
```

### MCP Protocol Tests

```go
func TestMCPTool_QueryFiles(t *testing.T) {
    handler := createCortexFilesHandler(testDB)

    request := mcp.CallToolRequest{
        Params: mcp.CallToolParams{
            Arguments: `{
                "operation": "query",
                "query": {
                    "from": "files",
                    "where": {"field": "language", "operator": "=", "value": "go"},
                    "limit": 10
                }
            }`,
        },
    }

    result, err := handler(ctx, request)
    require.NoError(t, err)

    var response QueryResponse
    json.Unmarshal([]byte(result.Content[0].Text), &response)
    assert.NotEmpty(t, response.Rows)
    assert.Contains(t, response.Columns, "file_path")
}

func TestMCPTool_InvalidTable(t *testing.T) {
    request := mcp.CallToolRequest{
        Params: mcp.CallToolParams{
            Arguments: `{
                "operation": "query",
                "query": {"from": "invalid_table"}
            }`,
        },
    }

    result, err := handler(ctx, request)
    require.NoError(t, err)
    assert.True(t, result.IsError)
    assert.Contains(t, result.Content[0].Text, "invalid table")
}
```

## Implementation Checklist

**Note:** SQLite schema and storage layer already implemented in `internal/storage/`. This checklist focuses on the query interface and MCP tool.

### Phase 1: Query Builder Foundation (1-2 days)
- ✅ Create `internal/files/` package for cortex_files implementation (with tests)
- ✅ Define Go types for JSON query schema (QueryDefinition, Filter, FieldFilter, AndFilter, OrFilter, Join, Aggregation, OrderBy) (with tests)
- ✅ Implement schema validation (valid tables, valid fields, valid operators) (with tests)
- ✅ Create table schema registry (map tables to their valid columns) (with tests)

### Phase 2: Query Translation (2 days)
- ✅ Implement filter translation to Squirrel (FieldFilter, AndFilter, OrFilter, NotFilter) (with tests)
- ✅ Implement JOIN translation (INNER, LEFT, RIGHT) (with tests)
- ✅ Implement aggregation translation (COUNT, SUM, AVG, MIN, MAX with DISTINCT) (with tests)
- ✅ Implement SELECT, WHERE, GROUP BY, HAVING, ORDER BY, LIMIT, OFFSET translation (with tests)
- ✅ Add SQL injection prevention validation (with tests)

### Phase 3: Query Execution (1 day)
- ✅ Implement query executor (executes SQL, returns rows) (with tests)
- ✅ Implement result formatter (rows → JSON with columns array) (with tests)
- ✅ Add performance measurement (timing in metadata) (with tests)
- ✅ Add comprehensive error handling (query build errors, execution errors) (with tests)

### Phase 4: MCP Tool Interface (1 day)
- ✅ Create MCP tool handler for cortex_files (with tests)
- ✅ Implement request parsing (operation + query JSON) (with tests)
- ✅ Implement response formatting (QueryResponse with metadata) (with tests)
- ✅ Register tool in MCP server with proper schema (with tests)

### Phase 5: Module Statistics (N/A - No separate implementation needed)
- ✅ Module aggregation via SQL GROUP BY (no separate implementation needed)
  - Module statistics computed on-demand with `GROUP BY module_path`
  - No separate table, triggers, or aggregation logic required
  - Standard SQL patterns provide <1ms query performance

### Phase 6: Integration Testing (1 day)
- ✅ Create integration test suite with real SQLite database
- ✅ Test simple SELECT queries (files, types, functions)
- ✅ Test aggregation queries (GROUP BY, COUNT, SUM)
- ✅ Test JOIN queries (files + types, functions + types)
- ✅ Test complex filters (AND/OR/NOT combinations)
- ✅ Test error cases (invalid tables, invalid fields, SQL errors)

### Phase 7: Documentation & Examples (1 day)
- ✅ Update CLAUDE.md with cortex_files usage section
- ✅ Add query examples (common queries for LLMs)
- ✅ Document hybrid workflows (cortex_files → cortex_search, cortex_files → cortex_graph)
- ✅ Add performance notes and query optimization tips

**Total estimated effort: 7-9 days**

**Dependencies:**
- ✅ SQLite schema (already in `internal/storage/schema.go`)
- ✅ Storage writers (already in `internal/storage/file_writer.go`, `graph_writer.go`)
- ✅ Squirrel library (already in dependencies)
- ⏳ Indexer integration to populate files/types/functions (separate task, may run in parallel)

## Future Enhancements

### Potential Additions

1. **Cyclomatic complexity**: Calculate and store complexity metrics
2. **Dependency graph**: Store function call relationships (overlap with cortex_graph)
3. **Code coverage**: Integrate test coverage data
4. **Historical trends**: Track stats over time (git commits)
5. **Custom SQL**: Allow raw SQL queries (for power users)
6. **Views**: Pre-defined SQL views for common queries
7. **Export**: Export results as CSV, JSON, or markdown tables

### Schema Evolution

**Adding new columns:**
```sql
ALTER TABLE files ADD COLUMN complexity_total INTEGER DEFAULT 0;
```

**Migration strategy:**
- Version schema (pragma user_version)
- Migration scripts for each version
- Backward-compatible reads (ignore unknown columns)
