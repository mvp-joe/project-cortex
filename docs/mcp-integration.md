# MCP Integration Guide

This guide walks you through integrating Project Cortex with MCP-compatible AI coding assistants like Claude Code, Cursor, and other tools that support the Model Context Protocol.

## What is MCP?

The [Model Context Protocol (MCP)](https://modelcontextprotocol.io/) is an open standard for connecting AI assistants to external data sources and tools. Project Cortex implements an MCP server that exposes your indexed codebase for semantic search.

## How It Works

```
┌─────────────────┐
│  AI Assistant   │
│ (Claude Code)   │
└────────┬────────┘
         │ MCP Protocol (stdio)
         ▼
┌─────────────────┐
│ cortex mcp      │  MCP Server
│ (chromem-go)    │
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ .cortex/chunks/ │  Indexed project
│   *.json        │
└─────────────────┘
```

When the AI assistant needs code context:
1. It sends a search query via MCP
2. Project Cortex searches the vector database
3. Relevant code chunks are returned
4. AI uses them to provide better answers

## Prerequisites

1. **Project Cortex installed**: `task install` or `go install`
2. **Project indexed**: Run `cortex index` in your project
3. **MCP-compatible AI assistant** (Claude Code, Cursor, etc.)

## Integration with Claude Code

Claude Code has native MCP support. Here's how to connect Project Cortex:

### 1. Index Your Project

Navigate to your project directory:

```bash
cd /path/to/your/project
cortex index
```

This creates `.cortex/chunks/` with your indexed code.

### 2. Configure Claude Code

Edit Claude Code's MCP configuration file:

**Location**: `~/.claude/mcp.json`

Add Project Cortex server:

```json
{
  "mcpServers": {
    "project-cortex": {
      "command": "cortex",
      "args": ["mcp"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

**Important**: Set `cwd` to your project directory (where `.cortex/` exists).

### 3. Restart Claude Code

Restart Claude Code to load the new MCP server configuration.

### 4. Test the Connection

Ask Claude Code questions about your codebase and documentation. Claude Code will automatically invoke `cortex_search` with appropriate filters:

**Architectural Understanding:**
```
You: "Why is authentication implemented this way?"
```
Claude Code uses: `cortex_search` with `chunk_types: ["documentation", "definitions"]` to query both design docs (ADRs, architecture guides) and code implementation.

**Design Context:**
```
You: "What are the best practices for error handling in this project?"
```
Claude Code uses: `cortex_search` with `tags: ["documentation"]` to find contribution guides, design docs, and documented patterns.

**Trade-off Discovery:**
```
You: "What were the performance vs. security trade-offs in the caching layer?"
```
Claude Code uses: `cortex_search` with `tags: ["architecture", "caching"]` to surface documented discussions and constraints.

Claude Code automatically queries Project Cortex's `cortex_search` tool with appropriate filters based on your question, providing context-aware answers grounded in both implementation and design intent.

### 5. Verify It's Working

You can check if MCP servers are connected in Claude Code:

```
/mcp status
```

You should see `project-cortex` listed as connected.

## Integration with Cursor

Cursor also supports MCP servers:

### 1. Index Your Project

```bash
cd /path/to/your/project
cortex index
```

### 2. Configure Cursor

Edit Cursor's MCP settings:

**Location**: `~/.cursor/mcp.json` (or via Cursor Settings → MCP)

```json
{
  "mcpServers": {
    "cortex": {
      "command": "cortex",
      "args": ["mcp"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

### 3. Restart Cursor

Restart Cursor to load the configuration.

## Per-Project Configuration

Instead of global configuration, you can configure MCP per-project using a local config file.

### Create `.claude/mcp.json` in Your Project

```json
{
  "mcpServers": {
    "cortex": {
      "command": "cortex",
      "args": ["mcp"]
    }
  }
}
```

No `cwd` needed - it defaults to the project directory.

### Advantages

- Team can share configuration via Git
- Different projects can have different MCP setups
- No global configuration pollution

## Advanced Configuration

### Running with Debug Logging

See what Project Cortex is doing:

```json
{
  "mcpServers": {
    "cortex": {
      "command": "cortex",
      "args": ["mcp", "--log-level", "debug"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

### Custom Chunk Directory

If you store chunks elsewhere:

```json
{
  "mcpServers": {
    "cortex": {
      "command": "cortex",
      "args": ["mcp", "--chunks-dir", "/custom/path/chunks"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

### Multiple Projects

You can configure multiple Project Cortex instances for different projects:

```json
{
  "mcpServers": {
    "cortex-api": {
      "command": "cortex",
      "args": ["mcp"],
      "cwd": "/path/to/api-project"
    },
    "cortex-frontend": {
      "command": "cortex",
      "args": ["mcp"],
      "cwd": "/path/to/frontend-project"
    }
  }
}
```

The AI assistant can search across both projects simultaneously.

## MCP Server Commands

Project Cortex MCP server provides these commands:

### `cortex mcp`

Start the MCP server (stdio mode):

```bash
cortex mcp
```

**Options:**
- `--log-level <level>`: Set log level (debug, info, warn, error)
- `--chunks-dir <path>`: Custom chunks directory
- `--config <path>`: Custom config file

### `cortex mcp status`

Check MCP server status:

```bash
cortex mcp status
```

Shows:
- Loaded chunks count
- Memory usage
- Collections available

## Available MCP Tools

Project Cortex exposes a single unified search tool via MCP:

### `cortex_search`

Semantic search across your entire project—code and documentation together.

**Request Parameters:**

```typescript
{
  "query": string,              // Required: Natural language search query
  "limit": number,              // Optional: Max results (1-100, default 15)
  "chunk_types": string[],      // Optional: Filter by chunk type
  "tags": string[]              // Optional: Filter by tags
}
```

**Chunk Types** (filter by content type):
- `"documentation"` - README files, guides, design docs, ADRs
- `"symbols"` - High-level code overview (list of functions, types, etc. in a file)
- `"definitions"` - Full function/type signatures with comments
- `"data"` - Constants, configs, enum values

*Leave `chunk_types` empty to search all types.*

**Tags** (filter by category):

Tags are automatically enriched from two sources:

1. **Context-derived** (from file metadata):
   - Language: `"typescript"`, `"go"`, `"python"`
   - Type: `"code"`, `"documentation"`, `"markdown"`
   - Path: `"docs"`, `"components"`, `"architecture"`

2. **Content-parsed** (from document content):
   - YAML frontmatter: `tags: [architecture, design-decisions]`
   - Inline hashtags: `#users #auth #knowledge`
   - Explicit tag lists in docs

**Example Tags:**
- `["code", "typescript"]` - TypeScript code
- `["documentation", "architecture"]` - Architecture docs
- `["auth", "security"]` - Auth-related content

---

### Response Structure

```typescript
{
  "results": [
    {
      "chunk": {
        "id": "unique-identifier",
        "title": "Chunk title or summary",
        "text": "Actual content text",
        "chunk_type": "documentation|symbols|definitions|data",
        "tags": ["tag1", "tag2", "tag3"],
        "metadata": {
          "source": "markdown|code",
          "file_path": "path/to/file.go",
          "start_line": 15,
          "end_line": 42,
          "language": "go",
          "package": "server",
          // For code symbols:
          "imports_count": 5,
          "types_count": 2,
          "functions_count": 8,
          // For chunked docs:
          "section_index": 1,
          "chunk_index": 0,
          "is_split_paragraph": false
        },
        "created_at": "2025-10-15T14:30:00Z",
        "updated_at": "2025-10-15T14:30:00Z"
      },
      "combined_score": 0.85  // Relevance score (0-1)
    }
  ],
  "total": 10  // Total results returned
}
```

---

### Query Examples

#### 1. Architecture Understanding (Documentation Only)

```json
{
  "query": "authentication design decisions",
  "chunk_types": ["documentation"],
  "tags": ["architecture"]
}
```

**Returns**: Design docs, ADRs explaining why auth was implemented this way.

#### 2. Code Navigation (Symbols + Definitions)

```json
{
  "query": "authentication handler implementation",
  "chunk_types": ["symbols", "definitions"]
}
```

**Returns**: High-level code overview + full function signatures for auth handlers.

#### 3. Configuration Discovery (Data Only)

```json
{
  "query": "database connection settings",
  "chunk_types": ["data"]
}
```

**Returns**: Constants, config values related to database connections.

#### 4. Unified Search (All Types)

```json
{
  "query": "how does authentication work",
  "limit": 20
}
```

**Returns**: Mixed results—design rationale from docs, code symbols, definitions, and configs. Complete picture of authentication in your project.

#### 5. Language-Specific Search

```json
{
  "query": "error handling patterns",
  "tags": ["typescript", "code"]
}
```

**Returns**: Only TypeScript code related to error handling.

#### 6. Domain-Specific Search

```json
{
  "query": "user management best practices",
  "tags": ["users", "auth"]
}
```

**Returns**: Content tagged with user/auth topics across code and docs.

---

### Why This Design?

**Single unified tool** instead of multiple separate tools because:
- Search code and docs together for complete understanding
- Filter precisely with `chunk_types` and `tags`
- Get structured results with full metadata (file paths, line numbers, etc.)
- AI assistants can decide what filters to use based on your question

## Troubleshooting

### MCP Server Won't Start

**Symptoms**: AI assistant can't connect to Project Cortex.

**Solutions**:

1. **Verify cortex is installed**:
   ```bash
   which cortex
   # Should show path to binary
   ```

2. **Check index exists**:
   ```bash
   ls .cortex/chunks/
   # Should show *.json files
   ```

3. **Test MCP server manually**:
   ```bash
   cortex mcp --log-level debug
   ```
   Should start without errors.

4. **Check MCP config path**:
   Ensure `cwd` points to correct project directory.

### No Search Results

**Symptoms**: AI assistant queries return empty results.

**Solutions**:

1. **Re-index project**:
   ```bash
   cortex index --force
   ```

2. **Check chunks were created**:
   ```bash
   cat .cortex/chunks/code-symbols.json | head
   ```

3. **Verify embeddings**:
   Check `.cortex/config.yml` embedding configuration.

4. **Lower similarity threshold**:
   ```yaml
   vector_db:
     min_score: 0.2  # Lower threshold
   ```

### High Memory Usage

**Symptoms**: MCP server uses too much RAM.

**Solutions**:

1. **Reduce chunk count**:
   ```yaml
   indexing:
     include_tests: false
     max_file_size: "200KB"
   ```

2. **Use compression**:
   ```yaml
   output:
     compress: true
   ```

3. **Limit collections**:
   Only load needed chunk files in MCP server.

### Slow Queries

**Symptoms**: AI assistant takes long to get results.

**Solutions**:

1. **Check index size**:
   ```bash
   cortex mcp status
   ```

2. **Reduce `top_k`**:
   ```yaml
   vector_db:
     top_k: 5  # Fewer results
   ```

3. **Enable incremental indexing**:
   ```yaml
   performance:
     incremental: true
   ```

## Watch Mode Integration

For active development, run Project Cortex in watch mode:

### Terminal 1: Watch and Re-index

```bash
cortex index --watch
```

This monitors your files and updates `.cortex/chunks/` automatically.

### Terminal 2: Use AI Assistant

The MCP server automatically picks up new chunks (may require restart depending on implementation).

### Alternative: Auto-Reload MCP

Some AI assistants support auto-reloading MCP servers when chunks change:

```json
{
  "mcpServers": {
    "cortex": {
      "command": "cortex",
      "args": ["mcp", "--auto-reload"],
      "cwd": "/path/to/your/project"
    }
  }
}
```

## Team Collaboration

### Commit Indexes to Git

Share indexed chunks with your team:

```bash
git add .cortex/chunks/
git commit -m "Add Project Cortex index"
```

**Benefits**:
- Team gets instant context without indexing
- Git handles incremental updates
- Shared knowledge base

**`.gitignore` Options**:

**Option 1 - Commit everything**:
```gitignore
# .gitignore
.cortex/config.yml  # Keep config local
```

**Option 2 - Ignore chunks**:
```gitignore
# .gitignore
.cortex/chunks/     # Each dev indexes locally
```

### Shared Config

Commit a template configuration:

```yaml
# .cortex/config.template.yml
embedding:
  provider: "local"
  endpoint: "http://localhost:8080/embed"

indexing:
  ignore_patterns:
    - "node_modules/**"
    - "vendor/**"
```

Team members copy to `.cortex/config.yml` and customize.

## CI/CD Integration

### Generate Index in CI

Update index on every commit:

```yaml
# .github/workflows/cortex.yml
name: Update Project Cortex Index

on: [push]

jobs:
  index:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3

      - name: Install cortex
        run: |
          curl -L https://github.com/mvp-joe/project-cortex/releases/latest/download/cortex-linux-amd64 -o cortex
          chmod +x cortex
          sudo mv cortex /usr/local/bin/

      - name: Run indexer
        run: cortex index

      - name: Commit updated index
        run: |
          git config user.name "GitHub Actions"
          git config user.email "actions@github.com"
          git add .cortex/chunks/
          git commit -m "Update Project Cortex index" || exit 0
          git push
```

### Benefits

- Always up-to-date index
- No manual re-indexing needed
- Automated for the team

## Best Practices

### 1. Index Before Committing

Add a Git pre-commit hook:

```bash
#!/bin/bash
# .git/hooks/pre-commit

if [ -d ".cortex" ]; then
  echo "Updating Project Cortex index..."
  cortex index
  git add .cortex/chunks/
fi
```

### 2. Use Watch Mode During Development

Keep index fresh while coding:

```bash
cortex index --watch &
```

### 3. Configure Per-Project

Use `.cortex/config.yml` in project root for team consistency.

### 4. Document MCP Setup

Add to your project's README:

```markdown
## AI Assistant Setup

This project uses Project Cortex for enhanced code understanding.

1. Install cortex: `task install` or `go install`
2. Index project: `cortex index`
3. Add to Claude Code MCP config (see docs/mcp-integration.md)
```

### 5. Keep Chunks in Git

For teams, commit chunks for shared context (unless very large).

## Comparison with Other Tools

### Project Cortex vs. Grep/Ripgrep

| Feature | grep/rg | Project Cortex |
|---------|---------|---------------|
| Search type | Keyword | Semantic |
| Understands code structure | No | Yes |
| Context aware | No | Yes |
| **Documentation search** | **Basic text match** | **Semantic meaning** |
| Requires exact keywords | Yes | No |
| **Example discovery** | **Manual grep** | **Automatic** |

**Real-World Documentation Example:**

You want to understand why a specific authentication approach was chosen:

**grep approach:**
```bash
grep -r "authentication" docs/
# Returns 200+ matches across many files
# Manually read through to find design rationale
# May miss relevant ADRs that use different terminology
```

**Project Cortex approach:**
```
Query: "authentication design decisions"
# Returns (ranked by semantic relevance):
# 1. docs/adrs/003-oauth-vs-jwt.md (design decision)
# 2. docs/architecture.md (section: Authentication Strategy)
# 3. docs/security-requirements.md (constraints)
```

**Key Difference**: Semantic search surfaces *why* decisions were made, not just *what* was implemented. It understands that "design rationale" relates to ADRs, trade-offs, and architectural constraints—even when those exact words aren't used.

### Project Cortex vs. LSP

| Feature | LSP | Project Cortex |
|---------|-----|---------------|
| Real-time | Yes | Batch/watch |
| Language support | Per-language | Universal |
| Semantic search | Limited | Full |
| AI integration | No | Yes |
| Cross-file understanding | Limited | Excellent |

### Project Cortex vs. AST Tools

| Feature | AST Tools | Project Cortex |
|---------|-----------|---------------|
| Exact queries | Yes | No (fuzzy) |
| Natural language | No | Yes |
| Multi-language | Hard | Easy |
| LLM integration | No | Native |

## Further Reading

- [MCP Protocol Specification](https://modelcontextprotocol.io/)
- [Configuration Guide](configuration.md)
- [Architecture Overview](architecture.md)
- [Language Support](languages.md)

## Getting Help

If you encounter issues:

1. Check MCP server logs: `cortex mcp --log-level debug`
2. Verify index: `ls -lh .cortex/chunks/`
3. Test queries manually: `cortex search "your query"`
4. Open an issue on GitHub with logs
