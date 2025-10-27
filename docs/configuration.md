# Configuration Guide

Project Cortex works out of the box with sensible defaults. Most users won't need to configure anything. This guide covers the minimal configuration options for customizing behavior.

## Configuration File Location

Project Cortex looks for `.cortex/config.yml` in your project directory. If it doesn't exist, built-in defaults are used.

## Minimal Configuration

Most projects don't need a config file. Project Cortex will:
- Auto-detect all supported languages
- Use reasonable ignore patterns (node_modules, vendor, .git, etc.)
- Use local embeddings if available, otherwise prompt for setup

## Basic Configuration Example

If you need to customize, create `.cortex/config.yml`:

```yaml
# Embedding configuration (optional)
embedding:
  provider: "local"  # or "openai"
  model: "BAAI/bge-small-en-v1.5"
  dimensions: 384
  endpoint: "http://localhost:8121/embed"

# Additional files/folders to ignore (optional)
indexing:
  ignore_patterns:
    - "legacy/**"
    - "experimental/**"
```

That's it for most projects.

## Embedding Providers

### Local (Privacy-First, Default)

For sensitive codebases, use local embeddings:

```yaml
embedding:
  provider: "local"
  model: "BAAI/bge-small-en-v1.5"
  dimensions: 384  # Vector size for BAAI/bge-small-en-v1.5
  endpoint: "http://localhost:8121/embed"
```

Start the local embedding server:
```bash
cortex embed-server
```

### OpenAI (Higher Quality)

For better search quality:

```yaml
embedding:
  provider: "openai"
  model: "text-embedding-3-small"
  dimensions: 1536  # Vector size for text-embedding-3-small
  api_key: "${OPENAI_API_KEY}"  # Use env var
```

Set your API key:
```bash
export OPENAI_API_KEY="sk-..."
```

**Common embedding dimensions:**
- `BAAI/bge-small-en-v1.5`: 384 (default for cortex-embed)
- `text-embedding-ada-002`: 1536
- `text-embedding-3-small`: 1536
- `text-embedding-3-large`: 3072

**Important**: All embeddings in your index must use the same dimensions. If you change models, re-run `cortex index` to regenerate embeddings.

## Common Customizations

### Ignore Patterns

Add patterns to skip certain files:

```yaml
indexing:
  ignore_patterns:
    - "legacy/**"
    - "experimental/**"
    - "*.generated.ts"
    - "**/fixtures/**"
```

**Default ignore patterns** (always applied):
- `node_modules/**`, `vendor/**`, `.git/**`
- `dist/**`, `build/**`, `.next/**`
- `*.min.js`, `*.bundle.js`

### Documentation Patterns

Customize which files are treated as documentation:

```yaml
documentation:
  patterns:
    - "**/*.md"
    - "**/*.rst"
    - "docs/**/*.txt"
```

Default: `**/*.md` and `**/*.rst`

### Exclude Test Files

```yaml
indexing:
  ignore_patterns:
    - "**/*.test.*"
    - "**/*_test.*"
    - "**/tests/**"
```

## Environment Variables

Use environment variables for sensitive values:

```bash
export OPENAI_API_KEY="sk-..."
export CORTEX_EMBEDDING_ENDPOINT="http://localhost:8080"
```

Reference in config:
```yaml
embedding:
  api_key: "${OPENAI_API_KEY}"
```

## Complete Reference

Here's a complete config showing all available options:

```yaml
# Embedding configuration
embedding:
  provider: "local"                 # "local" or "openai"
  model: "BAAI/bge-small-en-v1.5"   # Model name
  dimensions: 384                   # Vector size (must match model)
  endpoint: "http://localhost:8121/embed"  # For local provider
  api_key: ""                       # For cloud providers

# Indexing options
indexing:
  ignore_patterns:            # Files to skip
    - "node_modules/**"
    - "vendor/**"
  max_chunk_size: 1000        # Token limit per chunk
  chunk_overlap: 100          # Overlap between chunks

# Documentation options
documentation:
  patterns:                   # Files to treat as docs
    - "**/*.md"
    - "**/*.rst"
  semantic_chunking: true     # Chunk by headers vs fixed size

# Output options
output:
  chunks_dir: ".cortex/chunks"  # Where to store index
  pretty_json: true             # Human-readable JSON
```

## Example Configurations

### Monorepo

```yaml
indexing:
  ignore_patterns:
    - "node_modules/**"
    - "*/node_modules/**"
    - "vendor/**"
    - "*/vendor/**"
```

### Documentation-Heavy Project

```yaml
documentation:
  patterns:
    - "**/*.md"
    - "**/*.rst"
    - "docs/**/*.txt"
    - "**/*.adoc"
```

### Using OpenAI

```yaml
embedding:
  provider: "openai"
  model: "text-embedding-3-small"
  dimensions: 1536
  api_key: "${OPENAI_API_KEY}"
```

## Related Documentation

- [Architecture](architecture.md)
- [MCP Integration](mcp-integration.md)
- [Language Support](languages.md)
