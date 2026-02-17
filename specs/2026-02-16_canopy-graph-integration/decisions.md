# Decisions

## 2026-02-16: Canopy as Black Box Library

**Context:** Canopy stores all its data in a SQLite database (`.canopy/index.db`). Cortex also uses SQLite for its own data (chunks, embeddings, FTS5, file metadata). It would be tempting to query canopy's tables directly for performance or convenience, but canopy's schema is internal and subject to change.

**Decision:** ALL interaction with canopy goes through its public Go API (`canopy.Engine`, `canopy.QueryBuilder`). We never open, read from, or write to canopy's SQLite database. If canopy's public API does not expose something we need, we request the canopy maintainer to add it rather than working around it.

**Consequences:**
- (+) Decoupled from canopy's internal schema changes
- (+) Clear ownership boundary -- canopy manages its own database lifecycle
- (+) Upgrades to canopy are safe as long as public API is stable
- (-) May need to wait for canopy API additions if something is missing
- (-) Cannot optimize queries by joining canopy tables with cortex tables

## 2026-02-16: Separate Databases for Canopy and Cortex

**Context:** Cortex stores its data in branch-isolated SQLite databases at `~/.cortex/cache/{cache-key}/branches/{branch}.db`. Canopy expects a single database path. We need to decide where canopy's database lives.

**Decision:** Canopy's database lives at `.canopy/index.db` relative to the project root (canopy's default convention). It is NOT branch-isolated -- canopy handles its own change detection via content hashing and re-indexes incrementally. The `.canopy/` directory is already in `.gitignore`.

**Consequences:**
- (+) Follows canopy's conventions -- no custom configuration needed
- (+) Canopy handles its own incremental indexing and change detection
- (+) Single canopy index per project (no duplicate work per branch)
- (-) Branch-switching does not get isolated canopy indexes (but canopy re-indexes incrementally so this is fast)
- (-) Must ensure `.canopy/` is in `.gitignore`

## 2026-02-16: Target Resolution Strategy (String -> Symbol ID)

**Context:** The current `cortex_graph` MCP tool accepts a `target` string like `"embed.Provider"` or `"internal/mcp"`. Canopy's query API works with `int64` symbol IDs (from `SearchSymbols`) or `int64` file IDs (from `Files`). We need a strategy to bridge the two.

**Decision:** Implement a `resolveTarget` method in `CanopySearcher` that:
1. For function/type targets (e.g., `"embed.Provider"`, `"Actor.Start"`): Use `q.SearchSymbols(pattern, filter, sort, pagination)` with the target as a glob pattern. If the target contains a dot, split into receiver/name and search with kind filter. Return the best-matching symbol ID.
2. For package/path targets (e.g., `"internal/mcp"`): Use `q.Files(prefix, ...)` to find files under the path, then use `q.Symbols(filter)` with the file's package symbol.
3. For file:line:col targets (e.g., `"internal/mcp/server.go:42:10"`): Parse directly and use `q.SymbolAt(file, line, col)`.

The resolution is best-effort with fuzzy matching. If multiple symbols match, prefer exported over unexported, prefer exact name match over prefix, and return the first match.

**Consequences:**
- (+) Preserves the existing user-facing API (string targets)
- (+) Supports multiple target formats naturally
- (-) Fuzzy matching may return wrong symbol for ambiguous names
- (-) Extra round-trip to resolve target before actual query

## 2026-02-16: Canopy is the Only Graph Provider

**Context:** The old graph searcher (`graph.sqlSearcher`) queries cortex's own SQLite tables using a custom Go AST extractor. We considered a dual-searcher transition period with a config flag to choose between canopy and legacy.

**Decision:** Canopy is the sole graph provider. No config flag, no fallback, no dual searcher. The `CanopySearcher` implements `mcp.GraphQuerier` directly. The CLI code creates the canopy engine and passes a `GraphQuerier` to `NewMCPServer` — the MCP server has no knowledge of canopy. If canopy is not indexed yet, graph operations return empty results (not errors). The old graph code remains in the codebase for now but is not wired in.

**Consequences:**
- (+) Single code path — simpler, no config flags
- (+) MCP server stays decoupled from canopy (receives a `GraphQuerier` interface)
- (+) Forces us to validate canopy thoroughly before shipping
- (-) No fallback if canopy has issues (but that's acceptable — fix forward)

## 2026-02-16: Canopy Engine Lifecycle in Indexer Daemon

**Context:** The indexer daemon's `Actor` manages per-project lifecycle. Canopy's `Engine` needs to be created, used for indexing, and closed. The MCP server also needs query access.

**Decision:** The canopy `Engine` is created once per `Actor` (per project) during `NewActor`. It is passed to the indexer for `IndexDirectory` + `Resolve`. The engine is closed when the `Actor` stops.

For the MCP server (which runs in a separate process from the daemon), a separate canopy `Engine` is created using the same `.canopy/index.db` path. Canopy hardcodes WAL mode in its connection string (`_journal_mode=WAL`), so concurrent reads from MCP while the daemon writes are safe. No special read-only configuration is needed — the MCP server only calls query methods via `engine.Query()`.

**Consequences:**
- (+) Single engine per project avoids duplicate resource usage
- (+) WAL mode allows concurrent read/write safely
- (+) Engine lifecycle tied to Actor lifecycle (clean ownership)
- (-) MCP server needs its own engine instance (cannot share across processes)

## 2026-02-16: Depth Simulation for Callers/Callees

**Context:** Canopy's `Callers(symbolID)` and `Callees(symbolID)` return direct edges only (depth 1). Cortex's `cortex_graph` supports `depth` parameter for transitive callers/callees using WITH RECURSIVE CTEs.

**Decision:** Implement depth traversal in `CanopySearcher` by iterating: call `Callers`/`Callees`, collect new symbol IDs, repeat until desired depth or max_results reached. Use a visited set to avoid cycles.

**Consequences:**
- (+) Preserves existing depth parameter behavior
- (+) Simple iterative BFS implementation
- (-) Multiple round-trips to canopy API (one per depth level)
- (-) May be slower than single WITH RECURSIVE CTE for deep queries

## 2026-02-16: Code Context Extraction via File Read

**Context:** The current graph searcher extracts code context snippets from cortex's SQLite `files` table (which stores file content). Canopy does not store file content -- it stores positions only.

**Decision:** For `include_context=true`, read the source file directly from disk using the file path from canopy's Location. Extract lines around the target range. This replaces the SQLite-based `ContextExtractor`.

**Consequences:**
- (+) Always shows current file content (not stale cached content)
- (+) No dependency on cortex's files table for context
- (-) Disk I/O for each context extraction (but files are typically hot in OS cache)
- (-) File may have changed since canopy indexed it (rare edge case, acceptable)

## 2026-02-16: Canopy Internal Type Leakage (Pre-Requisite)

**Context:** Canopy's public API methods currently return types from `canopy/internal/store`:
- `Callers()`/`Callees()` return `[]*store.CallEdge`
- `Dependencies()`/`Dependents()` return `[]*store.Import`
- `SymbolAt()` returns `*store.Symbol`
- `Files()` returns `*PagedResult[store.File]`
- `canopy.SymbolResult` embeds `store.Symbol` via composition (so `SearchSymbols`, `Symbols`, `ProjectSummary`, `PackageSummary` all leak `store.Symbol` fields transitively)

Importing `canopy/internal/store` from cortex violates Go conventions and our black-box principle.

**Decision:** Before starting integration, canopy must re-export these types publicly in the root `canopy` package (e.g., `canopy.CallEdge`, `canopy.Import`, `canopy.Symbol`, `canopy.File`) — either as type aliases, wrapper types, or by moving the types to the public package. The `SymbolResult` embedding of `store.Symbol` must also be addressed (e.g., re-export `Symbol` so the embedding uses the public type). This is a pre-requisite for Phase 1.

**Consequences:**
- (+) Clean public API boundary — cortex only imports `github.com/jward/canopy`
- (+) Go tooling and linters will not complain about internal imports
- (+) Canopy can refactor internal types without breaking cortex
- (-) Requires canopy changes before cortex integration can begin
