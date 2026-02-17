# Implementation Plan

## Phase 0: Canopy Public API Pre-Requisites

Canopy currently leaks internal types through its public API. These must be re-exported publicly before cortex can import them.

- [ ] **In canopy repo**: Re-export `store.Symbol` as `canopy.Symbol` (type alias or move to public package)
- [ ] **In canopy repo**: Re-export `store.CallEdge` as `canopy.CallEdge`
- [ ] **In canopy repo**: Re-export `store.Import` as `canopy.Import`
- [ ] **In canopy repo**: Re-export `store.File` as `canopy.File`
- [ ] **In canopy repo**: Update `SymbolResult` to embed `canopy.Symbol` instead of `store.Symbol`
- [ ] Verify `Callers()`, `Callees()`, `Dependencies()`, `Dependents()`, `SymbolAt()`, `Files()` return public types
- [ ] Verify `SearchSymbols()`, `Symbols()`, `ProjectSummary()`, `PackageSummary()` return types with no internal leakage
- [ ] Verify no `canopy/internal/*` imports are needed by consumers

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

## Phase 3: Replace Graph Querier with CanopySearcher

Create `CanopySearcher` that implements `GraphQuerier` interface and replace the SQL-based searcher in the MCP server.

### Target Resolution

- [ ] Create `internal/graph/canopy_searcher.go` with `CanopySearcher` struct
- [ ] Implement `resolveTarget(target string) (*canopy.SymbolResult, error)`:
  - [ ] If target contains `:` (file:line:col format), parse and use `q.SymbolAt(file, line, col)`
  - [ ] If target looks like a path (contains `/`), use `resolveFileTarget()` for dependency operations
  - [ ] Otherwise, use `q.SearchSymbols(target, filter, sort, pagination)` with glob matching:
    - [ ] Try exact name match first: `q.SearchSymbols(target, SymbolFilter{}, ...)`
    - [ ] If target contains `.` (e.g., `embed.Provider`), split and search with the name part, then filter by file path containing the qualifier
    - [ ] Prefer exported symbols, prefer exact matches over prefix matches
- [ ] Implement `resolveFileTarget(target string) (int64, error)`:
  - [ ] Use `q.Files(target, "", sort, pagination)` to find files with matching path prefix
  - [ ] Return the first file's ID

### Existing Operations (backward compatible)

- [ ] Implement `queryCallers(ctx, req)`:
  - [ ] Resolve target to symbol ID via `resolveTarget()` (use `result.ID`)
  - [ ] For depth 1: call `q.Callers(symbolID)`, convert `[]*canopy.CallEdge` to `[]QueryResult`
  - [ ] For depth > 1: iterative BFS -- collect caller symbol IDs, call `q.Callers()` on each, track visited set
  - [ ] Apply `MaxResults` limit and context extraction
- [ ] Implement `queryCallees(ctx, req)`:
  - [ ] Same pattern as callers using `q.Callees(symbolID)`
- [ ] Implement `queryDependencies(ctx, req)`:
  - [ ] Resolve target to file ID via `resolveFileTarget()`
  - [ ] Call `q.Dependencies(fileID)`, convert `[]*canopy.Import` to `[]QueryResult` with `NodePackage` kind
- [ ] Implement `queryDependents(ctx, req)`:
  - [ ] Call `q.Dependents(target)`, convert `[]*canopy.Import` to `[]QueryResult`
  - [ ] Group by file to get unique dependent files/packages
- [ ] Implement `queryTypeUsages(ctx, req)`:
  - [ ] Resolve target to symbol ID
  - [ ] Call `q.ReferencesTo(symbolID)`, convert `[]Location` to `[]QueryResult`
- [ ] Implement `queryImpact(ctx, req)`:
  - [ ] Compose: implementations + callers (depth 1) + callers (depth N)
  - [ ] Use `q.Implementations(symbolID)` for Phase 1
  - [ ] Use `q.Callers(symbolID)` with iterative BFS for Phase 2+3
  - [ ] Tag results with `ImpactType` and `Severity`
- [ ] Implement `queryPath(ctx, req)`:
  - [ ] Resolve source from `req.Target` and destination from `req.To`
  - [ ] Iterative BFS using `q.Callees()` from source, checking if destination is reached
  - [ ] Build path response with full node details

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
- [ ] Implement `callEdgeToNode(edge, depth)`:
  - [ ] Look up callee/caller symbol via `q.SymbolAt()` or symbol ID resolution
  - [ ] Convert to `QueryResult` with depth and optional context

### MCP Server Integration

- [ ] Update `internal/cli/mcp.go` `runMCP()`:
  - [ ] Construct canopy DB path: `filepath.Join(projectDir, cfg.Graph.Canopy.DBPath)` (defaults to `".canopy/index.db"`)
  - [ ] Create `canopy.Engine` using the resolved DB path (same options as CanopyProvider)
  - [ ] Create `graph.NewCanopySearcher(engine.Query(), projectDir)` — this returns a `GraphQuerier`
  - [ ] Pass the `GraphQuerier` to `NewMCPServer()`
  - [ ] Defer `engine.Close()`
- [ ] Update `internal/mcp/server.go` `NewMCPServer()`:
  - [ ] Accept `GraphQuerier` parameter (replace current `graph.NewSQLSearcher(db, rootDir)` call)
  - [ ] Remove direct `graph.NewSQLSearcher` creation — MCP server receives a pre-built `GraphQuerier`
  - [ ] MCP server has no knowledge of canopy — it only knows the `GraphQuerier` interface

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

### Discovery Operations

- [ ] Implement `querySymbols(ctx, req)`:
  - [ ] Build `canopy.SymbolFilter` from request fields (kinds, visibility, path_prefix)
  - [ ] Call `q.Symbols(filter, sort, pagination)` -- this is a list/filter endpoint with no pattern matching
  - [ ] Convert `*PagedResult[SymbolResult]` to `QueryResponse`
- [ ] Implement `querySearch(ctx, req)`:
  - [ ] Build `canopy.SymbolFilter` from request fields
  - [ ] Call `q.SearchSymbols(pattern, filter, sort, pagination)` -- this performs glob-style name matching (e.g., `"Get*"`, `"*Provider"`) in addition to structured filtering
  - [ ] Convert results to `QueryResponse`
- [ ] Implement `querySummary(ctx, req)`:
  - [ ] Call `q.ProjectSummary(topN)` where topN defaults to 10
  - [ ] Convert `*ProjectSummary` to `QueryResponse` with summary data in metadata
- [ ] Implement `queryPackageSummary(ctx, req)`:
  - [ ] Call `q.PackageSummary(target, nil)` using target as package path
  - [ ] Convert `*PackageSummary` to `QueryResponse`

### MCP Tool Registration Updates

- [ ] Update `AddCortexGraphTool()` in `internal/mcp/graph_tool.go`:
  - [ ] Expand operation enum to include new operations
  - [ ] Add new optional parameters: `to`, `pattern`, `kinds`, `visibility`, `path_prefix`, `top_n`
  - [ ] Update `CortexGraphRequest` struct with new fields
  - [ ] Update validation and dispatch in handler
- [ ] Update operation dispatch in handler to route to new query methods
- [ ] Update CLAUDE.md documentation for `cortex_graph` tool

## Notes

- Phase 1 and 2 can be developed together since they are tightly coupled (engine creation + indexing integration)
- Phase 3 is the largest phase and should be done operation-by-operation, testing each before moving to the next
- Phase 4 can be done incrementally -- each new operation is independent
- The old graph code (`internal/graph/extractor.go`, `searcher_sql.go`, etc.) is NOT removed in this spec. A separate cleanup spec should be created once canopy integration is validated.
- Canopy's line numbers are 0-based (tree-sitter convention). Cortex displays 1-based. The mapping happens in `canopyLocationToNode()`.
