---
status: planned
started_at: null
completed_at: null
dependencies: []
---

# cortex_files Specification

## Purpose

The `cortex_files` tool provides fast, queryable access to code statistics and metadata via an in-memory SQLite database. It enables LLMs to answer quantitative questions about project structure ("Which modules have the most code?", "What's the test coverage ratio?") without resorting to slow bash command chains or manual file analysis.

## Core Concept

**Input**: Codebase files (parsed during indexing)

**Process**: Extract metadata (lines, types, functions, imports) → Store in 5-table SQLite schema → Load into memory → Accept type-safe JSON queries

**Output**: SQL query results with file stats, aggregations, and cross-table joins

## Technology Stack

- **Language**: Go 1.25+
- **Database**: SQLite 3 (in-memory, via `github.com/mattn/go-sqlite3`)
- **Query Builder**: Squirrel (`github.com/Masterminds/squirrel`)
- **Storage Format**: NDJSON (`.cortex/stats.ndjson`)
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

### Five Tables

#### 1. files table

**Purpose**: Per-file statistics and metadata

**Schema:**
```sql
CREATE TABLE files (
    file_path TEXT PRIMARY KEY,        -- Relative to project root
    language TEXT NOT NULL,             -- go, typescript, python, etc.
    is_test BOOLEAN NOT NULL,           -- Test file classification
    module_path TEXT NOT NULL,          -- Package/module path

    -- Line counts
    lines_total INTEGER NOT NULL,       -- Total lines
    lines_code INTEGER NOT NULL,        -- Code lines (excluding comments/blank)
    lines_comment INTEGER NOT NULL,     -- Comment lines
    lines_blank INTEGER NOT NULL,       -- Blank lines

    -- File metadata
    size_bytes INTEGER NOT NULL,        -- File size in bytes
    last_modified TEXT NOT NULL,        -- ISO8601 timestamp

    -- Counts (denormalized from other tables)
    type_count INTEGER NOT NULL,        -- Number of types defined
    function_count INTEGER NOT NULL,    -- Number of functions defined
    import_count INTEGER NOT NULL       -- Number of imports
);

CREATE INDEX idx_files_language ON files(language);
CREATE INDEX idx_files_is_test ON files(is_test);
CREATE INDEX idx_files_module_path ON files(module_path);
```

#### 2. types table

**Purpose**: Type definitions (structs, interfaces, classes, enums)

**Schema:**
```sql
CREATE TABLE types (
    type_id TEXT PRIMARY KEY,           -- Unique ID: {file_path}:{start_line}:{name}
    name TEXT NOT NULL,                 -- Type name
    kind TEXT NOT NULL,                 -- struct, interface, class, enum, type_alias
    file_path TEXT NOT NULL,            -- Foreign key to files.file_path
    module_path TEXT NOT NULL,          -- Denormalized from files

    -- Location
    start_line INTEGER NOT NULL,        -- 1-indexed
    end_line INTEGER NOT NULL,          -- 1-indexed

    -- Metadata
    is_exported BOOLEAN NOT NULL,       -- Public/exported flag
    field_count INTEGER NOT NULL,       -- Number of fields
    method_count INTEGER NOT NULL,      -- Number of methods

    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
);

CREATE INDEX idx_types_file_path ON types(file_path);
CREATE INDEX idx_types_module_path ON types(module_path);
CREATE INDEX idx_types_kind ON types(kind);
CREATE INDEX idx_types_is_exported ON types(is_exported);
```

#### 3. functions table

**Purpose**: Function and method metadata

**Schema:**
```sql
CREATE TABLE functions (
    function_id TEXT PRIMARY KEY,       -- Unique ID: {file_path}:{start_line}:{name}
    name TEXT NOT NULL,                 -- Function name
    file_path TEXT NOT NULL,            -- Foreign key to files.file_path
    module_path TEXT NOT NULL,          -- Denormalized from files

    -- Location
    start_line INTEGER NOT NULL,        -- 1-indexed
    end_line INTEGER NOT NULL,          -- 1-indexed

    -- Metadata
    is_exported BOOLEAN NOT NULL,       -- Public/exported flag
    is_method BOOLEAN NOT NULL,         -- Method vs function
    receiver_type TEXT,                 -- For methods (e.g., "*Handler")

    -- Signature
    param_count INTEGER NOT NULL,       -- Number of parameters
    return_count INTEGER NOT NULL,      -- Number of return values

    -- Complexity
    lines INTEGER NOT NULL,             -- Lines of code in function body
    complexity INTEGER,                 -- Cyclomatic complexity (optional, future)

    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
);

CREATE INDEX idx_functions_file_path ON functions(file_path);
CREATE INDEX idx_functions_module_path ON functions(module_path);
CREATE INDEX idx_functions_is_exported ON functions(is_exported);
CREATE INDEX idx_functions_param_count ON functions(param_count);
CREATE INDEX idx_functions_lines ON functions(lines);
```

#### 4. imports table

**Purpose**: Import/dependency relationships

**Schema:**
```sql
CREATE TABLE imports (
    import_id TEXT PRIMARY KEY,         -- Unique ID: {file_path}:{imported_module}
    file_path TEXT NOT NULL,            -- Foreign key to files.file_path
    imported_module TEXT NOT NULL,      -- Full import path (e.g., "github.com/foo/bar")

    -- Classification
    is_standard_lib BOOLEAN NOT NULL,   -- Part of language standard library
    is_external BOOLEAN NOT NULL,       -- External dependency (not in project)
    is_relative BOOLEAN NOT NULL,       -- Relative import (./foo, ../bar)

    -- Metadata
    symbol_count INTEGER,               -- Number of symbols imported (if tracked)

    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
);

CREATE INDEX idx_imports_file_path ON imports(file_path);
CREATE INDEX idx_imports_module ON imports(imported_module);
CREATE INDEX idx_imports_is_external ON imports(is_external);
```

#### 5. modules table

**Purpose**: Aggregated package/module statistics

**Schema:**
```sql
CREATE TABLE modules (
    module_path TEXT PRIMARY KEY,       -- Full module path (e.g., "internal/mcp")

    -- Aggregated counts
    file_count INTEGER NOT NULL,        -- Number of files in module
    total_lines INTEGER NOT NULL,       -- Sum of lines_total from files
    code_lines INTEGER NOT NULL,        -- Sum of lines_code from files
    test_file_count INTEGER NOT NULL,   -- Number of test files

    -- Type/function counts
    type_count INTEGER NOT NULL,        -- Types defined in module
    function_count INTEGER NOT NULL,    -- Functions defined in module
    exported_type_count INTEGER NOT NULL,    -- Public types
    exported_function_count INTEGER NOT NULL, -- Public functions

    -- Import stats
    import_count INTEGER NOT NULL,      -- Unique imports across all files
    external_import_count INTEGER NOT NULL,  -- External dependencies

    -- Depth
    depth INTEGER NOT NULL              -- Nesting level (e.g., internal/mcp = 2)
);

CREATE INDEX idx_modules_depth ON modules(depth);
CREATE INDEX idx_modules_file_count ON modules(file_count);
CREATE INDEX idx_modules_total_lines ON modules(total_lines);
```

### Schema Design Rationale

**Denormalization:**
- `module_path` stored in types/functions tables (avoid JOINs)
- Counts stored in files table (type_count, function_count)
- Benefits: Faster queries, simpler SQL
- Tradeoff: Slightly larger storage (negligible for 10K files)

**Foreign Keys:**
- CASCADE DELETE on file_path
- When file deleted, types/functions/imports auto-deleted

**Indexes:**
- Covering indexes for common filters (language, is_test, is_exported)
- Range indexes for numeric columns (lines, param_count)

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
    "modules": true,
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

## Storage Format

### NDJSON (Newline-Delimited JSON)

**File**: `.cortex/stats.ndjson`

**Format**: One JSON object per line (one per file):

```ndjson
{"file_path":"internal/mcp/server.go","language":"go","is_test":false,"module_path":"internal/mcp","lines_total":245,"lines_code":180,"lines_comment":40,"lines_blank":25,"size_bytes":8192,"last_modified":"2025-10-29T10:00:00Z","type_count":3,"function_count":12,"import_count":8,"types":[{"name":"Server","kind":"struct","start_line":24,"end_line":32,"is_exported":true,"field_count":4,"method_count":0}],"functions":[{"name":"NewServer","start_line":34,"end_line":45,"is_exported":true,"is_method":false,"param_count":2,"return_count":2,"lines":12}],"imports":[{"imported_module":"context","is_standard_lib":true,"is_external":false,"is_relative":false}]}
{"file_path":"internal/mcp/tool.go","language":"go","is_test":false,"module_path":"internal/mcp","lines_total":156,"lines_code":120,"lines_comment":20,"lines_blank":16,"size_bytes":5432,"last_modified":"2025-10-29T10:05:00Z","type_count":2,"function_count":8,"import_count":6,"types":[...],"functions":[...],"imports":[...]}
```

**Why NDJSON:**
- ✅ Streaming: Load line-by-line (low memory)
- ✅ Appending: Easy incremental updates (append new line)
- ✅ Debugging: Human-readable, each line is valid JSON
- ✅ Git-friendly: Line-based diffs

**Why not regular JSON:**
- ❌ Must parse entire file (high memory for large codebases)
- ❌ Atomic rewrites required (not incremental)

### Loading into SQLite

```go
func loadStatsFromNDJSON(dbPath string) (*sql.DB, error) {
    // Create in-memory database
    db, err := sql.Open("sqlite3", ":memory:")
    if err != nil {
        return nil, err
    }

    // Create schema
    if err := createSchema(db); err != nil {
        return nil, err
    }

    // Read NDJSON file
    file, err := os.Open(".cortex/stats.ndjson")
    if err != nil {
        return nil, err
    }
    defer file.Close()

    scanner := bufio.NewScanner(file)
    tx, _ := db.Begin()

    for scanner.Scan() {
        var fileStats FileMetadata
        if err := json.Unmarshal(scanner.Bytes(), &fileStats); err != nil {
            log.Printf("Failed to parse stats line: %v", err)
            continue
        }

        // Insert into files table
        insertFile(tx, &fileStats)

        // Insert into types table
        for _, typ := range fileStats.Types {
            insertType(tx, fileStats.FilePath, fileStats.ModulePath, &typ)
        }

        // Insert into functions table
        for _, fn := range fileStats.Functions {
            insertFunction(tx, fileStats.FilePath, fileStats.ModulePath, &fn)
        }

        // Insert into imports table
        for _, imp := range fileStats.Imports {
            insertImport(tx, fileStats.FilePath, &imp)
        }
    }

    tx.Commit()

    // Build modules table (aggregated)
    if err := buildModulesTable(db); err != nil {
        return nil, err
    }

    return db, nil
}
```

### Writing NDJSON

**Atomic write pattern** (same as chunks):

```go
func writeStats(stats []*FileMetadata) error {
    // Write to temp file
    tempPath := ".cortex/.tmp/stats.ndjson"
    file, err := os.Create(tempPath)
    if err != nil {
        return err
    }

    writer := bufio.NewWriter(file)
    for _, stat := range stats {
        data, err := json.Marshal(stat)
        if err != nil {
            return err
        }
        writer.Write(data)
        writer.WriteByte('\n')
    }
    writer.Flush()
    file.Close()

    // Atomic rename
    return os.Rename(tempPath, ".cortex/stats.ndjson")
}
```

## Incremental Updates

### File Change Detection

**Integrated with existing file watcher:**

```go
// In auto-daemon (file watcher)

func (w *FileWatcher) onFileChange(path string) {
    // Existing: Reload chunks
    w.reloadChunks(path)

    // NEW: Update stats
    w.updateStats(path)
}

func (w *FileWatcher) updateStats(path string) {
    // Re-parse file
    stats, err := w.parser.ParseFile(path)
    if err != nil {
        log.Printf("Failed to parse %s: %v", path, err)
        return
    }

    // Update database
    if err := w.statsDB.UpdateFile(stats); err != nil {
        log.Printf("Failed to update stats for %s: %v", path, err)
    }

    // Recompute affected module
    modulePath := stats.ModulePath
    if err := w.statsDB.RecomputeModule(modulePath); err != nil {
        log.Printf("Failed to recompute module %s: %v", modulePath, err)
    }
}
```

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

**For module aggregation (full re-compute):**

```go
func (db *StatsDB) RecomputeModule(modulePath string) error {
    // Aggregate from files table
    row := db.QueryRow(`
        SELECT
            COUNT(*) as file_count,
            SUM(lines_total) as total_lines,
            SUM(lines_code) as code_lines,
            SUM(CASE WHEN is_test THEN 1 ELSE 0 END) as test_file_count,
            SUM(type_count) as type_count,
            SUM(function_count) as function_count,
            SUM(import_count) as import_count
        FROM files
        WHERE module_path = ?
    `, modulePath)

    var stats ModuleStats
    row.Scan(&stats.FileCount, &stats.TotalLines, ...)

    // Count exported symbols
    row = db.QueryRow(`
        SELECT COUNT(*) FROM types WHERE module_path = ? AND is_exported = true
    `, modulePath)
    row.Scan(&stats.ExportedTypeCount)

    row = db.QueryRow(`
        SELECT COUNT(*) FROM functions WHERE module_path = ? AND is_exported = true
    `, modulePath)
    row.Scan(&stats.ExportedFunctionCount)

    // Count unique external imports
    row = db.QueryRow(`
        SELECT COUNT(DISTINCT imported_module)
        FROM imports
        WHERE file_path IN (SELECT file_path FROM files WHERE module_path = ?)
          AND is_external = true
    `, modulePath)
    row.Scan(&stats.ExternalImportCount)

    // Upsert module
    _, err := db.Exec(`
        INSERT OR REPLACE INTO modules (module_path, file_count, total_lines, ...)
        VALUES (?, ?, ?, ...)
    `, modulePath, stats.FileCount, stats.TotalLines, ...)

    return err
}
```

**Why full re-aggregate?**
- Modules table is small (<100 rows for most projects)
- Re-aggregating one module takes <1ms
- Simpler than tracking deltas (avoid drift)

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
- Modules: 100 × 300 bytes = 30 KB
- Indexes: ~5 MB
- **Total: ~30 MB**

**Acceptable:** In-memory SQLite uses ~30MB for 10K files. Most projects are smaller.

### Startup Time

**Loading from NDJSON:**
- Read file: ~10ms (streaming)
- Parse JSON: ~50ms (10K lines)
- Insert into SQLite: ~100ms (batched transactions)
- Build indexes: ~20ms
- Aggregate modules: ~30ms
- **Total: ~200ms**

**Cold start overhead:** Acceptable for daemon startup.

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

### Phase 1: Schema and Storage (2-3 days)
- [ ] Define SQLite schema (5 tables)
- [ ] Create table creation statements
- [ ] NDJSON read/write functions
- [ ] In-memory SQLite loading
- [ ] Unit tests for storage

### Phase 2: Data Extraction (2-3 days)
- [ ] Integrate with existing tree-sitter parsers
- [ ] Extract type metadata (per language)
- [ ] Extract function metadata (per language)
- [ ] Extract import metadata (per language)
- [ ] Line counting (code, comment, blank)
- [ ] Module path detection
- [ ] Test file classification
- [ ] Integration tests for extraction

### Phase 3: Query Builder (2-3 days)
- [ ] JSON schema types (QueryDefinition, Filter, etc.)
- [ ] Squirrel integration
- [ ] Filter translation (FieldFilter, AndFilter, OrFilter)
- [ ] JOIN translation
- [ ] Aggregation translation
- [ ] ORDER BY, LIMIT, OFFSET
- [ ] Schema validation
- [ ] Unit tests for query building

### Phase 4: Query Execution (1-2 days)
- [ ] Execute queries against SQLite
- [ ] Format results as JSON (columns + rows)
- [ ] Error handling
- [ ] Performance measurement (took_ms)
- [ ] Integration tests with real queries

### Phase 5: Incremental Updates (2 days)
- [ ] File change detection (integrate with watcher)
- [ ] Update database on file change
- [ ] Module re-aggregation
- [ ] Periodic persistence (60s + 100 changes)
- [ ] Integration tests for updates

### Phase 6: MCP Tool Integration (1 day)
- [ ] MCP tool registration
- [ ] Request/response schemas
- [ ] Error handling (validation errors vs system errors)
- [ ] MCP protocol tests

### Phase 7: Documentation & Polish (1 day)
- [ ] Update CLAUDE.md with usage examples
- [ ] Document common queries
- [ ] Document hybrid workflows
- [ ] Performance tuning
- [ ] Error message polish

**Total estimated effort: 2-3 weeks**

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
