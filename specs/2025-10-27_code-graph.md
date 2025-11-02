---
status: implemented
started_at: 2025-10-27T00:00:00Z
dependencies: [indexer, mcp-server]
---

# Code Graph Specification

## Purpose

The code graph provides structural relationship queries over code entities (types, functions, packages) to enable precise refactoring, impact analysis, and architectural exploration. This complements semantic search (understanding concepts) and full-text search (finding identifiers) with graph traversal (understanding relationships).

## Core Concept

**Input**: Source code files (already parsed during indexing)

**Process**: Extract → Build Graph → Store → Query → Enrich with Context

**Output**: Structural relationship data with optional code snippets

## Technology Stack

- **Language**: Go 1.25+
- **Graph Library**: `github.com/dominikbraun/graph` (cycle detection, traversal algorithms)
- **Storage**: `.cortex/graph/code-graph.json` (~5-10MB for typical project)
- **Query Engine**: In-memory graph with reverse indexes for O(1) lookups
- **Context Injection**: Post-query file reads via Go (not stored in graph)

## Why Separate from Semantic Search?

**Semantic search** (cortex_search):
- **Strengths**: Understands concepts, finds similar patterns, cross-references docs + code
- **Limitations**: Can't answer "who calls this function?" or "what breaks if I change this interface?"

**Full-text search** (cortex_exact):
- **Strengths**: Fast exact matches, finds specific identifiers
- **Limitations**: No understanding of relationships or dependencies

**Graph queries** (cortex_graph):
- **Strengths**: Precise structural queries (callers, implementations, dependencies, impact)
- **Limitations**: No semantic understanding, requires exact identifiers

**Together**: LLM uses all three based on task type.

## Architecture

```
┌─────────────────┐
│  Source Code    │ (Go, TypeScript, etc.)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Tree-sitter    │ ← Extract during existing parse
│  Parser         │   (parallel to chunk extraction)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  Graph          │ ← Build nodes + edges
│  Extractor      │   (functions, types, calls, imports)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  .cortex/graph/ │ ← Store relationships only
│  code-graph.json│   (NO code text, just positions)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  MCP Server     │ ← Load into dominikbraun/graph
│  (in-memory)    │   Build reverse indexes
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  cortex_graph   │ ← Query + Context Injection
│  Tool           │   (file reads for code snippets)
└─────────────────┘
```

## Graph Data Model

### Nodes

Represent code entities with precise source location and structural information:

```go
type Node struct {
    ID              string            // "embed.Provider", "localProvider", "internal/mcp"
    Kind            NodeKind          // Interface, Struct, Function, Method, Package
    File            string            // "internal/embed/provider.go"
    StartLine       int               // 15
    EndLine         int               // 20
    Methods         []MethodSignature // For interfaces and structs
    EmbeddedTypes   []string          // For embedded interfaces/structs
    ResolvedMethods []MethodSignature // Flattened method set after resolving embeddings
}

type NodeKind string
const (
    NodeInterface NodeKind = "interface"
    NodeStruct    NodeKind = "struct"
    NodeFunction  NodeKind = "function"
    NodeMethod    NodeKind = "method"
    NodePackage   NodeKind = "package"
)

type MethodSignature struct {
    Name       string      // "Embed", "Close"
    Parameters []Parameter // Function parameters
    Returns    []Parameter // Return values
}

type Parameter struct {
    Name string  // Parameter name (optional for returns)
    Type TypeRef // Type information
}

type TypeRef struct {
    Name      string // "Context", "error", "int", "Provider"
    Package   string // "context", "", "", "embed" (resolved from imports)
    IsPointer bool   // *Type
    IsSlice   bool   // []Type
    IsMap     bool   // map[K]V
    // Future: support for generics (TypeParams []TypeRef)
}
```

**Examples**:

**Interface node with method signatures:**
```json
{
  "id": "embed.Provider",
  "kind": "interface",
  "file": "internal/embed/provider.go",
  "start_line": 20,
  "end_line": 39,
  "methods": [
    {
      "name": "Embed",
      "parameters": [
        {
          "name": "ctx",
          "type": {"name": "Context", "package": "context", "is_pointer": false}
        },
        {
          "name": "texts",
          "type": {"name": "string", "package": "", "is_slice": true}
        },
        {
          "name": "mode",
          "type": {"name": "EmbedMode", "package": "embed"}
        }
      ],
      "returns": [
        {
          "type": {"name": "float32", "package": "", "is_slice": true, "is_slice_2d": true}
        },
        {
          "type": {"name": "error", "package": ""}
        }
      ]
    },
    {
      "name": "Close",
      "parameters": [],
      "returns": [
        {
          "type": {"name": "error", "package": ""}
        }
      ]
    }
  ],
  "resolved_methods": []
}
```

**Struct node with methods:**
```json
{
  "id": "localProvider",
  "kind": "struct",
  "file": "internal/embed/local.go",
  "start_line": 16,
  "end_line": 22,
  "methods": [
    {
      "name": "Embed",
      "parameters": [
        {
          "name": "ctx",
          "type": {"name": "Context", "package": "context"}
        },
        {
          "name": "texts",
          "type": {"name": "string", "package": "", "is_slice": true}
        },
        {
          "name": "mode",
          "type": {"name": "EmbedMode", "package": "embed"}
        }
      ],
      "returns": [
        {
          "type": {"name": "float32", "is_slice": true, "is_slice_2d": true}
        },
        {
          "type": {"name": "error"}
        }
      ]
    },
    {
      "name": "Close",
      "parameters": [],
      "returns": [
        {
          "type": {"name": "error"}
        }
      ]
    }
  ]
}
```

### Edges

Represent relationships between entities:

```go
type Edge struct {
    From     string      // Source node ID
    To       string      // Target node ID
    Type     EdgeType    // Relationship type
    Location *Location   // Where relationship occurs in source
}

type EdgeType string
const (
    EdgeImplements EdgeType = "implements" // struct -> interface
    EdgeEmbeds     EdgeType = "embeds"     // struct -> struct/interface
    EdgeCalls      EdgeType = "calls"      // function -> function
    EdgeImports    EdgeType = "imports"    // package -> package
)

type Location struct {
    File   string // Where edge originates
    Line   int    // Line number of relationship
    Column int    // Optional column
}
```

**Examples**:
```json
{
  "from": "localProvider",
  "to": "embed.Provider",
  "type": "implements",
  "location": {
    "file": "internal/embed/local.go",
    "line": 16
  }
}
```

```json
{
  "from": "indexer.IndexIncremental",
  "to": "provider.Embed",
  "type": "calls",
  "location": {
    "file": "internal/indexer/impl.go",
    "line": 234
  }
}
```

### Reverse Indexes

Built at load time for O(1) queries:

```go
type CodeGraph struct {
    // Primary graph (dominikbraun/graph)
    graph graph.Graph[string, *Node]

    // Reverse indexes for fast lookups
    implementations map[string][]string  // interface -> [implementors]
    callers         map[string][]string  // function -> [callers]
    callees         map[string][]string  // function -> [callees]
    dependencies    map[string][]string  // package -> [dependencies]
    dependents      map[string][]string  // package -> [dependents]

    // File cache for context extraction
    fileCache *lru.Cache // Recently read files (~50MB)
}
```

**Why reverse indexes?**
- Query "who calls `provider.Embed`?" → O(1) lookup in `callers["provider.Embed"]`
- Without index: O(E) traversal of all edges
- Memory cost: ~100 bytes per entry, ~5MB for 50K edges

## Storage Format

### File Structure

```
.cortex/
  config.yml
  generator-output.json
  chunks/
    code-symbols.json
    code-definitions.json
    code-data.json
    doc-chunks.json
  graph/                     # NEW - separate directory for graph data
    code-graph.json          # Structural relationships
```

**Rationale for separate directory**:
- Graph data is fundamentally different from searchable chunks (no embeddings, different query patterns)
- Clear separation of concerns: `chunks/` = search, `graph/` = relationships
- Future extensibility: Can add other graph types (e.g., `concept-graph.json`)
- Cleaner file watching: MCP server can watch directories independently

### code-graph.json Format

```json
{
  "_metadata": {
    "version": "1.0",
    "generated_at": "2025-10-27T12:00:00Z",
    "model": "n/a",
    "dimensions": 0,
    "node_count": 1247,
    "edge_count": 3856
  },
  "nodes": [
    {
      "id": "embed.Provider",
      "kind": "interface",
      "file": "internal/embed/provider.go",
      "start_line": 20,
      "end_line": 39,
      "methods": [
        {
          "name": "Embed",
          "parameters": [
            {
              "name": "ctx",
              "type": {"name": "Context", "package": "context"}
            },
            {
              "name": "texts",
              "type": {"name": "string", "is_slice": true}
            },
            {
              "name": "mode",
              "type": {"name": "EmbedMode", "package": "embed"}
            }
          ],
          "returns": [
            {
              "type": {"name": "float32", "is_slice": true}
            },
            {
              "type": {"name": "error"}
            }
          ]
        },
        {
          "name": "Close",
          "parameters": [],
          "returns": [
            {
              "type": {"name": "error"}
            }
          ]
        }
      ],
      "embedded_types": [],
      "resolved_methods": []
    },
    {
      "id": "localProvider",
      "kind": "struct",
      "file": "internal/embed/local.go",
      "start_line": 16,
      "end_line": 22,
      "methods": [
        {
          "name": "Embed",
          "parameters": [
            {
              "name": "ctx",
              "type": {"name": "Context", "package": "context"}
            },
            {
              "name": "texts",
              "type": {"name": "string", "is_slice": true}
            },
            {
              "name": "mode",
              "type": {"name": "EmbedMode", "package": "embed"}
            }
          ],
          "returns": [
            {
              "type": {"name": "float32", "is_slice": true}
            },
            {
              "type": {"name": "error"}
            }
          ]
        },
        {
          "name": "Close",
          "parameters": [],
          "returns": [
            {
              "type": {"name": "error"}
            }
          ]
        }
      ]
    }
  ],
  "edges": [
    {
      "from": "localProvider",
      "to": "embed.Provider",
      "type": "implements",
      "location": {
        "file": "internal/embed/local.go",
        "line": 16
      }
    },
    {
      "from": "indexer.IndexIncremental",
      "to": "provider.Embed",
      "type": "calls",
      "location": {
        "file": "internal/indexer/impl.go",
        "line": 234
      }
    }
  ]
}
```

**Key points**:
- **No code text in nodes/edges**: Keeps file small (~5-10MB for typical project)
- **Method signatures included**: Enables signature-based interface matching (~3% size increase)
- **Position metadata**: File path, line numbers for all entities
- **Context injected at query time**: MCP reads files to add code snippets
- **Atomic writes**: Use temp → rename pattern (same as other chunk files)

### Size Estimates

With method signature storage:

| Project Size | Nodes | Edges | Base Storage | +Signatures | Total Storage | Memory |
|--------------|-------|-------|--------------|-------------|---------------|--------|
| Small (100 files) | ~1K | ~5K | ~400KB | ~50KB | ~450KB | ~5MB |
| Medium (1K files) | ~10K | ~50K | ~4.5MB | ~150KB | ~4.65MB | ~10MB |
| Large (10K files) | ~100K | ~500K | ~45MB | ~1.5MB | ~46.5MB | ~100MB |

**Storage overhead**: Method signatures add ~3% to graph file size. This is negligible compared to the value they provide (accurate interface matching).

## MCP Tool Interface

### Tool Registration

```go
func AddCortexGraphTool(s *server.MCPServer, querier GraphQuerier) {
    tool := mcp.NewTool(
        "cortex_graph",
        mcp.WithDescription("Query structural code relationships for refactoring, impact analysis, and dependency exploration"),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
    )
    s.AddTool(tool, createGraphHandler(querier))
}
```

### Request Schema

```go
type CortexGraphRequest struct {
    // Core parameters
    Operation string `json:"operation" jsonschema:"required,enum=implementations|callers|callees|dependencies|dependents|path|impact"`
    Target    string `json:"target" jsonschema:"required"`

    // Filtering
    Scope           string   `json:"scope,omitempty"`
    ExcludePatterns []string `json:"exclude_patterns,omitempty"`

    // Context enrichment
    IncludeContext bool `json:"include_context,omitempty" jsonschema:"default=true"`
    ContextLines   int  `json:"context_lines,omitempty" jsonschema:"default=3,minimum=0,maximum=20"`

    // Traversal controls
    Depth         int `json:"depth,omitempty" jsonschema:"default=1,minimum=1,maximum=10"`
    MaxResults    int `json:"max_results,omitempty" jsonschema:"default=100,maximum=500"`
    MaxPerLevel   int `json:"max_per_level,omitempty" jsonschema:"default=50,maximum=100"`
}
```

JSON representation:
```json
{
  "operation": "callers|callees|implementations|dependencies|dependents|path|impact",
  "target": "string (required, fully qualified identifier)",
  "scope": "string (optional, glob pattern like 'internal/mcp/**')",
  "exclude_patterns": ["string array (optional, globs like '**/*_test.go')"],
  "include_context": "boolean (optional, default: true)",
  "context_lines": "number (optional, default: 3, max: 20)",
  "depth": "number (optional, default: 1, max: 10)",
  "max_results": "number (optional, default: 100, max: 500)",
  "max_per_level": "number (optional, default: 50, max: 100)"
}
```

### Response Schema

```go
type CortexGraphResponse struct {
    Operation     string          `json:"operation"`
    Target        string          `json:"target"`
    Results       []*GraphResult  `json:"results"`
    TotalFound    int             `json:"total_found"`
    TotalReturned int             `json:"total_returned"`
    Truncated     bool            `json:"truncated"`
    TruncatedAt   int             `json:"truncated_at_depth,omitempty"`
    Suggestion    string          `json:"suggestion,omitempty"`
    Metadata      ResponseMetadata `json:"metadata"`
}

type GraphResult struct {
    Node    *GraphNode `json:"node"`
    Context string     `json:"context,omitempty"` // Code snippet if include_context=true
    Depth   int        `json:"depth,omitempty"`   // For recursive operations
}

type GraphNode struct {
    ID        string `json:"id"`
    Kind      string `json:"kind"`
    File      string `json:"file"`
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
}

type ResponseMetadata struct {
    TookMs int    `json:"took_ms"`
    Source string `json:"source"` // "graph"
}
```

## Query Operations

### 1. implementations

**Purpose**: Find all types that implement a given interface

**Use case**: "Show me all implementations of `embed.Provider` so I can update them"

**Request**:
```json
{
  "operation": "implementations",
  "target": "embed.Provider",
  "include_context": true,
  "context_lines": 5
}
```

**Response**:
```json
{
  "operation": "implementations",
  "target": "embed.Provider",
  "results": [
    {
      "node": {
        "id": "localProvider",
        "kind": "struct",
        "file": "internal/embed/local.go",
        "start_line": 16,
        "end_line": 22
      },
      "context": "// Lines 11-27\n\ntype localProvider struct {\n    config     Config\n    binaryPath string\n    process    *exec.Cmd\n    client     *http.Client\n    mu         sync.Mutex\n}\n\nfunc (p *localProvider) Embed(ctx context.Context, texts []string, mode EmbedMode) ([][]float32, error) {"
    },
    {
      "node": {
        "id": "MockProvider",
        "kind": "struct",
        "file": "internal/embed/mock.go",
        "start_line": 12,
        "end_line": 18
      },
      "context": "..."
    }
  ],
  "total_found": 2,
  "total_returned": 2,
  "truncated": false,
  "metadata": {
    "took_ms": 5,
    "source": "graph"
  }
}
```

**Performance**: <1ms (index lookup)

### 2. callers

**Purpose**: Find all functions that call a given function

**Use case**: "Who calls `provider.Embed`? I need to update the signature"

**Request**:
```json
{
  "operation": "callers",
  "target": "provider.Embed",
  "depth": 2,
  "exclude_patterns": ["**/*_test.go"]
}
```

**Response**:
```json
{
  "operation": "callers",
  "target": "provider.Embed",
  "results": [
    {
      "node": {
        "id": "indexer.embedChunks",
        "kind": "method",
        "file": "internal/indexer/impl.go",
        "start_line": 760,
        "end_line": 779
      },
      "context": "...",
      "depth": 1
    },
    {
      "node": {
        "id": "mcp.chromemSearcher.Query",
        "kind": "method",
        "file": "internal/mcp/chromem_searcher.go",
        "start_line": 145,
        "end_line": 248
      },
      "context": "...",
      "depth": 1
    },
    {
      "node": {
        "id": "indexer.Index",
        "kind": "method",
        "file": "internal/indexer/impl.go",
        "start_line": 137,
        "end_line": 241
      },
      "context": "...",
      "depth": 2
    }
  ],
  "total_found": 3,
  "total_returned": 3,
  "truncated": false,
  "metadata": {
    "took_ms": 8,
    "source": "graph"
  }
}
```

**Performance**: ~1ms per depth level

### 3. callees

**Purpose**: Find all functions called by a given function (call tree)

**Use case**: "What does `indexer.Index` call? I want to understand the flow"

**Request**:
```json
{
  "operation": "callees",
  "target": "indexer.Index",
  "depth": 2,
  "scope": "internal/**"
}
```

**Response**:
```json
{
  "operation": "callees",
  "target": "indexer.Index",
  "results": [
    {
      "node": {
        "id": "indexer.processCodeFiles",
        "kind": "method",
        "file": "internal/indexer/impl.go",
        "start_line": 541,
        "end_line": 687
      },
      "context": "...",
      "depth": 1
    },
    {
      "node": {
        "id": "parser.ParseFile",
        "kind": "method",
        "file": "internal/indexer/parser.go",
        "start_line": 49,
        "end_line": 86
      },
      "context": "...",
      "depth": 2
    }
  ],
  "total_found": 12,
  "total_returned": 12,
  "truncated": false,
  "metadata": {
    "took_ms": 10,
    "source": "graph"
  }
}
```

**Performance**: ~5-10ms for depth 2-3

### 4. dependencies

**Purpose**: Find all packages imported by a given package

**Use case**: "What does `internal/mcp` depend on?"

**Request**:
```json
{
  "operation": "dependencies",
  "target": "internal/mcp",
  "include_context": false
}
```

**Response**:
```json
{
  "operation": "dependencies",
  "target": "internal/mcp",
  "results": [
    {
      "node": {
        "id": "internal/embed",
        "kind": "package",
        "file": "internal/mcp/chromem_searcher.go",
        "start_line": 4,
        "end_line": 4
      }
    },
    {
      "node": {
        "id": "github.com/mark3labs/mcp-go",
        "kind": "package",
        "file": "internal/mcp/server.go",
        "start_line": 8,
        "end_line": 8
      }
    },
    {
      "node": {
        "id": "github.com/philippgille/chromem-go",
        "kind": "package",
        "file": "internal/mcp/chromem_searcher.go",
        "start_line": 5,
        "end_line": 5
      }
    }
  ],
  "total_found": 3,
  "total_returned": 3,
  "truncated": false,
  "metadata": {
    "took_ms": 1,
    "source": "graph"
  }
}
```

**Performance**: <1ms (package-level graph is small)

### 5. dependents

**Purpose**: Find all packages that import a given package (reverse dependencies)

**Use case**: "What would break if I change `internal/embed`?"

**Request**:
```json
{
  "operation": "dependents",
  "target": "internal/embed",
  "include_context": false
}
```

**Response**:
```json
{
  "operation": "dependents",
  "target": "internal/embed",
  "results": [
    {
      "node": {
        "id": "internal/indexer",
        "kind": "package",
        "file": "internal/indexer/impl.go",
        "start_line": 6,
        "end_line": 6
      }
    },
    {
      "node": {
        "id": "internal/mcp",
        "kind": "package",
        "file": "internal/mcp/chromem_searcher.go",
        "start_line": 4,
        "end_line": 4
      }
    },
    {
      "node": {
        "id": "cmd/cortex",
        "kind": "package",
        "file": "cmd/cortex/main.go",
        "start_line": 3,
        "end_line": 3
      }
    }
  ],
  "total_found": 3,
  "total_returned": 3,
  "truncated": false,
  "metadata": {
    "took_ms": 1,
    "source": "graph"
  }
}
```

**Performance**: <1ms (reverse index lookup)

### 6. path

**Purpose**: Find call path from one function to another

**Use case**: "How does `http.Handler` reach `database.Query`?"

**Request**:
```json
{
  "operation": "path",
  "target": "http.Handler.ServeHTTP",
  "to": "database.Query",
  "max_depth": 10
}
```

**Response**:
```json
{
  "operation": "path",
  "target": "http.Handler.ServeHTTP",
  "results": [
    {
      "node": {
        "id": "http.Handler.ServeHTTP",
        "kind": "method",
        "file": "internal/server/handler.go",
        "start_line": 45,
        "end_line": 78
      },
      "context": "...",
      "depth": 0
    },
    {
      "node": {
        "id": "service.ProcessRequest",
        "kind": "function",
        "file": "internal/service/processor.go",
        "start_line": 23,
        "end_line": 67
      },
      "context": "...",
      "depth": 1
    },
    {
      "node": {
        "id": "repository.GetUser",
        "kind": "function",
        "file": "internal/repository/user.go",
        "start_line": 34,
        "end_line": 56
      },
      "context": "...",
      "depth": 2
    },
    {
      "node": {
        "id": "database.Query",
        "kind": "function",
        "file": "internal/database/client.go",
        "start_line": 156,
        "end_line": 189
      },
      "context": "...",
      "depth": 3
    }
  ],
  "total_found": 4,
  "total_returned": 4,
  "truncated": false,
  "metadata": {
    "took_ms": 15,
    "source": "graph"
  }
}
```

**Performance**: ~5-20ms (depends on graph distance, uses BFS)

**Note**: If no path found, returns empty results. If multiple paths exist, returns shortest path.

### 7. impact

**Purpose**: Analyze blast radius of changing a function/type

**Use case**: "What breaks if I change `embed.Provider.Embed` signature?"

**Request**:
```json
{
  "operation": "impact",
  "target": "embed.Provider.Embed",
  "change_type": "signature"
}
```

**Response**:
```json
{
  "operation": "impact",
  "target": "embed.Provider.Embed",
  "results": [
    {
      "node": {
        "id": "localProvider.Embed",
        "kind": "method",
        "file": "internal/embed/local.go",
        "start_line": 128,
        "end_line": 165
      },
      "context": "...",
      "impact_type": "implementation",
      "severity": "must_update"
    },
    {
      "node": {
        "id": "MockProvider.Embed",
        "kind": "method",
        "file": "internal/embed/mock.go",
        "start_line": 55,
        "end_line": 82
      },
      "context": "...",
      "impact_type": "implementation",
      "severity": "must_update"
    },
    {
      "node": {
        "id": "indexer.embedChunks",
        "kind": "method",
        "file": "internal/indexer/impl.go",
        "start_line": 760,
        "end_line": 779
      },
      "context": "...",
      "impact_type": "direct_caller",
      "severity": "review_needed"
    }
  ],
  "summary": {
    "implementations": 2,
    "direct_callers": 8,
    "transitive_callers": 45,
    "external_packages": 0
  },
  "total_found": 55,
  "total_returned": 10,
  "truncated": true,
  "suggestion": "Showing top 10 impacted sites. Use scope or exclude_patterns to narrow results.",
  "metadata": {
    "took_ms": 20,
    "source": "graph"
  }
}
```

**Performance**: ~10-20ms (combines implementations + callers queries)

## Context Injection

### Why Not Store Code in Graph?

**Decision**: Store **only** relationships and positions in graph, inject code context via file reads after queries.

**Problems with storing code snippets in graph**:
1. **Bloat**: Storing code increases graph size unnecessarily when code lives in files.
2. **Staleness**: If files change between indexing and query, stored snippets are outdated.
3. **Inflexibility**: Can't adjust context window (e.g., show 5 lines vs 20 lines) without reindexing.
4. **Duplication**: Code already exists in actual files—why store twice (and commit to git)?
5. **Reload cost**: Every file change requires reloading ALL code snippets, not just changed relationships.

**Benefits of post-query file reads**:
1. **Always fresh**: Read current file content at query time (never stale).
2. **Small index**: Graph stays ~5-10MB (just relationships), not 20-50MB (with code).
3. **Fast reload**: Only reindex changed relationships, not code content.
4. **Flexible context**: Adjust window size per query without reindexing.
5. **Performance**: LRU cache + OS page cache = <2ms per file read (acceptable overhead).

**Solution**: Inject context post-query via cached file reads

### Implementation

```go
func extractCodeContext(file string, startLine, endLine, contextLines int) (string, error) {
    // Read file (use LRU cache for recently read files)
    content, err := readFileWithCache(file)
    if err != nil {
        return "", err
    }

    lines := strings.Split(string(content), "\n")

    // Extract with context padding
    from := max(0, startLine - contextLines - 1)
    to := min(len(lines), endLine + contextLines)

    snippet := strings.Join(lines[from:to], "\n")

    // Add line number comment
    prefix := fmt.Sprintf("// Lines %d-%d\n", from+1, to)
    return prefix + snippet, nil
}
```

**Caching strategy**:
- LRU cache of last 100 files read (~50MB assuming 500KB avg file size)
- OS page cache provides additional speedup (files recently indexed likely in cache)
- Invalidate on fsnotify events (same watch as chunks)

**Performance**:
- Cold read: ~5ms (disk I/O)
- Warm read (LRU cache): <0.1ms (memory)
- Hot read (OS cache): ~0.5ms (kernel)
- Typical: ~1-2ms per result (mix of cache hits/misses)

**Benefits**:
1. Small index: Graph file stays ~5-10MB
2. Always fresh: Reads current file content
3. Flexible: Adjust `context_lines` without reindexing
4. No duplication: Single source of truth (the actual files)

## Graph Extraction

### Integration Point

Extract graph data **during existing parse phase** (parallel to chunk extraction):

```go
// Current flow (internal/indexer/parser.go)
func (p *parser) ParseFile(ctx context.Context, filePath string) (*CodeExtraction, error) {
    // Parse AST
    node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)

    // Extract chunks (existing)
    codeExtraction := &CodeExtraction{
        Symbols:     extractSymbols(node),
        Definitions: extractDefinitions(node),
        Data:        extractData(node),
    }

    // NEW: Extract graph (same AST walk, minimal overhead)
    codeExtraction.Graph = extractGraph(node, filePath)

    return codeExtraction, nil
}
```

### Go Extraction

#### 1. Function Calls

Extract from AST `CallExpr` nodes:

```go
func extractCalls(funcDecl *ast.FuncDecl, fset *token.FileSet, currentFile string) []Edge {
    var edges []Edge

    ast.Inspect(funcDecl.Body, func(n ast.Node) bool {
        callExpr, ok := n.(*ast.CallExpr)
        if !ok {
            return true
        }

        var callee string
        switch fun := callExpr.Fun.(type) {
        case *ast.Ident:
            // Direct call: foo()
            callee = fun.Name
        case *ast.SelectorExpr:
            // Method call: obj.Method() or pkg.Function()
            if ident, ok := fun.X.(*ast.Ident); ok {
                callee = ident.Name + "." + fun.Sel.Name
            }
        }

        if callee != "" {
            edges = append(edges, Edge{
                From: getCurrentFunctionName(funcDecl),
                To:   callee,
                Type: EdgeCalls,
                Location: &Location{
                    File: currentFile,
                    Line: fset.Position(callExpr.Pos()).Line,
                },
            })
        }

        return true
    })

    return edges
}
```

**Limitations without type checking**:
- Can extract call **syntax** but not always call **target**
- Variables: `handler.ServeHTTP()` - what's `handler`'s type?
- For MVP: Extract what's obvious (direct calls, package.Function), skip ambiguous cases

#### 2. Package Imports

Extract from AST `ImportSpec` nodes:

```go
func extractImports(node *ast.File, currentFile string, currentPackage string) []Edge {
    var edges []Edge

    for _, imp := range node.Imports {
        importPath := strings.Trim(imp.Path.Value, "\"")

        edges = append(edges, Edge{
            From: currentPackage,
            To:   importPath,
            Type: EdgeImports,
            Location: &Location{
                File: currentFile,
                Line: fset.Position(imp.Pos()).Line,
            },
        })
    }

    return edges
}
```

#### 3. Method Signature Extraction

Extract complete method signatures from tree-sitter AST (no type system required):

```go
func extractMethodSignatures(node ast.Node, imports map[string]string) []MethodSignature {
    var signatures []MethodSignature

    // Walk interface or struct methods
    for _, method := range extractMethods(node) {
        sig := MethodSignature{
            Name:       method.Name,
            Parameters: extractParameters(method.Params, imports),
            Returns:    extractParameters(method.Results, imports),
        }
        signatures = append(signatures, sig)
    }

    return signatures
}

func extractParameters(fieldList *ast.FieldList, imports map[string]string) []Parameter {
    var params []Parameter

    for _, field := range fieldList.List {
        typeRef := resolveTypeRef(field.Type, imports)

        // Handle multiple names with same type (e.g., a, b int)
        if len(field.Names) == 0 {
            params = append(params, Parameter{Type: typeRef})
        } else {
            for _, name := range field.Names {
                params = append(params, Parameter{
                    Name: name.Name,
                    Type: typeRef,
                })
            }
        }
    }

    return params
}

func resolveTypeRef(expr ast.Expr, imports map[string]string) TypeRef {
    switch t := expr.(type) {
    case *ast.Ident:
        // Simple type: int, string, MyType
        return TypeRef{Name: t.Name}

    case *ast.SelectorExpr:
        // Qualified type: context.Context, http.Handler
        if ident, ok := t.X.(*ast.Ident); ok {
            pkg := imports[ident.Name] // Resolve import alias
            return TypeRef{
                Name:    t.Sel.Name,
                Package: pkg,
            }
        }

    case *ast.StarExpr:
        // Pointer: *Type
        ref := resolveTypeRef(t.X, imports)
        ref.IsPointer = true
        return ref

    case *ast.ArrayType:
        // Slice: []Type
        ref := resolveTypeRef(t.Elt, imports)
        ref.IsSlice = true
        return ref

    case *ast.MapType:
        // Map: map[K]V (simplified - just mark as map)
        return TypeRef{Name: "map", IsMap: true}
    }

    return TypeRef{Name: "unknown"}
}
```

**Key Points**:
- Tree-sitter provides full AST with type expressions
- Resolve package names from import statements
- Handle pointers, slices, maps via type modifiers
- No type system required—pure syntactic analysis
- Storage overhead: ~50 bytes per method signature

#### 4. Interface Implementation Inference

**Challenge**: Go has implicit interface satisfaction (no explicit `implements` keyword). Must infer implementations by comparing method signatures.

**Approach**: Signature-based matching with embedded interface resolution

```go
// Step 1: Extract method signatures during parsing (tree-sitter)
func extractNode(decl ast.Decl, file string) *Node {
    node := &Node{
        ID:            extractID(decl),
        Kind:          extractKind(decl),
        File:          file,
        Methods:       extractMethodSignatures(decl, imports),
        EmbeddedTypes: extractEmbeddedTypes(decl),
    }
    return node
}

// Step 2: Flatten embedded interfaces after all files parsed
func resolveEmbeddings(nodes map[string]*Node) {
    for _, node := range nodes {
        if node.Kind != NodeInterface {
            continue
        }

        node.ResolvedMethods = flattenMethods(node, nodes, make(map[string]bool))
    }
}

func flattenMethods(node *Node, allNodes map[string]*Node, visited map[string]bool) []MethodSignature {
    if visited[node.ID] {
        return nil // Prevent cycles
    }
    visited[node.ID] = true

    methods := append([]MethodSignature{}, node.Methods...)

    // Recursively flatten embedded interfaces
    for _, embeddedID := range node.EmbeddedTypes {
        if embedded, exists := allNodes[embeddedID]; exists {
            embeddedMethods := flattenMethods(embedded, allNodes, visited)
            methods = append(methods, embeddedMethods...)
        }
    }

    return methods
}

// Step 3: Match struct methods against interface signatures
func inferImplementations(interfaces, structs []*Node) []Edge {
    var edges []Edge

    for _, iface := range interfaces {
        for _, strct := range structs {
            if implementsInterface(strct, iface) {
                edges = append(edges, Edge{
                    From: strct.ID,
                    To:   iface.ID,
                    Type: EdgeImplements,
                    Location: &Location{
                        File: strct.File,
                        Line: strct.StartLine,
                    },
                })
            }
        }
    }

    return edges
}

func implementsInterface(strct, iface *Node) bool {
    // Build index of struct methods by signature
    structMethods := indexMethodsBySignature(strct.Methods)

    // Check if struct has all interface methods
    for _, ifaceMethod := range iface.ResolvedMethods {
        if !hasMatchingMethod(structMethods, ifaceMethod) {
            return false
        }
    }

    return true
}

func hasMatchingMethod(structMethods map[string]MethodSignature, ifaceMethod MethodSignature) bool {
    structMethod, exists := structMethods[ifaceMethod.Name]
    if !exists {
        return false
    }

    // Compare full signatures
    return signaturesEqual(structMethod, ifaceMethod)
}

func signaturesEqual(a, b MethodSignature) bool {
    // Check parameter count
    if len(a.Parameters) != len(b.Parameters) {
        return false
    }

    // Check return count
    if len(a.Returns) != len(b.Returns) {
        return false
    }

    // Compare parameter types
    for i := range a.Parameters {
        if !typeRefsEqual(a.Parameters[i].Type, b.Parameters[i].Type) {
            return false
        }
    }

    // Compare return types
    for i := range a.Returns {
        if !typeRefsEqual(a.Returns[i].Type, b.Returns[i].Type) {
            return false
        }
    }

    return true
}

func typeRefsEqual(a, b TypeRef) bool {
    return a.Name == b.Name &&
           a.Package == b.Package &&
           a.IsPointer == b.IsPointer &&
           a.IsSlice == b.IsSlice &&
           a.IsMap == b.IsMap
}
```

**Accuracy**:
- ✅ **90-95% accuracy** for real-world Go code
- ✅ Handles method name + signature matching
- ✅ Resolves embedded interfaces correctly
- ✅ Normalizes import paths for cross-package types
- ⚠️ **Known limitations** (~5% edge cases):
  - **Type aliases**: `type MyInt = int` not resolved (requires type system)
  - **Generics**: `Processor[T any]` requires instantiation checking
  - **Complex embedded structs**: Promoted methods from deeply nested structs

**Performance**:
- Extraction: ~100-200ms for medium codebase (tree-sitter parse + method extraction)
- Inference: O(interfaces × structs × avg_methods) = ~10-50ms for typical projects
- Total: **~100ms** vs 2-3s for full type checking

**Storage overhead**:
- ~50 bytes per method signature
- Interface with 5 methods: ~250 bytes
- 50 interfaces + 500 structs with avg 3 methods: ~150KB additional
- **Total increase**: ~3% (5MB → 5.15MB)

**Why not use go/types for perfect accuracy?** See "Future Considerations" section below.

### Multi-Language Support

Each language has different relationship semantics:

**TypeScript**:
- Structural typing (duck typing) - implicit interface satisfaction like Go
- Optional explicit `implements` keyword (when used, easy to extract)
- Explicit imports (easy to extract)
- Function calls (similar to Go)
- Challenges: Dynamic imports, decorators

**Python**:
- Explicit class inheritance (easy to extract)
- Imports are explicit (easy to extract)
- Duck typing makes interface detection hard
- Challenges: Monkey patching, dynamic method resolution

**Future**: Add language-specific extractors in `internal/graph/extractors/`

## Incremental Updates

### Algorithm

Graph follows same incremental strategy as chunks:

```
1. Load previous code-graph.json
2. Load previous generator-output.json (file checksums)
3. Calculate current checksums
4. Identify changed/deleted files
5. Load existing graph
6. Remove nodes/edges from changed/deleted files
7. Re-extract graph from changed files
8. Re-infer cross-file relationships (interface implementations)
9. Merge preserved + new graph
10. Write code-graph.json atomically (temp → rename)
11. Update generator-output.json
```

### Cross-File Edges

**Problem**: Edge from File A → File B. What if only A changes?

**Solution**: Track edge ownership by file

```go
type EdgeMetadata struct {
    SourceFile string // File where edge originates
    TargetFile string // File where target is defined (may differ)
}

// When File A changes:
// - Remove edges where SourceFile == "A"
// - Preserve edges where SourceFile != "A" (even if they reference symbols in A)

// When File B changes:
// - Remove edges where TargetFile == "B" OR SourceFile == "B"
// - Re-extract may recreate some edges (if still valid)
```

**Example**:
```
File A: func foo() { bar() }  → Edge(foo → bar, SourceFile=A, TargetFile=B)
File B: func bar() {}

If A changes: Remove edge, re-extract A, recreate edge if call still exists
If B changes: Remove edge (target changed), re-extract B, re-extract cross-file edges
```

### Embedded Interface Re-Flattening

**Problem**: Interface A embeds Interface B. When B changes, A's resolved methods must be recomputed.

**Solution**: Track embedding relationships and invalidate downstream

```go
// During initial load: Build embedding dependency graph
type EmbeddingGraph struct {
    embedders map[string][]string  // B -> [A, C, D] (who embeds B?)
}

// When interface changes:
func handleInterfaceChange(changedInterfaceID string, graph *CodeGraph) {
    // Find all interfaces that embed this one (directly or transitively)
    affected := findAffectedInterfaces(changedInterfaceID, graph.embedders)

    // Re-flatten affected interfaces
    for _, ifaceID := range affected {
        iface := graph.nodes[ifaceID]
        iface.ResolvedMethods = flattenMethods(iface, graph.nodes, make(map[string]bool))
    }
}

func findAffectedInterfaces(changedID string, embedders map[string][]string) []string {
    var affected []string
    visited := make(map[string]bool)

    var traverse func(id string)
    traverse = func(id string) {
        if visited[id] {
            return
        }
        visited[id] = true

        // Who embeds this interface?
        for _, embedderID := range embedders[id] {
            affected = append(affected, embedderID)
            traverse(embedderID)  // Recursively find transitive embedders
        }
    }

    traverse(changedID)
    return affected
}
```

**Complexity**:
- Finding affected: O(embedding edges) = typically 10-20 interfaces
- Re-flattening: O(affected interfaces × avg embedded depth) = ~5-10ms
- **Much faster** than re-inferring all implementations (no struct comparison needed)

**Example**:
```
Interface hierarchy:
  Reader (changed)
    ├─ ReadWriter (embeds Reader)
    └─ ReadCloser (embeds Reader)
        └─ ReadWriteCloser (embeds ReadCloser + Writer)

When Reader changes:
  1. Affected: [ReadWriter, ReadCloser, ReadWriteCloser]
  2. Re-flatten each (3 interfaces)
  3. No struct matching needed
```

### Interface Implementation Re-Inference

**Problem**: When interface or struct changes, must re-check which structs implement which interfaces.

**Optimization**: Only re-infer when method signatures actually change

```go
// Track method signature changes
func needsReInference(oldNode, newNode *Node) bool {
    if oldNode.Kind != NodeInterface && oldNode.Kind != NodeStruct {
        return false
    }

    // Compare method signatures
    return !methodSetsEqual(oldNode.Methods, newNode.Methods)
}

// During incremental update:
changedInterfaces := []string{}
changedStructs := []string{}

for _, changedFile := range changedFiles {
    oldNodes := previousGraph.nodesByFile[changedFile]
    newNodes := currentGraph.nodesByFile[changedFile]

    for id, newNode := range newNodes {
        oldNode := oldNodes[id]
        if needsReInference(oldNode, newNode) {
            if newNode.Kind == NodeInterface {
                changedInterfaces = append(changedInterfaces, id)
            } else if newNode.Kind == NodeStruct {
                changedStructs = append(changedStructs, id)
            }
        }
    }
}

// Only re-infer if signatures actually changed
if len(changedInterfaces) > 0 || len(changedStructs) > 0 {
    // Load affected interfaces and structs
    interfaces := loadInterfaces(changedInterfaces, currentGraph)
    structs := loadStructs(changedStructs, currentGraph)

    // Re-infer only affected implementations
    newImpls := inferImplementations(interfaces, structs)

    // Remove old implementation edges for changed entities
    graph.RemoveImplementationEdges(changedInterfaces, changedStructs)

    // Add new implementation edges
    graph.AddEdges(newImpls)
}
```

**Complexity**:
- **Best case** (code change, no signature change): 0ms (skip re-inference)
- **Typical** (1 interface changed): 1 interface × 500 structs = ~5ms
- **Worst case** (many changes): O(changed interfaces × all structs) = ~50-100ms

**Still O(changes)**, not O(all types). File-level incremental model preserved.

### Atomic Writes

Use same pattern as chunk files:

```go
// Write to temp file
tempFile := filepath.Join(graphDir, ".tmp", "code-graph.json")
err := writeJSON(tempFile, graphData)

// Atomic rename
finalFile := filepath.Join(graphDir, "code-graph.json")
err = os.Rename(tempFile, finalFile)
```

**Benefits**:
- MCP server never sees partial writes
- Single fsnotify event triggers reload
- Crash-safe (old file preserved on error)

## Performance & Limits

### Query Performance

| Operation | Typical Time | Notes |
|-----------|--------------|-------|
| implementations | <1ms | Index lookup |
| callers (depth 1) | ~1ms | Index lookup |
| callers (depth 5) | ~5-10ms | Multi-level traversal |
| callees (depth 1) | ~1ms | Index lookup |
| callees (depth 5) | ~5-10ms | Multi-level traversal |
| dependencies | <1ms | Package graph (small) |
| dependents | <1ms | Reverse index |
| path | ~5-20ms | BFS shortest path |
| impact | ~10-20ms | Combines multiple queries |

With context enrichment (+1-2ms per result):
- 10 results: +10-20ms
- 50 results: +50-100ms
- 100 results: +100-200ms

**Total typical query**: 10-50ms (graph query) + 50-100ms (context) = **60-150ms**

### Memory Footprint

| Component | Size |
|-----------|------|
| dominikbraun/graph | ~5MB (10K nodes, 50K edges) |
| Reverse indexes | ~5MB (callers, callees, implementations) |
| File cache (LRU) | ~50MB (last 100 files) |
| **Total** | **~60MB** |

**Scalability**:
- 100K nodes, 500K edges → ~100MB graph + indexes
- Still fits comfortably in memory on modern machines
- Query times scale sub-linearly (graph algorithms are efficient)

### Response Size Limits

**Problem**: Deep call graphs could return thousands of results

**Solution**: Truncation with metadata

```go
const (
    DefaultMaxResults   = 100
    DefaultMaxPerLevel  = 50
    DefaultDepth        = 1
    MaxDepth            = 10
)

func (q *Querier) truncateResults(results []*GraphResult, maxResults, maxPerLevel int) (*Response, bool) {
    truncated := false
    truncatedAtDepth := -1

    // Cap results per depth level
    levelCounts := make(map[int]int)
    filtered := []*GraphResult{}

    for _, result := range results {
        if levelCounts[result.Depth] >= maxPerLevel {
            truncated = true
            if truncatedAtDepth < 0 {
                truncatedAtDepth = result.Depth
            }
            continue
        }

        if len(filtered) >= maxResults {
            truncated = true
            break
        }

        filtered = append(filtered, result)
        levelCounts[result.Depth]++
    }

    response := &Response{
        Results:       filtered,
        TotalFound:    len(results),
        TotalReturned: len(filtered),
        Truncated:     truncated,
    }

    if truncated {
        response.TruncatedAt = truncatedAtDepth
        response.Suggestion = "Narrow scope or reduce depth to see more results"
    }

    return response, truncated
}
```

**LLM behavior**:
- Sees truncation metadata
- Can refine query (narrower scope, exclude patterns, reduce depth)
- Or accept partial results and synthesize answer

### Cycle Detection

**Problem**: Recursive calls create cycles (A → B → A)

**Solution**: dominikbraun/graph detects cycles during traversal

```go
// Cycle detected during BFS/DFS traversal
if graph.CreatesCycle(from, to) {
    // Don't follow edge, mark in response
    result.CycleDetected = true
    result.CyclePath = []string{from, to, from}
    continue
}
```

**Behavior**:
- Stop traversal at cycle (don't infinitely loop)
- Mark cycle in response for LLM awareness
- Optionally add dedicated `cycles` operation (future)

## Integration with Existing Tools

### Three-Tool Strategy

**cortex_search** (semantic, vector-based):
- **When**: "how does authentication work?", "find similar error handling patterns"
- **Strengths**: Understands concepts, cross-references code + docs
- **Limitations**: Can't answer "who calls X?"

**cortex_exact** (full-text, keyword-based):
- **When**: "find all uses of sync.RWMutex", "locate Provider interface"
- **Strengths**: Fast exact matches, boolean search
- **Limitations**: No understanding of meaning or relationships

**cortex_graph** (structural, graph-based):
- **When**: "who implements Provider?", "what calls this?", "impact of changing X?"
- **Strengths**: Precise structural queries, refactoring support
- **Limitations**: Requires exact identifiers, no semantic understanding

### LLM Tool Routing

**Decision tree for LLM**:

```
User query contains "how" or "why"?
  → cortex_search (semantic understanding)

User query contains exact identifier (Provider, http.Handler)?
  → Check if asking about relationships
    → YES: cortex_graph (who calls, who implements, dependencies)
    → NO: cortex_exact (find definition/usage)

User query about refactoring or impact?
  → cortex_graph (impact analysis, call graphs)

User query about architecture or dependencies?
  → cortex_graph (dependencies, dependents)
```

### Hybrid Query Patterns

**Example 1: Refactoring with context**

User: "I need to change the `Provider.Embed` signature. Show me all implementations."

```javascript
// Step 1: Find implementations (graph)
const impls = await cortex_graph({
  operation: "implementations",
  target: "embed.Provider"
})

// Step 2: LLM reads the context from graph results
// Graph response includes code context via file reads:
// {
//   "results": [{
//     "node": {"id": "localProvider", "file": "internal/embed/local.go", ...},
//     "context": "// Lines 45-60\nfunc (p *localProvider) Embed(...) ([][]float32, error) {\n  resp, err := p.client.Post(...)\n  if err != nil {\n    return nil, fmt.Errorf(\"embedding request failed: %w\", err)\n  }\n  ...\n}"
//   }]
// }

// LLM can now analyze implementations directly from context:
// "Found 2 implementations: localProvider wraps HTTP errors,
//  MockProvider returns configurable test errors"
```

**Example 2: Impact analysis with documentation**

User: "What breaks if I remove the `Close()` method from Provider?"

```javascript
// Step 1: Find implementations (graph)
const impls = await cortex_graph({
  operation: "implementations",
  target: "embed.Provider"
})

// Step 2: Find callers of Close() (graph)
const callers = await cortex_graph({
  operation: "callers",
  target: "Provider.Close",
  depth: 3
})

// Step 3: Search docs for cleanup/resource management (semantic)
const docs = await cortex_search({
  query: "resource cleanup lifecycle management",
  chunk_types: ["documentation"]
})

// LLM synthesizes: "Removing Close() would break 8 call sites. The Provider
// interface contract (from docs) requires cleanup for graceful shutdown..."
```

**Example 3: Architecture exploration**

User: "Explain the dependency relationship between MCP and embed packages"

```javascript
// Step 1: Get dependencies (graph)
const mcpDeps = await cortex_graph({
  operation: "dependencies",
  target: "internal/mcp"
})

// Step 2: Get specific calls (graph)
const calls = await cortex_graph({
  operation: "callees",
  target: "mcp.chromemSearcher.Query",
  scope: "internal/embed/**"
})

// Step 3: Find architectural context (semantic)
const arch = await cortex_search({
  query: "MCP server embedding provider architecture",
  chunk_types: ["documentation"]
})

// LLM synthesizes: "MCP depends on embed for query embeddings. The
// chromemSearcher calls provider.Embed() to generate vectors. Architecture
// docs explain this is to support pluggable embedding backends..."
```

## Implementation Phases

### Phase 1: MVP - Core Graph Operations (Go only)

**Goal**: Basic graph with immediate value

**Scope**:
- Extract function calls (local + package-level)
- Extract package imports
- Build graph with dominikbraun/graph
- Implement operations: `callers`, `callees`, `dependencies`, `dependents`
- MCP tool with basic parameters (operation, target, include_context)
- Incremental indexing support

**Exclude from MVP**:
- Interface implementations (requires type checking)
- Multi-language support
- Advanced operations (`path`, `impact`)
- Negative glob patterns

**Complexity**: Medium
**Timeline**: 1-2 weeks
**Value**: High (enables basic refactoring queries)

### Phase 2: Interface Inference & Advanced Operations

**Goal**: Add Go-specific features

**Scope**:
- Interface implementation inference (heuristic method matching)
- `implementations` operation
- `impact` operation (combines implementations + callers)
- `path` operation (shortest path between functions)
- Filtering: `scope`, `exclude_patterns`
- Response limits: `max_results`, `max_per_level`, `depth`

**Complexity**: High (interface inference is tricky)
**Timeline**: 1-2 weeks
**Value**: Very High (unlocks "what implements" queries)

### Phase 3: Multi-Language Support

**Goal**: TypeScript, Python, Rust

**Scope**:
- TypeScript extractor (explicit implements, imports, calls)
- Python extractor (class inheritance, imports, calls)
- Language-specific extractors in `internal/graph/extractors/`
- Handle mixed-language codebases

**Complexity**: Medium per language
**Timeline**: 1 week per language
**Value**: High (broader applicability)

### Phase 4: Optimizations & Polish

**Goal**: Production-ready performance

**Scope**:
- Query optimization (smarter traversal)
- Better incremental updates (minimize re-inference)
- Memory optimizations (graph compression)
- Advanced features (cycle detection queries, cross-language calls)

**Complexity**: Medium
**Timeline**: 1-2 weeks
**Value**: Medium (polish for scale)

## Non-Goals

1. **Graph visualization**: LLM handles rendering (Mermaid, DOT, ASCII)
2. **Query caching**: In-memory queries fast enough, low cache hit rate
3. **Documentation relationships**: Separate future spec (doc → code references)
4. **Real-time updates**: Hot reload with debounce sufficient
5. **Regex target matching**: High complexity for MVP, defer
6. **Cross-language calls**: Future (requires polyglot type resolution)
7. **Semantic graph search**: Graph is structural, use cortex_search for semantics
8. **Persistent graph DB**: In-memory sufficient, fast startup from JSON

## Future Considerations

### Why Not Use go/types for Interface Detection?

The signature-based approach using tree-sitter is the **primary strategy** for interface implementation detection. Here's why we don't use Go's type system (`go/types`):

#### Fundamental Incompatibility with Incremental Model

**Our incremental model**: File-level checksums detect changes. Only changed files are re-parsed and re-indexed. Unchanged files preserve their nodes and edges.

**go/types requirement**: Package-level type checking requires loading **all dependencies** of a package to build the type graph. When a single file changes:

1. Must load entire package containing the file
2. Must load all packages that import it (transitive)
3. Must type-check the full package graph to maintain correctness
4. Must re-infer **all** interface implementations in affected packages

**Result**: A one-file change in a core package triggers re-checking hundreds of files. File-level incremental updates become impossible—you need package-level or even module-level granularity.

**Example**:
```
Change internal/embed/provider.go (defines Provider interface)
→ Must re-check internal/embed package
→ Must re-check internal/indexer (imports embed)
→ Must re-check internal/mcp (imports embed)
→ Must re-check cmd/cortex (imports all above)
→ Total: 20+ files re-checked for 1 file change
```

With signature-based matching: Only `provider.go` is re-parsed. Embeddings are re-flattened (O(interfaces), ~5ms). Other files untouched.

#### Performance Cost

| Operation | Tree-sitter + Signatures | go/types |
|-----------|-------------------------|----------|
| Initial full index | ~200ms | ~2-3s |
| Single file change | ~50ms | ~500ms-1s |
| Memory overhead | +150KB (~3%) | +50-100MB (~100-200%) |

**Why so slow?**
- go/types builds full package graph (loads, parses, resolves all imports)
- Type-checks method sets with full semantic analysis
- Maintains internal type caches that need invalidation

#### Complexity Cost

**go/types dependencies**:
```
golang.org/x/tools/go/packages
golang.org/x/tools/go/loader
golang.org/x/tools/internal/...
```

- External dependency on Go toolchain internals
- Version compatibility concerns (Go 1.25 vs 1.26)
- Larger binary size (~2-3MB additional)
- More failure modes (broken go.mod, missing dependencies, version conflicts)

**Tree-sitter approach**:
- Single dependency: tree-sitter-go (already required for chunking)
- Self-contained: No Go toolchain needed
- Version-independent: Parses syntax, not semantics

#### What We Actually Lose

The signature-based approach has **~90-95% accuracy**. The 5% edge cases:

1. **Type aliases** (~2% of code):
   ```go
   type MyInt = int
   interface Foo { Bar(MyInt) }
   struct Baz {}
   func (b Baz) Bar(int) {}  // Actually satisfies Foo, we miss it
   ```

2. **Generics** (~2% of code):
   ```go
   interface Processor[T any] { Process(T) error }
   struct IntProcessor {}
   func (i IntProcessor) Process(int) error {}  // Satisfies Processor[int], we miss it
   ```

3. **Complex embedded structs** (~1% of code):
   ```go
   type A struct { B }
   type B struct { C }
   type C struct {}
   func (c C) Method() {}  // Promoted to A, but requires deep analysis
   ```

**Are these worth 20-30x slowdown + breaking incremental model?** No.

### Signature-Based Matching is Sufficient

The approach documented in this spec achieves the right balance:

**What it handles well** (90-95% of real code):
- ✅ Standard interface implementations
- ✅ Method signature matching (names, params, returns)
- ✅ Embedded interfaces (flattened correctly)
- ✅ Cross-package types (resolved via imports)
- ✅ Pointer receivers vs value receivers
- ✅ Variadic functions

**What it misses** (~5% edge cases):
- ⚠️ Type aliases (rare in interface contracts)
- ⚠️ Generic type instantiation (Go 1.18+, still uncommon)
- ⚠️ Deeply promoted methods (rare architectural pattern)

**False positives**: Extremely rare. Method name + full signature match is highly specific.

**False negatives**: The 5% we miss. LLM can still find these implementations via semantic search on the code context.

**User experience**: If user asks "what implements Provider?", they get 95% of implementations instantly. For the edge cases, semantic search fills the gap.

### Optional go/types Deep Validation

Instead of using go/types for primary indexing, offer it as a **periodic validation tool**:

```bash
# Normal indexing (fast, incremental, signature-based)
cortex index

# Deep validation (slow, one-time, go/types comparison)
cortex graph validate --deep
```

**What `--deep` does**:

1. Run normal signature-based inference (fast)
2. Run go/types-based inference (slow, requires full type checking)
3. Compare results:
   - **Matched**: Both methods agree → High confidence
   - **Signature-only**: Found by signatures, not by go/types → Investigate (likely false positive)
   - **Types-only**: Found by go/types, not by signatures → Investigate (false negative, edge case)
4. Report discrepancies with file paths and method details

**Use cases**:

- **Pre-release validation**: Run in CI before major releases to catch edge cases
- **Refactoring confidence**: After large interface changes, validate implementations
- **Accuracy metrics**: Track how well signature-based matching performs over time
- **Edge case discovery**: Identify patterns that need special handling in future versions

**Implementation**:

```go
type ValidationReport struct {
    TotalInterfaces       int
    AgreedImplementations int      // Both methods found
    SignatureOnly         []Edge   // False positives?
    TypesOnly             []Edge   // False negatives (edge cases)
    Accuracy              float64  // Agreed / (Agreed + Discrepancies)
}

func ValidateWithTypes(graphPath string) (*ValidationReport, error) {
    // Load signature-based graph
    sigGraph := loadGraph(graphPath)
    sigImpls := extractImplementations(sigGraph)

    // Run go/types analysis
    typeImpls, err := inferWithGoTypes(".")
    if err != nil {
        return nil, err
    }

    // Compare and report
    return compareResults(sigImpls, typeImpls), nil
}
```

**Performance**: Doesn't matter—it's a one-time validation command, not part of hot path.

**Value**: Builds confidence in signature-based approach without compromising speed/incrementality of normal indexing.

### Cross-Language Calls

**Use case**: Go service calls Python script via `exec.Command`

**Challenge**: Requires analyzing command-line invocations, environment setup

**Future**: Specialized extractors for common patterns (Go→Python, TypeScript→Python)

### Documentation Relationships

**Use case**: "Find all code referenced by this design doc"

**Approach**: Parse code references in docs (`` `Provider` ``, file paths), create edges

**Separate spec**: `docs-graph.md` (different relationship semantics)

### Performance Metrics

Track graph query performance:

```go
type Metrics struct {
    QueryCount        int64
    AvgQueryTimeMs    float64
    CacheHitRate      float64
    GraphSizeMB       float64
    LastReloadTimeMs  float64
}
```

Expose via `cortex mcp --stats` or MCP tool

### Graph Analytics

**Cyclomatic complexity**: Which functions have most callers/callees?
**Dead code**: Functions with zero callers outside tests
**Coupling metrics**: Which packages have highest dependency fan-out?

Future: Dedicated analytics queries or separate tool

## Testing Strategy

### Unit Tests

```go
// internal/graph/extractor_test.go
func TestExtractFunctionCalls(t *testing.T) {
    source := `
    package main

    func foo() {
        bar()
        baz()
    }
    `
    edges := extractCalls(source)
    assert.Equal(t, 2, len(edges))
    assert.Equal(t, "foo", edges[0].From)
    assert.Equal(t, "bar", edges[0].To)
}

// internal/graph/querier_test.go
func TestQueryCallers(t *testing.T) {
    graph := buildTestGraph()
    results, err := graph.Query(context.Background(), &GraphQuery{
        Operation: "callers",
        Target:    "provider.Embed",
    })
    assert.NoError(t, err)
    assert.True(t, len(results) > 0)
}
```

### Integration Tests

```go
// internal/graph/integration_test.go
// +build integration

func TestGraphEndToEnd(t *testing.T) {
    // 1. Index test project
    idx := indexer.New(testProjectDir)
    stats, err := idx.Index(context.Background())
    assert.NoError(t, err)

    // 2. Load graph
    graph, err := LoadGraphFromFile(".cortex/graph/code-graph.json")
    assert.NoError(t, err)

    // 3. Query
    results, err := graph.Query(ctx, &GraphQuery{
        Operation: "implementations",
        Target:    "TestInterface",
    })
    assert.NoError(t, err)
    assert.Equal(t, 2, len(results)) // Expect 2 implementations
}
```

### E2E Tests

```bash
# tests/e2e/graph_test.sh

# Index test project
cortex index ./testdata/sample-project

# Verify graph.json created
test -f ./testdata/sample-project/.cortex/graph/code-graph.json

# Start MCP server (background)
cortex mcp &
MCP_PID=$!

# Query via MCP protocol
echo '{"method":"tools/call","params":{"name":"cortex_graph","arguments":{"operation":"implementations","target":"Provider"}}}' | cortex mcp

# Cleanup
kill $MCP_PID
```

### Benchmarks

```go
// internal/graph/bench_test.go
func BenchmarkQueryCallers(b *testing.B) {
    graph := loadLargeGraph() // 100K nodes

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        graph.Query(context.Background(), &GraphQuery{
            Operation: "callers",
            Target:    "commonFunction",
            Depth:     3,
        })
    }
}

func BenchmarkIncrementalUpdate(b *testing.B) {
    for i := 0; i < b.N; i++ {
        // Simulate file change
        modifyFile("internal/server/handler.go")

        // Rebuild graph incrementally
        graph.Update([]string{"internal/server/handler.go"})
    }
}
```

## Summary

**cortex_graph** provides structural code relationship queries that complement semantic and full-text search:

- **Storage**: ~5-10MB JSON file with nodes/edges (no code text)
- **Memory**: ~60MB in-memory graph with reverse indexes
- **Performance**: <10ms for most queries, <150ms with context enrichment
- **Operations**: implementations, callers, callees, dependencies, dependents, path, impact
- **Context**: Post-query file reads inject code snippets (always fresh)
- **Incremental**: Preserves unchanged relationships, re-infers cross-file edges
- **Library**: dominikbraun/graph for proven graph algorithms

**Value proposition**: Transforms LLM from "code reader" to "refactoring partner" by enabling precise structural queries impossible with semantic/full-text search alone.
