---
status: archived
started_at: 2025-10-26T00:00:00Z
completed_at: 2025-10-26T17:55:00Z
dependencies: [cortex-embed]
---

# MCP Server Specification

## Purpose

The MCP server loads indexed code chunks into an in-memory vector database and exposes semantic search via the Model Context Protocol (MCP). This enables LLM-powered coding assistants like Claude Code to understand and navigate codebases through natural language queries.

## Core Concept

**Input**: JSON chunk files from `.cortex/chunks/`

**Process**: Load → Index → Query → Filter → Respond

**Output**: MCP tool responses with relevant code/doc chunks

## Technology Stack

- **Language**: Go 1.25+
- **Vector Database**: `github.com/philippgille/chromem-go` (in-memory)
- **MCP Protocol**: `github.com/mark3labs/mcp-go` v0.37.0+
- **Embedding Client**: HTTP requests to cortex-embed server
- **Communication**: stdio (standard MCP transport)
- **File Watching**: `github.com/fsnotify/fsnotify` (for chunk reload)

## Architecture

```
┌─────────────────┐
│  Claude Code    │ ← LLM-powered coding assistant
└────────┬────────┘
         │ MCP Protocol (stdio)
         │ {"query": "How does auth work?"}
         ▼
┌─────────────────┐
│  cortex mcp     │ ← This server
│  (chromem-go)   │
└────────┬────────┘
         │
         ├─→ Load chunks from .cortex/chunks/*.json
         ├─→ Query embeddings from cortex-embed
         ├─→ Vector similarity search
         └─→ Filter by tags/chunk_types
         │
         ▼
┌─────────────────┐
│  Search Results │ ← Chunks with metadata
└─────────────────┘
```

## MCP Tool Interface (Proven Design)

The server implements the proven interface from `.scratch/Overwatch` using `mcp-go`:

### Tool Registration Pattern

```go
// Composable tool registration (allows combining with other tools)
func AddCortexSearchTool(s *server.MCPServer, searcher ContextSearcher) {
    tool := mcp.NewTool(
        "cortex_search",
        mcp.WithDescription("Search for relevant context in the project codebase and documentation"),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
    )
    s.AddTool(tool, createHandler(searcher))
}
```

### Request Schema

```go
type CortexSearchRequest struct {
    Query      string   `json:"query" jsonschema:"required"`
    Limit      int      `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=15"`
    Tags       []string `json:"tags,omitempty"`
    ChunkTypes []string `json:"chunk_types,omitempty"`
}
```

JSON representation:
```json
{
  "query": "string (required)",
  "limit": "number (optional, 1-100, default 15)",
  "chunk_types": "string[] (optional)",
  "tags": "string[] (optional)"
}
```

### Chunk Types Filter

- `"documentation"` - README files, guides, design docs, ADRs
- `"symbols"` - High-level code overview (list of functions, types, etc.)
- `"definitions"` - Full function/type signatures with comments
- `"data"` - Constants, configs, enum values

**Leave empty to search all types.**

### Tags Filter

Tags are automatically generated from:

**Context-derived** (file metadata):
- Language: `"go"`, `"typescript"`, `"python"`, etc.
- Type: `"code"`, `"documentation"`
- Path: `"docs"`, `"internal"`, etc.

**Content-parsed** (from documents):
- YAML frontmatter: `tags: [architecture, design]`
- Inline hashtags: `#auth #users`

**Example tag combinations**:
- `["code", "go"]` - Go code only
- `["documentation", "architecture"]` - Architecture docs
- `["typescript", "definitions"]` - TypeScript function signatures

**AND logic**: Result must have ALL specified tags.

### Response Schema

**Go struct** (what handler returns):
```go
type CortexSearchResponse struct {
    Results []*SearchResult `json:"results"`
    Total   int             `json:"total"`
}

type SearchResult struct {
    Chunk         *ContextChunk `json:"chunk"`
    CombinedScore float64       `json:"combined_score"`
}
```

**JSON representation** (MCP tool response):
```json
{
  "results": [
    {
      "chunk": {
        "id": "code-symbols-internal-auth-handler-go",
        "chunk_type": "symbols",
        "title": "internal/auth/handler.go symbols",
        "text": "Package: auth\n\nImports: 5 packages\n\nTypes:\n  - AuthHandler (struct) (lines 10-15)\n...",
        "tags": ["code", "go", "symbols"],
        "metadata": {
          "source": "code",
          "file_path": "internal/auth/handler.go",
          "language": "go",
          "package": "auth",
          "imports_count": 5,
          "types_count": 2,
          "functions_count": 3
        },
        "created_at": "2025-10-16T12:35:00Z",
        "updated_at": "2025-10-16T12:35:00Z"
      },
      "combined_score": 0.87
    }
  ],
  "total": 1
}
```

**Critical**:
- Response contains **full SearchResult structs**, not formatted text
- LLM receives structured data with metadata (file paths, line numbers)
- `embedding` field is **excluded** via custom JSON marshaler (reduces payload ~60%)
- Handler returns `mcp.NewToolResultText(jsonString)` (mcp-go convention)

## Query Examples

### 1. Architecture Understanding

**Query**: "authentication design decisions"

**Filters**:
```json
{
  "chunk_types": ["documentation"],
  "tags": ["architecture"]
}
```

**Returns**: Design docs and ADRs explaining why auth was implemented this way.

### 2. Code Navigation

**Query**: "authentication handler implementation"

**Filters**:
```json
{
  "chunk_types": ["symbols", "definitions"]
}
```

**Returns**: High-level overview + full function signatures for auth handlers.

### 3. Configuration Discovery

**Query**: "database connection settings"

**Filters**:
```json
{
  "chunk_types": ["data"]
}
```

**Returns**: Constants and config values related to database.

### 4. Unified Search

**Query**: "how does authentication work"

**Filters**: (none - search all types)

**Returns**: Mixed results—design rationale, code symbols, definitions, and configs. Complete picture.

### 5. Language-Specific

**Query**: "error handling patterns"

**Filters**:
```json
{
  "tags": ["typescript", "code"]
}
```

**Returns**: Only TypeScript code related to error handling.

## Server Lifecycle

### Startup

1. Load configuration from `.cortex/config.yml`
2. Read chunk files from `.cortex/chunks/`
3. Initialize chromem-go database
4. Create vector collection with 384 dimensions
5. Add all chunks to collection (with embeddings)
6. **Start file watcher** on `.cortex/chunks/` directory
7. Start MCP server on stdio
8. Ready for queries

### Query Execution

1. Receive MCP tool call: `cortex_search`
2. Validate request parameters
3. Generate query embedding via cortex-embed
4. Execute vector similarity search in chromem-go
5. Apply filters (chunk_types, tags)
6. Sort by combined_score (descending)
7. Limit results
8. Return MCP response

### Hot Reload (New Feature)

**Motivation**: `cortex index --watch` keeps chunks up-to-date, MCP server should automatically reload without restart.

**Behavior**:
1. Watch `.cortex/chunks/` directory using `fsnotify`
2. Detect file modifications (WRITE, CREATE events)
3. **Debounce**: Wait 500ms after last event (indexer writes multiple files)
4. On debounce timeout:
   - Read all chunk files
   - Rebuild chromem-go collection (atomic swap)
   - Log reload completion
5. Continue serving queries with new data

**Debouncing Strategy**:
- Indexer writes 4 files: `doc-chunks.json`, `code-symbols.json`, `code-definitions.json`, `code-data.json`
- Events come in rapid succession (~50-200ms apart)
- Wait 500ms of quiet time before reloading
- Avoids partial state (loading while indexer still writing)

**Thread Safety**:
- Use `sync.RWMutex` around searcher
- Queries acquire read lock
- Reload acquires write lock (blocks queries briefly)
- Typical reload time: 100-500ms depending on chunk count

**Error Handling**:
- If reload fails (corrupted JSON, missing files), keep old state
- Log error but don't crash server
- Next successful reload will fix state

### Shutdown

1. Graceful shutdown on SIGTERM/SIGINT
2. Stop file watcher
3. No persistence needed (ephemeral in-memory DB)

## Configuration

Loaded from `.cortex/config.yml`:

```yaml
embedding:
  endpoint: "http://localhost:8121/embed"
  dimensions: 384

server:
  chunks_dir: ".cortex/chunks"
```

### Environment Variables

- `CORTEX_CHUNKS_DIR` - Override chunks directory
- `CORTEX_EMBEDDING_ENDPOINT` - Override embedding service URL

## Search Algorithm

### Vector Similarity

Uses cosine similarity on 384-dimensional embeddings:

```
similarity = dot(query_embedding, chunk_embedding) / (||query|| * ||chunk||)
```

Scores range from -1.0 to 1.0, higher is better.

### Filtering Strategy

1. **Vector search**: Fetch top N candidates (N = limit * 2 for headroom)
2. **Chunk type filter**: Keep only matching chunk_types (if specified)
3. **Tag filter**: Keep only chunks with ALL specified tags (AND logic)
4. **Sort**: Order by combined_score descending
5. **Limit**: Return top K results (K = limit)

### Why 2x Multiplier

Fetching 2x results before filtering ensures we have enough matches after post-filtering. Example:
- User requests `limit: 10`
- Server fetches top 20 from vector search
- Applies filters (might reduce to 8-15 results)
- Returns up to 10 final results

## Data Model (Proven Implementation)

### Core Interfaces

**ContextSearcher** (allows different backend implementations):
```go
type ContextSearcher interface {
    Query(ctx context.Context, query string, options *SearchOptions) ([]*SearchResult, error)
}
```

### Internal Structures

**ContextChunk**:
```go
type ContextChunk struct {
    ID        string                 `json:"id"`
    Title     string                 `json:"title"`
    Text      string                 `json:"text"`
    ChunkType string                 `json:"chunk_type,omitempty"`
    Embedding []float32              `json:"embedding,omitempty"`  // Excluded via MarshalJSON
    Tags      []string               `json:"tags,omitempty"`
    Metadata  map[string]interface{} `json:"metadata,omitempty"`
    CreatedAt time.Time              `json:"created_at"`
    UpdatedAt time.Time              `json:"updated_at"`
}

// Custom JSON marshaling to exclude Embedding field
func (c ContextChunk) MarshalJSON() ([]byte, error) {
    // Implementation excludes embedding field
}
```

**SearchOptions**:
```go
type SearchOptions struct {
    Limit      int      `json:"limit,omitempty"`
    MinScore   float64  `json:"min_score,omitempty"`
    Tags       []string `json:"tags,omitempty"`
    ChunkTypes []string `json:"chunk_types,omitempty"`
}

func DefaultSearchOptions() *SearchOptions {
    return &SearchOptions{
        Limit:    15,
        MinScore: 0.0,
    }
}
```

**SearchResult**:
```go
type SearchResult struct {
    Chunk         *ContextChunk `json:"chunk"`
    CombinedScore float64       `json:"combined_score"`
}
```

**MCPServerConfig**:
```go
type MCPServerConfig struct {
    ChunksDir        string
    EmbeddingService *EmbeddingServiceConfig
}

type EmbeddingServiceConfig struct {
    BaseURL    string
    Dimensions int
}

func DefaultMCPServerConfig() *MCPServerConfig {
    return &MCPServerConfig{
        ChunksDir: ".cortex/chunks",
        EmbeddingService: &EmbeddingServiceConfig{
            BaseURL:    "http://localhost:8121",
            Dimensions: 384,
        },
    }
}
```

### Embedding Provider Interface

**Design**: MCP server uses same provider interface as indexer for consistency.

```go
// Interface in internal/embed/provider.go
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

**Usage in MCP Server**:
```go
// Create provider via factory
provider, err := embed.NewProvider(embed.Config{
    Provider:   "local",
    BinaryPath: "cortex-embed",
})

// Generate query embedding (use query mode)
queryEmbedding, err := provider.Embed(ctx, []string{query}, embed.EmbedModeQuery)
```

**Implementations**:
- `LocalProvider` - Manages cortex-embed binary, auto-starts if needed
- Future: `OpenAIProvider`, `AnthropicProvider`

**Benefits**:
- Clean abstraction - no HTTP/JSON details in MCP code
- Same interface as indexer
- Mode parameter ensures correct embedding type (query for searches)

## MCP Integration Pattern (mcp-go)

### Server Creation

```go
import (
    "github.com/mark3labs/mcp-go/server"
    "github.com/mark3labs/mcp-go/mcp"
)

func main() {
    // Create server
    s := server.NewMCPServer(
        "cortex-mcp",
        "1.0.0",
        server.WithToolCapabilities(true),
    )

    // Add tool (composable - can add multiple tools)
    AddCortexSearchTool(s, searcher)

    // Serve on stdio
    if err := server.ServeStdio(s); err != nil {
        log.Fatal(err)
    }
}
```

### Tool Handler Pattern

```go
func createCortexSearchHandler(searcher ContextSearcher)
    func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {

    return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        // 1. Parse arguments (with jsonschema validation)
        var args CortexSearchRequest
        if err := request.BindArguments(&args); err != nil {
            return nil, fmt.Errorf("invalid arguments: %w", err)
        }

        // 2. Execute search
        results, err := searcher.Query(ctx, args.Query, buildOptions(args))
        if err != nil {
            return nil, fmt.Errorf("search failed: %w", err)
        }

        // 3. Build response
        response := CortexSearchResponse{
            Results: results,
            Total:   len(results),
        }

        // 4. Return as JSON text (mcp-go convention)
        jsonData, _ := json.Marshal(response)
        return mcp.NewToolResultText(string(jsonData)), nil
    }
}
```

### Composability

The `AddCortexSearchTool` pattern allows combining with other tools:

```go
// Combined server with multiple tools
s := server.NewMCPServer("combined-mcp", "1.0.0")
AddCortexSearchTool(s, contextSearcher)
AddAgentPromptTool(s, agentConfig)        // From another package
AddFileReadTool(s, fileHandler)           // Another tool
```

This matches the proven `.scratch/Overwatch/cmd/overwatch-mcp/main.go` pattern.

## Key Behaviors

1. **Hot reload**: Automatically reloads chunks when files change (500ms debounce)
2. **Embedding exclusion**: Never send embeddings in MCP responses (optimization)
3. **Graceful fallback**: If embedding service unavailable at startup, fail fast with clear error
4. **Stateless queries**: Each query is independent, no session state
5. **Thread-safe**: Concurrent queries + hot reload using RWMutex
6. **Deterministic results**: Same query + same chunks = same results

## Performance Characteristics

### Memory Usage

- Base: ~50MB (Go runtime + chromem-go)
- Chunks: ~1MB per 1000 chunks (with 384-dim embeddings)
- Typical project (10K chunks): ~60MB total

### Query Latency

- Embedding generation: ~50-100ms (cortex-embed)
- Vector search: <10ms (chromem-go in-memory)
- Filtering: <1ms
- **Total**: ~60-110ms per query

### Scalability

| Chunks | Memory | Query Time |
|--------|--------|------------|
| 1K     | ~51MB  | ~60ms      |
| 10K    | ~60MB  | ~70ms      |
| 100K   | ~150MB | ~100ms     |

## Error Handling

### Startup Errors

- **Missing chunks directory**: Fail with error message
- **Invalid chunk files**: Log warning, skip malformed files
- **Embedding service unreachable**: Fail (can't generate query embeddings)
- **Wrong embedding dimensions**: Fail (mismatch prevents vector search)

### Runtime Errors

- **Empty query**: Return MCP error `-32602` (invalid params)
- **Invalid limit**: Clamp to 1-100 range
- **Embedding timeout**: Return MCP error `-32603` (internal error)
- **Search failure**: Log error, return MCP error `-32603`

## MCP Protocol Compliance

Implements MCP v1.0 specification:
- `tools/list` - Returns ProjectContext tool definition
- `tools/call` - Executes ProjectContext searches
- JSON-RPC 2.0 transport via stdio
- Proper error codes and messages

## Non-Goals

- Real-time chunk updates (server loads chunks at startup only, restart to reload)
- Persistent vector index (ephemeral in-memory, fast startup is priority)
- Multi-project support (one server per project, Claude Code manages multiple servers)
- Authentication (local stdio communication, no network security needed)
- Query history or analytics (stateless design)
