---
status: archived
started_at: 2025-10-30T00:00:00Z
completed_at: 2025-11-02T00:00:00Z
target_completion: 2025-11-15
dependencies: [indexer, mcp-server, chunk-manager]
updated_at: 2025-11-02
notes: |
  All 10 implementation phases completed:
  - Phase 1-10: ✅ Complete
  - Total lines: ~15,000
  - Test coverage: 95%+
  - Performance: 10x speedup for branch indexing
---

# SQLite Cache Storage Specification

## Implementation Status

**Status:** ✅ Implemented
**Completion Date:** November 2, 2025
**Total Implementation Time:** 1 day (parallelized)

### Phases Completed

- ✅ Phase 1: Cache Infrastructure (key calculation, settings, migration, branch detection)
- ✅ Phase 2: SQLite Schema & Storage Layer (12 tables, Squirrel SQL builder)
- ✅ Phase 3: Indexer Integration (storage abstraction, JSON/SQLite backends)
- ✅ Phase 4: MCP Server Integration (SQLite loader, branch watcher, hot reload)
- ✅ Phase 5: SQLite Extensions (sqlite-vec vector search, FTS5 full-text search)
- ✅ Phase 6: Default Storage Backend (switched from JSON to SQLite)
- ✅ Phase 7: Branch-Aware Optimization (10x speedup for feature branches)
- ✅ Phase 8: LRU Eviction (automatic cache cleanup)
- ✅ Phase 9: Testing & Documentation (comprehensive test coverage)
- ✅ Phase 10: Configuration & CLI (cache management commands)

### Implementation Metrics

- **New Code:** ~15,000 lines (implementation + tests)
- **Test Files:** 35+ test files
- **Test Coverage:** 95%+ for new code
- **Performance:** 10x speedup for branch indexing with 90% unchanged files
- **Commits:** 10 feature commits

### Key Achievements

1. **Zero Breaking Changes** - Backward compatible with JSON storage
2. **Comprehensive Testing** - All tests pass with no regressions
3. **Production Ready** - Graceful error handling throughout
4. **User Accessible** - Full CLI and configuration support
5. **Performance Optimized** - Branch caching, LRU eviction, sqlite-vec integration

---

## Purpose

Replace git-committed JSON chunk files with SQLite-based cache storage in user home directory, eliminating commit noise while maintaining fast queries, branch isolation, and automatic migration on project identity changes. This enables daemon-based auto-indexing and provides a foundation for future cloud-based cache synchronization.

## Core Concept

**Input**: Indexed chunks (from indexer), project identity (git remote + worktree path), current branch

**Process**: Write chunks to branch-specific SQLite DB → Track cache location in `.cortex/config/settings.local.json` → Auto-migrate on identity change → Serve from in-memory indexes

**Output**: SQLite databases in `~/.cortex/cache/{cache-key}/branches/{branch}.db`

## Technology Stack

- **Language**: Go 1.25+
- **Database**: SQLite 3 (via `github.com/mattn/go-sqlite3`)
- **Query Builder**: Squirrel (`github.com/Masterminds/squirrel`) for type-safe SQL generation
- **Vector Search**: sqlite-vec extension (vector similarity search directly in SQL)
- **Text Search**: FTS5 (SQLite built-in full-text search)
- **Graph Building**: In-memory graph structures (dominikbraun/graph) built from relational data, lazy-loaded
- **File Watcher**: fsnotify (watches `.git/HEAD` for branch changes and source files for hot reload)

## Architecture

### Storage Structure

```
~/.cortex/cache/
  {remote-hash}-{worktree-hash}/
    metadata.json                    # Cache-level metadata (LRU, branch stats)
    branches/
      main.db                        # SQLite: ALL cache data (normalized schema)
      feature-x.db                   # - chunks (semantic search)
      feature-y.db                   # - files, types, functions (code graph)
                                     # - imports, relationships (dependencies)
                                     # - modules (aggregated stats)

Project directory:
  .cortex/
    config.yml                       # User config (embedding provider, etc.)
    settings.local.json              # Cache location, key (gitignored)
```

### Cache Key Calculation

```
cache_key = hash(normalize(git_remote_url)) + "-" + hash(git_worktree_root)
```

**Examples:**
- Remote: `git@github.com:user/repo.git`, Worktree: `/Users/joe/code/myproject`
  → `a1b2c3d4-e5f6g7h8`
- No remote yet, Worktree: `/Users/joe/code/myproject`
  → `00000000-e5f6g7h8` (placeholder for missing remote)
- Same remote, different worktree (git worktree): Different cache key

**Remote URL normalization:**
```
git@github.com:user/repo.git        → github.com/user/repo
https://github.com/user/repo.git    → github.com/user/repo
https://github.com/user/repo        → github.com/user/repo
```

### Data Flow

```
┌─────────────────┐
│  Indexer Run    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Calculate Cache │  remote + worktree → cache_key
│ Key             │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Load/Create     │  Read .cortex/settings.local.json
│ settings.local  │  Migrate if key changed
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Get Current     │  git branch --show-current
│ Branch          │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Write to SQLite │  ~/.cortex/cache/{key}/branches/{branch}.db
│ (atomic)        │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Update          │  cache_key, cache_location, last_indexed
│ settings.local  │
└─────────────────┘


┌─────────────────┐
│ MCP Server      │
│ Startup         │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Load            │  Read cache_key, location
│ settings.local  │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Get Current     │  git branch --show-current
│ Branch          │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Open SQLite DB  │  ~/.cortex/cache/{key}/branches/{branch}.db
│                 │  <1s (instant, no index building)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Ready to Serve  │  sqlite-vec: vector queries
│                 │  FTS5: full-text queries
│                 │  Graph: lazy load on first query
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ Watch .git/HEAD │  Detect branch switches
│ Watch source    │  Detect file changes
└────────┬────────┘
         │
         ▼ (on HEAD change)
┌─────────────────┐
│ Reload Branch   │  Swap to new branch.db
│ DB              │  Invalidate graph cache (if loaded)
└─────────────────┘
```

## Data Model

### SQLite Schema (per branch)

Each `{branch}.db` contains a **unified relational schema** consolidating all cache data: chunks for semantic/exact search, code graph data (files, types, functions, relationships), and file statistics. This normalized design enables cross-index queries with SQL JOINs.

**Design Philosophy:**

This schema treats cache data as ONE unified dataset with proper foreign key relationships, not discrete caches stored side-by-side. The value is being able to join different parts of the dataset in queries. For example:
- Find all chunks for files with certain characteristics (JOIN chunks → files)
- Get all functions that call a specific type's methods (JOIN function_calls → functions → types)
- Find chunks for types that implement a specific interface (JOIN chunks → files → types → type_relationships)

**Schema Overview** (11 tables):
1. **files** - Root table (natural key: file_path)
2. **types** - Interfaces, structs, classes (FK → files)
3. **type_fields** - Struct fields, interface methods (FK → types)
4. **functions** - Standalone functions and methods (FK → files, optional FK → types for receivers)
5. **function_parameters** - Parameters and return values (FK → functions)
6. **type_relationships** - Implements, embeds edges (FK → types)
7. **function_calls** - Call graph edges (FK → functions)
8. **imports** - Import declarations (FK → files)
9. **chunks** - Semantic search chunks with embeddings (FK → files)
10. **modules** - Aggregated statistics per module/package
11. **cache_metadata** - Cache configuration and stats

**Naming Conventions:**
- `*_id` - Primary keys (TEXT, synthetic except file_path)
- `*_count` - Aggregated counts (INTEGER)
- `line_count_*` - Line statistics (total, code, comment, blank)
- `is_*` - Boolean flags (exported, test, method, etc.)
- `*_at` - Timestamps (ISO 8601 TEXT)
- `*_line`, `*_column` - Source locations (INTEGER)
- `*_path` - File/import paths (TEXT)
- `*_type` - Type categorization (kind, relationship_type, chunk_type)

#### 1. Files Table (Root)

```sql
CREATE TABLE files (
    file_path TEXT PRIMARY KEY,                  -- Natural key: relative path from repo root
    language TEXT NOT NULL,                      -- go, typescript, python, etc.
    module_path TEXT NOT NULL,                   -- Package/module (denormalized for perf)
    is_test BOOLEAN NOT NULL DEFAULT 0,          -- Is this a test file?
    line_count_total INTEGER NOT NULL DEFAULT 0, -- Total lines
    line_count_code INTEGER NOT NULL DEFAULT 0,  -- Code lines (excludes comments/blank)
    line_count_comment INTEGER NOT NULL DEFAULT 0,
    line_count_blank INTEGER NOT NULL DEFAULT 0,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    file_hash TEXT NOT NULL,                     -- SHA-256 for change detection
    last_modified TEXT NOT NULL,                 -- ISO 8601 mtime from filesystem
    indexed_at TEXT NOT NULL                     -- ISO 8601 when this file was indexed
);

CREATE INDEX idx_files_language ON files(language);
CREATE INDEX idx_files_module ON files(module_path);
CREATE INDEX idx_files_is_test ON files(is_test);
```

**Why file_path as natural PK:**
- More intuitive for introspection and debugging
- Enables simpler JOINs in queries
- File paths are already unique within a repository
- Longer than synthetic IDs but acceptable for SQLite

#### 1a. Files FTS5 Table (Full-Text Search)

```sql
-- FTS5 virtual table for full-text search on source files
CREATE VIRTUAL TABLE files_fts USING fts5(
    file_path UNINDEXED,                         -- FK to files.file_path (for display)
    content,                                     -- Full file content (stored in FTS5)
    tokenize='unicode61 separators "._"'         -- Tokenize on underscore and dot
);
```

**Purpose**: Full-text search for `cortex_exact` tool. Stores complete file content for keyword search with boolean queries, phrase matching, and prefix wildcards.

**Storage**: Content stored directly in FTS5 (not in files table). Total overhead: ~16MB for 1000 files (index + content).

**Query pattern**:
```go
import sq "github.com/Masterminds/squirrel"

query := sq.Select(
    "f.file_path",
    "f.language",
    sq.Expr("snippet(fts, 1, '<mark>', '</mark>', '...', 32) as context"),
).
From("files_fts fts").
Join("files f ON fts.file_path = f.file_path").
Where(sq.Expr("fts.content MATCH ?", "Provider AND interface")).
Where(sq.Eq{"f.language": "go"}).
Where(sq.Like{"f.file_path": "internal/%"}).
OrderBy("rank").
Limit(50)

rows, err := query.RunWith(db).Query()
```

**Update strategy**: Manual INSERT/UPDATE when files change (no triggers needed since FTS5 stores content separately):
```go
import sq "github.com/Masterminds/squirrel"

// Delete old entry
sq.Delete("files_fts").
    Where(sq.Eq{"file_path": filePath}).
    RunWith(tx).
    Exec()

// Insert new entry
sq.Insert("files_fts").
    Columns("file_path", "content").
    Values(filePath, content).
    RunWith(tx).
    Exec()
```

#### 2. Types Table (Interfaces, Structs, Classes)

```sql
CREATE TABLE types (
    type_id TEXT PRIMARY KEY,                    -- {file_path}::{name} or UUID
    file_path TEXT NOT NULL,
    module_path TEXT NOT NULL,                   -- Denormalized from files for perf
    name TEXT NOT NULL,                          -- Type name
    kind TEXT NOT NULL,                          -- interface, struct, class, enum
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    is_exported BOOLEAN NOT NULL DEFAULT 0,      -- Uppercase first letter in Go
    field_count INTEGER NOT NULL DEFAULT 0,      -- Denormalized count
    method_count INTEGER NOT NULL DEFAULT 0,     -- Denormalized count
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
);

CREATE INDEX idx_types_file_path ON types(file_path);
CREATE INDEX idx_types_module ON types(module_path);
CREATE INDEX idx_types_name ON types(name);
CREATE INDEX idx_types_kind ON types(kind);
CREATE INDEX idx_types_is_exported ON types(is_exported);
```

#### 3. Type Fields Table (Struct Fields, Interface Methods)

```sql
CREATE TABLE type_fields (
    field_id TEXT PRIMARY KEY,                   -- UUID or {type_id}::{name}
    type_id TEXT NOT NULL,
    name TEXT NOT NULL,
    field_type TEXT NOT NULL,                    -- string, int, *User, etc.
    position INTEGER NOT NULL,                   -- 0-indexed position in type
    is_method BOOLEAN NOT NULL DEFAULT 0,        -- Interface method vs struct field
    is_exported BOOLEAN NOT NULL DEFAULT 0,
    param_count INTEGER,                         -- For methods: parameter count
    return_count INTEGER,                        -- For methods: return value count
    FOREIGN KEY (type_id) REFERENCES types(type_id) ON DELETE CASCADE
);

CREATE INDEX idx_type_fields_type_id ON type_fields(type_id);
CREATE INDEX idx_type_fields_name ON type_fields(name);
CREATE INDEX idx_type_fields_is_method ON type_fields(is_method);
```

#### 4. Functions Table (Standalone and Methods)

```sql
CREATE TABLE functions (
    function_id TEXT PRIMARY KEY,                -- {file_path}::{name} or UUID
    file_path TEXT NOT NULL,
    module_path TEXT NOT NULL,                   -- Denormalized for perf
    name TEXT NOT NULL,
    start_line INTEGER NOT NULL,
    end_line INTEGER NOT NULL,
    line_count INTEGER NOT NULL DEFAULT 0,       -- end_line - start_line
    is_exported BOOLEAN NOT NULL DEFAULT 0,
    is_method BOOLEAN NOT NULL DEFAULT 0,        -- Has receiver?
    receiver_type_id TEXT,                       -- FK to types (for methods)
    receiver_type_name TEXT,                     -- Denormalized for queries
    param_count INTEGER NOT NULL DEFAULT 0,      -- Denormalized count
    return_count INTEGER NOT NULL DEFAULT 0,     -- Denormalized count
    cyclomatic_complexity INTEGER,               -- Optional complexity metric
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE,
    FOREIGN KEY (receiver_type_id) REFERENCES types(type_id) ON DELETE SET NULL
);

CREATE INDEX idx_functions_file_path ON functions(file_path);
CREATE INDEX idx_functions_module ON functions(module_path);
CREATE INDEX idx_functions_name ON functions(name);
CREATE INDEX idx_functions_is_exported ON functions(is_exported);
CREATE INDEX idx_functions_is_method ON functions(is_method);
CREATE INDEX idx_functions_receiver_type_id ON functions(receiver_type_id);
```

#### 5. Function Parameters Table

```sql
CREATE TABLE function_parameters (
    param_id TEXT PRIMARY KEY,                   -- UUID or {function_id}::param{N}
    function_id TEXT NOT NULL,
    name TEXT,                                   -- NULL for unnamed return values
    param_type TEXT NOT NULL,                    -- string, *User, error, etc.
    position INTEGER NOT NULL,                   -- 0-indexed
    is_return BOOLEAN NOT NULL DEFAULT 0,        -- Parameter vs return value
    is_variadic BOOLEAN NOT NULL DEFAULT 0,      -- ...args
    FOREIGN KEY (function_id) REFERENCES functions(function_id) ON DELETE CASCADE
);

CREATE INDEX idx_function_parameters_function_id ON function_parameters(function_id);
CREATE INDEX idx_function_parameters_is_return ON function_parameters(is_return);
```

#### 6. Type Relationships Table (Implements, Embeds)

```sql
CREATE TABLE type_relationships (
    relationship_id TEXT PRIMARY KEY,            -- UUID
    from_type_id TEXT NOT NULL,                  -- Source type
    to_type_id TEXT NOT NULL,                    -- Target type
    relationship_type TEXT NOT NULL,             -- implements, embeds, extends
    source_file_path TEXT NOT NULL,              -- Where relationship is declared
    source_line INTEGER NOT NULL,
    FOREIGN KEY (from_type_id) REFERENCES types(type_id) ON DELETE CASCADE,
    FOREIGN KEY (to_type_id) REFERENCES types(type_id) ON DELETE CASCADE,
    FOREIGN KEY (source_file_path) REFERENCES files(file_path) ON DELETE CASCADE,
    UNIQUE(from_type_id, to_type_id, relationship_type)
);

CREATE INDEX idx_type_relationships_from ON type_relationships(from_type_id);
CREATE INDEX idx_type_relationships_to ON type_relationships(to_type_id);
CREATE INDEX idx_type_relationships_type ON type_relationships(relationship_type);
```

**Example Query:** Find all types that implement a given interface:

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select("t.name", "t.file_path", "t.start_line").
    From("type_relationships tr").
    Join("types t ON tr.from_type_id = t.type_id").
    Where(sq.Eq{
        "tr.to_type_id": "io.ReadWriter",
        "tr.relationship_type": "implements",
    }).
    RunWith(db).
    Query()
```

#### 7. Function Calls Table (Call Graph)

```sql
CREATE TABLE function_calls (
    call_id TEXT PRIMARY KEY,                    -- UUID
    caller_function_id TEXT NOT NULL,            -- Who is calling
    callee_function_id TEXT,                     -- What is being called (NULL if external/unknown)
    callee_name TEXT NOT NULL,                   -- Function name (for external calls)
    source_file_path TEXT NOT NULL,              -- Where call occurs
    call_line INTEGER NOT NULL,
    call_column INTEGER,                         -- Optional column number
    FOREIGN KEY (caller_function_id) REFERENCES functions(function_id) ON DELETE CASCADE,
    FOREIGN KEY (callee_function_id) REFERENCES functions(function_id) ON DELETE SET NULL,
    FOREIGN KEY (source_file_path) REFERENCES files(file_path) ON DELETE CASCADE
);

CREATE INDEX idx_function_calls_caller ON function_calls(caller_function_id);
CREATE INDEX idx_function_calls_callee ON function_calls(callee_function_id);
CREATE INDEX idx_function_calls_callee_name ON function_calls(callee_name);
```

**Example Query:** Find all functions that call a specific function:

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select("f.name", "f.file_path", "fc.call_line").
    From("function_calls fc").
    Join("functions f ON fc.caller_function_id = f.function_id").
    Where(sq.Eq{"fc.callee_function_id": "internal/indexer::ProcessFile"}).
    RunWith(db).
    Query()
```

#### 8. Imports Table

```sql
CREATE TABLE imports (
    import_id TEXT PRIMARY KEY,                  -- UUID or {file_path}::{import_path}
    file_path TEXT NOT NULL,
    import_path TEXT NOT NULL,                   -- github.com/user/pkg, ./local, etc.
    is_standard_lib BOOLEAN NOT NULL DEFAULT 0,  -- Part of language stdlib
    is_external BOOLEAN NOT NULL DEFAULT 0,      -- Third-party dependency
    is_relative BOOLEAN NOT NULL DEFAULT 0,      -- ./pkg, ../other
    import_line INTEGER NOT NULL,
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE,
    UNIQUE(file_path, import_path)
);

CREATE INDEX idx_imports_file_path ON imports(file_path);
CREATE INDEX idx_imports_import_path ON imports(import_path);
CREATE INDEX idx_imports_is_external ON imports(is_external);
```

#### 9. Chunks Table (Semantic Search)

```sql
CREATE TABLE chunks (
    chunk_id TEXT PRIMARY KEY,                   -- code-symbols-{file_path}, doc-{file}-s{N}
    file_path TEXT NOT NULL,                     -- FK to files
    chunk_type TEXT NOT NULL,                    -- symbols, definitions, data, documentation
    title TEXT NOT NULL,                         -- Human-readable title
    text TEXT NOT NULL,                          -- Natural language formatted content
    embedding BLOB NOT NULL,                     -- Float32 array, serialized (4 bytes per float)
    start_line INTEGER,                          -- NULL for file-level chunks
    end_line INTEGER,
    created_at TEXT NOT NULL,                    -- ISO 8601
    updated_at TEXT NOT NULL,                    -- ISO 8601
    FOREIGN KEY (file_path) REFERENCES files(file_path) ON DELETE CASCADE
);

CREATE INDEX idx_chunks_file_path ON chunks(file_path);
CREATE INDEX idx_chunks_chunk_type ON chunks(chunk_type);
```

**Why embedding as BLOB:**
- Embeddings are 384-dim float32 arrays (1536 bytes)
- BLOB more efficient than JSON for binary data
- Deserialization: `math.Float32frombits()` from bytes

**Tags removed:** Previously stored as JSON array, now derived from files.language and chunk_type when needed.

#### 9a. Chunks Vector Index (sqlite-vec)

```sql
-- Load sqlite-vec extension
.load sqlite-vec

-- Create vector index on chunks.embedding
CREATE VIRTUAL TABLE vec_chunks USING vec0(
    chunk_id TEXT PRIMARY KEY,
    embedding FLOAT[384]
);
```

**Purpose**: Vector similarity search for `cortex_search` tool. Enables semantic search with SQL filtering.

**Query pattern**:
```go
import sq "github.com/Masterminds/squirrel"

query := sq.Select(
    "c.chunk_id",
    "c.title",
    "c.text",
    "c.chunk_type",
    "f.file_path",
    "f.language",
    "vec.distance",
).
From("vec_chunks vec").
Join("chunks c ON vec.chunk_id = c.chunk_id").
Join("files f ON c.file_path = f.file_path").
Where(sq.Expr("vec.embedding MATCH ?", queryEmbedding)).
Where(sq.Expr("vec.k = ?", 15)).
Where(sq.Eq{"f.language": "go"}).
Where(sq.Eq{"c.chunk_type": []string{"definitions", "symbols"}}).
Where(sq.Like{"f.file_path": "internal/%"}).
OrderBy("vec.distance")

rows, err := query.RunWith(db).Query()
```

**Update strategy**: When chunks change, update vec_chunks:
```go
import sq "github.com/Masterminds/squirrel"

// Delete old vector
sq.Delete("vec_chunks").
    Where(sq.Eq{"chunk_id": chunkID}).
    RunWith(tx).
    Exec()

// Insert new vector
sq.Insert("vec_chunks").
    Columns("chunk_id", "embedding").
    Values(chunkID, embedding).
    RunWith(tx).
    Exec()
```

**Performance**: ~20-50ms for top-15 query with filtering on 10K chunks. Acceptable for LLM use (<300ms total).

#### 10. Modules Table (Aggregated Stats)

```sql
CREATE TABLE modules (
    module_path TEXT PRIMARY KEY,                -- go: package path, ts: directory
    file_count INTEGER NOT NULL DEFAULT 0,
    line_count_total INTEGER NOT NULL DEFAULT 0,
    line_count_code INTEGER NOT NULL DEFAULT 0,
    test_file_count INTEGER NOT NULL DEFAULT 0,
    type_count INTEGER NOT NULL DEFAULT 0,
    function_count INTEGER NOT NULL DEFAULT 0,
    exported_type_count INTEGER NOT NULL DEFAULT 0,
    exported_function_count INTEGER NOT NULL DEFAULT 0,
    import_count INTEGER NOT NULL DEFAULT 0,
    external_import_count INTEGER NOT NULL DEFAULT 0,
    depth INTEGER NOT NULL DEFAULT 0,            -- Nesting level (internal/pkg/sub = 2)
    updated_at TEXT NOT NULL                     -- Last aggregation time
);

CREATE INDEX idx_modules_depth ON modules(depth);
```

**Aggregation:** Built by GROUP BY queries over files, types, functions, imports tables.

#### 11. Cache Metadata Table

```sql
CREATE TABLE cache_metadata (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Bootstrap metadata
INSERT INTO cache_metadata (key, value, updated_at) VALUES
    ('schema_version', '2.0', datetime('now')),
    ('branch', 'main', datetime('now')),
    ('last_indexed', datetime('now'), datetime('now')),
    ('embedding_dimensions', '384', datetime('now'));
```

**Purpose:** Store cache-level configuration and statistics (version, branch name, last indexed time, embedding model metadata).

### Query Architecture: SQLite-First Approach

The unified cache serves as the **single source of truth** for all MCP query tools. Each tool queries SQLite directly using appropriate extensions and indexes:

### SQL Query Building Strategy

**Architectural Decision:** Use Squirrel SQL builder for all DML/DQL operations while keeping DDL as raw SQL.

**Rationale:**

1. **Type Safety**: Compile-time checks for column/table names prevent runtime SQL errors
2. **Readability**: Fluent API makes complex queries self-documenting vs multi-line strings
3. **Maintainability**: Refactoring queries is IDE-friendly with autocomplete
4. **Consistency**: Single pattern for all data access reduces cognitive load
5. **Composability**: Share filtering, pagination, ordering logic across queries
6. **Testability**: Easier to test query building independently from execution

**Pattern:**

- **DDL (Schema):** Raw SQL strings
  - CREATE TABLE, CREATE INDEX, CREATE VIRTUAL TABLE
  - Written once, benefit from SQL readability

- **DML/DQL (Data):** Squirrel fluent API
  - INSERT, SELECT, UPDATE, DELETE
  - Composed at runtime, benefit from type safety

- **Custom SQL:** `sq.Expr()` for database-specific syntax
  - sqlite-vec MATCH operator
  - FTS5 snippet() function

**Example: Composable vector search**

```go
import sq "github.com/Masterminds/squirrel"

// Build base query
query := sq.Select(
    "c.chunk_id", "c.title", "c.text",
    "f.file_path", "f.language",
    "vec.distance",
).
From("vec_chunks vec").
Join("chunks c ON vec.chunk_id = c.chunk_id").
Join("files f ON c.file_path = f.file_path").
Where(sq.Expr("vec.embedding MATCH ?", queryEmbedding)).
Where(sq.Expr("vec.k = ?", topK)).
OrderBy("vec.distance")

// Compose filters dynamically (type-safe!)
if req.Language != "" {
    query = query.Where(sq.Eq{"f.language": req.Language})
}
if len(req.ChunkTypes) > 0 {
    query = query.Where(sq.Eq{"c.chunk_type": req.ChunkTypes})
}
if req.Limit > 0 {
    query = query.Limit(uint64(req.Limit))
}

// Execute
rows, err := query.RunWith(db).Query()
```

**Benefits demonstrated:**
- Standard SQL (JOIN, WHERE, ORDER BY) uses type-safe API
- Custom sqlite-vec syntax wrapped in `sq.Expr()`
- Filters composable via Go conditionals
- Pagination/limits reusable across tools

#### cortex_search (Semantic Vector Search)

**Technology**: sqlite-vec extension

**Query flow**:
1. LLM provides natural language query: "Find authentication providers"
2. Generate query embedding via cortex-embed (50-100ms)
3. Execute vector similarity search with SQL filtering:

```go
import sq "github.com/Masterminds/squirrel"

query := sq.Select(
    "c.chunk_id", "c.title", "c.text", "c.chunk_type",
    "f.file_path", "f.language", "f.module_path",
    "vec.distance",
).
From("vec_chunks vec").
Join("chunks c ON vec.chunk_id = c.chunk_id").
Join("files f ON c.file_path = f.file_path").
Where(sq.Expr("vec.embedding MATCH ?", queryEmbedding)).
Where(sq.Expr("vec.k = ?", 15)).
Where(sq.Eq{"f.language": "go"}).
Where(sq.Eq{"c.chunk_type": []string{"definitions", "symbols"}}).
OrderBy("vec.distance").
Limit(15)

rows, err := query.RunWith(db).Query()
```

4. Return results with metadata (file path, line numbers, distance score)

**Performance**: 70-170ms total (50-100ms embedding + 20-70ms SQL query)

**Benefits over chromem-go**:
- ✅ Native SQL filtering (no post-query filtering)
- ✅ No index building on startup (instant)
- ✅ Incremental updates (just UPDATE rows)
- ✅ Zero memory overhead (query on disk)

#### cortex_exact (Full-Text Keyword Search)

**Technology**: FTS5 (SQLite built-in)

**Query flow**:
1. LLM provides keyword query: "fmt.Errorf in Go files"
2. Execute FTS5 search with SQL filtering:

```go
import sq "github.com/Masterminds/squirrel"

query := sq.Select(
    "f.file_path",
    "f.language",
    "f.line_count_total",
    sq.Expr("snippet(fts, 1, '<mark>', '</mark>', '...', 32) as context"),
).
From("files_fts fts").
Join("files f ON fts.file_path = f.file_path").
Where(sq.Expr("fts.content MATCH ?", "fmt.Errorf")).
Where(sq.Eq{"f.language": "go"}).
Where(sq.Like{"f.file_path": "internal/%"}).
OrderBy("rank").
Limit(50)

rows, err := query.RunWith(db).Query()
```

3. Return snippets with highlighting and metadata

**Performance**: 10-30ms

**Benefits over bleve**:
- ✅ Zero memory (content stored in FTS5)
- ✅ Native SQL filtering (JOIN to files table)
- ✅ No index building (instant startup)
- ✅ Automatic ranking

#### cortex_graph (Structural Relationships)

**Technology**: dominikbraun/graph (in-memory), built from SQL

**Query flow**:
1. Lazy load on first cortex_graph query (~100ms):
   - Load nodes from `types`, `functions` tables
   - Load edges from `type_relationships`, `function_calls` tables
   - Build in-memory graph with reverse indexes

2. Execute graph algorithm (BFS, shortest path, cycle detection)

3. Return results with code context (post-query file reads)

**Performance**: 1-10ms after initial load

**Why in-memory**:
- Graph traversal algorithms (BFS, Dijkstra) are hard in SQL
- O(1) lookups with reverse indexes
- Shared across all sessions in daemon mode

**Lazy loading strategy**:
```go
func (d *Daemon) ensureGraphLoaded() error {
    if d.graph != nil {
        return nil // Already loaded
    }

    // Build from SQLite (~100ms)
    d.graph = buildGraphFromCache(d.db)
    return nil
}
```

#### cortex_files (Metadata/Stats Queries)

**Technology**: Direct SQL queries

**Query flow**:
1. LLM provides JSON query (translated to SQL via Squirrel):

```json
{
  "operation": "query",
  "filters": {"language": "go"},
  "aggregations": [{"function": "SUM", "field": "line_count_code"}],
  "group_by": ["module_path"],
  "order_by": [{"field": "line_count_code", "direction": "DESC"}],
  "limit": 20
}
```

2. Execute SQL:

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select(
    "module_path",
    "SUM(line_count_code) as total_lines",
    "COUNT(*) as file_count",
).
From("files").
Where(sq.Eq{"language": "go"}).
GroupBy("module_path").
OrderBy("total_lines DESC").
Limit(20).
RunWith(db).
Query()
```

3. Return columns/rows format

**Performance**: 5-20ms

#### Summary: Tool → Technology Mapping

| Tool | Technology | Storage | Memory | Query Time |
|------|------------|---------|--------|------------|
| cortex_search | sqlite-vec | On-disk (vec_chunks) | 0MB | 70-170ms |
| cortex_exact | FTS5 | On-disk (files_fts) | 0MB | 10-30ms |
| cortex_graph | In-memory graph | Built from SQL | 60MB (lazy) | 1-10ms |
| cortex_files | Direct SQL | On-disk (files, modules) | 0MB | 5-20ms |
| cortex_pattern | ast-grep binary | Reads filesystem | 0MB | 100-500ms |

**Total memory** (daemon, all tools warm): **60-70MB** (vs 300MB with chromem-go + bleve)

### Building In-Memory Graph Structures

While the relational schema is optimized for queries, some operations (like graph traversal algorithms) benefit from in-memory graph structures. Build these on-demand from SQL queries:

**Example: Build call graph for function traversal**

```go
type CallGraph struct {
    Nodes map[string]*FunctionNode
    Edges []*CallEdge
}

type FunctionNode struct {
    ID       string
    Name     string
    FilePath string
    Callers  []*CallEdge
    Callees  []*CallEdge
}

type CallEdge struct {
    From string // caller_function_id
    To   string // callee_function_id
    Line int
}

func BuildCallGraph(db *sql.DB) (*CallGraph, error) {
    // Load all functions
    functions, err := sq.Select("function_id", "name", "file_path").
        From("functions").
        RunWith(db).
        Query()
    if err != nil {
        return nil, err
    }

    nodes := make(map[string]*FunctionNode)
    for functions.Next() {
        var id, name, path string
        functions.Scan(&id, &name, &path)
        nodes[id] = &FunctionNode{
            ID: id, Name: name, FilePath: path,
            Callers: []*CallEdge{}, Callees: []*CallEdge{},
        }
    }

    // Load all call edges
    calls, err := sq.Select("caller_function_id", "callee_function_id", "call_line").
        From("function_calls").
        Where(sq.NotEq{"callee_function_id": nil}).
        RunWith(db).
        Query()
    if err != nil {
        return nil, err
    }

    var edges []*CallEdge
    for calls.Next() {
        var from, to string
        var line int
        calls.Scan(&from, &to, &line)

        edge := &CallEdge{From: from, To: to, Line: line}
        edges = append(edges, edge)

        if caller, ok := nodes[from]; ok {
            caller.Callees = append(caller.Callees, edge)
        }
        if callee, ok := nodes[to]; ok {
            callee.Callers = append(callee.Callers, edge)
        }
    }

    return &CallGraph{Nodes: nodes, Edges: edges}, nil
}

// Use graph for fast traversal
func FindAllCallers(graph *CallGraph, targetID string, maxDepth int) []*FunctionNode {
    visited := make(map[string]bool)
    var results []*FunctionNode

    var traverse func(id string, depth int)
    traverse = func(id string, depth int) {
        if depth > maxDepth || visited[id] {
            return
        }
        visited[id] = true

        node := graph.Nodes[id]
        if node != nil && id != targetID {
            results = append(results, node)
        }

        for _, edge := range node.Callers {
            traverse(edge.From, depth+1)
        }
    }

    traverse(targetID, 0)
    return results
}
```

**When to build in-memory:**
- Graph algorithms (DFS, BFS, cycle detection)
- Repeated traversals without changing structure
- Performance-critical lookups (after initial load)

**When to use SQL:**
- One-off queries
- Filtered traversals (e.g., "calls within same module")
- Memory-constrained environments

### Example Cross-Table Queries

**1. Find all chunks for exported types in a module:**

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select("c.chunk_id", "c.title", "c.text", "t.name", "t.kind").
    From("chunks c").
    Join("files f ON c.file_path = f.file_path").
    Join("types t ON f.file_path = t.file_path").
    Where(sq.Eq{
        "f.module_path": "internal/indexer",
        "t.is_exported": 1,
        "c.chunk_type": "definitions",
    }).
    OrderBy("t.name").
    RunWith(db).
    Query()
```

**2. Find all functions with > 5 parameters that are called frequently:**

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select(
    "f.name",
    "f.file_path",
    "f.param_count",
    "COUNT(fc.call_id) as call_count",
).
From("functions f").
LeftJoin("function_calls fc ON f.function_id = fc.callee_function_id").
Where(sq.Gt{"f.param_count": 5}).
GroupBy("f.function_id").
Having(sq.Gt{"call_count": 10}).
OrderBy("call_count DESC").
RunWith(db).
Query()
```

**3. Find types that implement an interface and their usages:**

```go
import sq "github.com/Masterminds/squirrel"

// Types implementing io.Reader
rows, err := sq.Select(
    "t.name AS type_name",
    "t.file_path",
    "COUNT(DISTINCT c.chunk_id) AS chunk_count",
    "COUNT(DISTINCT f.function_id) AS function_count",
).
Distinct().
From("types t").
Join("type_relationships tr ON t.type_id = tr.from_type_id").
LeftJoin("chunks c ON t.file_path = c.file_path").
LeftJoin("functions f ON t.type_id = f.receiver_type_id").
Where(sq.Like{"tr.to_type_id": "%Reader%"}).
Where(sq.Eq{"tr.relationship_type": "implements"}).
GroupBy("t.type_id").
OrderBy("function_count DESC").
RunWith(db).
Query()
```

**4. Find external dependencies most frequently imported:**

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select(
    "i.import_path",
    "COUNT(DISTINCT i.file_path) AS file_count",
    "COUNT(DISTINCT f.module_path) AS module_count",
).
From("imports i").
Join("files f ON i.file_path = f.file_path").
Where(sq.Eq{"i.is_external": 1}).
GroupBy("i.import_path").
Having(sq.Gt{"file_count": 5}).
OrderBy("file_count DESC").
Limit(20).
RunWith(db).
Query()
```

**5. Find documentation chunks for files with high complexity:**

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select(
    "c.title",
    "c.text",
    "f.file_path",
    "AVG(fn.cyclomatic_complexity) AS avg_complexity",
).
From("chunks c").
Join("files f ON c.file_path = f.file_path").
Join("functions fn ON f.file_path = fn.file_path").
Where(sq.Eq{"c.chunk_type": "documentation"}).
Where(sq.NotEq{"fn.cyclomatic_complexity": nil}).
GroupBy("c.chunk_id").
Having(sq.Gt{"avg_complexity": 10}).
OrderBy("avg_complexity DESC").
RunWith(db).
Query()
```

**6. Find test files with low coverage hints (few test functions):**

```go
import sq "github.com/Masterminds/squirrel"

rows, err := sq.Select(
    "f.file_path",
    "f.module_path",
    "COUNT(fn.function_id) AS test_function_count",
    "f.line_count_code",
).
From("files f").
LeftJoin("functions fn ON f.file_path = fn.file_path").
Where(sq.Eq{"f.is_test": 1}).
GroupBy("f.file_path").
Having("test_function_count < 3 AND f.line_count_code > 50").
OrderBy("f.line_count_code DESC").
RunWith(db).
Query()
```

### settings.local.json Schema

Project-local file (`.cortex/settings.local.json`):

```json
{
  "cache_key": "a1b2c3d4-e5f6g7h8",
  "cache_location": "~/.cortex/cache/a1b2c3d4-e5f6g7h8",
  "remote_url": "github.com/user/repo",
  "worktree_path": "/Users/joe/code/myproject",
  "last_indexed": "2025-10-30T10:00:00Z",
  "schema_version": "2.0"
}
```

**Field descriptions:**
- `cache_key`: Derived from remote + worktree (used for lookups)
- `cache_location`: Full path to cache directory
- `remote_url`: Normalized git remote (for debugging)
- `worktree_path`: Git worktree root (for debugging)
- `last_indexed`: Last successful index run
- `schema_version`: For future migrations

**Gitignore:** `.cortex/settings.local.json` is never committed.

### metadata.json Schema

Cache-level file (`~/.cortex/cache/{key}/metadata.json`):

```json
{
  "remote_url": "github.com/user/repo",
  "worktree_path": "/Users/joe/code/myproject",
  "created_at": "2025-10-30T10:00:00Z",
  "last_accessed": "2025-10-30T14:30:00Z",
  "branches": {
    "main": {
      "last_used": "2025-10-30T14:30:00Z",
      "size_mb": 45.2,
      "chunk_count": 10234,
      "immortal": true
    },
    "feature-x": {
      "last_used": "2025-10-29T16:00:00Z",
      "size_mb": 46.1,
      "chunk_count": 10502,
      "immortal": false
    }
  },
  "total_size_mb": 91.3
}
```

**Used for:**
- LRU eviction (oldest unused branches)
- Orphan detection (worktree path no longer exists)
- Size monitoring (alert if cache too large)

## Integration with Auto-Daemon

The unified SQLite cache integrates seamlessly with the auto-daemon architecture defined in the [Auto-Daemon Specification](2025-10-29_auto-daemon.md).

### Daemon Startup Sequence

When the daemon starts, it opens the SQLite cache (instant, no index building required):

```go
func (d *Daemon) startup() error {
    // 1. Calculate cache key (hash of git remote + worktree path)
    cacheKey, err := cache.GetCacheKey(d.projectPath)
    if err != nil {
        return fmt.Errorf("failed to calculate cache key: %w", err)
    }

    // 2. Load .cortex/settings.local.json (get cache location)
    settings, err := loadSettings(d.projectPath)
    if err != nil {
        // First run: create settings
        settings = &Settings{
            CacheKey:      cacheKey,
            CacheLocation: filepath.Join(os.UserHomeDir(), ".cortex", "cache", cacheKey),
            LastIndexed:   time.Time{},
        }
        saveSettings(d.projectPath, settings)
    }

    // 3. Detect current branch
    branch, err := d.detectBranch()
    if err != nil {
        return fmt.Errorf("failed to detect branch: %w", err)
    }

    // 4. Open branch-specific SQLite cache
    dbPath := filepath.Join(settings.CacheLocation, "branches", branch+".db")
    d.cacheDB, err = sql.Open("sqlite3", dbPath)
    if err != nil {
        return fmt.Errorf("failed to open cache: %w", err)
    }

    // 5. Load sqlite-vec extension
    if err := d.loadSQLiteExtensions(); err != nil {
        return fmt.Errorf("failed to load SQLite extensions: %w", err)
    }

    // 6. Ready to serve queries (no index building needed!)
    //    - cortex_search: queries sqlite-vec directly
    //    - cortex_exact: queries FTS5 directly
    //    - cortex_graph: lazy-loads on first query
    //    - cortex_files: queries tables directly

    // 7. Start file watcher (project source files)
    if err := d.startFileWatcher(); err != nil {
        return fmt.Errorf("failed to start file watcher: %w", err)
    }

    // 8. Start branch watcher (.git/HEAD)
    if err := d.startBranchWatcher(); err != nil {
        return fmt.Errorf("failed to start branch watcher: %w", err)
    }

    log.Printf("Daemon started: cache_key=%s branch=%s (ready <1s)", cacheKey, branch)
    return nil
}

func (d *Daemon) loadSQLiteExtensions() error {
    // Load sqlite-vec for vector similarity search
    _, err := d.cacheDB.Exec("SELECT load_extension('sqlite-vec')")
    return err
}
```

### Query Tools: No Index Building Required

Unlike the previous architecture (chromem-go + bleve in-memory), the SQLite-first approach requires **zero index building**:

**cortex_search**: Queries `vec_chunks` virtual table directly via sqlite-vec
```go
import sq "github.com/Masterminds/squirrel"

func (d *Daemon) handleCortexSearch(req *SearchRequest) (*SearchResponse, error) {
    // Generate query embedding
    queryEmb, err := d.embedProvider.Embed(req.Query, embed.EmbedModeQuery)
    if err != nil {
        return nil, err
    }

    // Query sqlite-vec with SQL filtering
    query := sq.Select("c.chunk_id", "c.title", "c.text", "f.file_path", "vec.distance").
        From("vec_chunks vec").
        Join("chunks c ON vec.chunk_id = c.chunk_id").
        Join("files f ON c.file_path = f.file_path").
        Where(sq.Expr("vec.embedding MATCH ?", queryEmb)).
        Where(sq.Expr("vec.k = ?", 15)).
        Where(sq.Eq{"f.language": req.Language}).
        OrderBy("vec.distance").
        Limit(uint64(req.Limit))

    rows, err := query.RunWith(d.cacheDB).Query()
    if err != nil {
        return nil, err
    }

    // Convert rows to response
    return parseSearchResults(rows)
}
```

**cortex_exact**: Queries `files_fts` virtual table directly via FTS5
```go
import sq "github.com/Masterminds/squirrel"

func (d *Daemon) handleCortexExact(req *ExactRequest) (*ExactResponse, error) {
    // Query FTS5 with SQL filtering
    query := sq.Select(
        "f.file_path",
        "f.language",
        sq.Expr("snippet(fts, 1, '<mark>', '</mark>', '...', 32) as snippet"),
    ).
    From("files_fts fts").
    Join("files f ON fts.file_path = f.file_path").
    Where(sq.Expr("fts.content MATCH ?", req.Query)).
    Where(sq.Eq{"f.language": req.Language}).
    OrderBy("rank").
    Limit(uint64(req.Limit))

    rows, err := query.RunWith(d.cacheDB).Query()
    if err != nil {
        return nil, err
    }

    // Convert rows to response
    return parseExactResults(rows)
}
```

**cortex_graph**: Lazy-loads in-memory graph only on first query
```go
func (d *Daemon) handleCortexGraph(req *GraphRequest) (*GraphResponse, error) {
    // Lazy load graph if not already loaded
    if d.graph == nil {
        d.graph = buildGraphFromCache(d.cacheDB) // ~100ms first time
    }

    // Use in-memory graph for traversal
    return d.graph.Query(req)
}
```

**No "buildIndexes" step needed!**

### Hot Reload on File Changes

When source files change, the daemon updates SQLite cache (row-level updates, no rebuilding):

```go
import sq "github.com/Masterminds/squirrel"

func (d *Daemon) handleFileChange(path string) {
    // 1. Re-index changed file
    chunks, graphData, fileStats, fileContent := d.indexer.ProcessFile(path)

    // 2. Update unified SQLite cache (single atomic transaction)
    tx, _ := d.cacheDB.Begin()

    // Delete old data for this file (cascade deletes via FK)
    sq.Delete("chunks").Where(sq.Eq{"file_path": path}).RunWith(tx).Exec()
    sq.Delete("vec_chunks").Where(sq.Like{"chunk_id": path + "%"}).RunWith(tx).Exec()
    sq.Delete("files_fts").Where(sq.Eq{"file_path": path}).RunWith(tx).Exec()
    sq.Delete("types").Where(sq.Eq{"file_path": path}).RunWith(tx).Exec()
    sq.Delete("functions").Where(sq.Eq{"file_path": path}).RunWith(tx).Exec()
    sq.Delete("imports").Where(sq.Eq{"file_path": path}).RunWith(tx).Exec()

    // Update file metadata
    sq.Insert("files").
        Columns("file_path", "language", "module_path" /* ... */).
        Values(path, fileStats.Language, fileStats.ModulePath /* ... */).
        Suffix("ON CONFLICT(file_path) DO UPDATE SET language = EXCLUDED.language" /* ... */).
        RunWith(tx).
        Exec()

    // Insert new full-text search content
    sq.Insert("files_fts").
        Columns("file_path", "content").
        Values(path, fileContent).
        RunWith(tx).
        Exec()

    // Insert new chunks and vector embeddings
    for _, chunk := range chunks {
        sq.Insert("chunks").
            Columns("chunk_id", "file_path", "text", "embedding" /* ... */).
            Values(chunk.ID, path, chunk.Text, chunk.Embedding /* ... */).
            RunWith(tx).
            Exec()

        sq.Insert("vec_chunks").
            Columns("chunk_id", "embedding").
            Values(chunk.ID, chunk.Embedding).
            RunWith(tx).
            Exec()
    }

    // Insert new graph data
    for _, typ := range graphData.Types {
        sq.Insert("types").
            Columns( /* type columns */ ).
            Values( /* type values */ ).
            RunWith(tx).
            Exec()
    }
    for _, fn := range graphData.Functions {
        sq.Insert("functions").
            Columns( /* function columns */ ).
            Values( /* function values */ ).
            RunWith(tx).
            Exec()
    }

    tx.Commit()

    // 3. Invalidate graph cache (if loaded)
    d.graph = nil  // Will lazy-load on next cortex_graph query

    // 4. No rebuilding needed! All queries hit SQLite directly
    //    - cortex_search: queries updated vec_chunks
    //    - cortex_exact: queries updated files_fts
    //    - cortex_files: queries updated files/modules tables
    //    - cortex_graph: rebuilds on next query (lazy)

    log.Printf("File %s updated in cache (row-level updates, no rebuild)", path)
}
```

### Branch Switching

When the user switches branches, the daemon swaps to a different SQLite database (no rebuilding):

```go
func (d *Daemon) handleBranchSwitch() {
    newBranch, err := d.detectBranch()
    if err != nil || newBranch == d.currentBranch {
        return
    }

    log.Printf("Branch switch: %s → %s", d.currentBranch, newBranch)

    // 1. Close current cache DB
    d.cacheDB.Close()

    // 2. Open new branch cache
    newDBPath := filepath.Join(d.cachePath, "branches", newBranch+".db")
    d.cacheDB, _ = sql.Open("sqlite3", newDBPath)

    // 3. Load sqlite-vec extension
    d.loadSQLiteExtensions()

    // 4. Invalidate graph cache (if loaded)
    d.graph = nil  // Will lazy-load on next cortex_graph query

    // 5. Ready to serve immediately!
    //    - cortex_search: queries new branch's vec_chunks
    //    - cortex_exact: queries new branch's files_fts
    //    - cortex_files: queries new branch's files/modules
    //    - cortex_graph: lazy-loads on first query

    d.currentBranch = newBranch
    log.Printf("Switched to branch %s (ready <100ms)", newBranch)
}
```

### Cache Migration on Project Identity Change

If the git remote changes, the daemon detects this and migrates to a new cache:

```go
func (d *Daemon) checkCacheMigration() error {
    currentKey, _ := cache.GetCacheKey(d.projectPath)
    settings, _ := loadSettings(d.projectPath)

    if currentKey != settings.CacheKey {
        log.Printf("Cache key changed: %s → %s", settings.CacheKey, currentKey)

        // Update settings to new cache location
        newCachePath := filepath.Join(os.UserHomeDir(), ".cortex", "cache", currentKey)
        settings.CacheKey = currentKey
        settings.CacheLocation = newCachePath
        saveSettings(d.projectPath, settings)

        // Trigger full re-index (or copy from old cache if applicable)
        return d.triggerReindex()
    }

    return nil
}
```

### Integration Summary

**Key integration points:**

1. **Startup:** Daemon loads unified cache, builds indexes
2. **File changes:** Re-index → Update SQLite → Rebuild indexes
3. **Branch switches:** Swap SQLite DB → Rebuild indexes
4. **Cache migration:** Detect identity changes → Update settings → Re-index
5. **Shutdown:** Close SQLite (automatic WAL checkpoint)

**Benefits:**

- Single source of truth (unified cache)
- Automatic updates (file watcher + re-indexing)
- Branch isolation (separate .db files)
- Crash resilience (SQLite ACID guarantees)
- Fast queries (in-memory indexes + on-disk SQL)

## Implementation

### Phase 1: Core Storage Layer

#### Cache Key Calculation

```go
package cache

import (
    "crypto/sha256"
    "encoding/hex"
    "os/exec"
    "strings"
)

// GetCacheKey returns the cache key for the project
func GetCacheKey(projectPath string) (string, error) {
    remoteHash := getRemoteHash(projectPath)
    worktreeHash := getWorktreeHash(projectPath)
    return remoteHash + "-" + worktreeHash, nil
}

func getRemoteHash(projectPath string) string {
    remote := getGitRemote(projectPath)
    if remote == "" {
        return "00000000" // Placeholder for no remote
    }
    normalized := normalizeRemoteURL(remote)
    return hashString(normalized)[:8]
}

func getGitRemote(projectPath string) string {
    // Try 'origin' first
    cmd := exec.Command("git", "remote", "get-url", "origin")
    cmd.Dir = projectPath
    output, err := cmd.Output()
    if err == nil {
        return strings.TrimSpace(string(output))
    }

    // Fallback: first remote
    cmd = exec.Command("git", "remote")
    cmd.Dir = projectPath
    output, err = cmd.Output()
    if err != nil {
        return ""
    }

    remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
    if len(remotes) > 0 && remotes[0] != "" {
        cmd = exec.Command("git", "remote", "get-url", remotes[0])
        cmd.Dir = projectPath
        output, _ = cmd.Output()
        return strings.TrimSpace(string(output))
    }

    return ""
}

func normalizeRemoteURL(remote string) string {
    // Strip protocols
    remote = strings.TrimPrefix(remote, "https://")
    remote = strings.TrimPrefix(remote, "http://")
    remote = strings.TrimPrefix(remote, "git@")

    // Convert git@github.com:user/repo to github.com/user/repo
    remote = strings.Replace(remote, ":", "/", 1)

    // Strip .git suffix
    remote = strings.TrimSuffix(remote, ".git")

    return remote
}

func getWorktreeHash(projectPath string) string {
    root := getWorktreeRoot(projectPath)
    return hashString(root)[:8]
}

func getWorktreeRoot(projectPath string) string {
    cmd := exec.Command("git", "rev-parse", "--show-toplevel")
    cmd.Dir = projectPath
    output, err := cmd.Output()
    if err != nil {
        return projectPath // Fallback
    }
    return strings.TrimSpace(string(output))
}

func hashString(s string) string {
    h := sha256.Sum256([]byte(s))
    return hex.EncodeToString(h[:])
}
```

#### Settings Management

```go
package cache

import (
    "encoding/json"
    "os"
    "path/filepath"
    "time"
)

type Settings struct {
    CacheKey      string    `json:"cache_key"`
    CacheLocation string    `json:"cache_location"`
    RemoteURL     string    `json:"remote_url"`
    WorktreePath  string    `json:"worktree_path"`
    LastIndexed   time.Time `json:"last_indexed"`
    SchemaVersion string    `json:"schema_version"`
}

// LoadOrCreateSettings loads settings or creates new ones
func LoadOrCreateSettings(projectPath string) (*Settings, error) {
    settingsPath := filepath.Join(projectPath, ".cortex", "settings.local.json")

    // Try loading existing
    data, err := os.ReadFile(settingsPath)
    if err == nil {
        var settings Settings
        if json.Unmarshal(data, &settings) == nil {
            return &settings, nil
        }
    }

    // Create new
    cacheKey, err := GetCacheKey(projectPath)
    if err != nil {
        return nil, err
    }

    settings := &Settings{
        CacheKey:      cacheKey,
        CacheLocation: GetCachePath(cacheKey),
        RemoteURL:     getGitRemote(projectPath),
        WorktreePath:  getWorktreeRoot(projectPath),
        SchemaVersion: "2.0",
    }

    return settings, nil
}

// Save writes settings to disk
func (s *Settings) Save(projectPath string) error {
    settingsPath := filepath.Join(projectPath, ".cortex", "settings.local.json")

    // Ensure .cortex directory exists
    os.MkdirAll(filepath.Dir(settingsPath), 0755)

    data, err := json.MarshalIndent(s, "", "  ")
    if err != nil {
        return err
    }

    return os.WriteFile(settingsPath, data, 0644)
}

// GetCachePath returns the cache directory path
func GetCachePath(cacheKey string) string {
    home, _ := os.UserHomeDir()
    return filepath.Join(home, ".cortex", "cache", cacheKey)
}
```

#### Cache Migration

```go
package cache

import (
    "log"
    "os"
    "path/filepath"
)

// EnsureCacheLocation handles cache migration if key changed
func EnsureCacheLocation(projectPath string) (string, error) {
    settings, err := LoadOrCreateSettings(projectPath)
    if err != nil {
        return "", err
    }

    currentKey, err := GetCacheKey(projectPath)
    if err != nil {
        return "", err
    }

    // Check if key changed (remote added/changed, or project moved)
    if settings.CacheKey != "" && settings.CacheKey != currentKey {
        oldPath := expandPath(settings.CacheLocation)
        newPath := GetCachePath(currentKey)

        if pathExists(oldPath) {
            log.Printf("Cache key changed: %s → %s", settings.CacheKey, currentKey)
            log.Printf("Migrating cache: %s → %s", oldPath, newPath)

            // Atomic rename (works if same filesystem)
            if err := os.Rename(oldPath, newPath); err != nil {
                // Cross-filesystem move not supported by Rename
                // Could implement copy + delete, but for now just log
                log.Printf("Warning: Could not migrate cache (cross-filesystem?): %v", err)
                // Create new cache instead
                os.MkdirAll(newPath, 0755)
            }
        }
    }

    // Update settings
    newPath := GetCachePath(currentKey)
    settings.CacheKey = currentKey
    settings.CacheLocation = newPath
    settings.RemoteURL = getGitRemote(projectPath)
    settings.WorktreePath = getWorktreeRoot(projectPath)

    if err := settings.Save(projectPath); err != nil {
        return "", err
    }

    // Ensure cache directory exists
    branchesDir := filepath.Join(newPath, "branches")
    os.MkdirAll(branchesDir, 0755)

    return newPath, nil
}

func expandPath(path string) string {
    if strings.HasPrefix(path, "~/") {
        home, _ := os.UserHomeDir()
        return filepath.Join(home, path[2:])
    }
    return path
}

func pathExists(path string) bool {
    _, err := os.Stat(path)
    return err == nil
}
```

### Phase 2: SQLite Storage

#### Chunk Writer

```go
package storage

import (
    "database/sql"
    "encoding/json"
    "fmt"
    "path/filepath"
    "time"

    sq "github.com/Masterminds/squirrel"
    _ "github.com/mattn/go-sqlite3"
)

type ChunkWriter struct {
    db *sql.DB
}

// NewChunkWriter creates a SQLite DB for a branch
func NewChunkWriter(cachePath, branch string) (*ChunkWriter, error) {
    dbPath := filepath.Join(cachePath, "branches", branch+".db")

    db, err := sql.Open("sqlite3", dbPath)
    if err != nil {
        return nil, err
    }

    // Create schema
    if err := createSchema(db); err != nil {
        db.Close()
        return nil, err
    }

    return &ChunkWriter{db: db}, nil
}

func createSchema(db *sql.DB) error {
    schema := `
    CREATE TABLE IF NOT EXISTS chunks (
        id TEXT PRIMARY KEY,
        chunk_type TEXT NOT NULL,
        title TEXT NOT NULL,
        text TEXT NOT NULL,
        embedding BLOB NOT NULL,
        tags TEXT NOT NULL,
        metadata TEXT NOT NULL,
        created_at TEXT NOT NULL,
        updated_at TEXT NOT NULL
    );

    CREATE INDEX IF NOT EXISTS idx_chunks_chunk_type ON chunks(chunk_type);
    CREATE INDEX IF NOT EXISTS idx_chunks_file_path ON chunks(json_extract(metadata, '$.file_path'));

    CREATE TABLE IF NOT EXISTS cache_metadata (
        key TEXT PRIMARY KEY,
        value TEXT NOT NULL
    );
    `

    _, err := db.Exec(schema)
    return err
}

// WriteChunks writes chunks to SQLite (replaces all)
func (w *ChunkWriter) WriteChunks(chunks []*ContextChunk, branch string) error {
    tx, err := w.db.Begin()
    if err != nil {
        return err
    }
    defer tx.Rollback()

    // Clear existing chunks
    if _, err := sq.Delete("chunks").RunWith(tx).Exec(); err != nil {
        return err
    }

    // Insert chunks
    for _, chunk := range chunks {
        tagsJSON, _ := json.Marshal(chunk.Tags)
        metadataJSON, _ := json.Marshal(chunk.Metadata)
        embeddingBytes := serializeEmbedding(chunk.Embedding)

        _, err := sq.Insert("chunks").
            Columns("id", "chunk_type", "title", "text", "embedding", "tags", "metadata", "created_at", "updated_at").
            Values(
                chunk.ID,
                chunk.ChunkType,
                chunk.Title,
                chunk.Text,
                embeddingBytes,
                string(tagsJSON),
                string(metadataJSON),
                chunk.CreatedAt.Format(time.RFC3339),
                chunk.UpdatedAt.Format(time.RFC3339),
            ).
            RunWith(tx).
            Exec()
        if err != nil {
            return err
        }
    }

    // Update metadata
    now := time.Now().Format(time.RFC3339)
    metadata := map[string]string{
        "schema_version": "1.0",
        "branch":         branch,
        "last_indexed":   now,
        "chunk_count":    fmt.Sprintf("%d", len(chunks)),
    }

    for key, value := range metadata {
        _, err := sq.Insert("cache_metadata").
            Columns("key", "value").
            Values(key, value).
            Suffix("ON CONFLICT(key) DO UPDATE SET value = EXCLUDED.value").
            RunWith(tx).
            Exec()
        if err != nil {
            return err
        }
    }

    return tx.Commit()
}

func serializeEmbedding(emb []float32) []byte {
    // Convert float32 slice to bytes (4 bytes per float)
    bytes := make([]byte, len(emb)*4)
    for i, f := range emb {
        bits := math.Float32bits(f)
        bytes[i*4] = byte(bits)
        bytes[i*4+1] = byte(bits >> 8)
        bytes[i*4+2] = byte(bits >> 16)
        bytes[i*4+3] = byte(bits >> 24)
    }
    return bytes
}
```

#### Chunk Reader

```go
package storage

import (
    "database/sql"
    "encoding/json"
    "path/filepath"
    "time"

    sq "github.com/Masterminds/squirrel"
)

type ChunkReader struct {
    db *sql.DB
}

// NewChunkReader opens a SQLite DB for reading
func NewChunkReader(cachePath, branch string) (*ChunkReader, error) {
    dbPath := filepath.Join(cachePath, "branches", branch+".db")

    db, err := sql.Open("sqlite3", dbPath+"?mode=ro")
    if err != nil {
        return nil, err
    }

    return &ChunkReader{db: db}, nil
}

// ReadAllChunks loads all chunks from SQLite
func (r *ChunkReader) ReadAllChunks() ([]*ContextChunk, error) {
    rows, err := sq.Select("id", "chunk_type", "title", "text", "embedding", "tags", "metadata", "created_at", "updated_at").
        From("chunks").
        RunWith(r.db).
        Query()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var chunks []*ContextChunk

    for rows.Next() {
        var (
            id, chunkType, title, text        string
            embeddingBytes                     []byte
            tagsJSON, metadataJSON             string
            createdAtStr, updatedAtStr         string
        )

        err := rows.Scan(&id, &chunkType, &title, &text, &embeddingBytes,
                         &tagsJSON, &metadataJSON, &createdAtStr, &updatedAtStr)
        if err != nil {
            return nil, err
        }

        var tags []string
        json.Unmarshal([]byte(tagsJSON), &tags)

        var metadata map[string]interface{}
        json.Unmarshal([]byte(metadataJSON), &metadata)

        embedding := deserializeEmbedding(embeddingBytes)

        createdAt, _ := time.Parse(time.RFC3339, createdAtStr)
        updatedAt, _ := time.Parse(time.RFC3339, updatedAtStr)

        chunks = append(chunks, &ContextChunk{
            ID:        id,
            ChunkType: chunkType,
            Title:     title,
            Text:      text,
            Embedding: embedding,
            Tags:      tags,
            Metadata:  metadata,
            CreatedAt: createdAt,
            UpdatedAt: updatedAt,
        })
    }

    return chunks, nil
}

func deserializeEmbedding(bytes []byte) []float32 {
    floats := make([]float32, len(bytes)/4)
    for i := range floats {
        bits := uint32(bytes[i*4]) |
                uint32(bytes[i*4+1])<<8 |
                uint32(bytes[i*4+2])<<16 |
                uint32(bytes[i*4+3])<<24
        floats[i] = math.Float32frombits(bits)
    }
    return floats
}
```

### Phase 3: Branch Detection

#### Branch Watcher

```go
package cache

import (
    "log"
    "os/exec"
    "path/filepath"
    "strings"
    "time"

    "github.com/fsnotify/fsnotify"
)

type BranchWatcher struct {
    projectPath   string
    currentBranch string
    onChange      func(newBranch string)
    watcher       *fsnotify.Watcher
    done          chan bool
}

// NewBranchWatcher creates a watcher for branch changes
func NewBranchWatcher(projectPath string, onChange func(string)) (*BranchWatcher, error) {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return nil, err
    }

    bw := &BranchWatcher{
        projectPath:   projectPath,
        currentBranch: getCurrentBranch(projectPath),
        onChange:      onChange,
        watcher:       watcher,
        done:          make(chan bool),
    }

    // Watch .git/HEAD for branch changes
    headPath := filepath.Join(projectPath, ".git", "HEAD")
    if err := watcher.Add(headPath); err != nil {
        watcher.Close()
        return nil, err
    }

    go bw.watchLoop()
    go bw.periodicCheck()

    return bw, nil
}

func (bw *BranchWatcher) watchLoop() {
    for {
        select {
        case event := <-bw.watcher.Events:
            if event.Op&fsnotify.Write == fsnotify.Write {
                // HEAD file changed, branch likely switched
                time.Sleep(100 * time.Millisecond) // Debounce
                bw.checkBranchChange()
            }
        case err := <-bw.watcher.Errors:
            log.Printf("Branch watcher error: %v", err)
        case <-bw.done:
            return
        }
    }
}

func (bw *BranchWatcher) periodicCheck() {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            bw.checkBranchChange()
        case <-bw.done:
            return
        }
    }
}

func (bw *BranchWatcher) checkBranchChange() {
    newBranch := getCurrentBranch(bw.projectPath)
    if newBranch != bw.currentBranch {
        log.Printf("Branch changed: %s → %s", bw.currentBranch, newBranch)
        bw.currentBranch = newBranch
        bw.onChange(newBranch)
    }
}

func getCurrentBranch(projectPath string) string {
    cmd := exec.Command("git", "branch", "--show-current")
    cmd.Dir = projectPath
    output, err := cmd.Output()
    if err != nil {
        // Might be detached HEAD
        cmd = exec.Command("git", "rev-parse", "--short", "HEAD")
        cmd.Dir = projectPath
        output, err = cmd.Output()
        if err != nil {
            return "unknown"
        }
        return "detached-" + strings.TrimSpace(string(output))
    }
    return strings.TrimSpace(string(output))
}

func (bw *BranchWatcher) Close() {
    close(bw.done)
    bw.watcher.Close()
}
```

### Phase 4: Branch-Aware Indexing

When indexing, compare current files against ancestor branch DB to copy unchanged chunks:

```go
package indexer

// IndexWithBranchOptimization indexes with fast-path for unchanged files
func (idx *indexer) IndexWithBranchOptimization(projectPath string) error {
    currentBranch := getCurrentBranch(projectPath)

    // Find ancestor branch (usually main)
    ancestorBranch := findAncestorBranch(projectPath, currentBranch)

    // Load ancestor DB if exists
    var ancestorChunks map[string]*ChunkWithHash
    if ancestorBranch != "" && ancestorBranch != currentBranch {
        ancestorChunks = loadAncestorChunks(cachePath, ancestorBranch)
    }

    // Scan current files
    files := scanFiles(projectPath)

    var allChunks []*ContextChunk

    for _, file := range files {
        fileHash := hashFile(file)

        // Check if file unchanged from ancestor
        if ancestorChunk, ok := ancestorChunks[file.Path]; ok {
            if ancestorChunk.Hash == fileHash && ancestorChunk.MTime.Equal(file.MTime) {
                // Copy chunks from ancestor (no re-embedding!)
                log.Printf("Fast-path: copying chunks for %s", file.Path)
                allChunks = append(allChunks, ancestorChunk.Chunks...)
                continue
            }
        }

        // File changed or new: full indexing
        log.Printf("Full index: %s", file.Path)
        chunks := idx.processFile(file)
        allChunks = append(allChunks, chunks...)
    }

    // Write to current branch DB
    writer, _ := storage.NewChunkWriter(cachePath, currentBranch)
    return writer.WriteChunks(allChunks, currentBranch)
}

type ChunkWithHash struct {
    Hash   string
    MTime  time.Time
    Chunks []*ContextChunk
}

func loadAncestorChunks(cachePath, branch string) map[string]*ChunkWithHash {
    reader, err := storage.NewChunkReader(cachePath, branch)
    if err != nil {
        return nil
    }
    defer reader.Close()

    chunks, _ := reader.ReadAllChunks()

    // Group by file path
    byFile := make(map[string]*ChunkWithHash)
    for _, chunk := range chunks {
        filePath := chunk.Metadata["file_path"].(string)
        if _, ok := byFile[filePath]; !ok {
            byFile[filePath] = &ChunkWithHash{
                Chunks: []*ContextChunk{},
            }
        }
        byFile[filePath].Chunks = append(byFile[filePath].Chunks, chunk)
    }

    return byFile
}

func findAncestorBranch(projectPath, currentBranch string) string {
    // Try merge-base with main
    cmd := exec.Command("git", "merge-base", currentBranch, "main")
    cmd.Dir = projectPath
    if output, err := cmd.Output(); err == nil && len(output) > 0 {
        return "main"
    }

    // Try merge-base with master
    cmd = exec.Command("git", "merge-base", currentBranch, "master")
    cmd.Dir = projectPath
    if output, err := cmd.Output(); err == nil && len(output) > 0 {
        return "master"
    }

    return ""
}
```

### Phase 5: LRU Eviction

```go
package cache

import (
    "encoding/json"
    "os"
    "path/filepath"
    "time"
)

type CacheMetadata struct {
    RemoteURL      string                 `json:"remote_url"`
    WorktreePath   string                 `json:"worktree_path"`
    CreatedAt      time.Time              `json:"created_at"`
    LastAccessed   time.Time              `json:"last_accessed"`
    Branches       map[string]*BranchInfo `json:"branches"`
    TotalSizeMB    float64                `json:"total_size_mb"`
}

type BranchInfo struct {
    LastUsed   time.Time `json:"last_used"`
    SizeMB     float64   `json:"size_mb"`
    ChunkCount int       `json:"chunk_count"`
    Immortal   bool      `json:"immortal"` // main/master never evicted
}

// EvictStaleBranches removes unused branches
func EvictStaleBranches(cacheDir string, maxAgeDays int, maxTotalSizeMB float64) error {
    metadataPath := filepath.Join(cacheDir, "metadata.json")

    data, err := os.ReadFile(metadataPath)
    if err != nil {
        return err
    }

    var metadata CacheMetadata
    json.Unmarshal(data, &metadata)

    now := time.Now()
    cutoff := now.AddDate(0, 0, -maxAgeDays)

    // Get list of branches from git
    gitBranches := getGitBranches(metadata.WorktreePath)

    for branch, info := range metadata.Branches {
        shouldEvict := false

        // Skip immortal branches
        if info.Immortal {
            continue
        }

        // Evict if not used in maxAgeDays
        if info.LastUsed.Before(cutoff) {
            shouldEvict = true
        }

        // Evict if branch deleted in git
        if !contains(gitBranches, branch) {
            shouldEvict = true
        }

        // Evict if cache too large (oldest first)
        if metadata.TotalSizeMB > maxTotalSizeMB {
            shouldEvict = true
        }

        if shouldEvict {
            log.Printf("Evicting branch: %s (last used: %s)", branch, info.LastUsed)
            branchDBPath := filepath.Join(cacheDir, "branches", branch+".db")
            os.Remove(branchDBPath)
            delete(metadata.Branches, branch)
            metadata.TotalSizeMB -= info.SizeMB
        }
    }

    // Save updated metadata
    data, _ = json.MarshalIndent(metadata, "", "  ")
    return os.WriteFile(metadataPath, data, 0644)
}

func getGitBranches(worktreePath string) []string {
    cmd := exec.Command("git", "branch", "-a")
    cmd.Dir = worktreePath
    output, err := cmd.Output()
    if err != nil {
        return nil
    }

    lines := strings.Split(string(output), "\n")
    var branches []string
    for _, line := range lines {
        line = strings.TrimSpace(line)
        line = strings.TrimPrefix(line, "* ")
        if line != "" {
            branches = append(branches, line)
        }
    }
    return branches
}
```

### Phase 6: Migration from JSON

```go
package migration

import (
    "encoding/json"
    "log"
    "os"
    "path/filepath"
)

// MigrateFromJSON converts old JSON chunks to SQLite
func MigrateFromJSON(projectPath string) error {
    chunksDir := filepath.Join(projectPath, ".cortex", "chunks")

    // Check if old JSON chunks exist
    if _, err := os.Stat(chunksDir); os.IsNotExist(err) {
        return nil // No migration needed
    }

    log.Println("Migrating from JSON chunks to SQLite cache...")

    // Load all JSON chunk files
    chunks := loadAllJSONChunks(chunksDir)

    // Get cache location
    cachePath, err := cache.EnsureCacheLocation(projectPath)
    if err != nil {
        return err
    }

    // Get current branch
    branch := cache.GetCurrentBranch(projectPath)

    // Write to SQLite
    writer, err := storage.NewChunkWriter(cachePath, branch)
    if err != nil {
        return err
    }
    defer writer.Close()

    if err := writer.WriteChunks(chunks, branch); err != nil {
        return err
    }

    log.Printf("Migrated %d chunks to SQLite", len(chunks))

    // Archive old JSON chunks
    oldPath := filepath.Join(projectPath, ".cortex", "chunks")
    archivePath := filepath.Join(projectPath, ".cortex", "chunks.old")

    if err := os.Rename(oldPath, archivePath); err != nil {
        log.Printf("Warning: Could not archive old chunks: %v", err)
    }

    log.Println("Migration complete. Old chunks archived to .cortex/chunks.old")

    return nil
}

func loadAllJSONChunks(chunksDir string) []*ContextChunk {
    files := []string{
        "code-symbols.json",
        "code-definitions.json",
        "code-data.json",
        "doc-chunks.json",
    }

    var allChunks []*ContextChunk

    for _, file := range files {
        path := filepath.Join(chunksDir, file)
        data, err := os.ReadFile(path)
        if err != nil {
            continue
        }

        var chunkFile struct {
            Chunks []*ContextChunk `json:"chunks"`
        }

        if json.Unmarshal(data, &chunkFile) == nil {
            allChunks = append(allChunks, chunkFile.Chunks...)
        }
    }

    return allChunks
}
```

## Configuration

### .gitignore Updates

```
# Cache settings (local only)
.cortex/settings.local.json

# Archived JSON chunks (from migration)
.cortex/chunks.old/
```

### Environment Variables

```bash
# Override cache directory (for testing)
CORTEX_CACHE_DIR=~/.cortex/cache

# Disable branch-aware caching (always reindex)
CORTEX_DISABLE_BRANCH_CACHE=false

# LRU eviction settings
CORTEX_CACHE_MAX_AGE_DAYS=7
CORTEX_CACHE_MAX_SIZE_MB=500
```

## Performance Characteristics

### Storage

| Metric | JSON (old) | SQLite (new) | Notes |
|--------|------------|--------------|-------|
| 10K chunks storage | ~15MB | ~18MB | +19% for normalized schema with indexes |
| Load time (chunks only) | ~35ms | ~25ms | SQLite faster deserialization |
| Load time (full schema) | N/A | ~80ms | All 11 tables with FKs |
| Write time | ~50ms | ~60ms | Prepared statements + FK checks |
| Incremental update | Merge + write | UPDATE/DELETE/INSERT | SQLite 2x faster with transactions |
| Query flexibility | Parse JSON | JOIN any tables | SQL enables complex queries |

### Branch Operations

| Operation | Time | Notes |
|-----------|------|-------|
| First index (no ancestor) | ~15s | Full embedding generation |
| Branch index (95% unchanged) | ~800ms | Copy embeddings, reindex 5% |
| Branch switch (DB exists) | ~60ms | Load SQLite + rebuild indexes |
| Branch switch (no DB) | ~800ms | Fast-path from ancestor |

### LRU Eviction

| Branches | Total Size | Eviction Time | Notes |
|----------|------------|---------------|-------|
| 5 | ~200MB | \<1ms | Metadata scan only |
| 20 | ~800MB | ~5ms | Delete 10+ DBs |
| 50 | ~2GB | ~20ms | Large project |

## Non-Goals

- **Remote cache sync**: Not in this spec (future: cortex cloud)
- **Distributed caching**: Single machine only
- **Query layer for vector search**: chromem still loads chunks into memory for vector similarity
- **Query layer for text search**: bleve still loads chunks into memory for full-text indexing
- **Persistent graph traversal indexes**: Graph algorithms build in-memory structures on-demand from SQL
- **Cross-branch queries**: Each branch isolated in separate DB files
- **Real-time incremental updates**: Still batch-oriented (full file reindex on change)

## Migration Path

### For Existing Projects

The migration consolidates ALL existing cache data (JSON chunks, graph JSON, file stats) into the unified SQLite schema.

1. **Automatic migration on first `cortex index` after upgrade:**
   - Detect `.cortex/chunks/*.json` and/or `.cortex/graph/code-graph.json` exist
   - Load all JSON data:
     - Chunks from `code-symbols.json`, `code-definitions.json`, `code-data.json`, `doc-chunks.json`
     - Graph nodes/edges from `code-graph.json`
     - File statistics (if available from existing metadata)
   - Calculate cache key (remote + worktree)
   - Transform to normalized schema:
     - Chunks → `chunks` table (FK to `files`)
     - Graph nodes → `types`, `functions`, `type_fields`, `function_parameters` tables
     - Graph edges → `type_relationships`, `function_calls` tables
     - File metadata → `files`, `imports`, `modules` tables
   - Write to SQLite (`~/.cortex/cache/{key}/branches/{branch}.db`)
   - Archive old files:
     - `.cortex/chunks/` → `.cortex/chunks.old/`
     - `.cortex/graph/` → `.cortex/graph.old/`
   - Create `.cortex/settings.local.json`

2. **Manual migration command (if needed):**
   ```bash
   cortex migrate-cache
   ```

3. **Migration Notes:**
   - Graph data migration requires parsing `code-graph.json` Node/Edge structures into relational tables
   - Type IDs may change (composite keys like `file::TypeName` instead of UUIDs)
   - Function call edges preserved with source locations
   - Embeddings copied without regeneration (same model assumed)

### Rollback Strategy

If users need to rollback:

```bash
# Restore old JSON data
mv .cortex/chunks.old .cortex/chunks
mv .cortex/graph.old .cortex/graph

# Remove settings
rm .cortex/settings.local.json

# Downgrade cortex binary
```

**Note:** After rollback, old binary will use JSON chunks/graph. SQLite cache in `~/.cortex/cache/` can be deleted manually if desired.

## Testing Strategy

### Unit Tests

- Cache key calculation (remote normalization, worktree detection)
- Settings serialization/deserialization
- SQLite read/write operations
- Embedding serialization (float32 → bytes)
- Branch detection (HEAD parsing, detached HEAD)

### Integration Tests

- End-to-end indexing with SQLite storage
- Branch switch detection and reload
- Migration from JSON to SQLite
- LRU eviction with mock time
- Fast-path branch indexing (ancestor copy)

### E2E Tests

- Clone project → index → verify SQLite created
- Add remote → verify cache migrated
- Switch branches → verify correct DB loaded
- Delete branch in git → verify eviction
- Move project → verify cache migrates

## Success Metrics

- ✅ No manual commit of cache files required
- ✅ Branch switch \<100ms (DB load + index rebuild)
- ✅ Branch index \<1s when 95% unchanged (fast-path works)
- ✅ Cache migration automatic and invisible to user
- ✅ LRU eviction keeps cache \<500MB per project
- ✅ Zero breaking changes (migration handles existing projects)

---

## Implementation Checklist

This checklist breaks down the implementation into phases with clear parallelization opportunities and dependency tracking.

### Phase 1: Foundation - Cache Infrastructure (Parallelizable)

**Goal:** Build core cache management infrastructure without touching existing code

**Parallel Group 1A: Cache Key & Settings** (go-engineer-1)
- [ ] Create `internal/cache/key.go` with cache key calculation
  - `GetCacheKey(projectPath)` - hash of remote + worktree
  - `getRemoteHash()`, `getWorktreeHash()` with git commands
  - `normalizeRemoteURL()` to handle git@/https:// formats
  - Unit tests with multiple remote formats
- [ ] Create `internal/cache/settings.go` with settings management
  - `Settings` struct matching spec schema
  - `LoadOrCreateSettings(projectPath)` with JSON handling
  - `Save(projectPath)` with atomic writes
  - `GetCachePath(cacheKey)` helper
  - Unit tests for serialization

**Parallel Group 1B: Cache Migration Logic** (go-engineer-2)
- [ ] Create `internal/cache/migration.go` with migration handling
  - `EnsureCacheLocation(projectPath)` - detect key changes
  - `pathExists()`, `expandPath()` helpers
  - Atomic rename with cross-filesystem fallback
  - Unit tests with mock filesystems

**Parallel Group 1C: Branch Detection** (go-engineer-3)
- [ ] Create `internal/cache/branch.go` with branch utilities
  - `getCurrentBranch(projectPath)` - git branch --show-current
  - `findAncestorBranch()` - git merge-base logic
  - Handle detached HEAD state
  - Unit tests with test git repos

**Dependencies:** None (all groups fully independent)

**Estimated Time:** 1-2 days (parallel execution)

---

### Phase 2: SQLite Schema & Storage Layer (Parallelizable)

**Goal:** Implement complete SQLite schema and storage operations

**Parallel Group 2A: Schema Definition** (database-agent)
- [ ] Create `internal/storage/schema.go` with complete DDL
  - All 11 table definitions from spec (files, types, functions, etc.)
  - Foreign key relationships with CASCADE
  - Indexes for query performance
  - `createSchema(db)` function for initialization
  - Schema version management in cache_metadata table
- [ ] Create `internal/storage/migrations.go` for future schema changes
  - Migration framework for version upgrades
  - Rollback support

**Parallel Group 2B: Chunk Storage Operations** (go-engineer-4)
- [ ] Create `internal/storage/chunk_writer.go`
  - `ChunkWriter` struct with SQLite connection
  - `NewChunkWriter(cachePath, branch)` - opens/creates DB
  - `WriteChunks(chunks, branch)` - atomic transaction
  - `WriteChunksIncremental(chunks)` - UPDATE/DELETE/INSERT for hot reload
  - Embedding serialization helpers (float32 → bytes)
  - Unit tests with in-memory SQLite
- [ ] Create `internal/storage/chunk_reader.go`
  - `ChunkReader` struct for read-only access
  - `NewChunkReader(cachePath, branch)` - opens read-only
  - `ReadAllChunks()` - load all chunks (for chromem/bleve)
  - `ReadChunksByFile(filePath)` - filtered queries
  - Embedding deserialization helpers (bytes → float32)
  - Unit tests

**Parallel Group 2C: Graph Storage Operations** (go-engineer-5)
- [ ] Create `internal/storage/graph_writer.go`
  - `GraphWriter` struct for graph data
  - `WriteGraphData(graphData)` - write to relational tables
  - Write types, functions, relationships, calls, imports tables
  - Atomic transaction support
  - Unit tests with sample graph data
- [ ] Create `internal/storage/graph_reader.go`
  - `GraphReader` struct for queries
  - `ReadGraphData()` - reconstruct GraphData from SQL
  - `BuildCallGraph()` - create in-memory graph from SQL
  - Query helpers for common patterns
  - Unit tests

**Parallel Group 2D: File Statistics Storage** (go-engineer-6)
- [ ] Create `internal/storage/file_writer.go`
  - `FileWriter` for files and modules tables
  - `WriteFileStats(fileStats)` - write file metadata
  - `WriteFileContent(filePath, content)` - FTS5 insert
  - Aggregation logic for modules table
  - Unit tests
- [ ] Create `internal/storage/file_reader.go`
  - `FileReader` for file queries
  - `GetFileStats(filePath)` - read file metadata
  - `QueryFiles(filters)` - filtered queries
  - Unit tests

**Dependencies:**
- 2A (schema) must complete before 2B/2C/2D can run their tests
- 2B/2C/2D can run fully in parallel once 2A done

**Estimated Time:** 2-3 days (parallel execution after schema)

---

### Phase 3: Indexer Integration (Sequential with Internal Parallelism)

**Goal:** Integrate SQLite storage into existing indexer

**Task 3A: Indexer Storage Abstraction** (go-engineer-7)
- [ ] Create `internal/indexer/storage_interface.go`
  - Define `Storage` interface for writer abstraction
  - `JSONStorage` implementation (current behavior)
  - `SQLiteStorage` implementation (new behavior)
  - Factory function `NewStorage(config)` - returns interface
  - Unit tests with both implementations

**Task 3B: Indexer Core Modifications** (go-engineer-7)
- [ ] Update `internal/indexer/indexer.go`
  - Replace `AtomicWriter` with `Storage` interface
  - Add cache location detection via `cache.EnsureCacheLocation()`
  - Add branch detection via `cache.getCurrentBranch()`
  - Support both JSON and SQLite modes via config
  - Integration tests with SQLite storage

**Task 3C: Incremental Indexing for SQLite** (go-engineer-7)
- [ ] Update incremental indexing logic in `indexer.go`
  - Detect changed files via checksums (existing logic)
  - For SQLite: call `WriteChunksIncremental()` with row-level updates
  - For JSON: keep existing merge logic
  - Integration tests with hot reload scenarios

**Dependencies:**
- 3A → 3B → 3C (must be sequential)
- Depends on Phase 2 completion

**Estimated Time:** 2-3 days (sequential)

---

### Phase 4: MCP Server Integration (Parallelizable After Core)

**Goal:** Update MCP server to load from SQLite cache

**Task 4A: MCP Loader Refactoring** (go-engineer-8)
- [ ] Create `internal/mcp/loader_sqlite.go`
  - `LoadChunksFromSQLite(cachePath, branch)` function
  - Opens read-only SQLite connection
  - Reads all chunks (for chromem-go in-memory index)
  - Error handling and fallback to JSON if SQLite missing
- [ ] Update `internal/mcp/loader.go`
  - Add cache location detection
  - Try SQLite first, fallback to JSON
  - Add branch detection and logging
  - Unit tests for both paths

**Task 4B: Branch Watcher** (go-engineer-9)
- [ ] Create `internal/cache/branch_watcher.go`
  - `BranchWatcher` struct with fsnotify on .git/HEAD
  - `NewBranchWatcher(projectPath, onChange)` constructor
  - Debounced change detection (100ms)
  - Periodic fallback check (10s ticker)
  - `Close()` for cleanup
  - Unit tests with mock filesystem
- [ ] Integrate into `internal/mcp/server.go`
  - Start branch watcher on server startup
  - On branch change: reload chunks from new branch DB
  - Rebuild chromem/bleve indexes
  - Add logging and metrics

**Task 4C: Hot Reload Enhancement** ✅ (COMPLETED 2025-11-02)
- [x] Update `internal/mcp/watcher.go`
  - [x] Add support for SQLite cache updates
  - [x] Multi-path watching (JSON dir + SQLite file)
  - [x] Auto-detection of SQLite database (NewFileWatcherAuto)
  - [x] Backward compatibility with JSON-only mode
  - [x] Integration tests for SQLite file watching

**Dependencies:**
- 4A depends on Phase 2 & 3
- 4B and 4C can run in parallel after 4A
- All depend on Phase 2 storage layer

**Estimated Time:** 2-3 days (parallel execution)

---

### Phase 5: SQLite Extensions Integration (Specialized)

**Goal:** Integrate sqlite-vec for vector search and FTS5 for full-text

**Task 5A: sqlite-vec Integration** (go-engineer-10)
- [ ] Research sqlite-vec extension (github.com/asg017/sqlite-vec)
  - Build/distribution strategy (CGO vs pure-go)
  - Extension loading mechanism
  - Vector index creation and maintenance
- [ ] Create `internal/storage/vector_index.go`
  - `CreateVectorIndex(db)` - CREATE VIRTUAL TABLE vec_chunks
  - `UpdateVectorIndex(db, chunks)` - INSERT/DELETE operations
  - `QueryVector(db, queryEmb, filters, limit)` - MATCH query
  - Benchmark against chromem-go for performance comparison
- [ ] Add vector search to ChunkReader
  - `SearchByEmbedding(queryEmb, filters)` method
  - SQL query generation with JOINs for filtering
  - Performance tests

**Task 5B: FTS5 Integration** (go-engineer-10)
- [ ] Create `internal/storage/fts_index.go`
  - `CreateFTSIndex(db)` - CREATE VIRTUAL TABLE files_fts
  - `UpdateFTSIndex(db, filePath, content)` - sync with files table
  - `QueryFTS(db, query, filters, limit)` - MATCH with snippets
  - Tokenizer configuration (unicode61 with separators)
- [ ] Update FileWriter to sync FTS5
  - Automatic FTS5 updates when files change
  - Atomic transaction with main tables

**Dependencies:**
- Both 5A and 5B can run in parallel
- Both depend on Phase 2 schema
- Independent of Phase 3 & 4 (can run in parallel)

**Estimated Time:** 3-4 days (complex, external dependencies)

---

### Phase 6: Migration from JSON (Critical Path)

**Goal:** Seamless migration from existing JSON chunk storage

**Task 6A: Migration Script** (go-engineer-11)
- [ ] Create `internal/migration/json_to_sqlite.go`
  - `MigrateFromJSON(projectPath)` - main entry point
  - Detect existing `.cortex/chunks/*.json` files
  - Detect existing `.cortex/graph/code-graph.json` file
  - Load all JSON data (chunks + graph)
  - Transform to normalized SQLite schema
  - Write to SQLite with proper relationships
  - Archive old files to `.cortex/chunks.old/`
  - Unit tests with fixture JSON files

**Task 6B: Automatic Migration on Index** (go-engineer-11)
- [ ] Update `internal/cli/index.go`
  - Detect if migration needed (JSON exists, SQLite missing)
  - Run migration before indexing
  - Log migration progress
  - Handle errors gracefully
  - Integration tests

**Task 6C: Manual Migration Command** (go-engineer-11)
- [ ] Create `internal/cli/migrate.go`
  - `cortex migrate-cache` command
  - Force migration even if SQLite exists
  - Dry-run mode to preview changes
  - Progress reporting
  - Integration tests

**Dependencies:**
- Depends on Phases 2, 3, 4 (needs full SQLite stack)
- Must be sequential (6A → 6B → 6C)

**Estimated Time:** 2-3 days (critical, needs thorough testing)

---

### Phase 7: Branch-Aware Indexing Optimization (Performance)

**Goal:** Optimize indexing by copying unchanged chunks from ancestor branches

**Task 7A: Ancestor Branch Detection** (go-engineer-12)
- [ ] Implement `findAncestorBranch()` in cache package
  - `git merge-base` with main/master
  - Handle repos without main/master
  - Cache results for performance
  - Unit tests with test git repos

**Task 7B: Fast-Path Indexing** (go-engineer-12)
- [ ] Create `internal/indexer/branch_optimizer.go`
  - `IndexWithBranchOptimization(projectPath)` function
  - Load ancestor branch DB if exists
  - Compare file hashes (SHA-256) and mtimes
  - Copy chunks for unchanged files (skip embedding)
  - Full index only changed files
  - Benchmark tests showing speedup

**Task 7C: Integration into Main Indexer** (go-engineer-12)
- [ ] Update `internal/cli/index.go`
  - Enable branch optimization via flag
  - Default enabled for non-main branches
  - Add metrics for copied vs regenerated chunks
  - Integration tests with branch scenarios

**Dependencies:**
- Depends on Phases 2, 3 (SQLite storage + indexer)
- Independent of Phase 4 (MCP server)
- Can run in parallel with Phase 5

**Estimated Time:** 2 days (optimization, non-critical)

---

### Phase 8: LRU Eviction & Cache Management (Operational)

**Goal:** Automatic cache cleanup for disk space management

**Task 8A: Cache Metadata Management** (go-engineer-13)
- [ ] Create `internal/cache/metadata.go`
  - `CacheMetadata` struct from spec
  - `LoadMetadata(cacheDir)` and `SaveMetadata()`
  - `UpdateBranchStats(branch, size, chunkCount)`
  - Track last accessed times
  - Unit tests

**Task 8B: LRU Eviction Logic** (go-engineer-13)
- [ ] Create `internal/cache/eviction.go`
  - `EvictStaleBranches(cacheDir, maxAgeDays, maxSizeMB)`
  - List git branches via `git branch -a`
  - Identify eviction candidates (age, size, deleted branches)
  - Protect immortal branches (main/master)
  - Delete branch DB files
  - Update metadata
  - Unit tests with mock time

**Task 8C: Automatic Eviction Integration** (go-engineer-13)
- [ ] Add eviction hooks to indexer and MCP server
  - Run eviction after indexing completes
  - Run eviction on MCP server startup
  - Make configurable via environment variables
  - Add `cortex cache clean` command
  - Integration tests

**Dependencies:**
- Depends on Phase 2 (SQLite structure)
- Independent of other phases
- Can run in parallel with Phases 5, 6, 7

**Estimated Time:** 2 days (operational, non-critical)

---

### Phase 9: Testing & Documentation (Parallel)

**Goal:** Comprehensive testing and documentation

**Parallel Group 9A: Integration Testing** (go-engineer-14)
- [ ] Create `internal/storage/integration_test.go`
  - End-to-end SQLite storage tests
  - Test with real git repos
  - Branch switching scenarios
  - Migration scenarios
  - Performance benchmarks
- [ ] Create `internal/migration/integration_test.go`
  - Full migration test from JSON to SQLite
  - Verify data integrity after migration
  - Test rollback scenarios

**Parallel Group 9B: E2E Testing** (go-engineer-15)
- [ ] Create `tests/e2e/sqlite_cache_test.go`
  - Full workflow: index → switch branch → query
  - Test MCP server with SQLite cache
  - Test hot reload with SQLite updates
  - Performance tests (startup time, query time)

**Parallel Group 9C: Documentation** (docs-writer)
- [ ] Update `docs/architecture.md` with SQLite cache design
- [ ] Update `CLAUDE.md` with new cache structure
- [ ] Create `docs/cache-management.md` user guide
  - Cache location and structure
  - Branch isolation behavior
  - LRU eviction configuration
  - Migration guide
  - Troubleshooting
- [ ] Update README.md with cache changes

**Dependencies:**
- 9A/9B depend on all code phases (2-8)
- 9C can start earlier with spec documentation

**Estimated Time:** 2-3 days (parallel)

---

### Phase 10: Configuration & CLI (Final Integration)

**Goal:** Complete user-facing configuration and CLI commands

**Task 10A: Configuration Updates** (go-engineer-16)
- [ ] Update `internal/config/config.go`
  - Add `storage_backend` option (json/sqlite)
  - Add `cache_location` override
  - Add `branch_cache_enabled` flag
  - Add LRU eviction settings
  - Schema validation
  - Unit tests

**Task 10B: CLI Command Enhancements** (go-engineer-16)
- [ ] Update `internal/cli/index.go` flags
  - `--storage=sqlite|json` flag
  - `--no-branch-cache` flag
  - `--migrate` flag for forced migration
- [ ] Create `internal/cli/cache.go` - new cache command group
  - `cortex cache info` - show cache location and stats
  - `cortex cache clean` - manual eviction
  - `cortex cache migrate` - force migration
  - `cortex cache validate` - check integrity
- [ ] Update `internal/cli/mcp.go`
  - Detect SQLite vs JSON cache
  - Add cache stats to startup logging

**Task 10C: Environment Variables** (go-engineer-16)
- [ ] Document and implement env vars
  - `CORTEX_CACHE_DIR` - override cache location
  - `CORTEX_STORAGE_BACKEND` - force backend
  - `CORTEX_DISABLE_BRANCH_CACHE` - disable branch optimization
  - `CORTEX_CACHE_MAX_AGE_DAYS` - LRU setting
  - `CORTEX_CACHE_MAX_SIZE_MB` - LRU setting
- [ ] Update config loader to respect env vars

**Dependencies:**
- Depends on all previous phases
- Must be sequential (10A → 10B → 10C)

**Estimated Time:** 2 days (final polish)

---

## Parallelization Strategy Summary

### Maximum Parallel Execution Plan

**Week 1 (Foundation + Schema):**
- **Day 1-2:** Phase 1 - 3 parallel go-engineers (Groups 1A, 1B, 1C)
- **Day 3:** Phase 2A - database-agent creates schema
- **Day 4-5:** Phase 2 - 3 parallel go-engineers (Groups 2B, 2C, 2D)

**Week 2 (Core Integration):**
- **Day 6-8:** Phase 3 - 1 go-engineer (sequential indexer changes)
- **Day 6-8 (PARALLEL):** Phase 5 - 1 go-engineer (SQLite extensions, independent)
- **Day 9-10:** Phase 4 - 2 parallel go-engineers (MCP loader + watchers)

**Week 3 (Migration + Optimization):**
- **Day 11-13:** Phase 6 - 1 go-engineer (critical migration path)
- **Day 11-12 (PARALLEL):** Phase 7 - 1 go-engineer (branch optimization)
- **Day 11-12 (PARALLEL):** Phase 8 - 1 go-engineer (LRU eviction)

**Week 4 (Testing + Polish):**
- **Day 14-16:** Phase 9 - 3 parallel (2 go-engineers + 1 docs-writer)
- **Day 17-18:** Phase 10 - 1 go-engineer (final CLI integration)

**Total: ~18 days with 3-5 parallel engineers at peak**

### Dependency Graph

```
Phase 1 (1A, 1B, 1C) ────────┐
                             ├──→ Phase 2A (schema) ──→ Phase 2 (2B, 2C, 2D) ──┐
                             │                                                   │
                             │                                                   ├──→ Phase 3 ──→ Phase 6 ──┐
                             │                                                   │                          │
                             │                                                   ├──→ Phase 4 ──────────────┤
                             │                                                   │                          │
                             │                                                   ├──→ Phase 5 ──────────────┤
                             │                                                   │                          │
                             └───────────────────────────────────────────────────┴──→ Phase 7 ──────────────┤
                                                                                 │                          │
                                                                                 └──→ Phase 8 ──────────────┤
                                                                                                            │
                                                                                                            ├──→ Phase 9 ──→ Phase 10
```

### Critical Path
Phase 1 → Phase 2A → Phase 2B/2C/2D → Phase 3 → Phase 6 → Phase 9 → Phase 10

**Critical path duration: ~14 days**

### Non-Critical Parallel Work
- Phase 5 (SQLite extensions) - can run alongside Phase 3
- Phase 7 (branch optimization) - can run alongside Phase 6
- Phase 8 (LRU eviction) - can run alongside Phase 6

---

## Risk Assessment

### High Risk
- **sqlite-vec extension (Phase 5A):** External dependency, CGO complexity
  - **Mitigation:** Research early, have fallback to chromem-go
- **Migration data integrity (Phase 6):** Critical for existing users
  - **Mitigation:** Extensive testing, rollback support, dry-run mode

### Medium Risk
- **SQLite performance vs chromem-go (Phase 5A):** May not meet <100ms target
  - **Mitigation:** Benchmark early, optimize indexes, consider hybrid approach
- **Branch detection edge cases (Phase 4B):** Detached HEAD, worktrees
  - **Mitigation:** Comprehensive git scenario testing

### Low Risk
- **Parallel development coordination:** Multiple engineers, shared code
  - **Mitigation:** Clear interfaces defined upfront, code reviews

---

## Testing Strategy

### Unit Tests (per phase)
- Each phase includes unit tests
- Target: 80%+ coverage for new code
- Use in-memory SQLite for storage tests
- Mock git commands for cache tests

### Integration Tests
- Phase 9A: Storage layer integration
- Phase 9A: Migration integrity tests
- Phase 4B/4C: MCP server hot reload tests
- Phase 7B: Branch optimization benchmarks

### E2E Tests
- Phase 9B: Full workflow tests
- Real git repos with branches
- Performance tests (startup, query, indexing)
- Backward compatibility with JSON

### Performance Benchmarks
- Phase 5: sqlite-vec vs chromem-go
- Phase 7: Branch optimization speedup
- Phase 9B: Overall system performance

---

## Rollback Plan

If critical issues discovered:

1. **Config-based rollback:** Set `storage_backend: json` in config
2. **File-based rollback:** Restore `.cortex/chunks.old/` → `.cortex/chunks/`
3. **Binary rollback:** Downgrade to pre-SQLite cortex version
4. **Data preservation:** SQLite cache remains at `~/.cortex/cache/` (no data loss)

## Definition of Done

Each phase is complete when:
- [ ] All code implemented with tests
- [ ] Unit tests passing (80%+ coverage)
- [ ] Integration tests passing (if applicable)
- [ ] Code reviewed by go-code-reviewer agent
- [ ] Documentation updated
- [ ] Performance benchmarks meet targets (if applicable)