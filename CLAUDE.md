# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Project Cortex is a Go application that enables deep semantic search of **both code and documentation** for LLM-powered coding assistants.

**cortex** - Single CLI binary for indexing code/docs, running MCP server, and managing the embedding server

The architecture follows a daemon-based continuous indexing pipeline:
- **Indexing**: Parse code (tree-sitter) + docs → Extract structured knowledge → Chunk → Embed → Store in SQLite
- **Storage**: Branch-isolated SQLite databases in `~/.cortex/cache/{cache-key}/branches/{branch}.db` (not version controlled)
- **Serving**: MCP server queries SQLite directly using sqlite-vec for vector search and FTS5 for keyword search
- **Daemon**: Single machine-wide indexer daemon watches all registered projects and incrementally updates caches

## Indexer Daemon

The indexer daemon (`cortex indexer`) provides continuous, automatic code indexing:

- **Auto-start**: `cortex index` automatically starts the daemon if not running
- **Continuous watching**: Git branch switches and file changes trigger incremental re-indexing
- **Resource efficient**: Single daemon instance watches all registered projects (75% memory savings vs per-process watching)
- **Zero configuration**: Projects are automatically registered on first index

**Commands:**
```bash
cortex indexer start   # Start daemon manually (rarely needed - auto-starts)
cortex indexer stop    # Stop daemon gracefully
cortex indexer status  # Show daemon and all watched projects
cortex indexer logs -f # Stream indexing logs (tail -f style)
```

**Workflow:**
1. Run `cortex index` in a project (first time)
2. Daemon registers project and performs initial indexing
3. Daemon continues watching for changes (git branch switches + file modifications)
4. Branch switches trigger automatic re-indexing with branch-isolated databases
5. MCP servers query always-fresh SQLite caches directly

**Note**: All MCP tools query SQLite databases maintained by the daemon. No manual re-indexing needed after initial setup.

## Using the Cortex MCP Search Tools

**IMPORTANT**: When working with this project, use the Cortex MCP search tools as your **starting point** for understanding code, architecture, and design decisions.

Project Cortex provides **two complementary search tools**:

1. **`cortex_search`** - Semantic vector search (understands concepts and meaning)
2. **`cortex_exact`** - Full-text keyword search (finds exact matches and boolean queries)

### Choosing Between cortex_search and cortex_exact

**Use `cortex_search` (semantic) for:**
- Natural language questions ("how does X work?", "why was Y implemented this way?")
- Conceptual searches ("authentication flow", "error handling patterns")
- Cross-referencing code + documentation together
- Finding related code by similarity, not exact wording
- Discovering architectural patterns and design decisions

**Use `cortex_exact` (keyword) for:**
- Finding exact identifiers (`sync.RWMutex`, `Provider`, `handleRequest`)
- Boolean queries (`Provider AND interface`, `handler OR controller`)
- Excluding terms (`authentication AND -test`)
- Prefix matching (`Prov*` finds Provider, ProviderConfig, etc.)
- Phrase search (`"error handling"` - exact sequence)
- When you know the exact term but not where it's used

**Use both (hybrid approach) for:**
- "Find sync.RWMutex and explain how it's used" (exact → semantic)
- "Search for Provider implementations and show their error handling" (exact → semantic)

### When to Use Semantic Search

**Use `mcp__project-cortex__cortex_search` for:**
- Understanding "why" something was built a certain way (architecture, design decisions)
- Finding code by concept/functionality (not just exact text matching)
- Discovering what exists and where (navigation)
- Understanding how features work end-to-end (combining docs + code)
- Finding configuration values, constants, defaults

**Don't use for:**
- Exact identifier searches (use cortex_exact instead)
- Boolean keyword queries (use cortex_exact instead)
- Listing all files (use Glob/file system tools)
- Reading specific known files (use Read tool)

### Quick Reference

```typescript
mcp__project-cortex__cortex_search({
  query: string,              // Natural language query (required)
  limit?: number,             // Max results, 1-100 (default: 15)
  chunk_types?: string[],     // Filter: ["documentation", "symbols", "definitions", "data"]
  tags?: string[],            // Filter: ["go", "typescript", etc.] (AND logic)
  include_stats?: boolean     // Include reload metrics (default: false)
})
```

**Chunk Types** (filter by granularity):
- `documentation` - README, guides, design docs, ADRs (the "why")
- `symbols` - High-level code overview (package, type/function names with line numbers)
- `definitions` - Full function/type signatures with comments (the "what")
- `data` - Constants, configs, enum values, defaults

**Tags** (filter by context, AND logic):
- Language: `go`, `typescript`, `python`, `rust`, etc.
- Type: `code`, `documentation`, `markdown`
- Custom: `architecture`, `auth`, `security`, etc.

### Common Search Patterns

#### 1. Understanding Architecture/Design
```typescript
// Find design rationale and decisions
mcp__project-cortex__cortex_search({
  query: "why was authentication implemented this way",
  chunk_types: ["documentation"],
  limit: 10
})
```

#### 2. Code Navigation
```typescript
// Find code structure and signatures
mcp__project-cortex__cortex_search({
  query: "authentication handler implementation",
  chunk_types: ["symbols", "definitions"]
})
```

#### 3. Finding Configuration
```typescript
// Discover constants and default values
mcp__project-cortex__cortex_search({
  query: "database connection settings",
  chunk_types: ["data"]
})
```

#### 4. Complete Context (Default)
```typescript
// Get full picture: docs + code
mcp__project-cortex__cortex_search({
  query: "how does hot reload work",
  limit: 20
  // No filters = search all chunk types
})
```

#### 5. Language-Specific Search
```typescript
// Find patterns in specific language
mcp__project-cortex__cortex_search({
  query: "error handling patterns",
  tags: ["go", "code"]  // Must have BOTH tags (AND logic)
})
```

### How Filtering Works

- **`chunk_types`**: OR logic within array. `["symbols", "definitions"]` returns chunks matching ANY type.
- **`tags`**: AND logic. `["go", "code"]` returns chunks with BOTH tags.
- **Empty arrays**: No filtering (search all).

### Performance

Typical query: ~60-110ms (50-100ms embedding + <10ms vector search)

### Return Format

Results include structured data with metadata:
- File paths (relative to project root)
- Line numbers (start_line, end_line)
- Chunk type, tags, language
- Relevance score (0-1, higher is better)
- Natural language formatted text optimized for embeddings

**Note**: Results are sorted by relevance score. Databases are always current because the indexer daemon updates them on every file/branch change.

## Using cortex_exact for Keyword Search

**`cortex_exact`** provides fast full-text search using SQLite FTS5 syntax. Unlike semantic search, it finds exact text matches with boolean logic and phrase matching.

### Quick Reference

```typescript
mcp__project-cortex__cortex_exact({
  query: string,              // SQLite FTS5 query syntax (required)
  limit?: number,             // Max results, 1-100 (default: 15)
  language?: string,          // Filter by language (e.g., "go", "typescript", "python")
  file_path?: string          // Filter by file path using SQL LIKE syntax (e.g., "internal/%", "%_test.go")
})
```

**Query Syntax** (SQLite FTS5):
- **Phrase search:** Use double quotes for exact phrases: `"sync.RWMutex"` or `"error handling"`
- **Boolean AND:** `Provider AND interface`, `chromem AND incremental`
- **Boolean OR:** `handler OR controller`, `chromem OR bleve`
- **Boolean NOT:** `embedding NOT test`, `authentication NOT test`
- **Prefix wildcard:** `handler*` (matches handler, handlers, handleRequest)
- **Grouping:** `(vector OR semantic) AND search`
- **Complex queries:** `(handler OR controller) AND typescript NOT test`

**Important:** For dotted identifiers like `sync.RWMutex`, use phrase search with double quotes: `"sync.RWMutex"`.

**Filters:**
- **`language`:** Exact match filter (e.g., `language="go"` only returns Go files)
- **`file_path`:** SQL LIKE pattern filter (e.g., `file_path="internal/%"` matches all files in internal/, `file_path="%_test.go"` matches all test files)

**Performance:** Typical queries execute in 2-8ms, making exact search 10-50x faster than semantic search.

### Common Query Patterns

#### 1. Find Exact Identifier
```typescript
// Find all occurrences of sync.RWMutex
mcp__project-cortex__cortex_exact({
  query: '"sync.RWMutex"',  // Use double quotes for dotted identifiers
  limit: 20
})
```

#### 2. Filter by Language
```typescript
// Find Provider interfaces in Go files only
mcp__project-cortex__cortex_exact({
  query: "Provider AND interface",
  language: "go",
  limit: 10
})
```

#### 3. Filter by File Path
```typescript
// Find handler functions in internal/ directory only
mcp__project-cortex__cortex_exact({
  query: "handler",
  file_path: "internal/%",
  limit: 10
})
```

#### 4. Combined Filters
```typescript
// Find handlers in Go files within internal/ directory
mcp__project-cortex__cortex_exact({
  query: "handler",
  language: "go",
  file_path: "internal/%",
  limit: 10
})
```

#### 5. Phrase Search
```typescript
// Find exact phrase "error handling"
mcp__project-cortex__cortex_exact({
  query: '"error handling"',
  limit: 10
})
```

#### 6. Complex Boolean
```typescript
// Find handlers or controllers in TypeScript, not in tests
mcp__project-cortex__cortex_exact({
  query: "(handler OR controller) NOT test",
  language: "typescript"
})
```

### Response Format

Results include:
- **Full chunk data** with metadata (file path, line numbers, tags, chunk type)
- **Relevance score** (0-1, based on TF-IDF and query match quality)
- **Highlights** - Matching snippets with `<mark>` tags around matches
- **Performance metadata** - Query execution time

Example response:
```json
{
  "query": "text:Provider AND tags:go",
  "results": [
    {
      "chunk": {
        "id": "code-definitions-internal/embed/provider.go",
        "text": "type Provider interface {...}",
        "chunk_type": "definitions",
        "tags": ["code", "go", "definitions"],
        "metadata": {"file_path": "internal/embed/provider.go"}
      },
      "score": 1.659,
      "highlights": [
        "type <mark>Provider</mark> interface {",
        "// <mark>Provider</mark> specifies which embedding <mark>provider</mark> to use"
      ]
    }
  ],
  "total_found": 10,
  "total_returned": 10,
  "metadata": {
    "took_ms": 3,
    "source": "exact"
  }
}
```

### Performance

Typical query: ~0-5ms (sub-millisecond for simple queries, <10ms for complex boolean)

### Hybrid Search Patterns

Combine `cortex_exact` and `cortex_search` for powerful workflows:

**Pattern 1: Find → Understand**
```typescript
// 1. Find exact identifier with cortex_exact
const matches = await cortex_exact({
  query: "text:sync.RWMutex",
  chunk_types: ["definitions"]
})

// 2. Understand usage with cortex_search
const context = await cortex_search({
  query: "mutex synchronization patterns in searcher",
  limit: 10
})
```

**Pattern 2: Boolean Filter → Semantic Explore**
```typescript
// 1. Boolean filter to specific files
const providers = await cortex_exact({
  query: "text:Provider AND chunk_type:definitions"
})

// 2. Semantic search within those contexts
const errorHandling = await cortex_search({
  query: "error handling patterns",
  tags: providers.results.map(r => r.chunk.tags)
})
```

### Field Reference

All fields are indexed and searchable:
- **`text`** - Chunk content (PRIMARY search target, default if no field specified)
- **`chunk_type`** - Filter by: `symbols`, `definitions`, `data`, `documentation`
- **`tags`** - Language tags (`go`, `typescript`), type tags (`code`, `documentation`), etc.
- **`file_path`** - File location (supports partial matching)
- **`title`** - Chunk title

**Recommendation:** Always scope to `text:` field to avoid noise from metadata matches.

## Using cortex_files for Code Statistics

**`cortex_files`** provides SQL-queryable access to code statistics and metadata. Use it to answer quantitative questions about project structure without slow bash commands.

### Quick Reference

```typescript
mcp__project-cortex__cortex_files({
  operation: "query",              // Always "query"
  query: {
    from: string,                  // Table: files, types, functions, imports (required)
    fields?: string[],             // Columns to select (default: *)
    where?: Filter,                // WHERE clause
    joins?: Join[],                // JOIN clauses
    groupBy?: string[],            // GROUP BY columns
    having?: Filter,               // HAVING clause
    orderBy?: OrderBy[],           // ORDER BY clauses
    limit?: number,                // LIMIT (1-1000)
    offset?: number,               // OFFSET
    aggregations?: Aggregation[]   // COUNT, SUM, AVG, MIN, MAX
  }
})
```

**Available Tables:**
- `files` - File metadata (language, is_test, module_path, line_count_total, line_count_code, etc.)
- `types` - Type definitions (name, kind, is_exported, field_count, method_count)
- `functions` - Function/method metadata (name, line_count, is_exported, is_method, param_count)
- `imports` - Import declarations (import_path, is_standard_lib, is_external)

**Pro tip:** Use `module_path` with GROUP BY for module-level statistics (no separate modules table needed!).

### Common Query Patterns

#### 1. Find Largest Files
```typescript
mcp__project-cortex__cortex_files({
  operation: "query",
  query: {
    from: "files",
    fields: ["file_path", "line_count_code"],
    where: {field: "is_test", operator: "=", value: false},
    orderBy: [{field: "line_count_code", direction: "DESC"}],
    limit: 10
  }
})
```

#### 2. Count Files by Language
```typescript
mcp__project-cortex__cortex_files({
  operation: "query",
  query: {
    from: "files",
    aggregations: [
      {function: "COUNT", alias: "file_count"},
      {function: "SUM", field: "line_count_code", alias: "total_code"}
    ],
    groupBy: ["language"],
    orderBy: [{field: "total_code", direction: "DESC"}]
  }
})
```

#### 3. Module Statistics
```typescript
// Get top 10 modules by code size
mcp__project-cortex__cortex_files({
  operation: "query",
  query: {
    from: "files",
    aggregations: [
      {function: "COUNT", alias: "file_count"},
      {function: "SUM", field: "line_count_code", alias: "code_lines"}
    ],
    groupBy: ["module_path"],
    orderBy: [{field: "code_lines", direction: "DESC"}],
    limit: 10
  }
})
```

#### 4. Find Exported Types
```typescript
mcp__project-cortex__cortex_files({
  operation: "query",
  query: {
    from: "types",
    fields: ["name", "kind", "file_path"],
    where: {
      and: [
        {field: "is_exported", operator: "=", value: true},
        {field: "kind", operator: "=", value: "interface"}
      ]
    }
  }
})
```

#### 5. Complex Filters
```typescript
// Files with >500 lines OR in internal/
mcp__project-cortex__cortex_files({
  operation: "query",
  query: {
    from: "files",
    where: {
      or: [
        {field: "line_count_code", operator: ">", value: 500},
        {field: "module_path", operator: "LIKE", value: "internal/%"}
      ]
    }
  }
})
```

### Filter Operators

- **Comparison:** `=`, `!=`, `>`, `>=`, `<`, `<=`
- **Pattern matching:** `LIKE`, `NOT LIKE` (use `%` wildcards)
- **Set operations:** `IN`, `NOT IN` (value must be array)
- **NULL checks:** `IS NULL`, `IS NOT NULL` (no value needed)
- **Range:** `BETWEEN` (value must be `[min, max]`)
- **Logical:** `AND`, `OR`, `NOT` (combine filters)

### Aggregation Functions

- `COUNT` - Count rows (use without field for COUNT(*))
- `SUM` - Sum numeric values
- `AVG` - Average numeric values
- `MIN` - Minimum value
- `MAX` - Maximum value
- Add `distinct: true` for COUNT(DISTINCT field)

### Performance

Typical query: <5ms
Complex aggregations: <10ms
No pre-aggregation or materialized views needed - SQLite GROUP BY is instant for thousands of files.

### Hybrid Workflows

**Pattern 1: Statistics → Semantic exploration**
```
1. cortex_files: "Which modules have >5000 lines?"
   → Returns: internal/storage, internal/indexer
2. cortex_search: "How does the indexer work?"
   → Returns: Semantic explanation with code
```

**Pattern 2: Find large files → Examine exact code**
```
1. cortex_files: "Files >1000 lines"
   → Returns: server.go, handler.go
2. cortex_exact: "text:sync.RWMutex AND file_path:server.go"
   → Find specific patterns in large files
```

## Using cortex_graph for Code Relationships

**`cortex_graph`** enables structural code relationship queries for refactoring, impact analysis, and dependency exploration. All data is stored in SQLite (not a separate graph file).

### Quick Reference

```typescript
mcp__project-cortex__cortex_graph({
  operation: string,         // Required: operation type (see below)
  target: string,            // Required: e.g., "embed.Provider", "internal/mcp"
  include_context?: bool,    // Include code snippets (default: true)
  context_lines?: number,    // Context lines (default: 3, max: 20)
  depth?: number,            // Traversal depth (default: 1, max: 10)
  max_results?: number       // Max results (default: 100, max: 500)
})
```

### Operations

- **`callers`** - Find who calls this function
  - Target: Function name (e.g., "Actor.Start", "NewServer")
  - Returns: List of call sites with file paths and line numbers

- **`callees`** - Find what this function calls
  - Target: Function name
  - Returns: List of called functions

- **`dependencies`** - Find package imports
  - Target: Package path (e.g., "internal/mcp")
  - Returns: List of packages this package imports

- **`dependents`** - Find who imports this package
  - Target: Package path
  - Returns: List of packages importing this package

- **`type_usages`** - Find where type is used
  - Target: Type name (e.g., "Provider", "Actor")
  - Returns: Usage locations (parameters, returns, fields)

- **`implementations`** - Find types implementing an interface
  - Target: Interface name (e.g., "Provider")
  - Returns: Concrete types implementing the interface

- **`path`** - Find shortest path between nodes
  - Target: Two identifiers separated by " -> " (e.g., "main -> Provider.Embed")
  - Returns: Call chain connecting the two

- **`impact`** - Combined callers + dependents analysis
  - Target: Function or package name
  - Returns: Both direct callers and packages that depend on this

### Performance

Typical query: 1-20ms (SQL WITH RECURSIVE CTEs, no in-memory graph loading)

### Implementation Details

Graph data is stored in SQLite tables (`code_graph_*`) and queried directly using SQL. No separate graph file or in-memory graph structure. Lazy-loaded on first query per MCP server session.

### Common Query Patterns

#### 1. Impact Analysis
```typescript
// What code depends on this function?
mcp__project-cortex__cortex_graph({
  operation: "callers",
  target: "indexer.Index",
  depth: 2  // Find transitive callers
})
```

#### 2. Dependency Exploration
```typescript
// What does this package import?
mcp__project-cortex__cortex_graph({
  operation: "dependencies",
  target: "internal/indexer/daemon"
})
```

#### 3. Interface Implementations
```typescript
// Find all embedding providers
mcp__project-cortex__cortex_graph({
  operation: "implementations",
  target: "embed.Provider"
})
```

#### 4. Call Chain Analysis
```typescript
// How does main reach Embed()?
mcp__project-cortex__cortex_graph({
  operation: "path",
  target: "main -> Provider.Embed"
})
```

### Hybrid Workflow with Other Tools

**Pattern 1: Find exact → Graph relationships**
```
1. cortex_exact: Find "Provider" definitions
   → Returns: embed.Provider interface
2. cortex_graph: Find implementations
   → Returns: LocalProvider, DaemonProvider
3. cortex_search: "How do providers handle errors?"
   → Returns: Semantic explanation
```

**Pattern 2: Statistics → Graph → Code**
```
1. cortex_files: Find largest modules
   → Returns: internal/indexer (5000 lines)
2. cortex_graph: Find dependents
   → Returns: 15 packages depend on indexer
3. cortex_exact: Find specific usage patterns
```

## Context7 - External Library & API Documentation
Always use context7 when I need help with code generation, setup or configuration steps, or
library/API documentation FOR EXTERNAL LIBRARIES. For documentation about this project use project_cortext mcp. This means you should automatically use the Context7 MCP
tools to resolve library id and get library docs without me having to explicitly ask.

## Common Commands

### Indexing (Daemon-Based)

```bash
# First-time indexing (starts daemon, registers project)
cortex index

# Check daemon status
cortex indexer status

# Stream indexing logs
cortex indexer logs -f

# Stop daemon (optional, auto-restarts on demand)
cortex indexer stop
```

The daemon automatically:
- Watches for file changes and re-indexes incrementally
- Detects git branch switches and updates branch-specific DBs
- Manages all registered projects from a single process

### Building

```bash
# Build cortex CLI
task build

# Cross-compile for specific platform
task build:cross OS=linux ARCH=amd64

# Cross-compile for all platforms
task build:cross:all
```

### Testing

```bash
# Run all tests
task test

# Run with coverage report
task test:coverage

# Run with race detector
task test:race

# Run specific component tests
go test ./internal/indexer/...
go test ./internal/mcp/...
go test ./internal/embed/...
```

### Code Quality

```bash
# Format code
task fmt

# Run go vet
task vet

# Run linter (requires golangci-lint)
task lint

# Run all checks (fmt + vet + lint + test)
task check
```

### Running

```bash
# Run cortex CLI
task run -- index           # Build and run cortex index
task run -- mcp             # Build and run MCP server
task run -- embed start     # Build and run embedding server
```

### Development

```bash
# Watch mode (requires entr or watchexec)
task dev

# Show build info
task info

# Clean build artifacts
task clean
```

## High-Level Architecture

### Three-Tier Code Extraction

Code is extracted at three granularity levels, each stored in SQLite tables:

1. **Symbols**: High-level file overview
   - Package/module name, import count, type/function names with line numbers
   - Natural language format: "Package: server\n\nTypes:\n  - Handler (struct) (lines 10-15)"
   - Use for: Quick navigation, understanding file structure

2. **Definitions**: Full signatures without implementations
   - Complete type definitions, interfaces, function signatures with comments
   - Actual code format with line comments: "// Lines 10-15\ntype Handler struct {...}"
   - Use for: Understanding contracts, APIs, type relationships

3. **Data**: Constants and configuration
   - Constant declarations, global variables, enum values, defaults
   - Actual code format with line comments
   - Use for: Configuration discovery, finding default values

**SQLite Storage:**
- `vec_chunks` - All chunks with embeddings (vector search via sqlite-vec)
- `files_fts` - Full-text search index (FTS5 for cortex_exact)
- `files`, `types`, `functions`, `imports` - Structured metadata (for cortex_files)
- `code_graph_*` - Graph relationships for traversal queries (for cortex_graph)

### Documentation Chunking

Documentation is semantically chunked by headers and stored in the SQLite `vec_chunks` table:
- Split at `##` headers when possible (preserves topic coherence)
- Falls back to paragraph splitting for large sections
- Never splits inside code blocks
- Metadata includes section_index, chunk_index, file_path, start_line, end_line

**Why this matters**: LLMs get architectural context, design decisions, and the "why" behind code—not just the "what".

### Chunk Format

All chunks use a format optimized for vector embeddings:

```json
{
  "id": "unique-identifier",
  "chunk_type": "symbols|definitions|data|documentation",
  "title": "Human-readable title",
  "text": "Natural language formatted content",
  "embedding": [0.123, -0.456, ...],
  "tags": ["code", "go", "symbols"],
  "metadata": {
    "source": "code|markdown",
    "file_path": "relative/path/to/file.go",
    "language": "go",
    "start_line": 10,
    "end_line": 75
  },
  "created_at": "2025-10-16T12:35:00Z",
  "updated_at": "2025-10-16T12:35:00Z"
}
```

**Natural language formatting**: The `text` field contains natural language (not JSON structures) because embedding models understand "Package: server" better than `{"package": "server"}`.

**Note**: This JSON representation shows the logical structure. Actual storage is in normalized SQLite tables for query performance.

### Embedding Provider Interface

Both indexer and MCP server use a shared provider interface (`internal/embed/provider.go`):

```go
type Provider interface {
    Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error)
    Dimensions() int
    Close() error
}

type EmbedMode string
const (
    EmbedModeQuery   EmbedMode = "query"   // For search queries
    EmbedModePassage EmbedMode = "passage" // For document chunks
)
```

**Implementation**:
- `LocalProvider` (internal/embed/local.go): Calls Rust library via CGO FFI
- Rust implementation (internal/embeddings-ffi/): Uses tract-onnx + rayon for parallel inference
- Model: Quantized embedding model with 384 dimensions (~200MB)
- Performance: 2-3x faster than Python-based approach

**Factory pattern** (internal/embed/factory.go): `embed.NewProvider(config)` returns interface

**Rust FFI Architecture**:
```
Go (internal/embed/local.go)
  ↓ CGO call
Rust (internal/embeddings-ffi/src/lib.rs)
  ├── tract-onnx (ONNX model inference)
  ├── rayon (parallel batch processing)
  └── tokenizers (text tokenization)
```

**Daemon pattern**: Provider can spawn embedding daemon via Unix socket if needed, auto-restarts on crashes.

**Critical**: Use correct mode—`EmbedModePassage` for indexing documents, `EmbedModeQuery` for search queries.

### MCP Server Architecture

The MCP server (`cortex mcp`) uses mcp-go and queries SQLite directly:

1. **Startup**: Detect current branch → Open branch-specific SQLite DB (read-only) → Register MCP tools → Listen on stdio
2. **Query**: Receive MCP request → Generate query embedding (via provider) → SQLite vector similarity search via sqlite-vec → Filter by chunk_types/tags → Return results
3. **Branch switching**: Git watcher detects `.git/HEAD` change → Open new branch DB → Atomic swap (old queries fail gracefully, new queries use new DB)

**No hot reload needed**: Databases are always current because indexer daemon updates them on file/branch changes.

**Composable tool registration pattern**:
```go
func AddCortexSearchTool(s *server.MCPServer, dbProvider DBProvider)
```
Allows combining multiple MCP tools in one server.

**Tool interface**: All tools accept `DBProvider` interface for thread-safe database access.

**MCP Tools (all query same SQLite database)**:
- `cortex_search` - Vector similarity search (sqlite-vec extension)
- `cortex_exact` - Full-text keyword search (FTS5 index)
- `cortex_files` - SQL queries on file metadata
- `cortex_graph` - Code relationship queries (lazy-loaded from SQLite)

### Incremental Indexing

Tracks file changes via SHA-256 checksums (stored in SQLite `files` table):
- Only reprocess changed files (detected via mtime + checksum)
- Incremental updates via SQL transactions
- Branch-specific databases enable fast branch switching (copy from merge-base)
- Chunk IDs stable for unchanged files (no unnecessary embedding regeneration)

**Branch isolation benefits:**
- Switch branches instantly (just open different .db file)
- No re-indexing for unchanged files when switching back
- Each branch has independent search index

## Package Organization

```
cmd/
  cortex/           - Main CLI entry point (single binary)

internal/
  cli/              - Cobra CLI commands (index, mcp, indexer, version, etc.)
  config/           - Configuration loading (.cortex/config.yml)
  indexer/          - Tree-sitter parsing, chunking, embedding
    daemon/         - Actor model, RPC server, project registry ✨ NEW
  mcp/              - MCP server, SQLite queries, branch watching
  embed/
    provider.go     - Provider interface
    factory.go      - Factory for creating providers
    local.go        - Local provider (Rust FFI)
  embeddings-ffi/   - Rust FFI implementation with tract-onnx ✨ NEW
  daemon/           - Shared daemon infrastructure ✨ NEW
  cache/            - SQLite storage, branch isolation, cache keys ✨ NEW
  git/              - Git operations, branch detection ✨ NEW
  watcher/          - File and git watchers
  graph/            - Code graph extraction and querying

docs/               - User documentation (architecture, config, MCP integration)
specs/              - Technical specs (indexer, mcp-server, embedding)
tests/
  e2e/              - End-to-end CLI tests
  fixtures/         - Test codebases
testdata/           - Sample code for parser tests
```

## Code Conventions

### Public Interface Pattern

All major components use **public interfaces with unexported implementations**:

```go
// Public interface in internal/embed/provider.go
type Provider interface {
    Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error)
    Dimensions() int
    Close() error
}

// Public constructor returns interface
func NewProvider(config Config) (Provider, error) {
    return &localProvider{config: config}, nil
}

// Unexported implementation in internal/embed/local.go
type localProvider struct {
    config Config
}
```

**Benefits**: Encapsulation, testability, interface-driven development, easy mocking.

### Error Handling

Use standard Go error wrapping with `fmt.Errorf` and `%w`:

```go
if err != nil {
    return fmt.Errorf("failed to parse file: %w", err)
}

// Custom error types for errors.Is()
var ErrUnsupportedLanguage = errors.New("unsupported language")
```

For MCP protocol errors, return Go errors—mcp-go handles JSON-RPC error codes.

### Logging

Use standard `log` package for CLI output:

```go
log.Printf("Indexing %d files...", fileCount)
log.Printf("✓ Generated %d chunks", chunkCount)
log.Fatalf("Failed to start MCP server: %v", err)
```

Keep CLI output clean and user-friendly. Consider verbose flag for detailed logging.

### Configuration

Viper loads from `.cortex/config.yml` with environment variable overrides:
- `CORTEX_CACHE_BASE_DIR`: Override cache base directory (default: `~/.cortex/cache/`)
- `CORTEX_MODEL_DIR`: Override model directory (default: `~/.cortex/models/bge/`)
- `CORTEX_INDEXER_SOCKET_PATH`: Override indexer daemon socket path (default: `~/.cortex/indexer.sock`)
- `CORTEX_EMBED_SOCKET_PATH`: Override embedding daemon socket path (default: `~/.cortex/embed.sock`)
- `CORTEX_EMBED_IDLE_TIMEOUT`: Embedding daemon idle shutdown timeout (default: 10 minutes)

## Testing Strategy

### Layers

1. **Unit tests** (`*_test.go`): Test individual components with testify
   - parser_test.go, chunker_test.go, provider_test.go
   - Use real dependencies where possible (avoid excessive mocking)

2. **Integration tests** (`*_integration_test.go`): Test component interactions
   - Indexer end-to-end (parse → chunk → embed → write to SQLite)
   - MCP server (open SQLite DB → query → return results)
   - Daemon actor lifecycle (spawn → watch → index → stop)
   - Use real embedding provider and SQLite databases
   - Tagged with `//go:build integration` to separate from unit tests

3. **E2E tests** (`tests/e2e/`): Test complete CLI workflows
   - `cortex index` on test project → validate SQLite database
   - `cortex indexer status` → verify daemon running
   - `cortex mcp` → query → validate results
   - Branch switching with automatic re-indexing

4. **MCP protocol tests** (`internal/mcp/`): Validate MCP compliance
   - Tool registration and schema
   - Request/response serialization
   - Error codes per MCP spec

### Test Tools

- **testify**: Assertions and mocking
- **t.TempDir()**: Isolated test environments
- **tree-sitter**: Official Go bindings
- **mcp-go**: Protocol testing utilities
- **mattn/go-sqlite3**: SQLite with extensions (sqlite-vec, FTS5)

### Running Tests

```bash
task test                              # Unit tests only (fast)
go test ./internal/...                 # Specific packages (unit tests)
go test -tags=integration ./...        # Include integration tests
task test:race                         # With race detector
task test:coverage                     # Generate coverage report

# Targeted testing with CGO configuration
./scripts/test.sh <package-path>       # Run specific package tests with proper CGO settings
```

**Note**: The `test.sh` script is particularly useful for running targeted tests that require CGO (e.g., tests using sqlite3 or tree-sitter bindings). It automatically configures CGO environment variables and passes through test flags.

## Language Support

Supported via tree-sitter:
- Go, TypeScript/JavaScript (including JSX/TSX), Python, Rust, C/C++, PHP, Ruby, Java

Each language has tree-sitter queries for extracting symbols, definitions, and data. See `docs/languages.md` for extraction details.

## Important Implementation Details

### MCP Response Format

**Critical**: Handler returns **full SearchResult structs** (not formatted text):
- LLM receives structured data with metadata (file paths, line numbers)
- `embedding` field excluded via custom JSON marshaler (reduces payload ~60%)
- Return via `mcp.NewToolResultText(jsonString)` (mcp-go convention)

### Thread Safety (MCP Server)

Use `DBHolder` pattern with `sync.RWMutex`:
- Queries acquire read lock (concurrent database access)
- Branch switches acquire write lock (blocks queries briefly, ~100ms for DB swap)
- SQLite connections are read-only (no write conflicts)

### Branch Switching (DBHolder)

MCP server git watcher detects `.git/HEAD` changes:
- Close old database connection
- Open new branch database (e.g., `main.db` → `feature-x.db`)
- Atomic swap via write lock
- Old queries fail gracefully, new queries use new DB
- No debouncing needed (git operations are atomic)

### Embedding Dimensions

Must match across indexer, chunks, and MCP server. Default: 384 (BAAI/bge-small-en-v1.5). Mismatch prevents vector search.

## Key Dependencies

- **cobra**: CLI framework
- **viper**: Configuration management
- **tree-sitter/go-tree-sitter**: Code parsing
- **mark3labs/mcp-go**: MCP protocol implementation
- **mattn/go-sqlite3**: SQLite database (with sqlite-vec extension for vector search)
- **connectrpc.com/connect**: RPC framework for daemons (indexer, embedding)
- **Rust dependencies** (via CGO FFI):
  - **tract-onnx**: ONNX model inference
  - **rayon**: Parallel processing
  - **tokenizers**: Text tokenization
- **fsnotify/fsnotify**: File and git watching

## Gotchas and Pitfalls

1. **Rust FFI embedding**: Built into cortex binary via CGO. Model files (~200MB) downloaded to `~/.cortex/models/bge/` on first use. Requires Rust toolchain for building from source.

2. **SQLite transactions**: Indexer uses transactions for batch writes. Failed indexing rolls back automatically (partial updates impossible).

3. **Daemon lifecycle**: Indexer daemon auto-starts on `cortex index`. Runs continuously, watching all projects. Embedding daemon starts on demand, idles out after 10 minutes.

4. **Cache location**: `~/.cortex/cache/{cache-key}/` where cache-key = hash(git-remote) + hash(worktree-path). Moving projects requires cache migration (automatic via `.cortex/settings.local.json`).

5. **Branch switching**: Each branch gets separate SQLite DB. MCP servers detect branch changes via git watcher and swap databases atomically (<100ms).

6. **Embedding mode matters**: Use `EmbedModePassage` for documents, `EmbedModeQuery` for searches. Correct mode is critical for search quality.

7. **Natural language formatting**: The `text` field in chunks should be natural language, not JSON. Embeddings understand "Package: auth\n\nTypes:\n  - Handler" better than structured data.

8. **Chunk ID stability**: IDs include file path. Unchanged files preserve IDs (no re-embedding). File moves/renames trigger full reprocessing.

9. **Chunk types vs tags**: `chunk_types` is structural (symbols/definitions/data/documentation). `tags` is contextual (language, path, custom). Filters use AND logic.

10. **Vector search multiplier**: Fetch 2x limit from vector search before filtering to ensure enough post-filter results.

## Performance Characteristics

### Indexing
- Initial: ~1000 files/second
- Incremental: Only changed files processed
- Watch mode: File change detected <100ms

### Search (MCP Server)
- Embedding: ~50-100ms (Rust FFI embedding provider)
- Vector search: ~10-20ms (SQLite with sqlite-vec extension)
- Keyword search: 2-8ms (SQLite FTS5)
- Graph queries: 1-20ms (SQL WITH RECURSIVE CTEs)
- Total (semantic): ~60-120ms per query

### Memory
- MCP server: ~5MB base (no in-memory indexes)
- SQLite DB handle: <1MB
- Typical project: ~5-10MB total (90% reduction vs chromem-go)
- Daemon overhead: ~10MB base + ~5MB per watched project

## Related Documentation

- **README.md**: User-facing quick start and overview
- **docs/architecture.md**: Deep dive into system design
- **docs/embedding-server.md**: Rust FFI embedding implementation
- **docs/mcp-integration.md**: MCP server setup and usage
- **docs/coding-conventions.md**: Additional code patterns
- **docs/testing-strategy.md**: Complete testing philosophy
- **specs/2025-11-05_indexer-daemon.md**: Indexer daemon architecture (IMPLEMENTED)
- **specs/archived/**: Historical specs (chronological evolution of features)
- **Taskfile.yml**: All available commands and build tasks
