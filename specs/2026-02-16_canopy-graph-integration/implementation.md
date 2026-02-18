# Implementation Plan

## Phase 1: Canopy Engine Integration

Wire canopy into the project as a Go dependency and create the `CanopyProvider` wrapper that the indexer will use.

- [ ] Add `github.com/jward/canopy` to `go.mod` (already available via go workspace at `/home/jward/code/go.work`)
- [ ] Run `go mod tidy` to resolve the dependency
- [ ] Import canopy's embedded scripts: `import canopyscripts "github.com/jward/canopy/scripts"` -- canopy exports `scripts.FS` via `//go:embed` (no copying needed)
- [ ] Create `internal/indexer/canopy_provider.go` with `CanopyProvider` struct
  - [ ] `NewCanopyProvider(projectDir, dbPath, languages)` -- creates `canopy.Engine` with `canopy.New(dbPath, "", canopy.WithScriptsFS(canopyscripts.FS), canopy.WithParallel(true), canopy.WithLanguages(languages...))`. The empty `""` second parameter (scriptsDir) is intentional: when `WithScriptsFS` is used, scriptsDir is ignored for script loading (see canopy's `New()` doc comment). `WithLanguages` restricts indexing to specified languages (e.g., `"go"`, `"typescript"`); if `languages` is empty, all supported languages are indexed.
  - [ ] `Index(ctx)` -- calls `engine.IndexDirectory(ctx, projectDir)` then `engine.Resolve(ctx)`. This is the same workflow as `canopy index` CLI. Canopy handles its own file diffing, script changes, and DB migrations internally -- always call the full pipeline.
  - [ ] `Query()` -- returns `engine.Query()`
  - [ ] `Close()` -- calls `engine.Close()`
- [ ] Add `GraphConfig` and `CanopyConfig` to `internal/config/config.go`
  - [ ] Add `Graph GraphConfig` field to `Config` struct
  - [ ] Add defaults in `Default()`: `Graph: GraphConfig{Canopy: CanopyConfig{DBPath: ".canopy/index.db"}}`
- [ ] Ensure `.canopy/` is in `.gitignore` template

## Phase 2: Integrate Canopy Indexing into Daemon

Hook canopy indexing into the existing daemon `Actor` so that file changes trigger canopy indexing alongside cortex's own indexing.

- [ ] Add `canopyProvider *CanopyProvider` field to `internal/indexer/daemon/actor.go` `Actor` struct
- [ ] In `NewActor()`, create `CanopyProvider` using project config
  - [ ] Construct canopy DB path from configuration: `filepath.Join(projectDir, cfg.Graph.Canopy.DBPath)` (where `cfg.Graph.Canopy.DBPath` defaults to `".canopy/index.db"`)
  - [ ] Call `NewCanopyProvider(projectDir, dbPath, languages)`
- [ ] In `Actor.Index()`, after existing `idx.indexer.Index()`, call `canopyProvider.Index(ctx)`
  - [ ] Log timing separately: "Canopy graph indexing took Xms"
  - [ ] Canopy indexing failure should log warning but not fail the overall index
- [ ] In `Actor.handleFileChanges()`, after existing incremental indexing, call `canopyProvider.Index(ctx)` (same full pipeline -- canopy does its own diffing)
  - [ ] Same error handling: log warning, do not fail
- [ ] **Prerequisite (canopy):** Canopy's `IndexDirectory` must handle stale file cleanup — after discovering files via `git ls-files`, it should delete indexed entries for files that are no longer present on disk. This ensures the database reflects the current filesystem state after branch switches. Without this, ghost symbols from deleted files persist.
  - [ ] Verify canopy has this behavior before integrating; if not, add it to canopy first
- [ ] In `Actor.Stop()`, call `canopyProvider.Close()`
- [ ] Add canopy indexing timing to `IndexerV2Stats` (optional field: `CanopyIndexTime time.Duration`)

## Phase 3: CanopySearcher + MCP Integration

Create `CanopySearcher` and wire it directly into the MCP server, replacing the old SQL-based searcher. No `GraphQuerier` interface — the MCP handler uses `*graph.CanopySearcher` directly.

### Target Resolution

- [ ] Create `internal/graph/canopy_searcher.go` with `CanopySearcher` struct
- [ ] Implement `resolveTarget(target string) (*canopy.Symbol, error)`:
  - [ ] If target contains `:` (file:line:col format), parse and use `q.SymbolAt(file, line, col)` (returns `*canopy.Symbol` directly)
  - [ ] If target looks like a path (contains `/`), use `resolveFileTarget()` for dependency operations
  - [ ] Otherwise, use `q.SearchSymbols(target, filter, sort, pagination)` with glob matching:
    - [ ] Try exact name match first: `q.SearchSymbols(target, SymbolFilter{}, ...)`
    - [ ] If target contains `.` (e.g., `embed.Provider` or `Actor.Start`), split on `.` into qualifier and name:
      - [ ] Search for the name part via `q.SearchSymbols(name, ...)`
      - [ ] For package-style qualifiers (e.g., `embed.Provider`): filter results by file path containing the qualifier
      - [ ] For method-style qualifiers (e.g., `Actor.Start`): filter results where the symbol's parent/receiver matches the qualifier (check if the result's file also contains a symbol named "Actor")
      - [ ] Heuristic: if qualifier starts with uppercase, try method-style first; if lowercase, try package-style first
    - [ ] Prefer exported symbols, prefer exact matches over prefix matches
    - [ ] Extract `.Symbol` from `SymbolResult` to return `*canopy.Symbol` (the common type across all resolution paths)
- [ ] Implement `resolveFileTarget(target string) (int64, error)`:
  - [ ] Use `q.Files(target, "", sort, pagination)` to find files with matching path prefix
  - [ ] Return the first file's ID

### Existing Operations (backward compatible)

- [ ] Implement `queryCallers(ctx, req)`:
  - [ ] Resolve target to symbol ID via `resolveTarget()` (use `result.ID`)
  - [ ] Call `q.TransitiveCallers(symbolID, depth)` — returns `*CallGraph` with full `SymbolResult` in each node
  - [ ] Convert `[]CallGraphNode` to `[]QueryResult` via `callGraphNodeToResult()`
  - [ ] Apply `MaxResults` limit and context extraction
- [ ] Implement `queryCallees(ctx, req)`:
  - [ ] Same pattern as callers using `q.TransitiveCallees(symbolID, depth)`
- [ ] Implement `queryDependencies(ctx, req)`:
  - [ ] Resolve target to file ID via `resolveFileTarget()` (returns first matching file if path prefix matches multiple files)
  - [ ] Call `q.Dependencies(fileID)`, convert `[]*canopy.Import` to `[]QueryResult` with `NodePackage` kind
  - [ ] Note: operates on a single file's imports, not aggregated package imports. For package-level dependency info, use `cortex_analysis` → `dependency_graph`
- [ ] Implement `queryDependents(ctx, req)`:
  - [ ] Call `q.Dependents(target)`, convert `[]*canopy.Import` to `[]QueryResult`
  - [ ] Group by file to get unique dependent files/packages
- [ ] Implement `queryTypeUsages(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.ReferencesTo(symbolID)`, convert `[]Location` to `[]QueryResult`
- [ ] Implement `queryImpact(ctx, req)`:
  - [ ] Use `q.Implementations(symbolID)` for implementation locations — tag as "must_update"
  - [ ] Use `q.TransitiveCallers(symbolID, depth)` for caller subgraph
  - [ ] Filter out root node (depth 0) from `TransitiveCallers` result — it is the target itself, not a caller
  - [ ] Depth 1 callers tagged as "must_update", depth 2+ tagged as "review_needed"
  - [ ] Tag results with `ImpactType` and `Severity`
- [ ] Implement `queryPath(ctx, req)`:
  - [ ] Resolve source from `req.Target` and destination from `req.To`
  - [ ] Call `q.TransitiveCallees(sourceID, maxDepth)` to get call subgraph
  - [ ] BFS on returned `CallGraph.Edges` to find shortest path to destination (Note: this is the one exception to the no-custom-BFS decision — canopy has no shortest-path API, so cortex performs BFS on the pre-loaded call graph.)
  - [ ] Build path response with full node details from `CallGraph.Nodes`

### Post-Query Filtering (Scope/ExcludePatterns)

- [ ] Implement `applyFilters(results []QueryResult, req *QueryRequest) []QueryResult`:
  - [ ] If `req.Scope` is set, filter results to only include nodes whose `File` path matches the SQL LIKE pattern (use `filepath.Match` or simple string matching with `%` → `*` conversion)
  - [ ] If `req.ExcludePatterns` is set, filter out results whose `File` path matches any pattern
  - [ ] Apply scope first, then exclude
  - [ ] Note: these fields exist on `QueryRequest` but are NOT exposed via MCP parameters — they are used internally (e.g., by `queryImpact` to exclude test files)

### Code Context Extraction

- [ ] Implement `extractContext(loc canopy.Location, contextLines int) (string, error)`:
  - [ ] Read source file from disk: `os.ReadFile(loc.File)`
  - [ ] Split into lines, extract range `[loc.StartLine - contextLines, loc.EndLine + contextLines]`
  - [ ] Adjust for 0-based canopy lines to 1-based cortex display
  - [ ] Format with line number prefix: `"// Lines X-Y\n" + snippet`

### Result Mapping

- [ ] Implement `canopyLocationToNode(loc, symbolName, kind)`:
  - [ ] Map canopy's 0-based lines to cortex's 1-based lines (`+1`)
  - [ ] Set `Node.File` to relative path from projectDir
  - [ ] Set `Node.ID` to symbol name or qualified name
- [ ] Implement `callGraphNodeToResult(node CallGraphNode)`:
  - [ ] Map `node.Symbol` (SymbolResult) to `QueryResult` — name, kind, file, lines already available
  - [ ] Set depth from `node.Depth`
  - [ ] Extract context if `IncludeContext=true` using `extractContext()`

### MCP Server Integration

- [ ] Update `internal/cli/mcp.go` `runMCP()`:
  - [ ] Load `cfg.Graph.Canopy.DBPath` from config (requires `GraphConfig` added in Phase 1)
  - [ ] Construct canopy DB path: `filepath.Join(projectDir, cfg.Graph.Canopy.DBPath)` (defaults to `".canopy/index.db"`)
  - [ ] Check if canopy DB file exists; if not, log warning ("canopy index not found — run `cortex index` first; graph operations disabled") and pass nil searcher to `NewMCPServer()`
  - [ ] If DB exists: create `canopy.Engine` (with `canopy.WithScriptsFS`, query-only usage — no indexing, just `engine.Query()`), create `graph.NewCanopySearcher(engine.Query(), projectDir)` — returns `(*graph.CanopySearcher, error)`; handle the error (log and disable graph features if non-nil)
  - [ ] Pass `*graph.CanopySearcher` (or nil) to `NewMCPServer()`
  - [ ] Defer `engine.Close()` (only if engine was created)
- [ ] Update `internal/mcp/server.go` `NewMCPServer()`:
  - [ ] Change signature from `NewMCPServer(ctx, config, db, provider)` to `NewMCPServer(ctx, config, db, provider, canopySearcher *graph.CanopySearcher)` — canopySearcher is nullable (nil means graph features disabled)
  - [ ] Update `MCPServer` struct: replace `graphQuerier GraphQuerier` field with `canopySearcher *graph.CanopySearcher`
  - [ ] Remove `graph.NewSQLSearcher(db, rootDir)` creation (lines 79-86) — canopy searcher is now passed in
  - [ ] Only call `AddCortexGraphTool()` and `AddCortexAnalysisTool()` if canopySearcher is non-nil
  - [ ] Update `Close()` method: replace `s.graphQuerier.Close()` with `s.canopySearcher.Close()` (nil-safe)
  - [ ] Remove `graphWatcher` field (no longer needed — canopy DB is updated by daemon)
- [ ] Update `internal/mcp/graph_tool.go`:
  - [ ] Remove `GraphQuerier` interface definition (lines 13-17)
  - [ ] Update `AddCortexGraphTool()` to accept `*graph.CanopySearcher` instead of `GraphQuerier`
  - [ ] Handler calls `searcher.Query()` for all standard operations

## Phase 4: Expand cortex_graph + Add cortex_analysis Tool

Expand `cortex_graph` with new standard operations and create a new `cortex_analysis` MCP tool for discovery/analysis operations. This splits the 22 operations across two tools for clearer separation of concerns.

### New Standard Operations (added to `cortex_graph`)

These return `*QueryResponse` via `searcher.Query()`:

- [ ] Implement `queryReferences(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.ReferencesTo(symbolID)`
  - [ ] Convert `[]Location` to `[]QueryResult`
  - [ ] Apply `MaxResults` limit
- [ ] Implement `queryImplementations(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.Implementations(symbolID)`
  - [ ] Convert `[]Location` to `[]QueryResult`
- [ ] Implement `queryImplements(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.ImplementsInterfaces(symbolID)`, convert `[]Location` to `[]QueryResult`
- [ ] Implement `queryDefinition(ctx, req)`:
  - [ ] Parse target as `file:line:col` (required format for this operation)
  - [ ] Call `q.DefinitionAt(file, line, col)`
  - [ ] Convert `[]Location` to `[]QueryResult`

### Update `cortex_graph` MCP Tool Registration

- [ ] Update `AddCortexGraphTool()` in `internal/mcp/graph_tool.go`:
  - [ ] Expand operation enum: `callers`, `callees`, `dependencies`, `dependents`, `type_usages`, `implementations`, `implements`, `impact`, `path`, `references`, `definition` (11 operations)
  - [ ] Add `to` parameter for path operation
  - [ ] Update `CortexGraphRequest` struct with `To` field
  - [ ] Update tool description to document all 11 operations
  - [ ] All operations dispatch to `searcher.Query()` → marshal `QueryResponse` to JSON

### New `cortex_analysis` MCP Tool

Create `internal/mcp/analysis_tool.go` with a new `cortex_analysis` tool for discovery and analysis operations. These return `*AdvancedQueryResponse` via `searcher.AdvancedQuery()`.

**Discovery operations:**

- [ ] Implement `querySymbols(ctx, req)`:
  - [ ] Build `canopy.SymbolFilter` from request fields (kinds, visibility, path_prefix, ref_count_min, ref_count_max)
  - [ ] Call `q.Symbols(filter, sort, pagination)` -- list/filter endpoint, no pattern matching
  - [ ] Convert `*PagedResult[SymbolResult]` to `AdvancedQueryResponse.Symbols`
- [ ] Implement `querySearch(ctx, req)`:
  - [ ] Build `canopy.SymbolFilter` from request fields
  - [ ] Call `q.SearchSymbols(pattern, filter, sort, pagination)` -- glob-style name matching (e.g., `"Get*"`, `"*Provider"`)
  - [ ] Convert results to `AdvancedQueryResponse.Symbols`
- [ ] Implement `querySummary(ctx, req)`:
  - [ ] Call `q.ProjectSummary(topN)` where topN defaults to 10
  - [ ] Convert `*ProjectSummary` to `AdvancedQueryResponse.Summary`
- [ ] Implement `queryPackageSummary(ctx, req)`:
  - [ ] Call `q.PackageSummary(target, nil)` using target as package path
  - [ ] Convert `*PackageSummary` to `AdvancedQueryResponse.PackageSummary`

**Detail & Hierarchy:**

- [ ] Implement `queryDetail(ctx, req)`:
  - [ ] Resolve target to symbol ID (or parse file:line:col for `SymbolDetailAt`)
  - [ ] Call `q.SymbolDetail(symbolID)` or `q.SymbolDetailAt(file, line, col)`
  - [ ] Convert `*SymbolDetail` to `AdvancedQueryResponse.Detail`
- [ ] Implement `queryTypeHierarchy(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.TypeHierarchy(symbolID)`
  - [ ] Convert `*TypeHierarchy` to `AdvancedQueryResponse.Hierarchy`
- [ ] Implement `queryScope(ctx, req)`:
  - [ ] Parse target as `file:line:col` (required format)
  - [ ] Call `q.ScopeAt(file, line, col)`
  - [ ] Convert `[]*canopy.Scope` to `AdvancedQueryResponse.Scopes`
  - [ ] Note: `Scope.FileID` is `int64` — resolve to file path via `q.Files()` or maintain a file ID → path cache

**Analysis:**

- [ ] Implement `queryUnusedSymbols(ctx, req)`:
  - [ ] Build `canopy.SymbolFilter` from request fields
  - [ ] Call `q.UnusedSymbols(filter, sort, pagination)`
  - [ ] Convert `*PagedResult[SymbolResult]` to `AdvancedQueryResponse.Symbols`
- [ ] Implement `queryHotspots(ctx, req)`:
  - [ ] Call `q.Hotspots(topN)` where topN defaults to 10
  - [ ] Convert `[]*HotspotResult` to `AdvancedQueryResponse.Hotspots`
- [ ] Implement `queryCircularDependencies(ctx, req)`:
  - [ ] Call `q.CircularDependencies()`
  - [ ] Set `AdvancedQueryResponse.Cycles` directly
- [ ] Implement `queryDependencyGraph(ctx, req)`:
  - [ ] Call `q.PackageDependencyGraph()`
  - [ ] Convert `*DependencyGraph` to `AdvancedQueryResponse.DependencyGraph`

### `cortex_analysis` MCP Tool Registration

- [ ] Create `AddCortexAnalysisTool()` in `internal/mcp/analysis_tool.go`:
  - [ ] Register `cortex_analysis` tool with operations: `symbols`, `search`, `summary`, `package_summary`, `detail`, `type_hierarchy`, `scope`, `unused_symbols`, `hotspots`, `circular_dependencies`, `dependency_graph` (11 operations)
  - [ ] Parameters: `operation` (required), `target`, `pattern`, `kinds`, `visibility`, `path_prefix`, `top_n`, `ref_count_min`, `ref_count_max`, `max_results`
  - [ ] Create `CortexAnalysisRequest` struct with these fields
  - [ ] Handler dispatches to `searcher.AdvancedQuery()` → marshal `AdvancedQueryResponse` to JSON
- [ ] Register tool in `NewMCPServer()` alongside `cortex_graph` (both gated on canopySearcher != nil)

### Documentation

- [ ] Update CLAUDE.md documentation:
  - [ ] Update `cortex_graph` section with expanded operations (11 total)
  - [ ] Add new `cortex_analysis` section documenting all 11 analysis operations
  - [ ] Add usage examples for common analysis workflows

## Notes

- Phase 1 and 2 can be developed together since they are tightly coupled (engine creation + indexing integration)
- Phase 3 is the largest phase and should be done operation-by-operation, testing each before moving to the next
- Phase 4 can be done incrementally -- each new operation is independent. Start with discovery ops, then analysis ops
- The old graph code (`internal/graph/extractor.go`, `searcher_sql.go`, etc.) is NOT removed in this spec. A separate cleanup spec should be created once canopy integration is validated.
- Canopy's line numbers are 0-based (tree-sitter convention). Cortex displays 1-based. The mapping happens in `canopyLocationToNode()`.
- `Scope` and `ExcludePatterns` on `QueryRequest` are internal post-query filters only -- they are NOT exposed as MCP parameters (they were not exposed before either). Used internally by composed operations like `queryImpact`.
- The `cortex_graph` / `cortex_analysis` tool split maps cleanly to `Query()` / `AdvancedQuery()` on `CanopySearcher` -- 11 operations each.
