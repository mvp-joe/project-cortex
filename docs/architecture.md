# Architecture

This document explains how Project Cortex works under the hood.

## Overview

Project Cortex transforms your **codebase and documentation** into a semantically searchable knowledge base that AI coding assistants can query efficiently. The architecture consists of three main phases: **Parsing & Extraction**, **Chunking & Embedding**, and **Serving & Search**.

Both code and documentation are treated as first-class citizens, enabling unified semantic search across implementation details and usage instructions.

```
┌──────────────┐          ┌──────────────┐
│ Source Code  │          │ Documentation│
│  (.go, .ts)  │          │  (.md, .rst) │
└──────┬───────┘          └──────┬───────┘
       │                         │
       ▼                         ▼
┌──────────────┐          ┌──────────────┐
│ Tree-sitter  │          │   Markdown   │
│   Parser     │          │    Parser    │
└──────┬───────┘          └──────┬───────┘
       │                         │
       ▼                         ▼
┌──────────────┐          ┌──────────────┐
│  Three-Tier  │          │   Semantic   │
│  Extractor   │          │   Chunking   │
│  - Symbols   │          │ (by headers) │
│  - Defs      │          │              │
│  - Data      │          │              │
└──────┬───────┘          └──────┬───────┘
       │                         │
       └────────┬────────────────┘
                ▼
       ┌─────────────────┐
       │  Code Chunker   │  Create searchable chunks
       └────────┬────────┘
                ▼
       ┌─────────────────┐
       │  Embedding      │  Generate vectors
       │  Model          │
       └────────┬────────┘
                ▼
       ┌─────────────────┐
       │  .cortex/       │  Store as JSON
       │  chunks/*.json  │
       └────────┬────────┘
                ▼
       ┌─────────────────┐
       │  MCP Server     │  Load into chromem-go
       │  (chromem-go)   │  Serve queries
       └─────────────────┘
```

## Phase 1: Parsing & Extraction

### Tree-sitter Parsing

Project Cortex uses [tree-sitter](https://tree-sitter.github.io/tree-sitter/), a fast incremental parser that builds concrete syntax trees for source code. Tree-sitter provides:

- **Language-agnostic parsing**: Consistent API across all supported languages
- **Error recovery**: Can parse incomplete or invalid code
- **Incremental updates**: Only re-parses changed sections
- **Syntax node queries**: SQL-like query language for extracting patterns

### Three-Tier Extraction

For each source file, Project Cortex extracts code at three levels of detail. The extraction happens in two phases:

**Phase 1: Tree-sitter Extraction (Internal)**

Tree-sitter parses the code into a syntax tree and extracts structured data:

```json
// Internal extraction (not stored in chunks)
{
  "file": "internal/server/handler.go",
  "package": "server",
  "imports": ["net/http", "encoding/json"],
  "types": [
    {"name": "Handler", "kind": "struct", "line": 15},
    {"name": "Config", "kind": "struct", "line": 22}
  ],
  "functions": [
    {"name": "NewHandler", "line": 30},
    {"name": "ServeHTTP", "receiver": "Handler", "line": 45}
  ]
}
```

**Phase 2: Chunk Formatting (What Gets Embedded)**

The structured data is formatted as natural language text optimized for vector embeddings:

#### 1. Symbols (High-Level Overview)

**Purpose**: Quick navigation and understanding of file structure.

**Formatted Output** (what goes in chunk text field):
```
Package: server

Imports: 2 packages

Types:
  - Handler (struct) (lines 15-18)
  - Config (struct) (lines 22-25)

Functions:
  - NewHandler() (lines 30-45)
  - (Handler) ServeHTTP() (lines 50-75)
```

**Why natural language?** Embeddings understand "Package: server" better than JSON structures.

#### 2. Definitions (Full Signatures)

**Purpose**: Understanding types, interfaces, and contracts without implementation noise.

**Formatted Output** (actual code with line comments):
```go
// Lines 15-18
type Handler struct {
    router *http.ServeMux
    config *Config
}

// Lines 30
func NewHandler(config *Config) (*Handler, error) { ... }
```

**Why actual code?** Preserves syntax for LLM comprehension, line comments enable navigation.

#### 3. Data (Constants & Values)

**Purpose**: Understanding configuration values and defaults.

**Formatted Output** (actual code with line comments):
```go
// Lines 10-13
const (
    DefaultPort = 8080
    DefaultTimeout = 30 * time.Second
)
```

**Why actual code?** Constants need exact values, not descriptions.

### Documentation Extraction (First-Class Feature)

Documentation extraction is equally important as code extraction. Project Cortex treats docs as a primary knowledge source for understanding architectural decisions, design philosophy, and the "why" behind code.

**Chunking Strategy (Within Token Constraints):**

Markdown and documentation files are chunked with **best-effort semantic preservation**:

- **Header-based splitting**: Attempts to split at `#`, `##`, `###` headers when token limits allow
- **Token limit reality**: Local embedding models (384-512 tokens) often require mid-section splits; cloud models (1024+ tokens) have more flexibility
- **Context metadata**: Each chunk includes file path, section titles, and header hierarchy
- **Trade-off aware**: Long sections or large code blocks may be split across chunks—embeddings still capture semantic meaning
- **No perfect preservation**: We optimize for searchability within constraints, not perfect document structure

**Example Documentation Chunk:**

```json
{
  "id": "doc:README.md:authentication:section",
  "type": "documentation",
  "file": "README.md",
  "headers": ["Authentication", "Getting Started"],
  "content": "# Authentication\n\nTo authenticate users, use the `AuthHandler`...",
  "code_blocks": [
    {
      "language": "go",
      "content": "auth := NewAuthHandler(config)"
    }
  ],
  "links": ["docs/auth-guide.md", "examples/auth-example.go"],
  "embedding": [0.123, -0.456, ...]
}
```

**Why This Matters:**

When an AI assistant needs to understand authentication in your system, it gets:
- **Architectural rationale**: Why this auth approach was chosen (design docs)
- **Constraints and trade-offs**: Security requirements, performance considerations (ADRs)
- **Implementation**: Actual code implementing the design
- **Best practices**: Team conventions and patterns (contribution guides)

This unified approach surfaces the "why" behind the code, not just the "what"—enabling the LLM to understand your system the way a senior engineer would.

## Phase 2: Chunking & Embedding

### Chunk Creation

Extracted code and documentation are broken into chunks optimized for vector search:

**Chunking Strategy:**
- **Symbols**: One chunk per file (small, for quick lookup)
- **Definitions**: One chunk per type/function (medium, for understanding)
- **Data**: Grouped by related constants (small, for configuration)
- **Docs**: Semantic sections based on headers (medium, for context)

Each chunk includes metadata:
```json
{
  "id": "go:internal/server/handler.go:Handler:definition",
  "type": "code-definition",
  "language": "go",
  "file": "internal/server/handler.go",
  "start_line": 15,
  "end_line": 18,
  "content": "type Handler struct {...}",
  "embedding": [0.123, -0.456, ...]
}
```

### Embedding Generation

Chunks are converted to vector embeddings using configurable models:

**Local Model (Privacy-First):**
```
cortex embed-server
  ↓
Python embedding server (50 lines)
  ↓
Sentence transformers (all-MiniLM-L6-v2)
  ↓
Embeddings generated locally
```

**Cloud Models:**
- OpenAI: `text-embedding-ada-002`
- Anthropic: Custom embeddings
- Configurable via `.cortex/config.yml`

## Phase 3: Storage

### JSON Chunk Files

Chunks are stored as JSON files in `.cortex/chunks/`:

```
.cortex/
  config.yml
  chunks/
    code-symbols.json        # Quick file/function lookup
    code-definitions.json    # Type/interface understanding
    code-data.json           # Constants and configuration
    doc-chunks.json          # Documentation search
```

**Benefits:**
- **Human-readable**: Easy to inspect and debug
- **Git-friendly**: Version controlled alongside code
- **Incremental updates**: Git handles diffs automatically
- **Shareable**: Teams can commit indexes for instant context

### Metadata for Incremental Updates

Each chunk file includes metadata enabling smart re-indexing:

```json
{
  "version": "1.0",
  "indexed_at": "2025-10-15T14:30:00Z",
  "file_hashes": {
    "internal/server/handler.go": "a1b2c3d4...",
    "internal/config/config.go": "e5f6g7h8..."
  },
  "chunks": [...]
}
```

When running `cortex index`:
1. Compare file modification times
2. Check file hashes
3. Only re-parse changed files
4. Merge new chunks with existing index

## Phase 4: Serving & Search

### MCP Server

The `cortex mcp` command:

1. **Loads JSON chunks** from `.cortex/chunks/`
2. **Initializes chromem-go** in-memory vector database
3. **Adds chunks** to vector collections
4. **Listens via stdio** for MCP protocol requests

```go
// Simplified flow
func serveMCP() {
    db := chromem.NewDB()

    // Load chunks from JSON files
    symbolChunks := loadChunks("code-symbols.json")
    defChunks := loadChunks("code-definitions.json")
    dataChunks := loadChunks("code-data.json")
    docChunks := loadChunks("doc-chunks.json")

    // Create collections
    db.CreateCollection("symbols", nil, nil)
    db.CreateCollection("definitions", nil, nil)
    db.CreateCollection("data", nil, nil)
    db.CreateCollection("docs", nil, nil)

    // Add chunks
    for _, chunk := range symbolChunks {
        db.AddDocument("symbols", chunk.ID, chunk.Content, chunk.Embedding)
    }
    // ... repeat for other collections

    // Start MCP server
    server := mcp.NewServer(db)
    server.ListenStdio()
}
```

### Query Execution

When an AI assistant searches for code:

```
User: "How does authentication work?"
  ↓
AI Assistant generates search query
  ↓
MCP request to cortex
  ↓
Query embedded using same model
  ↓
Vector similarity search in chromem-go
  ↓
Top K results returned with metadata
  ↓
AI Assistant uses results as context
```

### Search Strategies

Project Cortex supports multiple search patterns:

1. **Hierarchical Code Search**:
   - Search symbols first (fast, high-level)
   - If needed, search definitions (detailed)
   - Rarely need data tier

2. **Documentation-First Search**:
   - Query docs for architectural context and design decisions
   - Then search code for implementation details
   - Common pattern: "Why was X built this way?" → design docs + code

3. **Unified Search (Recommended)**:
   - Search code + docs simultaneously
   - Combine results for complete understanding
   - Example: "authentication" returns AuthHandler code AND design rationale from docs

4. **Context-Driven Search**:
   - Surface architectural context for unfamiliar code
   - Find design decisions and constraints
   - Bridge gap between implementation and intent

5. **Filtered Search**:
   - By file path
   - By language
   - By code type (struct, function, etc.)
   - By documentation type (README, guides, API docs)

## Performance Characteristics

### Indexing Performance

- **Initial index**: ~1000 files/second (depends on file size)
- **Incremental updates**: Only changed files processed
- **Watch mode**: File change detected in <100ms

### Search Performance

- **In-memory vector DB**: Sub-millisecond searches
- **Typical query**: <10ms for top 10 results
- **Memory usage**: ~1MB per 1000 chunks

### Scalability

| Codebase Size | Chunks | Memory Usage | Index Time |
|---------------|--------|--------------|------------|
| Small (100 files) | ~1K | ~10MB | <1s |
| Medium (1K files) | ~10K | ~50MB | <10s |
| Large (10K files) | ~100K | ~200MB | ~30s |
| Very Large (100K files) | ~1M | ~1GB | ~5min |

## Why This Architecture?

### Design Decisions

**Q: Why three-tier extraction?**
- Provides flexible granularity for different queries
- Symbols for navigation, definitions for understanding, data for configuration
- Reduces noise in search results

**Q: Why JSON files instead of SQLite/other DB?**
- Human-readable and debuggable
- Git-friendly for team collaboration
- No external dependencies
- Easy backup and recovery

**Q: Why in-memory vector DB?**
- Fast startup (loads in seconds)
- No network latency
- Simple deployment (single binary)
- chromem-go is pure Go (no CGO dependencies)

**Q: Why tree-sitter?**
- Fast and reliable
- Language-agnostic API
- Good error recovery
- Active development

**Q: Why MCP?**
- Standard protocol for AI tool integration
- Works with Claude Code, Cursor, and others
- Easy to implement
- Growing ecosystem


## Related Reading

- [Configuration Guide](configuration.md)
- [Language Support](languages.md)
- [MCP Integration](mcp-integration.md)
- [Tree-sitter Documentation](https://tree-sitter.github.io/tree-sitter/)
- [MCP Protocol Spec](https://modelcontextprotocol.io/)
- [chromem-go GitHub](https://github.com/philippgille/chromem-go)
