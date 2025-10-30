# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Project Cortex is a dual-binary Go application that enables deep semantic search of **both code and documentation** for LLM-powered coding assistants. It consists of:

1. **cortex** - Main CLI for indexing code/docs and running MCP server
2. **cortex-embed** (~300MB) - Embedding service with embedded Python runtime

The architecture follows a three-phase pipeline:
- **Indexing**: Parse code (tree-sitter) + docs → Extract structured knowledge → Chunk → Embed → Store as JSON
- **Storage**: Git-friendly JSON chunk files in `.cortex/chunks/` (version controlled)
- **Serving**: MCP server loads chunks into in-memory vector DB (chromem-go) for semantic search

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

**Note**: Results are sorted by relevance score. The MCP server automatically reloads when `.cortex/chunks/` files change (500ms debounce).

## Using cortex_exact for Keyword Search

**`cortex_exact`** provides fast full-text search using bleve query syntax. Unlike semantic search, it finds exact text matches with boolean logic, wildcards, and phrase matching.

### Quick Reference

```typescript
mcp__project-cortex__cortex_exact({
  query: string,              // Bleve query syntax (required)
  limit?: number              // Max results, 1-100 (default: 15)
})
```

**Query Syntax** (bleve QueryStringQuery):
- **Field scoping:** `text:handler`, `tags:go`, `chunk_type:definitions`, `file_path:auth`
- **Boolean AND:** `text:Provider AND tags:go`
- **Boolean OR:** `text:handler OR text:controller`
- **Boolean NOT:** `text:auth AND -file_path:test`
- **Phrase search:** `text:"error handling"`
- **Prefix wildcard:** `text:Prov*` (matches Provider, ProviderConfig)
- **Fuzzy match:** `text:Provdier~1` (typo tolerance)
- **Grouping:** `(text:handler OR text:controller) AND tags:go`

**Default behavior:** If no field specified, searches `text` field only (avoids metadata noise).

### Common Query Patterns

#### 1. Find Exact Identifier
```typescript
// Find all occurrences of sync.RWMutex
mcp__project-cortex__cortex_exact({
  query: "text:sync.RWMutex",
  limit: 20
})
```

#### 2. Boolean Search
```typescript
// Find Provider interfaces in Go code
mcp__project-cortex__cortex_exact({
  query: "text:Provider AND text:interface AND tags:go",
  limit: 10
})
```

#### 3. Exclude Files
```typescript
// Find authentication code, exclude tests
mcp__project-cortex__cortex_exact({
  query: "text:authentication AND -file_path:test"
})
```

#### 4. Phrase Search
```typescript
// Find exact phrase "error handling"
mcp__project-cortex__cortex_exact({
  query: 'text:"error handling"',
  limit: 10
})
```

#### 5. Filter by Chunk Type
```typescript
// Find interfaces in definitions only
mcp__project-cortex__cortex_exact({
  query: "text:interface AND chunk_type:definitions"
})
```

#### 6. Prefix Matching
```typescript
// Find anything starting with "Prov"
mcp__project-cortex__cortex_exact({
  query: "text:Prov*"
})
```

#### 7. Complex Boolean
```typescript
// Find handlers or controllers in TypeScript, not tests
mcp__project-cortex__cortex_exact({
  query: "(text:handler OR text:controller) AND tags:typescript AND -tags:test"
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

## Context7 - External Library & API Documentation
Always use context7 when I need help with code generation, setup or configuration steps, or
library/API documentation FOR EXTERNAL LIBRARIES. For documentation about this project use project_cortext mcp. This means you should automatically use the Context7 MCP
tools to resolve library id and get library docs without me having to explicitly ask.

## Common Commands

### Building

```bash
# Build main CLI
task build

# Build embedding server (requires Python deps for your platform)
task build:embed

# Build both binaries
task build:all

# Cross-compile for specific platform
task build:cross OS=linux ARCH=amd64

# Cross-compile for all platforms
task build:cross:all
```

### Python Dependencies (cortex-embed only)

```bash
# Generate for your platform (fast, 2-3 min)
task python:deps:darwin-arm64    # macOS Apple Silicon
task python:deps:darwin-amd64    # macOS Intel
task python:deps:linux-amd64     # Linux x64
task python:deps:linux-arm64     # Linux ARM64
task python:deps:windows-amd64   # Windows x64

# Generate for all platforms (slow, 10-20 min, ~300MB)
task python:deps:all
```

**Note**: Only needed when `requirements.txt` changes or building `cortex-embed` for first time.

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

# Run embedding server
task run:embed              # Build and run cortex-embed
```

### Development

```bash
# Watch mode (requires entr or watchexec)
task dev

# Show build info
task info

# Clean build artifacts
task clean

# Clean Python dependencies (⚠️ requires regeneration)
task clean:python

# Clean everything
task clean:all
```

## High-Level Architecture

### Three-Tier Code Extraction

Code is extracted at three granularity levels, each stored as separate JSON chunk files:

1. **Symbols** (`code-symbols.json`): High-level file overview
   - Package/module name, import count, type/function names with line numbers
   - Natural language format: "Package: server\n\nTypes:\n  - Handler (struct) (lines 10-15)"
   - Use for: Quick navigation, understanding file structure

2. **Definitions** (`code-definitions.json`): Full signatures without implementations
   - Complete type definitions, interfaces, function signatures with comments
   - Actual code format with line comments: "// Lines 10-15\ntype Handler struct {...}"
   - Use for: Understanding contracts, APIs, type relationships

3. **Data** (`code-data.json`): Constants and configuration
   - Constant declarations, global variables, enum values, defaults
   - Actual code format with line comments
   - Use for: Configuration discovery, finding default values

### Documentation Chunking

Documentation (`doc-chunks.json`) is semantically chunked by headers with line tracking:
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

**Implementations**:
- `LocalProvider` (internal/embed/local.go): Manages cortex-embed binary, auto-starts if needed
- Future: `OpenAIProvider`, `AnthropicProvider`

**Factory pattern** (internal/embed/factory.go): `embed.NewProvider(config)` returns interface

**Critical**: Use correct mode—`EmbedModePassage` for indexing documents, `EmbedModeQuery` for search queries.

### MCP Server Architecture

The MCP server (`cortex mcp`) uses mcp-go v0.37.0+ and chromem-go:

1. **Startup**: Load chunks from `.cortex/chunks/*.json` → Initialize chromem-go → Add chunks to vector collection → Start file watcher → Listen on stdio
2. **Query**: Receive MCP request → Generate query embedding (via provider) → Vector similarity search → Filter by chunk_types/tags → Return results
3. **Hot Reload**: Watch `.cortex/chunks/` → Debounce 500ms → Rebuild collection → Swap atomically

**Composable tool registration pattern**:
```go
func AddCortexSearchTool(s *server.MCPServer, searcher ContextSearcher)
```
Allows combining multiple MCP tools in one server.

**Tool interface**: `cortex_search` with filters for chunk_types and tags (AND logic).

**Additional tools**:
- `cortex_exact` - Full-text keyword search using bleve (same chunks, different index)
- `cortex_graph` - Structural code graph queries (separate data source: `.cortex/graph/code-graph.json`)

### Incremental Indexing

Tracks file changes via SHA-256 checksums (`.cortex/generator-output.json`):
- Only reprocess changed files
- Merge new chunks with unchanged chunks
- Atomic writes (temp → rename) to prevent MCP server seeing partial state
- Chunk IDs stable for unchanged files (no unnecessary embedding regeneration)

### Atomic Write Strategy

**Problem**: MCP server watches chunk directory; must never read partial writes.

**Solution**: Write to `.cortex/chunks/.tmp/<file>.json` → Rename (atomic POSIX operation).

**Benefits**: MCP-safe, crash recovery, hot reload friendly (single fsnotify event).

## Package Organization

```
cmd/
  cortex/           - Main CLI entry point
  cortex-embed/     - Embedding server entry point

internal/
  cli/              - Cobra CLI commands (index, mcp, version, etc.)
  config/           - Configuration loading (.cortex/config.yml)
  indexer/          - Tree-sitter parsing, chunking, embedding
  mcp/              - MCP server, protocol implementation, hot reload
  embed/
    provider.go     - Provider interface
    factory.go      - Factory for creating providers
    client/
      local.go      - LocalProvider implementation
    server/         - cortex-embed Python embedding service

docs/               - User documentation (architecture, config, MCP integration)
specs/              - Technical specs (indexer, mcp-server, cortex-embed)
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
- `CORTEX_CHUNKS_DIR`: Override chunks directory
- `CORTEX_EMBEDDING_ENDPOINT`: Override embedding service URL

## Testing Strategy

### Layers

1. **Unit tests** (`*_test.go`): Test individual components with testify
   - parser_test.go, chunker_test.go, provider_test.go
   - Use real dependencies where possible (avoid excessive mocking)

2. **Integration tests** (`*_integration_test.go`): Test component interactions
   - Indexer end-to-end (parse → chunk → embed → write)
   - MCP server (load chunks → search)
   - File watcher hot reload
   - Use real cortex-embed binary and chromem-go
   - Tagged with `//go:build integration` to separate from unit tests

3. **E2E tests** (`tests/e2e/`): Test complete CLI workflows
   - `cortex index` on test project → validate chunk files
   - `cortex mcp` → query → validate results
   - Watch mode with file changes

4. **MCP protocol tests** (`internal/mcp/`): Validate MCP compliance
   - Tool registration and schema
   - Request/response serialization
   - Error codes per MCP spec

### Test Tools

- **testify**: Assertions and mocking
- **t.TempDir()**: Isolated test environments
- **tree-sitter**: Official Go bindings
- **mcp-go**: Protocol testing utilities
- **chromem-go**: In-memory vector DB

### Running Tests

```bash
task test                              # Unit tests only (fast)
go test ./internal/...                 # Specific packages (unit tests)
go test -tags=integration ./...        # Include integration tests
task test:race                         # With race detector
task test:coverage                     # Generate coverage report
```

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

Use `sync.RWMutex` around searcher:
- Queries acquire read lock (concurrent)
- Hot reload acquires write lock (blocks queries briefly, ~100-500ms)

### Debouncing (Hot Reload)

Indexer writes 4 files in rapid succession (~50-200ms apart). Wait 500ms of quiet time before reloading to avoid partial state.

### Embedding Dimensions

Must match across indexer, chunks, and MCP server. Default: 384 (BAAI/bge-small-en-v1.5). Mismatch prevents vector search.

## Key Dependencies

- **cobra**: CLI framework
- **viper**: Configuration management
- **tree-sitter/go-tree-sitter**: Code parsing
- **mark3labs/mcp-go**: MCP protocol implementation
- **philippgille/chromem-go**: In-memory vector database
- **kluctl/go-embed-python**: Embedded Python runtime (cortex-embed)
- **fsnotify/fsnotify**: File watching for hot reload

## Gotchas and Pitfalls

1. **Python deps platform-specific**: Must generate for target platform before building cortex-embed. Use `task python:deps:<platform>`.

2. **Atomic writes required**: Always use temp → rename pattern when writing chunks. MCP server watches directory.

3. **Embedding mode matters**: Use `EmbedModePassage` for documents, `EmbedModeQuery` for searches. Wrong mode degrades search quality.

4. **Natural language formatting**: The `text` field in chunks should be natural language, not JSON. Embeddings understand "Package: auth\n\nTypes:\n  - Handler" better than structured data.

5. **Chunk ID stability**: IDs include file path. Unchanged files preserve IDs (no re-embedding). File moves/renames trigger full reprocessing.

6. **MCP hot reload**: If indexer writes fail mid-update, server keeps old state until next successful reload. Design is resilient to partial failures.

7. **Chunk types vs tags**: `chunk_types` is structural (symbols/definitions/data/documentation). `tags` is contextual (language, path, custom). Filters use AND logic.

8. **Vector search multiplier**: Fetch 2x limit from vector search before filtering to ensure enough post-filter results.

## Performance Characteristics

### Indexing
- Initial: ~1000 files/second
- Incremental: Only changed files processed
- Watch mode: File change detected <100ms

### Search (MCP Server)
- Embedding: ~50-100ms (cortex-embed)
- Vector search: <10ms (chromem-go)
- Total: ~60-110ms per query

### Memory
- MCP server: ~50MB base + ~1MB per 1000 chunks
- Typical project (10K chunks): ~60MB total

## Related Documentation

- **README.md**: User-facing quick start and overview
- **docs/architecture.md**: Deep dive into system design
- **docs/coding-conventions.md**: Additional code patterns
- **docs/testing-strategy.md**: Complete testing philosophy
- **specs/2025-10-26_indexer.md**: Indexer technical specification
- **specs/2025-10-26_mcp-server.md**: MCP server technical specification
- **specs/2025-10-15_cortex-embed.md**: Embedding service specification
- **Taskfile.yml**: All available commands and build tasks
