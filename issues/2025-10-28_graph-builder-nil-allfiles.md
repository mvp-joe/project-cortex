# Graph Builder Receives Nil for allFiles Parameter

**Date:** 2025-10-28
**Severity:** High (silent data loss)
**Component:** Indexer → Graph Builder Integration
**Status:** Fixed (not yet committed)

---

## Summary

The indexer's `Index()` method was calling `buildAndSaveGraph()` with `nil` for the `allFiles` parameter, causing the graph builder to process zero files and produce an empty graph with 0 nodes and 0 edges, despite successfully indexing 114 Go files.

## Symptom

When running `cortex index`, the output showed:

```
Indexing files 100% |████████████████████████████████████████| (114/114, 3072 files/s)
...
Building code graph...
Resolving interface embeddings and inferring implementations...
Found 0 interface implementations
✓ Graph saved: 0 nodes, 0 edges
```

The graph file was never created at `.cortex/graph/graph.json`, and all graph-based queries failed.

## Root Cause

**File:** `internal/indexer/impl.go:208`

**Buggy Code:**
```go
// Build and save graph
log.Println("Building code graph...")
if err := idx.buildAndSaveGraph(ctx, codeFiles, nil, nil); err != nil {
    //                                                ^^^^ BUG: should be codeFiles
    log.Printf("Warning: failed to build graph: %v\n", err)
    // Don't fail indexing if graph fails - it's supplementary
}
```

**Correct Code:**
```go
// Build and save graph
log.Println("Building code graph...")
if err := idx.buildAndSaveGraph(ctx, codeFiles, nil, codeFiles); err != nil {
    log.Printf("Warning: failed to build graph: %v\n", err)
    // Don't fail indexing if graph fails - it's supplementary
}
```

### What Happened

The `buildAndSaveGraph()` function signature is:
```go
func (idx *indexer) buildAndSaveGraph(
    ctx context.Context,
    changedFiles []string,   // Files that changed (for incremental)
    deletedFiles []string,   // Files deleted (nil = full build)
    allFiles []string        // All files to process (for full build)
) error
```

When `deletedFiles == nil` (full build), it calls:
```go
graphData, err = builder.BuildFull(ctx, allFiles)  // allFiles was nil!
```

Since `allFiles` was `nil`, `BuildFull` processed 0 files and returned an empty but "valid" graph.

## Impact

- **Graph queries returned no results** - All MCP graph operations failed
- **Silent failure** - Error was logged as warning, indexing continued
- **No validation** - Empty graph is technically valid, no error raised
- **User confusion** - Chunks were created successfully, but graph was empty

## The Fix

**Change:** `internal/indexer/impl.go:208`

```diff
- if err := idx.buildAndSaveGraph(ctx, codeFiles, nil, nil); err != nil {
+ if err := idx.buildAndSaveGraph(ctx, codeFiles, nil, codeFiles); err != nil {
```

**Explanation:** The third parameter (`allFiles`) must be `codeFiles` for full builds so the graph builder knows which files to process.

## Why Tests Didn't Catch This

### 1. No Integration Test for Index() → Graph Output

**What exists:**
- `internal/graph/builder_test.go` - Tests builder in isolation
- `internal/indexer/incremental_test.go` - Tests indexer but only validates chunks

**What's missing:**
- No test calling `idx.Index()` and validating `.cortex/graph/graph.json` exists
- No assertion: "After indexing Go files, graph should have N > 0 nodes"

### 2. Silent Failure by Design

```go
if err := idx.buildAndSaveGraph(...); err != nil {
    log.Printf("Warning: failed to build graph: %v\n", err)
    // Don't fail indexing if graph fails - it's supplementary
}
```

Graph errors don't fail indexing. Passing `nil` to `BuildFull()` isn't an error—it just processes 0 files.

### 3. Builder Tested in Isolation

Tests call `builder.BuildFull(files)` directly with correct parameters. The bug was in the **indexer's call** to `buildAndSaveGraph`, not in the builder itself.

### 4. No Output Validation

Tests verify chunks are created but don't check:
- Graph file exists at `.cortex/graph/graph.json`
- Graph contains expected nodes/edges
- Graph is loadable by storage

## Suggested Test Improvements

### Integration Test (Critical)

```go
func TestIndexer_Index_CreatesGraphWithNodes(t *testing.T) {
    // Setup: Create indexer with real Go files
    tmpDir := t.TempDir()
    writeTestGoFile(t, filepath.Join(tmpDir, "main.go"), `
        package main
        func main() {}
    `)

    cfg := createTestConfig(tmpDir)
    idx := createTestIndexer(cfg)

    // Execute: Run full indexing
    _, err := idx.Index(context.Background())
    require.NoError(t, err)

    // Validate: Check graph file exists and has content
    graphPath := filepath.Join(tmpDir, ".cortex/graph/graph.json")
    require.FileExists(t, graphPath)

    // Load and validate graph
    storage, err := graph.NewStorage(filepath.Dir(graphPath))
    require.NoError(t, err)

    graphData, err := storage.Load()
    require.NoError(t, err)

    // CRITICAL ASSERTION - would have caught the bug:
    assert.Greater(t, len(graphData.Nodes), 0,
        "graph should contain nodes after indexing Go files")
    assert.Contains(t, graphData.Nodes,
        graph.Node{ID: "main.main", Kind: graph.NodeFunction},
        "graph should contain main.main function")
}
```

### Parameter Validation

Add validation in `BuildFull`:

```go
func (b *builder) BuildFull(ctx context.Context, files []string) (*GraphData, error) {
    if len(files) == 0 {
        return nil, fmt.Errorf("BuildFull called with empty file list")
    }
    // ... rest of implementation
}
```

This turns silent bugs into caught errors.

### Unit Test for buildAndSaveGraph

```go
func TestIndexer_buildAndSaveGraph_RequiresAllFiles(t *testing.T) {
    idx := createTestIndexer(t.TempDir())
    files := []string{"testdata/sample.go"}

    // Test correct usage
    err := idx.buildAndSaveGraph(ctx, files, nil, files)
    require.NoError(t, err)

    graphData, _ := loadGraph(idx.config.OutputDir)
    assert.Greater(t, len(graphData.Nodes), 0)
}
```

## Test Coverage Gap Summary

| Layer | What's Tested | Gap | Severity |
|-------|---------------|-----|----------|
| Unit: Builder | BuildFull with files | ✓ Good | - |
| Unit: buildAndSaveGraph | Parameter handling | ✗ Not tested | HIGH |
| Integration: Index() | Chunks created | ✓ Good | - |
| Integration: Index() | **Graph file with nodes** | ✗ **Missing** | **CRITICAL** |
| Integration: Graph file | Exists and loadable | ✗ Not tested | HIGH |
| Validation | allFiles != nil/empty | ✗ Not validated | MEDIUM |

## Resolution

- [x] Bug identified (2025-10-28)
- [x] Root cause analyzed
- [x] Fix implemented in code
- [ ] Fix tested (pending rebuild and re-run of indexer)
- [ ] Integration test added
- [ ] Commit created

## References

- **Bug location:** `internal/indexer/impl.go:208`
- **Affected methods:** `Index()`, `buildAndSaveGraph()`
- **Related code:** `internal/graph/builder.go:34-60` (BuildFull)
- **Test files:** `internal/graph/builder_test.go`, `internal/indexer/incremental_test.go`
