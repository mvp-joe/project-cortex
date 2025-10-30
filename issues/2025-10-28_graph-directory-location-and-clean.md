# Graph Directory Location and Clean Command Issues

**Date:** 2025-10-28
**Severity:** Medium
**Component:** Indexer, CLI (clean command)
**Status:** Open

---

## Observation 1: Graph Directory Location Mismatch

### Current Behavior

The graph file is created at:
```
.cortex/chunks/graph/code-graph.json
```

### Specification Says

According to `specs/chunk-manager.md` line 36 and `specs/cortex-exact.md` line 72:
```
.cortex/graph/code-graph.json
```

The graph directory should be a **sibling** of the chunks directory, not a child.

### Code Location

**File:** `internal/indexer/impl.go:887`

```go
graphDir := filepath.Join(idx.config.OutputDir, "graph")
```

Where `idx.config.OutputDir` is `.cortex/chunks`.

---

## Observation 2: Clean Command Doesn't Remove Graph

### Current Behavior

Running `cortex clean`:
- Removes: `*.json` files from `.cortex/chunks/`
- Does NOT remove: `.cortex/chunks/graph/` directory (current location)
- Does NOT remove: `.cortex/graph/` directory (spec location)

### Code Location

**File:** `internal/cli/clean.go:48`

```go
files, err := filepath.Glob(filepath.Join(chunksDir, "*.json"))
```

Only globs JSON files in the chunks root directory, missing the graph subdirectory.

### Result

After running `cortex clean`, graph files persist at their current location.

---

## Directory Structure

### Current (Actual)
```
.cortex/
├── chunks/
│   ├── code-data.json
│   ├── code-definitions.json
│   ├── code-symbols.json
│   ├── doc-chunks.json
│   ├── generator-output.json
│   └── graph/                    ← Child of chunks
│       ├── code-graph.json
│       └── .tmp/
└── config.yml
```

### Spec (Expected)
```
.cortex/
├── chunks/
│   ├── code-data.json
│   ├── code-definitions.json
│   ├── code-symbols.json
│   ├── doc-chunks.json
│   └── generator-output.json
├── graph/                         ← Sibling of chunks
│   ├── code-graph.json
│   └── .tmp/
└── config.yml
```

---

## Related Files

### Graph Storage
- `internal/graph/storage.go` - Creates graph directory and handles atomic writes

### Clean Command
- `internal/cli/clean.go:44-98` - Clean implementation

### Other Operations to Check
- Index command (full and incremental)
- Watch mode
- MCP server graph loading
- Any configuration that specifies graph path

---

## References

- **Spec:** `specs/chunk-manager.md:36`
- **Spec:** `specs/cortex-exact.md:72`
- **Code:** `internal/indexer/impl.go:887`
- **Code:** `internal/cli/clean.go:48`
