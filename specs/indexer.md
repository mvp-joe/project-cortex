# Indexer Specification

## Purpose

The indexer transforms a codebase (source code + documentation) into semantically searchable chunks stored as JSON files with vector embeddings. These chunks enable LLMs to understand and navigate codebases through semantic search rather than keyword matching.

## Core Concept

**Input**: Source files (code in multiple languages + markdown documentation)

**Process**: Parse → Extract → Format → Embed → Store

**Output**: JSON chunk files in `.cortex/chunks/` directory

## Technology Stack

- **Language**: Go 1.25+
- **Tree-sitter**: `github.com/tree-sitter/go-tree-sitter` (official Go bindings)
- **Supported Languages**:
  - Go (`.go`)
  - TypeScript (`.ts`, `.tsx`)
  - JavaScript (`.js`, `.jsx`)
  - Python (`.py`)
  - Rust (`.rs`)
  - C/C++ (`.c`, `.cpp`, `.cc`, `.h`, `.hpp`)
  - PHP (`.php`)
  - Ruby (`.rb`)
  - Java (`.java`)

## Three-Tier Code Extraction

The indexer extracts code at three levels of detail, each optimized for different query patterns:

### 1. Symbols (High-Level Overview)

**Purpose**: Quick navigation and file structure understanding

**Content**:
- Package/module name
- Import count (not full list)
- Type/class names with line numbers
- Function/method names with line numbers

**Format** (natural language text):
```
Package: server

Imports: 5 packages

Types:
  - Handler (struct) (lines 10-15)
  - Config (struct) (lines 20-25)

Functions:
  - NewHandler() (lines 30-45)
  - (Handler) ServeHTTP() (lines 50-75)
```

**Chunk ID**: `code-symbols-{file-path}`

### 2. Definitions (Full Signatures)

**Purpose**: Understanding types, interfaces, and function contracts

**Content**:
- Complete type definitions with fields
- Interface definitions
- Function signatures (no implementation bodies)
- Comments and docstrings

**Format** (actual code with line comments):
```go
// Lines 10-15
type Handler struct {
    router *http.ServeMux
    config *Config
}

// Lines 30
func NewHandler(config *Config) (*Handler, error) { ... }
```

**Chunk ID**: `code-definitions-{file-path}`

### 3. Data (Constants & Configuration)

**Purpose**: Finding configuration values and defaults

**Content**:
- Constant declarations
- Global variables with initializers
- Enum values
- Default configuration values

**Format** (actual code with line comments):
```go
// Lines 5-8
const (
    DefaultPort = 8080
    DefaultTimeout = 30 * time.Second
)

// Lines 15
var DefaultConfig = Config{Port: DefaultPort}
```

**Chunk ID**: `code-data-{file-path}`

## Documentation Chunking

**Strategy**: Semantic chunking by headers (preserves topic coherence)

### Chunking Algorithm

1. **Split by headers**: Use `##` (level 2) as primary boundaries
2. **Size check**: If section < target size, create single chunk
3. **Paragraph splitting**: If section > target size, split by paragraphs (double newline)
4. **Code block preservation**: Never split inside ` ```code blocks``` `
5. **Line tracking**: Track start_line and end_line for every chunk

### Chunk Metadata

- `section_index`: Which ## section (0-indexed)
- `chunk_index`: Which chunk within section (0-indexed for multi-part)
- `is_large_paragraph`: Boolean if single paragraph exceeded size
- `is_split_paragraph`: Boolean if paragraph was split by sentences

**Chunk ID**: `doc-{file-path}-s{section-idx}` or `doc-{file-path}-s{section-idx}-c{chunk-idx}`

## Output Format

All chunks use the proven `.scratch/Overwatch` format optimized for vector embeddings.

### Chunk Structure

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
    "package": "server",
    "start_line": 10,
    "end_line": 75,
    "imports_count": 5,
    "types_count": 2,
    "functions_count": 3
  },
  "created_at": "2025-10-16T12:35:00Z",
  "updated_at": "2025-10-16T12:35:00Z"
}
```

### File Structure

Output files in `.cortex/chunks/`:
- `doc-chunks.json` - Documentation chunks
- `code-symbols.json` - Code symbol chunks
- `code-definitions.json` - Code definition chunks
- `code-data.json` - Code data chunks

Each file includes metadata header:
```json
{
  "_metadata": {
    "model": "BAAI/bge-small-en-v1.5",
    "dimensions": 384,
    "chunk_type": "symbols",
    "generated": "2025-10-16T12:35:00Z",
    "version": "2.0.0"
  },
  "chunks": [...]
}
```

## Why Natural Language Formatting Works

The text field contains natural language (not JSON structures) because:

1. **Embedding models understand natural language**: "Package: server" embeds semantic meaning better than `{"package": "server"}`
2. **LLMs consume directly**: No parsing needed when LLM reads search results
3. **Token proximity matters**: "Handler struct at lines 10-15" keeps related concepts close in embedding space
4. **Human-readable**: Search results make sense to developers

## Pipeline

```
┌──────────────┐
│ Source Files │
└──────┬───────┘
       │
       ▼
┌─────────────────────┐
│  Tree-sitter Parse  │ ← Structured extraction (internal)
│  (per language)     │
└──────┬──────────────┘
       │
       ▼
┌─────────────────────┐
│  Chunk Formatter    │ ← Convert to natural language text
│  (proven format)    │
└──────┬──────────────┘
       │
       ▼
┌─────────────────────┐
│  Embedding Provider │ ← Generate vectors (384-dim)
│  (interface)        │ ← Local, OpenAI, etc.
└──────┬──────────────┘
       │
       ▼
┌─────────────────────┐
│  JSON Chunk Files   │ ← .cortex/chunks/*.json
└─────────────────────┘
```

## Embedding Provider Interface

**Design**: Indexer uses a provider interface to support multiple embedding backends.

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

**Usage in Indexer**:
```go
// Create provider via factory
provider, err := embed.NewProvider(embed.Config{
    Provider:   "local",
    BinaryPath: "cortex-embed",
})

// Generate embeddings for document chunks (use passage mode)
embeddings, err := provider.Embed(ctx, chunkTexts, embed.EmbedModePassage)
```

**Implementations**:
- `LocalProvider` - Manages cortex-embed binary, auto-starts if needed
- Future: `OpenAIProvider`, `AnthropicProvider`

**Configuration** (`.cortex/config.yml`):
```yaml
embedding:
  provider: "local"  # Currently only "local" supported
  model: "BAAI/bge-small-en-v1.5"
  dimensions: 384
```

**Benefits**:
- Clean abstraction - no HTTP/JSON details in indexer code
- Test indexer with mock provider
- Swap backends via config
- Mode parameter ensures correct embedding type (passage for documents)

## Incremental Indexing

Track file changes via SHA-256 checksums to avoid reprocessing unchanged files.

### Metadata File

`.cortex/generator-output.json`:
```json
{
  "version": "2.0.0",
  "generated_at": "2025-10-16T12:40:00Z",
  "file_checksums": {
    "README.md": "a1b2c3d4...",
    "internal/server/handler.go": "1234567890..."
  },
  "stats": {
    "docs_processed": 15,
    "code_files_processed": 125,
    "total_doc_chunks": 45,
    "total_code_chunks": 380,
    "processing_time_seconds": 12.5
  }
}
```

### Incremental Behavior

1. Load previous metadata
2. Calculate checksums for all files
3. Compare against previous run
4. Reprocess only changed files
5. Merge with unchanged chunks (see algorithm below)
6. Update metadata with new checksums

### Chunk Merge Algorithm

**Goal**: Preserve chunks from unchanged files, replace chunks from changed/deleted files.

```
1. Load existing chunks from .cortex/chunks/*.json
2. Build index: file_path → [chunk_ids]
3. For each changed file:
   a. Remove all chunks where metadata.file_path matches
   b. Add new chunks for changed file
4. For each deleted file:
   a. Remove all chunks where metadata.file_path matches
5. Write merged chunks to output files (atomic)
6. Update generator-output.json with new checksums
```

### Chunk ID Stability

**Chunks for unchanged files preserve their IDs**, ensuring:
- MCP server can maintain references
- Embeddings are not regenerated unnecessarily
- Incremental updates are truly incremental

**Chunk IDs include file path**, so:
- Renamed files → new chunk IDs (file reprocessed)
- Moved files → new chunk IDs (file reprocessed)
- Modified files → same IDs reused with new content

### Edge Cases

- **New files**: No previous chunks, all chunks added
- **Deleted files**: Remove chunks, remove from checksums
- **Empty files**: Remove all chunks for that file
- **Parse errors**: Log error, preserve old chunks (don't remove)

## Configuration

`.cortex/config.yml`:
```yaml
embedding:
  provider: "local"  # or "openai"
  model: "BAAI/bge-small-en-v1.5"
  dimensions: 384
  endpoint: "http://localhost:8121/embed"

paths:
  code:
    - "**/*.go"
    - "**/*.ts"
    - "**/*.tsx"
    - "**/*.js"
    - "**/*.jsx"
    - "**/*.py"
  docs:
    - "**/*.md"
    - "**/*.rst"
  ignore:
    - "node_modules/**"
    - "vendor/**"
    - ".git/**"
    - "dist/**"
    - "build/**"

chunking:
  strategies: ["symbols", "definitions", "data"]
  doc_chunk_size: 800  # tokens
  code_chunk_size: 2000  # characters
  overlap: 100  # tokens
```

## Key Behaviors

1. **Deterministic output**: Same input → same output (for full mode)
2. **Error resilience**: File processing failures don't corrupt existing chunks
3. **Atomic writes**: Write to temp file, then rename (prevents partial writes)
4. **Progress indication**: Show progress bars during processing
5. **Embedding fallback**: If embedding server unavailable, save chunks without embeddings and warn

## Atomic Write Strategy

**Problem**: MCP server watches `.cortex/chunks/` and reloads on file changes. Must prevent reading partial/corrupted state.

**Solution**: Atomic file writes using temp → rename pattern (POSIX atomic operation).

### Write Algorithm

```
For each chunk file:
1. Marshal chunks to JSON
2. Write to .cortex/chunks/.tmp/<filename>.json
3. Rename to .cortex/chunks/<filename>.json (atomic)
4. On error: delete temp file, preserve old file
```

### Benefits

- **MCP server safety**: Never reads incomplete files
- **Crash recovery**: Old files preserved if write fails
- **Hot reload friendly**: Rename triggers single fsnotify event

### Implementation Notes

- Temp directory: `.cortex/chunks/.tmp/` (gitignored)
- Cleanup: Remove stale temp files on startup
- Permissions: Match parent directory (preserve umask)

## Non-Goals

- Real-time indexing (use watch mode + incremental for near real-time)
- Semantic code analysis beyond structure (no type inference, no control flow)
- Language-specific optimizations (keep extraction logic consistent across languages)
- IDE integration (indexer is a CLI tool, MCP server handles IDE queries)
