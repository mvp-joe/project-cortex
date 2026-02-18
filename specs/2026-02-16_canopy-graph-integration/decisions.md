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

**Decision:** Canopy is the sole graph provider. No config flag, no fallback, no dual searcher, no `GraphQuerier` interface. The MCP handler uses `*graph.CanopySearcher` directly — the old `GraphQuerier` interface is removed entirely. The CLI code creates the canopy engine and passes a `*graph.CanopySearcher` to `NewMCPServer`. If canopy is not indexed yet, graph operations return empty results (not errors). The old graph code remains in the codebase for now but is not wired in.

**Consequences:**
- (+) Single code path — simpler, no config flags, no unnecessary abstraction
- (+) MCP handler can call both `Query()` and `AdvancedQuery()` directly on CanopySearcher
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

## 2026-02-16: Native Transitive Traversal via Canopy API v2

**Context:** Canopy's original API had `Callers(symbolID)` and `Callees(symbolID)` returning direct edges only (depth 1). Cortex's `cortex_graph` supports a `depth` parameter for transitive callers/callees. The original plan was to implement iterative BFS in cortex by calling `Callers()`/`Callees()` in a loop.

**Decision:** Use canopy's new `TransitiveCallers(symbolID, maxDepth)` and `TransitiveCallees(symbolID, maxDepth)` methods (added in Query API v2). These bulk-load all call edges into memory and perform BFS internally, returning a `CallGraph` with full `SymbolResult` details in each node. No custom traversal code needed in cortex.

**Consequences:**
- (+) No BFS implementation in cortex — canopy handles it natively with bulk-loaded edges
- (+) Each `CallGraphNode` includes full `SymbolResult` (name, file, lines, ref counts) — no separate symbol lookups needed
- (+) More efficient than iterative API calls (single bulk load vs N round-trips)
- (+) Depth capped at 100 by canopy (safe default)

## 2026-02-16: Code Context Extraction via File Read

**Context:** The current graph searcher extracts code context snippets from cortex's SQLite `files` table (which stores file content). Canopy does not store file content -- it stores positions only.

**Decision:** For `include_context=true`, read the source file directly from disk using the file path from canopy's Location. Extract lines around the target range. This replaces the SQLite-based `ContextExtractor`.

**Consequences:**
- (+) Always shows current file content (not stale cached content)
- (+) No dependency on cortex's files table for context
- (-) Disk I/O for each context extraction (but files are typically hot in OS cache)
- (-) File may have changed since canopy indexed it (rare edge case, acceptable)

## 2026-02-17: Stale File Cleanup Handled by Canopy

**Context:** Canopy's `IndexDirectory` uses `git ls-files` to discover files on the current branch and indexes them. However, it currently does NOT delete data for files that were previously indexed but no longer exist (e.g., after switching from a feature branch with new files back to main). This leaves "ghost" symbols from the old branch in canopy's database.

**Decision:** This is canopy's responsibility, not cortex's. Canopy's `IndexDirectory` should compare discovered files against indexed files and delete stale entries for files no longer on disk. This keeps cleanup logic in canopy where it owns the indexing lifecycle, avoids cortex reaching into canopy's `Store` internals, and makes `IndexDirectory` truly idempotent (database reflects current filesystem state after each call).

**Consequences:**
- (+) Clean graph data after branch switches — no ghost symbols
- (+) Cleanup logic lives in canopy alongside the indexing logic it depends on
- (+) Cortex remains a pure consumer of canopy's public API (Engine, QueryBuilder)
- (+) No need for cortex to access `Engine.Store()` — keeps the abstraction clean
- (-) Requires a canopy change before this spec can be fully implemented (prerequisite)

## 2026-02-17: Two MCP Tools (cortex_graph + cortex_analysis)

**Context:** The canopy integration adds 22 operations total (11 structural traversal + 11 discovery/analysis). A single `cortex_graph` tool with 22 operations would have a very long operation enum and mix two distinct use cases: code navigation (callers, references, definitions) and codebase analysis (summaries, hotspots, dependency graphs).

**Decision:** Split into two MCP tools:
- `cortex_graph` — structural traversal and navigation (11 ops): callers, callees, dependencies, dependents, type_usages, implementations, implements, impact, path, references, definition. Returns `QueryResponse`.
- `cortex_analysis` — discovery and analysis (11 ops): symbols, search, summary, package_summary, detail, type_hierarchy, scope, unused_symbols, hotspots, circular_dependencies, dependency_graph. Returns `AdvancedQueryResponse`.

**Consequences:**
- (+) Cleaner tool descriptions — each tool has a focused purpose
- (+) Maps directly to `CanopySearcher.Query()` / `CanopySearcher.AdvancedQuery()` split
- (+) LLM tool selection is easier with focused tool descriptions
- (+) Different parameter sets per tool (graph needs depth/context; analysis needs pattern/kinds/visibility)
- (-) Two tools to document instead of one

