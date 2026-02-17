# Test Specifications

## Unit Tests

### CanopyProvider (internal/indexer/canopy_provider_test.go)

**NewCanopyProvider:**
- Given valid project dir, db path, and scripts FS, should create provider without error
- Given invalid db path (unwritable directory), should return error
- Given invalid project dir that does not exist, should return error

**Index:**
- Given a project directory with Go files, should call engine.IndexDirectory and engine.Resolve without error
- Given cancellation via context, should stop and return context error
- Given a project with multiple languages, should index all supported files
- Given unchanged files since last run, canopy should skip them (content hash diffing) and return quickly

**ScriptsChanged:**
- Given first run (no stored hash), should return true
- Given same scripts as previous run, should return false
- Given modified scripts, should return true

**Close:**
- Should close engine without error
- Should be safe to call multiple times

### CanopySearcher -- Target Resolution (internal/graph/canopy_searcher_test.go)

**resolveTarget:**
- Given `"Provider"`, should find exported symbol named "Provider" via SearchSymbols
- Given `"embed.Provider"`, should find symbol "Provider" in files under path containing "embed"
- Given `"Actor.Start"`, should find method "Start" on type "Actor"
- Given `"internal/mcp/server.go:42:10"`, should parse file:line:col and call SymbolAt
- Given `"nonexistent.Symbol"`, should return descriptive error
- Given `""`, should return error (empty target)

**resolveFileTarget:**
- Given `"internal/mcp"`, should find files with path prefix "internal/mcp/" and return first file ID
- Given `"nonexistent/path"`, should return error (no matching files)

### CanopySearcher -- Callers/Callees (internal/graph/canopy_searcher_test.go)

**queryCallers (via TransitiveCallers):**
- Given a function with 3 direct callers and depth=1, should return 3 results each with `Symbol SymbolResult` populated
- Given a function with no callers, should return empty results (nil CallGraph)
- Given max_results=2 and 5 callers, should return only 2 results
- Given A calls B calls C, querying callers of C at depth 2 should return both B (depth 1) and A (depth 2)
- Given circular call chain A->B->A, should terminate (canopy's BFS uses visited set)
- Each `CallGraphNode` should have `Depth` set correctly (1 for direct, 2 for transitive)

**queryCallees (via TransitiveCallees):**
- Given a function that calls 4 other functions and depth=1, should return 4 results
- Given a function with no callees, should return empty results
- Given A calls B, B calls C, querying callees of A at depth 2 should return B (depth 1) and C (depth 2)

**callGraphNodeToResult:**
- Given a CallGraphNode with Symbol, should map name, kind, file, lines to QueryResult.Node
- Given IncludeContext=true, should call extractContext and include snippet

### CanopySearcher -- Dependencies/Dependents

**queryDependencies:**
- Given a file with 5 imports, should return 5 dependency results
- Given a path prefix target, should find file and return its imports
- Given a file with no imports, should return empty results

**queryDependents:**
- Given a package imported by 3 files, should return 3 dependent results
- Given a package with suffix matching (e.g., "util" matching "github.com/x/util"), should find dependents
- Given an unimported package, should return empty results

### CanopySearcher -- Type Usages / References

**queryTypeUsages:**
- Given an interface with 10 references, should return all reference locations
- Given a struct used in function parameters, should return those functions

**queryReferences:**
- Given a function with 5 references across 3 files, should return 5 locations
- Given an unreferenced symbol, should return empty results

### CanopySearcher -- Implementations

**queryImplementations:**
- Given an interface implemented by 3 structs, should return 3 results
- Given a non-interface type, should return empty results

### CanopySearcher -- Definition

**queryDefinition:**
- Given `"file.go:10:5"` pointing to a function call, should return the definition location
- Given `"file.go:1:0"` pointing to a package declaration, should return nil (no definition to go to)
- Given invalid file:line:col format, should return error

### CanopySearcher -- Impact

**queryImpact:**
- Given an interface method, should return implementations (must_update) + direct callers (must_update) + transitive callers (review_needed)
- Given a leaf function with no callers, should return empty or minimal results
- Should set ImpactSummary counts correctly

### CanopySearcher -- Path

**queryPath:**
- Given A->B->C call chain, path from A (target) to C (to) should return [A, B, C]
- Given no path exists, should return empty results with suggestion message
- Given source == destination, should return single-node path
- Given missing `to` field, should return error indicating `to` is required for path operation

### CanopySearcher -- Scope and ExcludePatterns

**Scope handling:**
- Given `Scope: "internal/%"`, results should only include nodes with file paths matching `internal/%`
- Given `Scope: "%_test.go"`, results should only include nodes in test files
- Given empty `Scope`, all results should be returned (no filtering)

**ExcludePatterns handling:**
- Given `ExcludePatterns: ["%_test.go"]`, results should exclude nodes in test files
- Given `ExcludePatterns: ["vendor/%", "%_test.go"]`, results should exclude both vendor and test files
- Given empty `ExcludePatterns`, all results should be returned (no filtering)
- Given both `Scope` and `ExcludePatterns`, both filters should apply (scope first, then exclude)

### CanopySearcher -- Advanced Operations

**querySymbols:**
- Given filter with kinds=["interface"], should return only interfaces
- Given filter with visibility="public", should return only exported symbols
- Given filter with path_prefix="internal/", should return only symbols in internal/
- Given pagination (offset=10, limit=5), should return correct page
- Given ref_count_min=5, should return only symbols with 5+ references
- Given ref_count_max=0, should return only unreferenced symbols

**querySearch:**
- Given pattern "Get*", should return symbols matching glob (GetStatus, GetConfig, etc.)
- Given pattern "*Provider" with kinds=["interface"], should return only interface symbols ending in Provider
- Given empty pattern, should return all symbols (same as querySymbols)

**querySummary:**
- Should return language stats (file count, line count, symbol count per language)
- Should return package count
- Given topN=5, should return top 5 symbols by external ref count

**queryPackageSummary:**
- Given "internal/mcp" as target, should return package info with exported symbols, kind counts, deps, dependents
- Given nonexistent package path, should return error

**queryDetail:**
- Given a function symbol ID, should return SymbolDetail with parameters and return values
- Given a struct symbol ID, should return SymbolDetail with members (fields and methods)
- Given a generic type, should return TypeParams with constraints
- Given file:line:col target, should use SymbolDetailAt and return correct detail
- Given nonexistent symbol ID, should return error

**queryTypeHierarchy:**
- Given an interface, should return ImplementedBy with concrete types
- Given a struct implementing interfaces, should return Implements with those interfaces
- Given a struct embedding another, should show Composes relationship
- Given a type with extension methods, should include Extensions

**queryScope:**
- Given file:line:col inside a function body, should return scope chain [block, function, file]
- Given file:line:col at file level, should return single file scope
- Given invalid file:line:col, should return error

**queryUnusedSymbols:**
- Given a project with unreferenced functions, should return those functions
- Should exclude package/module/namespace kinds (canopy convention)
- Should respect SymbolFilter (kinds, visibility, path_prefix)
- Should support pagination

**queryHotspots:**
- Given topN=3, should return 3 most-referenced symbols sorted by external_ref_count desc
- Each result should include CallerCount (fan-in) and CalleeCount (fan-out)
- Given topN=0, should return empty list

**queryCircularDependencies:**
- Given packages with A→B→C→A cycle, should return [["A", "B", "C", "A"]]
- Given no cycles, should return empty list
- Should detect multiple independent cycles

**queryDependencyGraph:**
- Should return all packages as PackageNodeInfo with file/line counts
- Should return all edges as DependencyEdgeInfo with import counts
- Should aggregate file-level imports to package-level edges

**queryImplements:**
- Given a struct that implements 2 interfaces, should return 2 location results
- Given a non-type symbol, should return empty results

### Code Context Extraction

**extractContext:**
- Given a valid location and contextLines=3, should return 3 lines before + target lines + 3 lines after
- Given location at start of file (line 0), should not go below line 0
- Given location at end of file, should not exceed file length
- Given contextLines=0, should return only the target lines
- Should format with `// Lines X-Y\n` prefix (1-based display)
- Should convert canopy 0-based lines to 1-based display correctly

### Line Number Conversion

**canopyLocationToNode:**
- Given canopy Location with StartLine=0, EndLine=5, should create Node with StartLine=1, EndLine=6
- Should set File to relative path from projectDir
- Should set Kind correctly based on input

## Integration Tests

### CanopyProvider + Real Project (//go:build integration)

**Given** a temporary directory with a small Go project (3 files with imports, types, functions),
**When** `NewCanopyProvider` is created and `Index(ctx)` is called,
**Then:**
- Canopy database file exists at expected path
- `Query().SearchSymbols("*", ...)` returns symbols from all 3 files
- `Query().Callers(symbolID)` returns correct call edges
- `Query().Implementations(interfaceID)` returns correct implementations

### CanopySearcher + MCP Server (//go:build integration)

**Given** a pre-indexed canopy database for a test project,
**When** `NewCanopySearcher(engine.Query(), projectDir)` is created,
**Then:**

- `Query(ctx, &QueryRequest{Operation: "callers", Target: "Handler.ServeHTTP"})` returns correct callers with SymbolResult details
- `Query(ctx, &QueryRequest{Operation: "callees", Target: "Handler.ServeHTTP"})` returns correct callees
- `Query(ctx, &QueryRequest{Operation: "dependencies", Target: "internal/server"})` returns correct imports
- `Query(ctx, &QueryRequest{Operation: "implementations", Target: "Handler"})` returns types implementing Handler
- `Query(ctx, &QueryRequest{Operation: "implements", Target: "ConcreteHandler"})` returns interfaces it implements
- `Query(ctx, &QueryRequest{Operation: "references", Target: "NewServer"})` returns all references
- `AdvancedQuery(ctx, &QueryRequest{Operation: "detail", Target: "NewServer"})` returns params and return types
- `AdvancedQuery(ctx, &QueryRequest{Operation: "type_hierarchy", Target: "Handler"})` returns hierarchy
- `AdvancedQuery(ctx, &QueryRequest{Operation: "summary"})` returns language stats and top symbols
- `AdvancedQuery(ctx, &QueryRequest{Operation: "hotspots"})` returns most-referenced symbols
- `AdvancedQuery(ctx, &QueryRequest{Operation: "unused_symbols"})` returns unreferenced symbols
- `AdvancedQuery(ctx, &QueryRequest{Operation: "dependency_graph"})` returns package graph
- `AdvancedQuery(ctx, &QueryRequest{Operation: "circular_dependencies"})` returns cycles (if any)

### Multi-Language Indexing (//go:build integration)

**Given** a project with Go and TypeScript files,
**When** canopy indexes the project,
**Then:**
- Both Go and TypeScript symbols appear in search results
- Cross-language queries (e.g., callers in different languages) are handled gracefully (no results, not errors)
- `ProjectSummary` shows both languages

### Daemon Actor + Canopy (//go:build integration)

**Given** a daemon `Actor` configured with canopy provider,
**When** `Actor.Index()` is called,
**Then:**
- Both cortex indexing and canopy indexing complete successfully
- Canopy database is created/updated
- Subsequent incremental indexing (`handleFileChanges`) updates canopy

**Given** a daemon `Actor` running with canopy,
**When** a Go file is modified (function added),
**Then:**
- `handleFileChanges` triggers canopy `Index` (full pipeline -- canopy diffs internally)
- New function is findable via `SearchSymbols`
- Call edges from new function are queryable

## E2E Tests

### cortex_graph MCP Tool (tests/e2e/)

1. Start MCP server with canopy-backed graph searcher
2. Send `cortex_graph` with `operation: "callers"`, `target: "main.run"` — verify caller results with file paths, line numbers, symbol names
3. Send `cortex_graph` with `operation: "references"`, `target: "NewServer"` — verify reference locations
4. Send `cortex_graph` with `operation: "summary"` — verify language stats and top symbols
5. Send `cortex_graph` with `operation: "search"`, `pattern: "New*"` — verify constructor functions
6. Send `cortex_graph` with `operation: "detail"`, `target: "NewServer"` — verify params and return types
7. Send `cortex_graph` with `operation: "type_hierarchy"`, `target: "Handler"` — verify hierarchy data
8. Send `cortex_graph` with `operation: "hotspots"`, `top_n: 5` — verify top symbols with fan-in/fan-out
9. Send `cortex_graph` with `operation: "unused_symbols"` — verify unreferenced symbols
10. Send `cortex_graph` with `operation: "dependency_graph"` — verify package nodes and edges
11. Send `cortex_graph` with `operation: "circular_dependencies"` — verify cycles (or empty)
12. Send `cortex_graph` with `operation: "implements"`, `target: "ConcreteType"` — verify interface locations

**Expected final state:** All 22 operations return valid JSON responses with correct metadata (took_ms, source: "graph"). Standard ops return QueryResponse shape, advanced ops return AdvancedQueryResponse shape.

### Backward Compatibility (tests/e2e/)

1. Index a Go project with canopy
2. Run `cortex_graph` queries for all existing operations (`callers`, `callees`, `dependencies`, `dependents`, `type_usages`)
3. Verify results contain expected data (correct callers, correct imports, etc.)
4. Verify response format matches existing JSON structure (`QueryResponse` with `nodes`, `metadata`)

## Error Scenarios

### Canopy Engine Unavailable

- If `.canopy/index.db` does not exist yet (not indexed), graph operations should return empty results (not errors)
- If `.canopy/index.db` is corrupt, canopy Engine creation should return error; MCP server should log warning and disable graph features (not crash)
- If canopy Engine creation fails for any reason, log warning and continue without graph features

### Target Resolution Failures

- If target string matches no symbols, return `QueryResponse` with empty results and helpful suggestion (not an error)
- If target is ambiguous (multiple matches), use the most specific match and log which was chosen
- If target format is invalid for the operation (e.g., non file:line:col for "definition"), return tool error

### Concurrent Access

- MCP server engine and daemon engine accessing same `.canopy/index.db` simultaneously should work via WAL mode (hardcoded by canopy)
- Multiple MCP queries executing concurrently on same `QueryBuilder` should not conflict (QueryBuilder is stateless)

### Canopy Indexing Failures

- If canopy indexing fails on a specific file, overall indexing should continue (canopy already handles this internally)
- If canopy Resolve() fails, graph queries may return incomplete results but should not error
- If canopy indexing takes too long, context cancellation should propagate and stop it
