# Duplicate Node ID Warnings During Indexing

**Date:** 2025-10-28
**Severity:** Low (logging noise)
**Component:** Graph Builder
**Status:** Open

---

## Observation

When running `cortex index`, the log shows many duplicate node ID warnings.

### Example Output

```
2025/10/28 22:06:26 Building code graph...
2025/10/28 22:06:26 [WARN] duplicate node ID 'main.main' found in cmd/cortex/main.go and cmd/cortex-embed/main.go
2025/10/28 22:06:26 [WARN] duplicate node ID 'internal/cli' found in internal/cli/clean.go and internal/cli/completion.go
2025/10/28 22:06:26 [WARN] duplicate node ID 'cli.init' found in internal/cli/clean.go and internal/cli/completion.go
2025/10/28 22:06:26 [WARN] duplicate node ID 'internal/cli' found in internal/cli/clean.go and internal/cli/index.go
2025/10/28 22:06:26 [WARN] duplicate node ID 'cli.init' found in internal/cli/clean.go and internal/cli/index.go
2025/10/28 22:06:26 [WARN] duplicate node ID 'internal/cli' found in internal/cli/clean.go and internal/cli/mcp.go
2025/10/28 22:06:26 [WARN] duplicate node ID 'cli.init' found in internal/cli/clean.go and internal/cli/mcp.go
... (100+ similar warnings)
2025/10/28 22:06:26 Resolving interface embeddings and inferring implementations...
2025/10/28 22:06:26 Found 23 interface implementations
2025/10/28 22:06:26 ✓ Graph saved: 862 nodes, 7959 edges
```

---

## Pattern

### Package Nodes
- `internal/cli` appears duplicated for every file in that package
- `internal/graph` appears duplicated for every file in that package
- `internal/config` appears duplicated for every file in that package
- `internal/embed` appears duplicated for every file in that package
- `internal/indexer` appears duplicated for every file in that package
- `internal/mcp` appears duplicated for every file in that package
- And so on for all packages

### Init Functions
- `cli.init` appears duplicated across multiple files in the package
- Same pattern for other packages

### Main Functions
- `main.main` appears in multiple locations:
  - `cmd/cortex/main.go`
  - `cmd/cortex-embed/main.go`
  - `internal/embed/server/generate/main.go`
  - `internal/embed/testdata/graceful_exit.go`
  - `internal/embed/testdata/ignore_sigterm.go`

---

## Frequency

- 114 files indexed
- 100+ duplicate node ID warnings
- Final result: 862 nodes, 7959 edges

---

## Code Locations

### Warning Logged From

**File:** `internal/graph/builder.go`

**Lines 67-68** (in BuildFull):
```go
log.Printf("[WARN] duplicate node ID '%s' found in %s and %s",
    node.ID, existing.File, node.File)
```

**Lines 181-182** (in BuildIncremental):
```go
log.Printf("[WARN] duplicate node ID '%s' found in %s and %s",
    node.ID, existing.File, node.File)
```

### Deduplication Logic

**File:** `internal/graph/builder.go:62-77`

The builder maintains a `nodeMap` to deduplicate nodes by ID. When a duplicate is found:
- Logs the warning
- Keeps the first encountered node, OR
- Prefers non-test file over test file

---

## Impact

- No functional impact (deduplication works correctly)
- Creates log noise (100+ warnings on every index operation)
- May obscure real issues in the logs
