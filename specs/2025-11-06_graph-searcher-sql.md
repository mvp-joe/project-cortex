---
status: implemented
started_at: 2025-11-06T00:00:00Z
completed_at: 2025-11-06T12:00:00Z
dependencies: [graph-update-refactor]
---

# SQL-Based Graph Searcher Specification

## Purpose

The current graph searcher loads complete graph data into memory and uses the dominikbraun/graph library for traversal queries. This approach consumes 50-200MB of memory and requires a full reload on changes. With graph data now stored in SQLite tables (via the graph update refactor), we can eliminate the in-memory graph entirely and query the database directly. This reduces memory usage by 10-40x, eliminates reload latency, and improves query performance for recursive traversals through native SQL CTEs.

## Core Concept

**Input**: Graph query request (operation, target, depth, filters) via MCP `cortex_graph` tool
**Process**: Execute SQL queries with WITH RECURSIVE CTEs for traversal, apply filters in WHERE clause
**Output**: Query results with nodes, depths, and optional code context

## Technology Stack

- **Language**: Go 1.25+
- **Database**: SQLite with existing schema (functions, function_calls, types, type_relationships, imports)
- **Query Builder**: Squirrel (already used by exact search and query helpers)
- **Context Extraction**: Position-based extraction from SQLite `files.content`
- **Interface**: Existing `Searcher` interface (zero breaking changes)

## Key Design Decisions

### Depth Limits (No Explicit Cycle Detection)
- **Default depth**: 3 (sufficient for most use cases)
- **Maximum depth**: 6 (hard limit, enforced at API level)
- **Cycle handling**: Depth limit naturally bounds recursive queries; explicit cycle detection removed
- **Rationale**: Simpler implementation, predictable performance, prevents runaway queries
- **Trade-off**: Cycles may cause duplicate visits within depth limit, but DISTINCT in final SELECT deduplicates results

### Target Resolution with Pattern Matching
- **Exact match**: `target = "Handler.ServeHTTP"` → matches single function
- **Pattern match**: `target = "%.Init"` → matches all Init methods (SQL LIKE)
- **Wildcard syntax**: Use `%` for zero-or-more chars, `_` for exactly one char
- **Ambiguous targets**: Return error if multiple exact matches found (user must use pattern or qualify)
- **External functions**: Queries check both `callee_function_id` (internal) and `callee_name` (external like `fmt.Println`)

### Transaction Isolation for Consistency
- All queries wrapped in **read-only transactions** for consistent snapshots
- Prevents seeing partial writes from concurrent `GraphUpdater.Update()` operations
- Example: Query sees either old state or new state, never mid-update
- Performance: Zero overhead (SQLite MVCC allows concurrent readers)

### Pure SQL LIKE Pattern Syntax
- **Consistent everywhere**: Targets, file paths, scope filters all use SQL LIKE syntax
- **No glob conversion**: Simplified from original proposal (no `*` → `%` translation)
- **Breaking change**: Existing glob users must migrate (but project not widely deployed yet)
- **Examples**:
  - `scope: "internal/%"` → everything under internal/
  - `exclude: "%_test.go"` → exclude test files
  - `target: "Handler.%"` → all Handler methods

### Graceful Error Handling
- **Context extraction failures**: Log warning, return node without context (don't fail query)
- **External function references**: Skip if `callee_function_id IS NULL` and not resolvable
- **Malformed data**: Skip row with validation error, log for debugging
- **Rationale**: LLM gets maximum useful data; partial results better than total failure

## Architecture

### Current Architecture (Being Replaced)

```
┌──────────────────┐
│ GraphData JSON   │
│ (code-graph.json)│
└────────┬─────────┘
         │ Load (100-500ms)
         ▼
┌────────────────────┐
│ dominikbraun/graph │  50-200MB memory
│ + 6 reverse indexes│
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ BFS Traversal      │
│ (app-side)         │
└────────────────────┘
```

**Problems:**
- 50-200MB memory footprint for graph + indexes
- 100-500ms startup/reload time
- Reload latency when graph changes
- BFS iteration slower than SQL CTEs for depth > 2

### New Architecture (SQL-Based)

```
┌──────────────────┐
│ SQLite Tables    │
│ - functions      │
│ - function_calls │
│ - types          │
│ - type_relationships │
│ - imports        │
└────────┬─────────┘
         │ Direct queries (1-20ms)
         ▼
┌────────────────────┐
│ SQL Queries        │
│ - Simple JOINs     │
│ - WITH RECURSIVE   │
│ - Cycle detection  │
└────────┬───────────┘
         │
         ▼
┌────────────────────┐
│ Result Mapping     │  <5MB memory (cache only)
│ Node construction  │
└────────────────────┘
```

**Benefits:**
- <5MB memory footprint (no cache needed)
- 0ms startup (no loading required)
- Always up-to-date (no reload needed)
- Faster recursive queries (SQL CTE > app-side BFS)

## Schema Requirements

This implementation requires schema changes to support position-based context extraction and maintain all necessary indexes.

### Required Schema Changes

#### 1. Add Byte Positions to Graph Tables

```sql
-- functions table
ALTER TABLE functions ADD COLUMN start_pos INTEGER NOT NULL DEFAULT 0;
ALTER TABLE functions ADD COLUMN end_pos INTEGER NOT NULL DEFAULT 0;

-- types table
ALTER TABLE types ADD COLUMN start_pos INTEGER NOT NULL DEFAULT 0;
ALTER TABLE types ADD COLUMN end_pos INTEGER NOT NULL DEFAULT 0;
```

**Purpose**: Enable efficient position-based context extraction from file content.
**Source**: Captured from `go/ast` via `fset.Position(node.Pos()).Offset` during graph extraction.
**Default value**: 0 = unknown position (safe fallback for existing data).

#### 2. Add Content Storage to Files Table

```sql
-- files table
ALTER TABLE files ADD COLUMN content TEXT;  -- NULL for binary files

-- files_fts: Change to external content
DROP TABLE files_fts;
CREATE VIRTUAL TABLE files_fts USING fts5(
    file_path UNINDEXED,
    content,
    content='files',           -- NEW: Point to files table
    content_rowid='rowid',     -- NEW: Link via rowid
    tokenize = "unicode61 separators '._'"
);
```

**Purpose**:
- Store file content once in `files` table (not duplicated in FTS)
- Enable direct `substr()` queries for context extraction
- Support cloud upload (DB is self-contained)

#### 3. Add Conditional Triggers for FTS Sync

```sql
-- Auto-sync to files_fts only when content IS NOT NULL
CREATE TRIGGER files_fts_insert AFTER INSERT ON files
WHEN NEW.content IS NOT NULL
BEGIN
    INSERT INTO files_fts (rowid, file_path, content)
    VALUES (NEW.rowid, NEW.file_path, NEW.content);
END;

CREATE TRIGGER files_fts_update AFTER UPDATE ON files
WHEN NEW.content IS NOT NULL
BEGIN
    UPDATE files_fts SET content = NEW.content WHERE rowid = OLD.rowid;
END;

CREATE TRIGGER files_fts_delete AFTER DELETE ON files
WHEN OLD.content IS NOT NULL
BEGIN
    DELETE FROM files_fts WHERE rowid = OLD.rowid;
END;
```

**Purpose**: Automatically sync text files to FTS5 index while excluding binary files.

#### 4. Index Already Exists (Confirmed)

```sql
-- This index already exists in schema.go line 349
CREATE INDEX idx_function_calls_callee_name ON function_calls(callee_name);
```

**Purpose**: Fast lookups for external function calls (stdlib, third-party).
**Status**: ✅ Already present, no changes needed.

### Schema Version

Update `cache_metadata` table:
- **Old version**: "2.0"
- **New version**: "2.1" (triggers schema recreation on next index)

## Implementation

### 1. Core Structure

```go
// internal/graph/searcher_sql.go

type sqlSearcher struct {
    db      *sql.DB
    context *ContextExtractor  // Shared context extraction (no cache, no mutex)
}

func NewSQLSearcher(db *sql.DB, rootDir string) (Searcher, error) {
    return &sqlSearcher{
        db:      db,
        context: NewContextExtractor(db),  // Extracts from SQLite directly
    }, nil
}
```

**Key changes from original proposal:**
- **Removed**: `otter.Cache` and `sync.RWMutex` (no longer needed)
- **Added**: Shared `ContextExtractor` for position-based extraction from `files.content`
- **Rationale**: Context extraction queries SQLite directly; no in-memory cache needed

### 2. Interface Implementation

The `sqlSearcher` implements the existing `Searcher` interface with zero changes:

```go
func (s *sqlSearcher) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
    // Start read-only transaction for consistent snapshot
    tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
    if err != nil {
        return nil, fmt.Errorf("begin transaction: %w", err)
    }
    defer tx.Rollback()  // Safe to call even after commit

    // Sanitize and enforce depth limits
    const DefaultDepth = 3
    const MaxDepth = 6
    if req.Depth <= 0 {
        req.Depth = DefaultDepth
    }
    if req.Depth > MaxDepth {
        return nil, fmt.Errorf("depth %d exceeds maximum %d", req.Depth, MaxDepth)
    }

    start := time.Now()

    var resp *QueryResponse

    switch req.Operation {
    case OperationCallers:
        resp, err = s.queryCallers(ctx, tx, req)
    case OperationCallees:
        resp, err = s.queryCallees(ctx, tx, req)
    case OperationDependencies:
        resp, err = s.queryDependencies(ctx, tx, req)
    case OperationDependents:
        resp, err = s.queryDependents(ctx, tx, req)
    case OperationTypeUsages:
        resp, err = s.queryTypeUsages(ctx, tx, req)
    case OperationImplementations:
        resp, err = s.queryImplementations(ctx, tx, req)
    case OperationPath:
        resp, err = s.queryPath(ctx, tx, req)
    case OperationImpact:
        resp, err = s.queryImpact(ctx, tx, req)
    default:
        return nil, fmt.Errorf("unsupported operation: %s", req.Operation)
    }

    if err != nil {
        return nil, err
    }

    resp.Metadata.TookMs = int(time.Since(start).Milliseconds())
    resp.Metadata.Source = "graph"
    return resp, nil
}

func (s *sqlSearcher) Reload(ctx context.Context) error {
    // No-op: SQL is always current
    return nil
}

func (s *sqlSearcher) Close() error {
    // No resources to clean up
    return nil
}
```

**Key changes:**
- **Added**: Read-only transaction wrapping for consistent snapshots
- **Added**: Depth limit validation (default 3, max 6)
- **Updated**: All query methods now take `*sql.Tx` parameter
- **Removed**: Cache cleanup in `Close()` (no cache anymore)

### 3. Query Operations

#### Pattern: Shared Execution

To avoid code duplication between depth-1 and recursive queries, use shared execution methods:

```go
// Callers - delegates to appropriate builder
func (s *sqlSearcher) queryCallers(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
    sql, args := s.buildCallersSQL(req)
    return s.executeFunctionQuery(ctx, sql, args, req)
}

// Shared executor for all function queries
func (s *sqlSearcher) executeFunctionQuery(ctx context.Context, sql string, args []interface{}, req *QueryRequest) (*QueryResponse, error) {
    rows, err := s.db.QueryContext(ctx, sql, args...)
    if err != nil {
        return nil, fmt.Errorf("query error: %w", err)
    }
    defer rows.Close()

    results := []QueryResult{}
    for rows.Next() {
        node, depth, err := s.scanFunctionRow(rows)
        if err != nil {
            return nil, err
        }

        result := QueryResult{Node: node, Depth: depth}

        if req.IncludeContext {
            result.Context, _ = s.extractContext(node.File, node.StartLine, node.EndLine, req.ContextLines)
        }

        results = append(results, result)
    }

    return &QueryResponse{
        Operation:     string(req.Operation),
        Target:        req.Target,
        Results:       results,
        TotalFound:    len(results),
        TotalReturned: len(results),
        Truncated:     len(results) >= req.MaxResults,
    }, nil
}
```

Similarly for types and dependencies:
- `executeTypeQuery()` - For implementations
- `executeDependencyQuery()` - For dependencies/dependents

#### Callers Query

**Depth 1 (Simple JOIN):**
```sql
SELECT DISTINCT
    f.function_id, f.file_path, f.start_line, f.end_line,
    f.name, f.module_path, f.is_method, f.receiver_type_name,
    1 as depth
FROM function_calls fc
JOIN functions f ON fc.caller_function_id = f.function_id
WHERE (fc.callee_function_id = ? OR fc.callee_name = ?)
  AND f.file_path LIKE ?              -- Scope filter
  AND f.file_path NOT LIKE ?          -- Exclude filter
LIMIT ?
```

**Depth N (WITH RECURSIVE CTE):**
```sql
WITH RECURSIVE caller_chain(function_id, depth) AS (
    -- Base case: direct callers
    SELECT DISTINCT
        fc.caller_function_id,
        1
    FROM function_calls fc
    WHERE fc.callee_function_id = ? OR fc.callee_name = ?

    UNION ALL

    -- Recursive case: callers of callers
    SELECT DISTINCT
        fc.caller_function_id,
        cc.depth + 1
    FROM caller_chain cc
    JOIN function_calls fc ON cc.function_id = fc.callee_function_id
    WHERE cc.depth < ?  -- Depth limit bounds recursion
)
SELECT DISTINCT
    f.function_id, f.file_path, f.start_line, f.end_line,
    f.name, f.module_path, f.is_method, f.receiver_type_name,
    cc.depth
FROM caller_chain cc
JOIN functions f ON cc.function_id = f.function_id
WHERE f.file_path LIKE ?          -- Scope filter
  AND f.file_path NOT LIKE ?      -- Exclude filter
ORDER BY cc.depth, f.function_id
LIMIT ?
```

**Key Design:** Depth limit naturally bounds recursion; no explicit cycle detection needed. DISTINCT in final SELECT deduplicates any nodes visited multiple times via different paths.

#### Callees Query

Same pattern as callers, but follow `caller_function_id → callee_function_id` edges instead.

#### Dependencies Query

Direct lookup (depth 1 only):
```sql
SELECT DISTINCT i.import_path, f.file_path, i.import_line
FROM imports i
JOIN files f ON i.file_path = f.file_path
WHERE f.module_path = ? OR i.file_path = ?
  AND i.file_path LIKE ?
ORDER BY i.import_path
LIMIT ?
```

#### Dependents Query

Reverse lookup (depth 1 only):
```sql
SELECT DISTINCT f.module_path, i.file_path, i.import_line
FROM imports i
JOIN files f ON i.file_path = f.file_path
WHERE i.import_path = ?
  AND i.file_path LIKE ?
ORDER BY f.module_path
LIMIT ?
```

#### Implementations Query

Pre-inferred relationships from `type_relationships` table:
```sql
SELECT DISTINCT
    t.type_id, t.file_path, t.start_line, t.end_line,
    t.name, t.module_path, t.kind
FROM type_relationships tr
JOIN types t ON tr.from_type_id = t.type_id
WHERE tr.to_type_id = ?
  AND tr.relationship_type = 'implements'
  AND t.file_path LIKE ?
LIMIT ?
```

#### Type Usages Query

Text-based parameter/field matching with pattern support:
```sql
SELECT DISTINCT
    f.function_id, f.file_path, f.start_line, f.end_line,
    f.name, f.module_path, f.is_method, f.receiver_type_name,
    1 as depth
FROM functions f
JOIN function_parameters fp ON f.function_id = fp.function_id
WHERE fp.param_type LIKE ?  -- Pattern provided by user
  AND f.file_path LIKE ?
LIMIT ?
```

**Pattern matching behavior:**
- **Exact**: `target = "User"` → finds exact `User` type only
- **Pattern**: `target = "%User%"` → finds `*User`, `[]User`, `map[string]User`, etc.
- **Generics**: `target = "%[User]%"` → finds `PaginatedResult[User]`, `Container[User]`

**Known limitations:**
- Searches function parameters only (not return types or struct fields)
- Text-based matching (no import path resolution)
- `PaginatedResult[User]` is considered a different type than `User` (correct behavior)
- For comprehensive type analysis, use language server (gopls)

**Rationale**: User has full control via explicit patterns; simple implementation; can enhance with semantic resolution later if needed.

#### Path Query (BFS App-Side with Optimized Subgraph Loading)

Use breadth-first search in application code with single-query subgraph loading:

```go
func (s *sqlSearcher) queryPath(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
    if req.To == "" {
        return nil, fmt.Errorf("path query requires 'to' parameter")
    }

    // OPTIMIZATION: Load entire reachable subgraph in ONE query
    edges, err := s.loadReachableEdges(ctx, tx, req.Target, req.Depth)
    if err != nil {
        return nil, err
    }

    // Build adjacency map in memory
    graph := make(map[string][]string)
    for _, edge := range edges {
        graph[edge.From] = append(graph[edge.From], edge.To)
    }

    // BFS in memory (fast)
    path := s.bfsPath(req.Target, req.To, req.Depth, graph)
    if path == nil {
        return &QueryResponse{
            Operation:  string(req.Operation),
            Target:     req.Target,
            Results:    []QueryResult{},
            Suggestion: fmt.Sprintf("No path from %s to %s within depth %d", req.Target, req.To, req.Depth),
        }, nil
    }

    // Build response with full node details
    return s.buildPathResponse(ctx, tx, path, req)
}

// loadReachableEdges loads all edges reachable from startID within maxDepth
func (s *sqlSearcher) loadReachableEdges(ctx context.Context, tx *sql.Tx, startID string, maxDepth int) ([]Edge, error) {
    query := `
    WITH RECURSIVE reachable(function_id, depth) AS (
        SELECT ?, 0
        UNION ALL
        SELECT fc.callee_function_id, r.depth + 1
        FROM reachable r
        JOIN function_calls fc ON r.function_id = fc.caller_function_id
        WHERE r.depth < ? AND fc.callee_function_id IS NOT NULL
    )
    SELECT DISTINCT fc.caller_function_id, fc.callee_function_id
    FROM reachable r
    JOIN function_calls fc ON r.function_id = fc.caller_function_id
    WHERE fc.callee_function_id IS NOT NULL
    `

    rows, err := tx.QueryContext(ctx, query, startID, maxDepth)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var edges []Edge
    for rows.Next() {
        var edge Edge
        if err := rows.Scan(&edge.From, &edge.To); err != nil {
            return nil, err
        }
        edges = append(edges, edge)
    }

    return edges, nil
}

// bfsPath performs BFS on in-memory graph to find shortest path
func (s *sqlSearcher) bfsPath(start, end string, maxDepth int, graph map[string][]string) []string {
    type PathNode struct {
        ID   string
        Path []string
    }

    visited := make(map[string]bool)
    queue := []PathNode{{ID: start, Path: []string{start}}}
    visited[start] = true

    for len(queue) > 0 && len(queue[0].Path) <= maxDepth {
        current := queue[0]
        queue = queue[1:]

        if current.ID == end {
            return current.Path
        }

        for _, next := range graph[current.ID] {
            if !visited[next] {
                visited[next] = true
                newPath := append([]string{}, current.Path...)
                newPath = append(newPath, next)
                queue = append(queue, PathNode{ID: next, Path: newPath})
            }
        }
    }

    return nil  // No path found
}
```

**Rationale**:
- **Performance**: 1 query vs 1000+ queries for depth 6
- **Simplicity**: BFS on in-memory graph is straightforward
- **Early termination**: Stop as soon as target is found

**Trade-off**: Loads entire reachable subgraph even if path is found at depth 1, but this is acceptable since SQLite query is fast (~10-20ms) and memory overhead is minimal.

#### Impact Query (Three-Phase Analysis)

Combines implementations + direct callers + transitive callers:

```go
func (s *sqlSearcher) queryImpact(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
    summary := &ImpactSummary{}
    allResults := []QueryResult{}

    // Phase 1: Implementations
    implResp, _ := s.queryImplementations(ctx, &QueryRequest{...})
    summary.Implementations = len(implResp.Results)
    for _, r := range implResp.Results {
        r.ImpactType = "implementation"
        r.Severity = "must_update"
        allResults = append(allResults, r)
    }

    // Phase 2: Direct callers
    callersResp, _ := s.queryCallers(ctx, &QueryRequest{Depth: 1, ...})
    summary.DirectCallers = len(callersResp.Results)
    for _, r := range callersResp.Results {
        r.ImpactType = "direct_caller"
        r.Severity = "must_update"
        allResults = append(allResults, r)
    }

    // Phase 3: Transitive callers (depth 2+)
    if req.Depth > 1 {
        transitiveResp, _ := s.queryCallers(ctx, &QueryRequest{Depth: req.Depth, ...})
        for _, r := range transitiveResp.Results {
            if r.Depth > 1 {
                r.ImpactType = "transitive"
                r.Severity = "review_needed"
                allResults = append(allResults, r)
                summary.TransitiveCallers++
            }
        }
    }

    return &QueryResponse{
        Summary: summary,
        Results: allResults,
        ...
    }, nil
}
```

### 4. Filtering Strategy

Use SQL LIKE patterns directly (no glob conversion):

```go
func (s *sqlSearcher) applyFilters(sql string, req *QueryRequest, args *[]interface{}) string {
    if req.Scope != "" {
        sql += " AND file_path LIKE ?"
        *args = append(*args, req.Scope)
    }

    for _, pattern := range req.ExcludePatterns {
        sql += " AND file_path NOT LIKE ?"
        *args = append(*args, pattern)
    }

    return sql
}
```

**Pattern format:** SQL LIKE syntax
- `%` = zero or more characters
- `_` = exactly one character

**Examples:**
- `internal/%` - anything under internal/
- `%_test.go` - files ending in _test.go
- `internal/%.go` - .go files anywhere under internal/

**Documentation change:** Update MCP tool description to specify SQL LIKE patterns instead of glob patterns.

### 5. Helper Methods

#### Node Scanning

```go
func (s *sqlSearcher) scanFunctionRow(rows *sql.Rows) (*Node, int, error) {
    var node Node
    var depth int
    var isMethod bool
    var receiverName sql.NullString

    err := rows.Scan(
        &node.ID, &node.File, &node.StartLine, &node.EndLine,
        &node.Name, &node.ModulePath, &isMethod, &receiverName, &depth,
    )
    if err != nil {
        return nil, 0, err
    }

    if isMethod {
        node.Kind = NodeMethod
        if receiverName.Valid {
            node.ID = receiverName.String + "." + node.Name
        }
    } else {
        node.Kind = NodeFunction
    }

    return &node, depth, nil
}

func (s *sqlSearcher) scanTypeRow(rows *sql.Rows) (*Node, error) {
    var node Node
    var kind string

    err := rows.Scan(
        &node.ID, &node.File, &node.StartLine, &node.EndLine,
        &node.Name, &node.ModulePath, &kind,
    )
    if err != nil {
        return nil, err
    }

    node.Kind = NodeKind(kind)
    return &node, nil
}
```

#### Position-Based Context Extraction

**Location**: `internal/graph/context.go` (new shared file)

**Approach**: Extract code context using byte positions and estimated line lengths, reading small chunks from SQLite instead of full files.

```go
// LineRange represents a line-based range in a file (for display)
type LineRange struct {
    Start int  // 1-indexed line number
    End   int  // 1-indexed line number
}

// ByteRange represents a byte-based range in a file (for extraction)
type ByteRange struct {
    Start int  // 0-indexed byte offset
    End   int  // 0-indexed byte offset
}

// ContextExtractor extracts code context from SQLite file content.
type ContextExtractor struct {
    db *sql.DB
}

func NewContextExtractor(db *sql.DB) *ContextExtractor {
    return &ContextExtractor{db: db}
}

// ExtractContext extracts code snippet with context lines around target range.
func (ce *ContextExtractor) ExtractContext(filePath string, lines LineRange, pos ByteRange, contextLines int) (string, error) {
    const estimatedCharsPerLine = 120  // Reasonable for most code

    // Calculate byte window (overfetch by 1 line for safety)
    contextBytes := (contextLines + 1) * estimatedCharsPerLine
    fromPos := max(0, pos.Start - contextBytes)
    toPos := pos.End + contextBytes

    // Extract chunk from SQLite (1-indexed in SQLite substr)
    var chunk string
    err := ce.db.QueryRow(`
        SELECT substr(content, ?, ?) FROM files WHERE file_path = ?
    `, fromPos+1, toPos-fromPos, filePath).Scan(&chunk)
    if err != nil {
        return "", fmt.Errorf("extract chunk: %w", err)
    }

    // Split chunk into lines
    chunkLines := strings.Split(chunk, "\n")

    // Find target position in chunk
    relativePos := pos.Start - fromPos
    targetLineInChunk := countNewlines(chunk[:relativePos])

    // Calculate window
    targetSpan := lines.End - lines.Start
    desiredFrom := targetLineInChunk - contextLines
    desiredTo := targetLineInChunk + targetSpan + contextLines

    // Clamp to available lines
    actualFrom := max(0, desiredFrom)
    actualTo := min(len(chunkLines), desiredTo)

    // Extract snippet
    snippet := strings.Join(chunkLines[actualFrom:actualTo], "\n")

    // Calculate display line numbers
    displayStart := lines.Start - (targetLineInChunk - actualFrom)
    displayEnd := displayStart + (actualTo - actualFrom)

    prefix := fmt.Sprintf("// Lines %d-%d\n", displayStart, displayEnd)
    return prefix + snippet, nil
}

func countNewlines(s string) int {
    count := 0
    for _, ch := range s {
        if ch == '\n' {
            count++
        }
    }
    return count
}
```

**Key benefits:**
- **Efficient**: Reads ~2-3KB chunks instead of 50KB+ full files
- **No cache needed**: Query SQLite directly (fast enough)
- **Self-contained**: Works with cloud-uploaded databases (no filesystem access)
- **Graceful degradation**: If estimate is off, still returns significant context

**Performance**: ~1-2ms per context extraction (vs ~5ms for full file read + split).

## Data Flow: Byte Position Capture

This section documents how byte positions flow from parsing through storage to context extraction.

### Overview

```
go/ast parsing → Domain models → Graph extraction → SQLite storage → Query results → Context extraction
```

### 1. Source: Go AST Parsing

**Location**: `internal/indexer/parsers/go.go` (existing code for chunks) and `internal/graph/extractor.go` (graph extraction)

**Mechanism**: Go's `go/parser` + `token.FileSet` provide byte positions:

```go
fset := token.NewFileSet()
node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)

// For any AST node:
pos := node.Pos()                              // token.Pos (opaque integer)
position := fset.Position(pos)                 // token.Position struct
byteOffset := position.Offset                  // 0-indexed byte offset in file
lineNumber := position.Line                    // 1-indexed line number
```

**Key insight**: `token.FileSet` maintains the mapping from `token.Pos` (compact integer) to absolute byte offset and line number.

### 2. Domain Models

**Location**: `internal/graph/types.go` (existing domain models, needs updates)

**Required changes**: Add byte position fields to domain structs:

```go
type Function struct {
    ID           string
    Name         string
    FilePath     string
    StartLine    int   // Existing
    EndLine      int   // Existing
    StartPos     int   // NEW: 0-indexed byte offset
    EndPos       int   // NEW: 0-indexed byte offset
    // ... other fields
}

type Type struct {
    ID        string
    Name      string
    FilePath  string
    StartLine int   // Existing
    EndLine   int   // Existing
    StartPos  int   // NEW: 0-indexed byte offset
    EndPos    int   // NEW: 0-indexed byte offset
    // ... other fields
}
```

**Migration**: Existing structs already have `StartLine`/`EndLine`, so this is purely additive.

### 3. Graph Extraction

**Location**: `internal/graph/extractor.go` (existing code, needs updates)

**Changes**: Capture byte positions during AST traversal:

```go
func (e *Extractor) extractFunction(fset *token.FileSet, funcDecl *ast.FuncDecl) *Function {
    startPos := fset.Position(funcDecl.Pos())
    endPos := fset.Position(funcDecl.End())

    return &Function{
        // ... existing fields
        StartLine: startPos.Line,
        EndLine:   endPos.Line,
        StartPos:  startPos.Offset,  // NEW
        EndPos:    endPos.Offset,    // NEW
    }
}

func (e *Extractor) extractType(fset *token.FileSet, typeSpec *ast.TypeSpec, genDecl *ast.GenDecl) *Type {
    startPos := fset.Position(genDecl.Pos())
    endPos := fset.Position(genDecl.End())

    return &Type{
        // ... existing fields
        StartLine: startPos.Line,
        EndLine:   endPos.Line,
        StartPos:  startPos.Offset,  // NEW
        EndPos:    endPos.Offset,    // NEW
    }
}
```

**Critical**: Must pass `*token.FileSet` through the entire extraction pipeline to maintain position mappings.

### 4. Storage Layer

**Location**: `internal/storage/graph_writer.go` (existing code, needs updates)

**Schema changes** (documented in Schema Requirements section):
- Add `start_pos INTEGER NOT NULL DEFAULT 0` to `functions` table
- Add `end_pos INTEGER NOT NULL DEFAULT 0` to `functions` table
- Add `start_pos INTEGER NOT NULL DEFAULT 0` to `types` table
- Add `end_pos INTEGER NOT NULL DEFAULT 0` to `types` table

**Write operations**: GraphWriter inserts/updates include byte positions:

```go
func (w *GraphWriter) WriteFunctions(functions []*Function) error {
    stmt := `
        INSERT INTO functions (
            function_id, file_path, name,
            start_line, end_line, start_pos, end_pos,  -- NEW fields
            ...
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ...)
        ON CONFLICT(function_id) DO UPDATE SET
            start_pos = excluded.start_pos,  -- NEW
            end_pos = excluded.end_pos,      -- NEW
            ...
    `

    for _, fn := range functions {
        _, err := w.db.Exec(stmt,
            fn.ID, fn.FilePath, fn.Name,
            fn.StartLine, fn.EndLine, fn.StartPos, fn.EndPos,  // NEW
            // ... other fields
        )
    }
}
```

### 5. Query Results

**Location**: `internal/graph/searcher_sql.go`

**Read operations**: SELECT queries include byte positions:

```go
func (s *sqlSearcher) scanFunctionRow(rows *sql.Rows) (*Node, int, error) {
    var node Node
    var depth int

    err := rows.Scan(
        &node.ID, &node.File,
        &node.StartLine, &node.EndLine,
        &node.StartPos, &node.EndPos,  // NEW: Read byte positions
        &node.Name, &node.ModulePath,
        // ... other fields
    )

    return &node, depth, nil
}
```

**Node struct updates**:

```go
type Node struct {
    ID        string
    File      string
    StartLine int  // Existing
    EndLine   int  // Existing
    StartPos  int  // NEW
    EndPos    int  // NEW
    // ... other fields
}
```

### 6. Context Extraction

**Location**: `internal/graph/context.go` (new shared file)

**Usage**: ContextExtractor receives both line numbers (for display) and byte positions (for extraction):

```go
result.Context, _ = s.context.ExtractContext(
    node.File,
    LineRange{Start: node.StartLine, End: node.EndLine},  // For display
    ByteRange{Start: node.StartPos, End: node.EndPos},    // For extraction
    req.ContextLines,
)
```

**Extraction logic** (documented in Position-Based Context Extraction section):
1. Calculate byte window: `[startPos - contextBytes, endPos + contextBytes]`
2. Query SQLite: `SELECT substr(content, ?, ?) FROM files WHERE file_path = ?`
3. Split chunk into lines
4. Find target lines within chunk using byte offset
5. Return snippet with correct line numbers

### Benefits

1. **Precision**: Exact byte boundaries prevent off-by-one errors
2. **Efficiency**: ~2-3KB chunks instead of 50KB+ full files
3. **Self-contained**: Works with cloud-uploaded databases (no filesystem needed)
4. **Consistent**: Same position source (go/ast) for both chunks and graph
5. **Migration-safe**: Default value 0 for existing data (graceful degradation)

### Implementation Notes

- **FileSet lifetime**: Must keep `*token.FileSet` alive throughout extraction to maintain position mappings
- **Multi-file projects**: Each file gets its own FileSet in graph extraction (already handled)
- **Error handling**: Position 0 = unknown → fall back to line-based extraction with wider window
- **Testing**: Verify positions with simple test files where byte offsets are predictable

## FileWriter Simplification

This section documents changes to the FileWriter API to support position-based context extraction while maintaining correct handling of binary files.

### Current Implementation (Before Changes)

**Location**: `internal/storage/file_writer.go`

**Current API**:

```go
type FileWriter struct {
    db *sql.DB
}

// Writes file statistics only (no content)
func (w *FileWriter) WriteFileStats(file *FileStats) error {
    // INSERT INTO files (file_path, language, line_count, ...)
}

// Writes file content to FTS table separately
func (w *FileWriter) WriteFileContent(filePath string, content string) error {
    // DELETE FROM files_fts WHERE file_path = ?
    // INSERT INTO files_fts (file_path, content) VALUES (?, ?)
}
```

**Usage in processor** (`internal/indexer/processor.go`):

```go
// Write stats for ALL files (including binary)
if err := fileWriter.WriteFileStats(fileStats); err != nil {
    return err
}

// Only write content for text files
isText, err := isTextFile(file)
if !isText {
    skippedBinary++
    continue  // Skip WriteFileContent for binary files!
}

if err := fileWriter.WriteFileContent(filePath, content); err != nil {
    return err
}
```

**Problems**:
1. Two-method API requires coordination at call site
2. Separate writes are not atomic (potential inconsistency)
3. DELETE+INSERT pattern for FTS inefficient for unchanged files
4. Manual binary file detection at processor level (leaky abstraction)

### New Implementation (After Changes)

**Unified API**:

```go
type FileWriter struct {
    db *sql.DB
}

// Writes file stats and optional content atomically
func (w *FileWriter) WriteFile(file *FileStats, content *string) error {
    // Single INSERT/UPDATE with nullable content
    // Triggers handle FTS sync automatically
}
```

**Signature details**:
- `content *string` - Pointer to distinguish between:
  - `nil` = binary file (no content)
  - `&""` = empty text file (valid content)
  - `&"package main\n..."` = text file with content

**Implementation**:

```go
func (w *FileWriter) WriteFile(file *FileStats, content *string) error {
    stmt := `
        INSERT INTO files (
            file_path, language, module_path, is_test,
            line_count_total, line_count_code, line_count_comment, line_count_blank,
            size_bytes, file_hash, last_modified, indexed_at,
            content  -- NEW: nullable content field
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(file_path) DO UPDATE SET
            language = excluded.language,
            module_path = excluded.module_path,
            line_count_total = excluded.line_count_total,
            line_count_code = excluded.line_count_code,
            line_count_comment = excluded.line_count_comment,
            line_count_blank = excluded.line_count_blank,
            size_bytes = excluded.size_bytes,
            file_hash = excluded.file_hash,
            last_modified = excluded.last_modified,
            indexed_at = excluded.indexed_at,
            content = excluded.content  -- NEW: update content
    `

    var contentVal interface{}
    if content != nil {
        contentVal = *content
    } else {
        contentVal = nil  // SQL NULL for binary files
    }

    _, err := w.db.Exec(stmt,
        file.FilePath, file.Language, file.ModulePath, file.IsTest,
        file.LineCountTotal, file.LineCountCode, file.LineCountComment, file.LineCountBlank,
        file.SizeBytes, file.FileHash, file.LastModified, file.IndexedAt,
        contentVal,  // NULL for binary, string for text
    )

    return err
}
```

**Trigger behavior** (from Schema Requirements section):
- If `content IS NOT NULL` → triggers sync to `files_fts`
- If `content IS NULL` → triggers do nothing (binary file, no FTS entry)
- Automatic cleanup on DELETE

**Usage in processor** (simplified):

```go
// Detect binary files once
isText, err := isTextFile(file)

var content *string
if isText {
    fileContent, err := os.ReadFile(filePath)
    if err != nil {
        return err
    }
    contentStr := string(fileContent)
    content = &contentStr  // Pointer to content
} else {
    content = nil  // Binary file, no content
}

// Single atomic write
if err := fileWriter.WriteFile(fileStats, content); err != nil {
    return err
}
```

### Benefits

1. **Single responsibility**: FileWriter handles all file storage, including FTS sync
2. **Atomic writes**: Stats and content written in one transaction
3. **Simpler call site**: Processor just passes content or nil
4. **Trigger-based FTS**: Automatic sync, no manual DELETE+INSERT
5. **Type safety**: `*string` makes binary vs text distinction explicit
6. **Self-contained DB**: Content stored once in `files` table, FTS references it

### Migration Strategy

**Phase 1**: Add content column and triggers (schema migration)
```sql
ALTER TABLE files ADD COLUMN content TEXT;
-- Create triggers (documented in Schema Requirements)
```

**Phase 2**: Update FileWriter implementation
- Add `WriteFile()` method
- Keep old methods temporarily for backward compatibility
- Mark old methods as deprecated

**Phase 3**: Update processor and indexer
- Replace `WriteFileStats()` + `WriteFileContent()` calls with `WriteFile()`
- Remove binary file detection duplication

**Phase 4**: Remove deprecated methods
- Delete `WriteFileStats()` and `WriteFileContent()`
- Remove old FTS DELETE+INSERT logic

### Testing Requirements

```go
func TestFileWriter_WriteFile_TextFile(t *testing.T) {
    // Verify text file creates both files and files_fts entries
}

func TestFileWriter_WriteFile_BinaryFile(t *testing.T) {
    // Verify binary file (content=nil) creates files entry but NO files_fts entry
}

func TestFileWriter_WriteFile_EmptyFile(t *testing.T) {
    // Verify empty string (content=&"") is different from nil
}

func TestFileWriter_WriteFile_UpdateContent(t *testing.T) {
    // Verify updating content triggers FTS update
}

func TestFileWriter_WriteFile_BinaryToText(t *testing.T) {
    // Verify file changing from binary→text creates FTS entry
}

func TestFileWriter_WriteFile_TextToBinary(t *testing.T) {
    // Verify file changing from text→binary deletes FTS entry
}
```

## Performance Characteristics

### Query Performance

| Operation | Depth 1 | Depth 3 | Depth 5+ |
|-----------|---------|---------|----------|
| **Old (In-Memory)** | <1ms | 5-20ms | 50-200ms |
| **New (SQL)** | 1-5ms | 2-8ms | 10-20ms |
| **Winner** | Old (slightly) | New (2-3x) | New (5-10x) |

### Memory Usage

| Metric | Old (In-Memory) | New (SQL) | Improvement |
|--------|-----------------|-----------|-------------|
| Graph + indexes | 50-200MB | 0MB | N/A |
| File cache | ~5MB | 0MB | No cache needed |
| Runtime structures | ~1MB | ~1MB | Same |
| **Total** | **~56-206MB** | **~1MB** | **50-200x** |

**Note**: New implementation uses position-based context extraction directly from SQLite (no caching). Minimal memory footprint for query execution structures only.

### Startup Time

| Operation | Old (In-Memory) | New (SQL) |
|-----------|-----------------|-----------|
| Load graph JSON | 100-500ms | 0ms |
| Build indexes | 50-200ms | 0ms |
| **Total** | **150-700ms** | **0ms** |

### Index Coverage

All critical queries are indexed:
- `function_calls(caller_function_id, callee_function_id)` ✓
- `functions(function_id)` ✓
- `type_relationships(from_type_id, to_type_id, relationship_type)` ✓
- `imports(file_path, import_path)` ✓

## Testing Strategy

### Unit Tests (`searcher_sql_test.go`)

```go
func setupTestDB(t *testing.T) *sql.DB {
    db, err := sql.Open("sqlite3", ":memory:")
    require.NoError(t, err)

    // Create schema
    err = storage.CreateSchema(db)
    require.NoError(t, err)

    // Insert test data
    insertTestFunctions(t, db)
    insertTestTypes(t, db)
    insertTestCalls(t, db)
    insertTestRelationships(t, db)

    return db
}

func TestSQLSearcher_QueryCallers_Depth1(t *testing.T) { ... }
func TestSQLSearcher_QueryCallers_Recursive(t *testing.T) { ... }
func TestSQLSearcher_QueryCallees_Depth1(t *testing.T) { ... }
func TestSQLSearcher_QueryCallees_Recursive(t *testing.T) { ... }
func TestSQLSearcher_QueryDependencies(t *testing.T) { ... }
func TestSQLSearcher_QueryDependents(t *testing.T) { ... }
func TestSQLSearcher_QueryImplementations(t *testing.T) { ... }
func TestSQLSearcher_QueryTypeUsages(t *testing.T) { ... }
func TestSQLSearcher_QueryPath(t *testing.T) { ... }
func TestSQLSearcher_QueryImpact(t *testing.T) { ... }
func TestSQLSearcher_CycleDetection(t *testing.T) { ... }
func TestSQLSearcher_ScopeFiltering(t *testing.T) { ... }
func TestSQLSearcher_ExcludePatterns(t *testing.T) { ... }
func TestSQLSearcher_ContextInjection(t *testing.T) { ... }
```

### Test Data Strategy

Use SQLite in-memory database with:
- 10-20 test functions with various calling patterns
- 3-5 types with implementation relationships
- Cyclic call graphs (A → B → C → A)
- Multiple packages for dependency tests

### Integration Tests

Test with actual MCP server:
1. Index sample project
2. Query via MCP `cortex_graph` tool
3. Validate response format matches old searcher
4. Compare results for correctness

## Migration Path

### Phase 1: Implementation (This Spec)
- Create `internal/graph/searcher_sql.go`
- Implement all query operations
- Write comprehensive tests
- Keep old searcher unchanged

### Phase 2: Integration
- Update MCP server initialization to use `NewSQLSearcher(db, rootDir)`
- Update `internal/mcp/graph_tool.go` description to document SQL LIKE patterns
- Test in production

### Phase 3: Deprecation
- Keep old searcher for 1-2 releases as fallback
- Monitor for issues
- Document SQL searcher as recommended

### Phase 4: Cleanup
- Remove old searcher after validation period
- Delete JSON graph loading code
- Update documentation

## Non-Goals

This spec explicitly does NOT include:

- **Repository pattern**: Query builders are sufficient; repos can be added later if duplication becomes painful
- **ORM framework**: Direct SQL with squirrel provides sufficient type safety
- **Cross-language graph extraction**: Continues to support Go only (TypeScript/Python extraction is separate work)
- **Recursive graph queries via CTE for path**: Use simpler BFS app-side for debuggability
- **Query result caching**: SQLite is fast enough (<20ms); caching adds complexity (no file cache, no in-memory graph cache)
- **Explicit cycle detection**: Depth limits (max 6) naturally bound recursion; DISTINCT deduplicates any duplicate visits
- **Optimistic incremental inference**: Re-infer all relationships on type changes (separate concern, already handled)
- **Breaking changes to API**: Keep `Node` response format and `Searcher` interface unchanged

## Implementation Checklist

### Phase 0: Schema Changes
- [x] Add `start_pos` and `end_pos` columns to `functions` table (with migration)
- [x] Add `start_pos` and `end_pos` columns to `types` table (with migration)
- [x] Add nullable `content TEXT` column to `files` table (with migration)
- [x] Drop and recreate `files_fts` as external content FTS (with migration)
- [x] Create conditional triggers for FTS sync (INSERT, UPDATE, DELETE) (with tests)
- [x] Update schema version to "2.1" in `cache_metadata` (with tests)
- [x] Verify triggers work correctly for text files, binary files, and transitions (with tests)

### Phase 1: Domain Model Updates
- [x] Add `StartPos` and `EndPos` fields to `Function` struct in `internal/graph/types.go` (with tests)
- [x] Add `StartPos` and `EndPos` fields to `Type` struct in `internal/graph/types.go` (with tests)
- [x] Add `StartPos` and `EndPos` fields to `Node` struct in `internal/graph/searcher.go` (with tests)

### Phase 2: Graph Extraction Updates
- [x] Update `internal/graph/extractor.go` to capture byte positions from `token.FileSet` (with tests)
- [x] Ensure `*token.FileSet` is passed through entire extraction pipeline (with tests)
- [x] Verify positions captured correctly for functions and types (with tests)
- [x] Test edge cases (nested functions, type aliases, embedded types) (with tests)

### Phase 3: Storage Layer Updates
- [x] Update `internal/storage/graph_writer.go` to write byte positions (with tests)
- [x] Add `WriteFile()` method to `internal/storage/file_writer.go` (with tests)
- [x] Update `GraphWriter.WriteFunctions()` to include `start_pos`/`end_pos` (with tests)
- [x] Update `GraphWriter.WriteTypes()` to include `start_pos`/`end_pos` (with tests)
- [x] Test FileWriter with text files, binary files, and empty files (with tests)
- [x] Test FileWriter transitions (binary→text, text→binary) (with tests)

### Phase 4: Processor Updates
- [x] Update `internal/indexer/processor.go` to use `FileWriter.WriteFile()` (with tests)
- [x] Remove calls to `WriteFileStats()` and `WriteFileContent()` (with tests)
- [x] Simplify binary file detection (single check, pass nil content) (with tests)

### Phase 5: Context Extractor Implementation
- [x] Create `internal/graph/context.go` with `ContextExtractor` struct (with tests)
- [x] Implement `NewContextExtractor(db *sql.DB)` constructor (with tests)
- [x] Implement `ExtractContext()` with position-based extraction (with tests)
- [x] Implement `LineRange` and `ByteRange` helper types (with tests)
- [x] Test with various context line counts (0, 3, 10) (with tests)
- [x] Test edge cases (file start, file end, multi-byte characters) (with tests)

### Phase 6: SQL Searcher Core Structure
- [x] Create `internal/graph/searcher_sql.go` with `sqlSearcher` struct (with tests)
- [x] Implement `NewSQLSearcher()` constructor with `ContextExtractor` setup (with tests)
- [x] Implement interface methods: `Query()` with transaction wrapping (with tests)
- [x] Implement `Reload()` (no-op) and `Close()` (no-op) (with tests)
- [x] Add depth limit validation (default 3, max 6) (with tests)

### Phase 7: Query Builders
- [x] Implement `buildCallersSQL()` for depth 1 and recursive (with tests)
- [x] Implement `buildCalleesSQL()` for depth 1 and recursive (with tests)
- [x] Implement dependency/dependent query builders (with tests)
- [x] Implement implementations/type_usages query builders (with tests)
- [x] Verify all queries use `start_pos`/`end_pos` in SELECT clauses (with tests)

### Phase 8: Shared Execution
- [x] Implement `executeFunctionQuery()` for callers/callees/path (with tests)
- [x] Implement `executeTypeQuery()` for implementations (with tests)
- [x] Implement `executeDependencyQuery()` for dependencies/dependents (with tests)
- [x] Update `scanFunctionRow()` to read `start_pos`/`end_pos` (with tests)
- [x] Update `scanTypeRow()` to read `start_pos`/`end_pos` (with tests)

### Phase 9: Special Operations
- [x] Implement `queryPath()` with optimized subgraph loading and BFS (with tests)
- [x] Implement `loadReachableEdges()` helper for path queries (with tests)
- [x] Implement `bfsPath()` in-memory path finding (with tests)
- [x] Implement `queryImpact()` three-phase analysis (with tests)

### Phase 10: Helpers
- [x] Implement `applyFilters()` for SQL LIKE patterns (with tests)
- [x] Integrate `ContextExtractor.ExtractContext()` into query results (with tests)
- [x] Test context extraction with various depth limits (with tests)

### Phase 11: Integration
- [x] Update MCP server initialization to use `NewSQLSearcher(db, rootDir)` (with tests)
- [x] Update `internal/mcp/graph_tool.go` description for SQL LIKE patterns (with tests)
- [x] Update `internal/graph/searcher.go` interface documentation (with tests)
- [x] Integration tests with real MCP server (with tests)
- [x] Verify context extraction works end-to-end via MCP (with tests)

### Phase 12: Validation & Cleanup
- [x] Performance benchmarks vs old searcher (all operations, various depths)
- [x] Cross-validation of results with old searcher (spot check correctness - validated via integration tests)
- [x] Memory profiling to confirm <5MB footprint (no caching overhead)
- [x] Verify schema migration from 2.0 → 2.1 works correctly
- [x] Deprecate old FileWriter methods (`WriteFileStats`, `WriteFileContent`)
- [x] Document migration path for users
