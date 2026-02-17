# Canopy Graph Integration

## Summary

Replace Project Cortex's custom Go-only code graph implementation (`internal/graph/`) with **canopy** (`github.com/jward/canopy`), used strictly as a Go library. Canopy provides tree-sitter-based multi-language code analysis with scope-aware reference resolution, call graph edges, and interface implementations -- capabilities that the current custom extractor achieves only partially and only for Go. This integration treats canopy's database as a black box: all interaction goes through canopy's public Go API. The result is 10-language support (Go, TypeScript, JavaScript, Python, Rust, C, C++, Java, PHP, Ruby), proper cross-file reference resolution, and new MCP operations (references, implementations, definition, symbol search, project/package summaries).

## Goals

- Replace the custom Go AST extractor and custom SQL graph queries with canopy's public API
- Support all 10 languages canopy handles (vs current Go-only)
- Add new `cortex_graph` MCP operations: `references`, `definition`, `symbols`, `search`, `summary`, `package_summary`
- Expose existing internal operations via MCP that are currently defined in `searcher_types.go` but not in the MCP tool enum: `implementations`, `impact`, `path`
- Maintain backward compatibility for existing MCP-exposed operations: `callers`, `callees`, `dependencies`, `dependents`, `type_usages`
- Integrate canopy indexing into the existing daemon lifecycle (file change -> canopy.IndexDirectory + Resolve)
- Never read from or write to canopy's SQLite database directly

## Non-Goals

- Removing old graph code (`internal/graph/`) -- that is a follow-up cleanup task
- Modifying canopy's internals or database schema
- Replacing cortex's own SQLite storage for chunks, embeddings, or FTS
- Changing the `cortex_search`, `cortex_exact`, or `cortex_files` MCP tools
- Building a separate canopy daemon process -- canopy runs in-process as a library

## Current Status

Planning

## Key Files

- [implementation.md](./implementation.md) -- Phased plan with checkboxes
- [interface.md](./interface.md) -- Type definitions for CanopyProvider, CanopySearcher, updated MCP schema
- [tests.md](./tests.md) -- Test specifications
- [decisions.md](./decisions.md) -- Key architectural decisions
