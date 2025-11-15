---
status: archived
started_at: 2025-11-05T00:00:00Z
completed_at: 2025-11-08T00:00:00Z
dependencies: [indexer-refactor]
---

# Graph Update Architecture Refactor

## Purpose

The current graph building architecture uses an in-memory GraphData abstraction with generic nodes and edges, inherited from the original JSON-based storage. With the migration to SQLite, this abstraction adds unnecessary complexity. This refactor shifts to a SQL-first architecture where the database schema IS the graph data model, eliminating the intermediate graph abstraction and enabling simpler incremental updates.

## Core Concept

**Input**: Changed files detected by ChangeDetector
**Process**: Extract → Write to SQL tables → Infer relationships (in-memory) → Write relationships
**Output**: Updated SQLite tables ready for cortex_graph queries

**Key insight**: SQLite can execute graph queries (callers, callees, dependencies, type_usages) with simple JOINs in <20ms. An in-memory graph structure is unnecessary overhead.

## Technology Stack

- **Language**: Go 1.25+
- **Database**: SQLite with existing schema (10 tables)
- **Query Builder**: Squirrel (existing, used by other components)
- **Parser**: tree-sitter (unchanged)
- **Pattern**: Domain models + query helpers (NOT full repository pattern)

## Current vs Proposed Architecture

### Current (Being Removed)

```
Tree-sitter → graph.Extractor
              ↓
          FileGraphData (nodes/edges)
              ↓
          graph.Builder.BuildIncremental()
              ↓
          GraphData (nodes/edges in memory)
              ↓
          GraphWriter (converts to SQL)
              ↓
          SQLite tables
```

**Problems:**
- Two representations: graph (nodes/edges) + SQL (tables)
- Conversion overhead between representations
- GraphData holds entire graph in memory even for small updates
- Complex incremental logic in graph.Builder
- Extractor outputs generic nodes instead of domain-specific data

### Proposed (SQL-First)

```
Tree-sitter → graph.Extractor
              ↓
          CodeStructure (schema-aligned structs)
              ↓
          GraphUpdater.Update()
              ↓
          ├─▶ Delete old rows (by file_path, CASCADE)
              ├─▶ Insert new rows (functions, types, imports, etc.)
              └─▶ InterfaceInferencer.InferImplementations()
                  ├─▶ Load interfaces/structs (SQL)
                  ├─▶ Compare in memory
                  └─▶ Write relationships (SQL)
              ↓
          SQLite tables (ready for queries)
```

**Benefits:**
- Single representation: SQL schema is the graph
- Direct extraction to domain models
- In-memory computation only during inference (short-lived)
- Simpler incremental updates (delete by file_path + re-insert)
- No conversion between graph and SQL

## Architecture

### 1. Domain Models Layer

Schema-aligned Go structs that mirror SQL tables:

```go
// internal/storage/models.go

type Function struct {
    ID              string
    FilePath        string
    ModulePath      string
    Name            string
    StartLine       int
    EndLine         int
    IsExported      bool
    IsMethod        bool
    ReceiverTypeID  *string
    ReceiverTypeName *string
    Parameters      []FunctionParameter  // Joined from function_parameters
    ReturnValues    []FunctionParameter  // Filtered by is_return
}

type Type struct {
    ID          string
    FilePath    string
    ModulePath  string
    Name        string
    Kind        string  // interface, struct, class, enum
    StartLine   int
    EndLine     int
    IsExported  bool
    Fields      []TypeField  // Joined from type_fields
    Methods     []TypeField  // Filtered by is_method
}

type TypeRelationship struct {
    FromTypeID       string
    ToTypeID         string
    RelationshipType string  // implements, embeds, extends
    SourceFilePath   string
    SourceLine       int
}

type FunctionCall struct {
    CallerFunctionID string
    CalleeFunctionID *string  // NULL for external calls
    CalleeName       string
    SourceFilePath   string
    CallLine         int
    CallColumn       *int
}

// ... other domain models for TypeField, FunctionParameter, Import
```

**Design decision**: These are NOT ORM models. They're lightweight structs used for data transfer between SQL and business logic.

### 2. Query Helpers Layer

Reusable SQL query functions that return fully-hydrated domain models:

```go
// internal/storage/query_helpers.go

// LoadInterfacesWithMethods loads all interface types with their method signatures.
// Used by interface inference to determine which structs implement which interfaces.
func LoadInterfacesWithMethods(db squirrel.BaseRunner) ([]Type, error) {
    query := squirrel.Select(
        "t.type_id", "t.name", "t.file_path", "t.module_path",
        "tf.name as method_name", "tf.param_count", "tf.return_count",
    ).
    From("types t").
    LeftJoin("type_fields tf ON t.type_id = tf.type_id AND tf.is_method = 1").
    Where("t.kind = ?", "interface").
    OrderBy("t.type_id", "tf.position")

    // Execute and scan into []Type with grouped methods
    return scanTypesWithFields(query.RunWith(db).Query())
}

// LoadStructsWithMethods loads all struct types with their methods (from functions table).
// Methods are identified by non-NULL receiver_type_id.
func LoadStructsWithMethods(db squirrel.BaseRunner) ([]Type, error) {
    query := squirrel.Select(
        "t.type_id", "t.name", "t.file_path", "t.module_path",
        "f.name as method_name", "f.param_count", "f.return_count",
    ).
    From("types t").
    LeftJoin("functions f ON t.type_id = f.receiver_type_id").
    Where("t.kind = ?", "struct").
    OrderBy("t.type_id", "f.name")

    return scanTypesWithMethods(query.RunWith(db).Query())
}

// LoadEmbeddedFields finds all type embedding relationships.
// In Go, embedded fields have empty names in the AST.
func LoadEmbeddedFields(db squirrel.BaseRunner) ([]TypeRelationship, error) {
    query := squirrel.Select(
        "type_id", "field_type",
    ).
    From("type_fields").
    Where("name = ?", "").  // Embedded fields have no name
    Where("is_method = ?", false)

    // Scan and convert to TypeRelationship with relationship_type = "embeds"
    return scanEmbedRelationships(query.RunWith(db).Query())
}

// BulkInsertRelationships writes type relationships in a single transaction.
func BulkInsertRelationships(tx *sql.Tx, rels []TypeRelationship) error {
    stmt, err := tx.Prepare(`
        INSERT INTO type_relationships
        (relationship_id, from_type_id, to_type_id, relationship_type, source_file_path, source_line)
        VALUES (?, ?, ?, ?, ?, ?)
    `)
    if err != nil {
        return fmt.Errorf("prepare statement: %w", err)
    }
    defer stmt.Close()

    for _, rel := range rels {
        _, err := stmt.Exec(
            generateRelationshipID(),
            rel.FromTypeID,
            rel.ToTypeID,
            rel.RelationshipType,
            rel.SourceFilePath,
            rel.SourceLine,
        )
        if err != nil {
            return fmt.Errorf("insert relationship: %w", err)
        }
    }

    return nil
}
```

**Design decision**: Query helpers are functions, not methods on a repository struct. This keeps them simple and composable without adding interface ceremony. If we later find we need repositories, these helpers can be wrapped in repo methods.

### 3. Extractor Refactor

Change extractor output from generic nodes/edges to schema-aligned domain structs:

```go
// internal/graph/extractor.go

// OLD (being removed):
type FileGraphData struct {
    Nodes []Node  // Generic node with type discriminator
    Edges []Edge  // Generic edge with type discriminator
}

// NEW (schema-aligned):
type CodeStructure struct {
    Functions      []Function           // Maps to functions table
    Types          []Type               // Maps to types table
    TypeFields     []TypeField          // Maps to type_fields table
    FunctionParams []FunctionParameter  // Maps to function_parameters table
    FunctionCalls  []FunctionCall       // Maps to function_calls table
    Imports        []Import             // Maps to imports table
}

type Extractor struct {
    rootDir string
}

func (e *Extractor) ExtractFile(filePath string) (*CodeStructure, error) {
    // Parse with tree-sitter (unchanged)
    ast := e.parseFile(filePath)

    data := &CodeStructure{}

    // Walk AST and populate domain structs directly
    e.extractFunctions(ast, data)
    e.extractTypes(ast, data)
    e.extractImports(ast, data)
    e.extractFunctionCalls(ast, data)

    return data, nil
}
```

**Design decision**: Extractor outputs domain models directly instead of generic graph nodes. This eliminates the conversion step and makes the extraction logic clearer (no type discriminators needed).

### 4. Interface Inference Service

Hybrid approach: Bulk load from SQL, compute in-memory, bulk write back:

```go
// internal/graph/inferencer.go (NEW FILE)

type InterfaceInferencer struct {
    db *sql.DB
}

func NewInterfaceInferencer(db *sql.DB) *InterfaceInferencer {
    return &InterfaceInferencer{db: db}
}

// InferImplementations determines which structs implement which interfaces.
// Uses hybrid approach: SQL load → in-memory comparison → SQL write.
// Typical performance: 15-30ms for large projects (1000 interfaces, 5000 structs).
func (inf *InterfaceInferencer) InferImplementations(ctx context.Context) error {
    // 1. Bulk load interfaces with methods (~1-5ms for 1000 interfaces)
    interfaces, err := storage.LoadInterfacesWithMethods(inf.db)
    if err != nil {
        return fmt.Errorf("load interfaces: %w", err)
    }

    // 2. Bulk load structs with methods (~5-10ms for 5000 structs)
    structs, err := storage.LoadStructsWithMethods(inf.db)
    if err != nil {
        return fmt.Errorf("load structs: %w", err)
    }

    // 3. Bulk load embedded fields (becomes "embeds" relationships)
    embeds, err := storage.LoadEmbeddedFields(inf.db)
    if err != nil {
        return fmt.Errorf("load embeds: %w", err)
    }

    // 4. In-memory comparison (~5-10ms for 25M comparisons)
    implements := inf.findImplementations(interfaces, structs)

    // 5. Combine implements + embeds
    allRelationships := append(implements, embeds...)

    // 6. Bulk write in transaction (~5-10ms for 10K relationships)
    tx, err := inf.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }
    defer tx.Rollback()

    // Clear old inferred relationships
    _, err = tx.Exec(`
        DELETE FROM type_relationships
        WHERE relationship_type IN ('implements', 'embeds')
    `)
    if err != nil {
        return fmt.Errorf("clear old relationships: %w", err)
    }

    // Insert new relationships
    if err := storage.BulkInsertRelationships(tx, allRelationships); err != nil {
        return fmt.Errorf("insert relationships: %w", err)
    }

    if err := tx.Commit(); err != nil {
        return fmt.Errorf("commit: %w", err)
    }

    log.Printf("✓ Inferred %d type relationships", len(allRelationships))
    return nil
}

// findImplementations compares structs against interfaces to find implementations.
// Uses map-based lookup for O(1) method matching.
func (inf *InterfaceInferencer) findImplementations(
    interfaces []storage.Type,
    structs []storage.Type,
) []storage.TypeRelationship {
    relationships := []storage.TypeRelationship{}

    for _, iface := range interfaces {
        // Build method signature map for interface
        requiredMethods := make(map[string]MethodSignature)
        for _, method := range iface.Methods {
            requiredMethods[method.Name] = MethodSignature{
                ParamCount:  method.ParamCount,
                ReturnCount: method.ReturnCount,
            }
        }

        // Check each struct
        for _, strct := range structs {
            if inf.implementsInterface(strct, requiredMethods) {
                relationships = append(relationships, storage.TypeRelationship{
                    FromTypeID:       strct.ID,
                    ToTypeID:         iface.ID,
                    RelationshipType: "implements",
                    SourceFilePath:   strct.FilePath,
                    SourceLine:       strct.StartLine,
                })
            }
        }
    }

    return relationships
}

// implementsInterface checks if a struct has all required methods.
func (inf *InterfaceInferencer) implementsInterface(
    strct storage.Type,
    requiredMethods map[string]MethodSignature,
) bool {
    if len(requiredMethods) == 0 {
        return true  // Empty interface matches everything
    }

    // Build struct's method map
    structMethods := make(map[string]MethodSignature)
    for _, method := range strct.Methods {
        structMethods[method.Name] = MethodSignature{
            ParamCount:  method.ParamCount,
            ReturnCount: method.ReturnCount,
        }
    }

    // Check if struct has all required methods
    for name, required := range requiredMethods {
        actual, exists := structMethods[name]
        if !exists {
            return false
        }

        if !signaturesMatch(required, actual) {
            return false
        }
    }

    return true
}

type MethodSignature struct {
    ParamCount  int
    ReturnCount int
    // Future: Add param types and return types for strict matching
}

func signaturesMatch(a, b MethodSignature) bool {
    return a.ParamCount == b.ParamCount && a.ReturnCount == b.ReturnCount
}
```

**Design decision**: Full re-inference when types change, rather than incremental tracking. Simpler logic, still fast enough (<30ms). Optimization can come later if needed.

### 5. Graph Updater Coordinator

Replaces the GraphBuilder interface and graphBuilderAdapter:

```go
// internal/indexer/graph_updater.go (NEW FILE)

type GraphUpdater struct {
    db         *sql.DB
    extractor  *graph.Extractor
    inferencer *graph.InterfaceInferencer
    rootDir    string
}

func NewGraphUpdater(db *sql.DB, rootDir string) *GraphUpdater {
    return &GraphUpdater{
        db:         db,
        extractor:  graph.NewExtractor(rootDir),
        inferencer: graph.NewInterfaceInferencer(db),
        rootDir:    rootDir,
    }
}

// Update performs incremental graph updates based on file changes.
// Returns error for logging (failures should not block indexing).
func (g *GraphUpdater) Update(ctx context.Context, changes *ChangeSet) error {
    hasTypeChanges := false

    // 1. Process deletions (CASCADE handles related data)
    for _, file := range changes.Deleted {
        if err := g.deleteCodeStructure(ctx, file); err != nil {
            return fmt.Errorf("delete %s: %w", file, err)
        }
        hasTypeChanges = true  // Deleted types affect inference
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
        data, err := g.extractor.ExtractFile(absPath)
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
func (g *GraphUpdater) insertCodeStructure(ctx context.Context, file string, data *graph.CodeStructure) error {
    tx, err := g.db.BeginTx(ctx, nil)
    if err != nil {
        return fmt.Errorf("begin transaction: %w", err)
    }
    defer tx.Rollback()

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
func (g *GraphUpdater) insertTypes(tx *sql.Tx, types []storage.Type, fields []storage.TypeField) error {
    // Use squirrel for cleaner SQL generation
    // Insert types...
    // Insert type_fields...
    return nil
}

// ... other insert methods
```

**Design decision**: GraphUpdater owns the complete update lifecycle (extract → delete → insert → infer). The indexer just calls `Update()` and logs warnings on failure.

### 6. Integration with Indexer V2

Minimal changes to existing indexer:

```go
// internal/indexer/indexer_v2.go (MODIFY)

type IndexerV2 struct {
    // ... existing fields ...

    // OLD (remove):
    // graphBuilder GraphBuilder

    // NEW:
    graphUpdater *GraphUpdater
}

func NewIndexerV2(
    rootDir string,
    db *sql.DB,
    processor *Processor,
    changeDetector *ChangeDetector,
    branchSync *BranchSynchronizer,
) *IndexerV2 {
    return &IndexerV2{
        // ... existing initialization ...

        graphUpdater: NewGraphUpdater(db, rootDir),
    }
}

func (idx *IndexerV2) Run(ctx context.Context) error {
    // ... existing chunk processing logic ...

    // 5. Update graph (incremental based on changes)
    if idx.graphUpdater != nil {
        if err := idx.graphUpdater.Update(ctx, changes); err != nil {
            log.Printf("Warning: graph update failed: %v\n", err)
            // Don't fail indexing if graph fails (supplementary data)
        }
    }

    return nil
}
```

**Design decision**: Graph updates are fire-and-forget. Failures log warnings but don't block indexing, since graph data is supplementary to core search functionality.

## Data Flow Example

### Scenario: User modifies `server.go`

```
1. ChangeDetector identifies: Modified = ["internal/server/server.go"]

2. GraphUpdater.Update() called:
   ├─▶ Extract tree-sitter AST from server.go
   ├─▶ Build CodeStructure:
   │   - 5 functions (including 2 methods on Server struct)
   │   - 1 type (Server struct)
   │   - 3 type fields
   │   - 12 function calls
   │   - 4 imports
   ├─▶ DELETE FROM types WHERE file_path = 'internal/server/server.go'
   │   └─▶ CASCADE deletes type_fields, functions with receiver_type_id
   ├─▶ DELETE FROM functions WHERE file_path = 'internal/server/server.go'
   │   └─▶ CASCADE deletes function_parameters, function_calls
   ├─▶ DELETE FROM imports WHERE file_path = 'internal/server/server.go'
   ├─▶ INSERT new data (types, functions, calls, imports)
   └─▶ hasTypeChanges = true → trigger inference

3. InterfaceInferencer.InferImplementations():
   ├─▶ LoadInterfacesWithMethods() - SQL query returns 47 interfaces
   ├─▶ LoadStructsWithMethods() - SQL query returns 213 structs
   ├─▶ LoadEmbeddedFields() - SQL query returns 18 embeds
   ├─▶ In-memory comparison: 47 × 213 = 10,011 checks (~5ms)
   ├─▶ Found: 89 implements relationships + 18 embeds = 107 total
   ├─▶ DELETE old relationships WHERE type IN ('implements', 'embeds')
   └─▶ INSERT 107 new relationships

4. Total time: ~25-40ms

5. Next cortex_graph query sees updated data immediately
```

## Performance Characteristics

### Indexing Performance

| Operation | Time (typical) | Notes |
|-----------|---------------|-------|
| Extract single file | 5-20ms | Tree-sitter parsing |
| Delete old file data | <1ms | 3 DELETE statements with CASCADE |
| Insert new file data | 2-5ms | Single transaction, ~50 rows |
| Load interfaces (1000) | 1-5ms | Single SQL query with JOIN |
| Load structs (5000) | 5-10ms | Single SQL query with JOIN |
| In-memory comparison | 5-10ms | 25M comparisons with map lookups |
| Write relationships (10K) | 5-10ms | Prepared statement in transaction |
| **Total per file change** | **20-50ms** | End-to-end including inference |

### Query Performance (Unchanged)

Graph queries continue to execute as fast SQL JOINs:

| Query Type | Time | Example |
|------------|------|---------|
| Find callers | <5ms | `JOIN function_calls ON ...` |
| Find callees | <5ms | `JOIN function_calls ON ...` |
| Find dependencies | <5ms | `SELECT FROM imports WHERE ...` |
| Find dependents | <10ms | `SELECT FROM imports WHERE import_path LIKE ...` |
| Find type usages | <10ms | Multiple JOINs (params, fields, receivers) |

## Migration Path

### Phase 1: Create New Components (No Breaking Changes)

1. Add `internal/storage/models.go` with domain structs
2. Add `internal/storage/query_helpers.go` with SQL functions
3. Add `internal/graph/inferencer.go` with inference service
4. Add `internal/indexer/graph_updater.go` with coordinator

**Status**: Existing code continues to work, new code unused

### Phase 2: Refactor Extractor

1. Modify `internal/graph/extractor.go` output to ExtractedData
2. Update extractor tests to use new output format

**Status**: Extractor changed but not yet wired into indexer

### Phase 3: Wire Up Graph Updater

1. Remove `graphBuilder GraphBuilder` from IndexerV2
2. Add `graphUpdater *GraphUpdater` to IndexerV2
3. Update `NewIndexerV2()` constructor
4. Update `Run()` method to call `graphUpdater.Update()`
5. Update CLI initialization in `internal/cli/index.go`

**Status**: New architecture active, old code removed

### Phase 4: Remove Old Code

1. Delete `internal/graph/builder.go` (GraphData, BuildIncremental)
2. Delete graphBuilderAdapter from `indexer_v2.go`
3. Delete unused node/edge types
4. Update tests

**Status**: Migration complete

## Testing Strategy

### Unit Tests

1. **Query Helpers** (`storage/query_helpers_test.go`)
   - LoadInterfacesWithMethods with sample data
   - LoadStructsWithMethods with various receiver types
   - LoadEmbeddedFields edge cases

2. **Interface Inferencer** (`graph/inferencer_test.go`)
   - Empty interface matches all structs
   - Exact method match (name + param count)
   - Missing method (no match)
   - Extra methods on struct (still matches)
   - Embedded field detection

3. **Graph Updater** (`indexer/graph_updater_test.go`)
   - Single file add/modify/delete
   - Multiple files in one update
   - Re-inference trigger logic
   - Error handling (extraction failure, SQL error)

### Integration Tests

1. **End-to-End Indexing** (`indexer/indexer_v2_integration_test.go`)
   - Index sample Go project
   - Verify all graph tables populated
   - Query cortex_graph and validate results
   - Modify file and verify incremental update
   - Verify interface implementations correct

2. **Cross-File Relationships** (`graph/integration_test.go`)
   - Function in fileA calls function in fileB
   - Type in fileA embeds type in fileB
   - Struct in fileA implements interface in fileB
   - Verify CASCADE deletes work correctly

### Performance Tests

Benchmark key operations:

```go
BenchmarkLoadInterfaces_1000     // Should be <5ms
BenchmarkLoadStructs_5000        // Should be <10ms
BenchmarkInference_Large         // Should be <30ms total
BenchmarkUpdateSingleFile        // Should be <50ms
```

## Non-Goals

This refactor explicitly does NOT include:

- **Repository pattern**: Query helpers suffice for current scale. Repos can be added later if duplication becomes painful.
- **ORM framework**: Direct SQL with squirrel provides sufficient type safety and composability.
- **Cross-language graph extraction**: Continues to support Go only. TypeScript/Python extraction is separate future work.
- **Recursive graph queries**: Support for transitive relationship queries (e.g., "find all indirect implementers") deferred to future MCP tool enhancement.
- **Query result caching**: SQLite is fast enough (<20ms). Caching adds complexity without clear benefit.
- **Optimistic incremental inference**: Re-infer all relationships on type changes. Scoped re-inference is future optimization if needed.

## Implementation Checklist

### Phase 1: Domain Models & Query Helpers
- ✅ Create `internal/storage/models.go` with domain structs (with tests)
- ✅ Create `internal/storage/query_helpers.go` with SQL functions (with tests)

### Phase 2: Extractor Refactor
- ✅ Modify `internal/graph/extractor.go` to output CodeStructure (with tests)
- ✅ Update extractor to populate schema-aligned structs (with tests)

### Phase 3: Inference Service
- ✅ Create `internal/storage/inferencer.go` with InterfaceInferencer (with tests)
- ✅ Implement hybrid in-memory inference algorithm (with tests)
- ✅ Add embed detection and relationship creation (with tests)

### Phase 4: Graph Updater
- ✅ Create `internal/indexer/graph_updater.go` coordinator (with tests)
- ✅ Implement file-level delete/insert logic (with tests)
- ✅ Wire up extractor and inferencer (with tests)

### Phase 5: Integration
- ✅ Remove GraphBuilder interface from `indexer_v2.go`
- ✅ Add GraphUpdater to IndexerV2 (with tests)
- ✅ Update CLI initialization in `internal/cli/index.go`
- ✅ Update integration tests for new architecture

### Phase 6: Cleanup
- [ ] Delete `internal/graph/builder.go` (old GraphData abstraction)
- [ ] Delete graphBuilderAdapter stub
- [ ] Remove unused node/edge types
- [ ] Update documentation in CLAUDE.md

## Open Questions

None. Architecture decisions finalized through collaborative design discussion.

## References

- **Indexer Refactor Spec**: `specs/2025-11-04_indexer-refactor.md` - Parent spec defining ChangeSet and incremental indexing
- **Schema**: `internal/storage/schema.go` - SQL table definitions this refactor builds upon
- **Testing Strategy**: `docs/testing-strategy.md` - Test-as-you-go approach for implementation
