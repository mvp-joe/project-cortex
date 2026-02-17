# Implementation Plan

## Phase 1: Canopy Engine Integration

Wire canopy into the project as a Go dependency and create the `CanopyProvider` wrapper that the indexer will use.

- [ ] Add `github.com/jward/canopy` to `go.mod` (already available via go workspace at `/home/jward/code/go.work`)
- [ ] Run `go mod tidy` to resolve the dependency
- [ ] Import canopy's embedded scripts: `import canopyscripts "github.com/jward/canopy/scripts"` -- canopy exports `scripts.FS` via `//go:embed` (no copying needed)
- [ ] Create `internal/indexer/canopy_provider.go` with `CanopyProvider` struct
  - [ ] `NewCanopyProvider(projectDir, dbPath, languages)` -- creates `canopy.Engine` with `canopy.New(dbPath, "", canopy.WithScriptsFS(canopyscripts.FS), canopy.WithParallel(true))`. The empty `""` second parameter (scriptsDir) is intentional: when `WithScriptsFS` is used, scriptsDir is ignored for script loading (see canopy's `New()` doc comment).
  - [ ] `Index(ctx)` -- calls `engine.IndexDirectory(ctx, projectDir)` then `engine.Resolve(ctx)`. This is the same workflow as `canopy index` CLI. Canopy handles its own file diffing via content hashing -- always call the full pipeline, never try to be clever with incremental hints.
  - [ ] `ScriptsChanged()` -- delegates to `engine.ScriptsChanged()`
  - [ ] `Query()` -- returns `engine.Query()`
  - [ ] `Close()` -- calls `engine.Close()`
- [ ] Add `GraphConfig` and `CanopyConfig` to `internal/config/config.go`
- [ ] Ensure `.canopy/` is in `.gitignore` template

## Phase 2: Integrate Canopy Indexing into Daemon

Hook canopy indexing into the existing daemon `Actor` so that file changes trigger canopy indexing alongside cortex's own indexing.

- [ ] Add `canopyProvider *CanopyProvider` field to `internal/indexer/daemon/actor.go` `Actor` struct
- [ ] In `NewActor()`, create `CanopyProvider` using project config
  - [ ] Construct canopy DB path from configuration: `filepath.Join(projectDir, cfg.Graph.Canopy.DBPath)` (where `cfg.Graph.Canopy.DBPath` defaults to `".canopy/index.db"`)
  - [ ] Call `NewCanopyProvider(projectDir, dbPath, languages)`
  - [ ] Check `ScriptsChanged()` -- if true, delete the canopy DB before creating engine
- [ ] In `Actor.Index()`, after existing `idx.indexer.Index()`, call `canopyProvider.Index(ctx)`
  - [ ] Log timing separately: "Canopy graph indexing took Xms"
  - [ ] Canopy indexing failure should log warning but not fail the overall index
- [ ] In `Actor.handleFileChanges()`, after existing incremental indexing, call `canopyProvider.Index(ctx)` (same full pipeline -- canopy does its own diffing)
  - [ ] Same error handling: log warning, do not fail
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
    - [ ] If target contains `.` (e.g., `embed.Provider`), split and search with the name part, then filter by file path containing the qualifier
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
  - [ ] Resolve target to file ID via `resolveFileTarget()`
  - [ ] Call `q.Dependencies(fileID)`, convert `[]*canopy.Import` to `[]QueryResult` with `NodePackage` kind
- [ ] Implement `queryDependents(ctx, req)`:
  - [ ] Call `q.Dependents(target)`, convert `[]*canopy.Import` to `[]QueryResult`
  - [ ] Group by file to get unique dependent files/packages
- [ ] Implement `queryTypeUsages(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.ReferencesTo(symbolID)`, convert `[]Location` to `[]QueryResult`
- [ ] Implement `queryImplements(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.ImplementsInterfaces(symbolID)`, convert `[]Location` to `[]QueryResult`
- [ ] Implement `queryImpact(ctx, req)`:
  - [ ] Use `q.Implementations(symbolID)` for implementation locations — tag as "must_update"
  - [ ] Use `q.TransitiveCallers(symbolID, depth)` for caller subgraph
  - [ ] Depth 1 callers tagged as "must_update", depth 2+ tagged as "review_needed"
  - [ ] Tag results with `ImpactType` and `Severity`
- [ ] Implement `queryPath(ctx, req)`:
  - [ ] Resolve source from `req.Target` and destination from `req.To`
  - [ ] Call `q.TransitiveCallees(sourceID, maxDepth)` to get call subgraph
  - [ ] BFS on returned `CallGraph.Edges` to find shortest path to destination
  - [ ] Build path response with full node details from `CallGraph.Nodes`

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
  - [ ] Construct canopy DB path: `filepath.Join(projectDir, cfg.Graph.Canopy.DBPath)` (defaults to `".canopy/index.db"`)
  - [ ] Create `canopy.Engine` using the resolved DB path (same options as CanopyProvider)
  - [ ] Create `graph.NewCanopySearcher(engine.Query(), projectDir)` — returns `*graph.CanopySearcher`
  - [ ] Pass `*graph.CanopySearcher` to `NewMCPServer()`
  - [ ] Defer `engine.Close()`
- [ ] Update `internal/mcp/server.go` `NewMCPServer()`:
  - [ ] Accept `*graph.CanopySearcher` parameter (replace current `graph.NewSQLSearcher(db, rootDir)` call)
  - [ ] Remove `GraphQuerier` interface entirely — MCP handler uses `*graph.CanopySearcher` directly
  - [ ] Remove `graph.NewSQLSearcher` creation
- [ ] Update `internal/mcp/graph_tool.go`:
  - [ ] Remove `GraphQuerier` interface definition
  - [ ] Update `AddCortexGraphTool()` to accept `*graph.CanopySearcher`
  - [ ] Handler calls `searcher.Query()` for traversal ops and `searcher.AdvancedQuery()` for symbols/search/summary/package_summary

## Phase 4: Expand MCP Operations

Add new operations to `cortex_graph` that leverage canopy's richer query API.

### New Graph Operations

- [ ] Implement `queryReferences(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.ReferencesTo(symbolID)`
  - [ ] Convert `[]Location` to `[]QueryResult`
  - [ ] Apply `MaxResults` limit
- [ ] Implement `queryImplementations(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.Implementations(symbolID)`
  - [ ] Convert `[]Location` to `[]QueryResult`
- [ ] Implement `queryDefinition(ctx, req)`:
  - [ ] Parse target as `file:line:col` (required format for this operation)
  - [ ] Call `q.DefinitionAt(file, line, col)`
  - [ ] Convert `[]Location` to `[]QueryResult`

### Advanced Operations (via `AdvancedQuery`)

All of these return `*AdvancedQueryResponse`, populating the operation-specific field.

**Discovery:**

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
  - [ ] Convert `[]*store.Scope` to `AdvancedQueryResponse.Scopes`

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

### MCP Tool Registration Updates

- [ ] Update `AddCortexGraphTool()` in `internal/mcp/graph_tool.go`:
  - [ ] Expand operation enum to include all new operations (22 total)
  - [ ] Add new optional parameters: `to`, `pattern`, `kinds`, `visibility`, `path_prefix`, `top_n`, `ref_count_min`, `ref_count_max`
  - [ ] Update `CortexGraphRequest` struct with new fields
  - [ ] Update validation and dispatch in handler
- [ ] Update operation dispatch in handler:
  - [ ] Standard ops → `searcher.Query()` → marshal `QueryResponse` to JSON
  - [ ] Advanced ops → `searcher.AdvancedQuery()` → marshal `AdvancedQueryResponse` to JSON
- [ ] Update CLAUDE.md documentation for `cortex_graph` tool

## Notes

- Phase 1 and 2 can be developed together since they are tightly coupled (engine creation + indexing integration)
- Phase 3 is the largest phase and should be done operation-by-operation, testing each before moving to the next
- Phase 4 can be done incrementally -- each new operation is independent. Start with discovery ops (already partially specified), then analysis ops
- The old graph code (`internal/graph/extractor.go`, `searcher_sql.go`, etc.) is NOT removed in this spec. A separate cleanup spec should be created once canopy integration is validated.
- Canopy's line numbers are 0-based (tree-sitter convention). Cortex displays 1-based. The mapping happens in `canopyLocationToNode()`.
