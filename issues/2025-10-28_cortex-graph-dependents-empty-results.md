# cortex_graph Tool Returns Empty Results for Type Dependents Query

**Date:** 2025-10-28
**Severity:** High (feature not working as expected)
**Component:** MCP Server → cortex_graph Tool → Graph Query
**Status:** Investigating

---

## Summary

The `cortex_graph` MCP tool with `operation: "dependents"` returns empty results when querying for types that should have known dependents in the codebase.

## Query Details

**Tool:** `mcp__project-cortex__cortex_graph`

**Parameters:**
```json
{
  "operation": "dependents",
  "target": "indexer.Chunk",
  "depth": 2,
  "include_context": true
}
```

**Expected Behavior:**
Should return packages/files that import and use the `indexer.Chunk` type, such as:
- `internal/indexer/impl.go` - Uses `[]Chunk`, `map[ChunkType][]Chunk`
- `internal/indexer/writer.go` - Uses `Chunk` in `ChunkFile` struct
- `internal/indexer/types.go` - Defines `ChunkFile` with `[]Chunk`
- `internal/mcp/loader.go` - Converts to `ContextChunk`
- Various test files

**Actual Result:**
```json
{
  "operation": "dependents",
  "target": "indexer.Chunk",
  "results": [],
  "total_found": 0,
  "total_returned": 0,
  "truncated": false,
  "truncated_at_depth": -1,
  "metadata": {
    "took_ms": 0,
    "source": "graph"
  }
}
```

## Evidence of Expected Dependents

Using `grep`, we can verify these files actually use `Chunk`:

```bash
# Files using []Chunk or map[...Chunk]:
internal/indexer/impl.go:474:    func (idx *indexer) loadAllChunks() (map[ChunkType][]Chunk, error)
internal/indexer/impl.go:567:    func (idx *indexer) processCodeFiles(ctx context.Context, files []string) (symbols, definitions, data []Chunk, err error)
internal/indexer/impl.go:716:    func (idx *indexer) processDocFiles(ctx context.Context, files []string) ([]Chunk, error)
internal/indexer/impl.go:786:    func (idx *indexer) embedChunks(ctx context.Context, chunks []Chunk) error
internal/indexer/impl.go:808:    func (idx *indexer) writeChunkFiles(symbols, definitions, data, docs []Chunk) error
internal/indexer/types.go:35:    Chunks   []Chunk           `json:"chunks"`
internal/indexer/writer.go:46:   func (w *AtomicWriter) WriteChunkFile(filename string, chunkFile *ChunkFile) error
```

## Possible Root Causes

### 1. Graph Not Built or Corrupted
- Graph file may not exist at `.cortex/chunks/graph/graph.json`
- Graph may be empty due to indexing issues (see related issue: `2025-10-28_graph-builder-nil-allfiles.md`)
- Graph file may exist but be unreadable by MCP server

### 2. Target Format Issue
- Query used `indexer.Chunk` but graph may store it as:
  - `github.com/mvp-joe/project-cortex/internal/indexer.Chunk` (fully qualified)
  - `Chunk` (unqualified)
  - Different node ID format

### 3. Graph Doesn't Track Type Dependencies
- Graph builder may only track function calls, not type usage
- Type dependencies (`[]Chunk`, `*Chunk` in signatures) may not be indexed as edges

### 4. MCP Tool Query Logic Bug
- `dependents` operation may have incorrect graph traversal
- Filter/matching logic may be too strict

### 5. Graph Not Loaded by MCP Server
- MCP server may not be loading graph data on startup
- Graph may not be hot-reloaded when chunks change

## Impact

- **Graph queries unusable** - Cannot find type dependents, limiting refactoring assistance
- **Feature incomplete** - The `cortex_graph` tool is advertised but doesn't work for type queries
- **User confusion** - Tool returns success (HTTP 200) but with empty results, unclear if bug or no matches
- **Refactoring risk** - Cannot identify all files affected by type changes

## Questions to Investigate

1. **Does the graph file exist?**
   ```bash
   ls -lh .cortex/chunks/graph/graph.json
   ```

2. **What node IDs are in the graph?**
   ```bash
   jq '.nodes[].id' .cortex/chunks/graph/graph.json | grep -i chunk
   ```

3. **What edge types exist?**
   ```bash
   jq '.edges[].type' .cortex/chunks/graph/graph.json | sort -u
   ```

4. **Does graph builder track type usage?**
   - Check `internal/graph/builder.go` for type dependency extraction
   - Look for edges with type: "uses_type", "imports_type", etc.

5. **Is MCP server loading the graph?**
   - Check `internal/mcp/graph_tools.go` (or similar) for graph initialization
   - Verify graph is loaded in server startup sequence

6. **What does the tool registration look like?**
   - Find where `cortex_graph` tool is registered
   - Check query implementation for `dependents` operation

## Next Steps

### Immediate (Debugging)
1. Verify graph file exists and has content
2. Inspect graph node IDs to understand format
3. Check if `Chunk` type appears anywhere in graph
4. Test simpler query (e.g., function callers) to verify tool works

### Investigation (Code Analysis)
1. Read `internal/graph/builder.go` to understand what relationships are tracked
2. Read MCP graph tool implementation to understand query logic
3. Check if type dependencies are explicitly excluded or unimplemented

### Potential Fixes
1. If graph is empty → Fix indexing (see related issue)
2. If type deps not tracked → Enhance graph builder to add type usage edges
3. If query format wrong → Document correct target format
4. If tool logic broken → Fix graph traversal in MCP handler

## Related Issues

- `2025-10-28_graph-builder-nil-allfiles.md` - Graph may be empty due to indexing bug
- `2025-10-28_duplicate-node-id-warnings.md` - Graph data quality issues

## Test Case to Add

```go
func TestMCPGraphTool_FindTypesDependents(t *testing.T) {
    // Setup: Index a codebase with known type usage
    // File A: defines type Foo
    // File B: uses []Foo in function signature
    // File C: embeds Foo in struct

    // Execute: Query dependents of "packageA.Foo"
    result := queryGraph("dependents", "packageA.Foo", depth: 1)

    // Validate: Should return File B and File C
    require.Greater(t, result.TotalFound, 0)
    assert.Contains(t, result.Results, "packageB.Function")
    assert.Contains(t, result.Results, "packageC.Struct")
}
```

## Resolution Checklist

- [ ] Graph file existence verified
- [ ] Node ID format documented
- [ ] Type dependency tracking assessed
- [ ] Root cause identified
- [ ] Fix implemented
- [ ] Test case added
- [ ] Documentation updated with correct query format

## Workaround

Until fixed, use semantic search instead:
```json
{
  "tool": "mcp__project-cortex__cortex_search",
  "query": "files that use Chunk type from indexer package",
  "chunk_types": ["symbols", "definitions"]
}
```

Or use grep/code search tools directly.

## References

- **Query location:** User request: "show me all the places that import Chunk"
- **Tool used:** `mcp__project-cortex__cortex_graph`
- **Expected source:** `.cortex/chunks/graph/graph.json`
- **Related specs:** `specs/mcp-server.md`, graph query documentation