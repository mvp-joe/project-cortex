# Project Cortex - Refactoring Roadmap
## Pre-Daemon Cleanup → Daemon Implementation → Post-Daemon Polish

**Created**: 2025-11-03
**Goal**: Clean technical debt BEFORE daemon implementation to avoid dragging legacy code forward
**Total Estimated Time**: 6-8 weeks

---

## Strategy Overview

### Why Clean Up First?

The daemon spec (2025-10-29_auto-daemon.md) represents a **fundamental architecture shift**:
- Single long-running process per project
- SQLite-only storage (no JSON)
- Source file watching (not chunk files)
- HTTP/SSE transport with session coordination

**If we implement daemon with current code**:
- ❌ Drag 1500+ LOC of JSON storage code into daemon
- ❌ Maintain dual storage backends in long-running process
- ❌ Keep file watcher monitoring wrong directory
- ❌ Carry god object complexity into daemon lifecycle
- ❌ Miss opportunity for clean architecture

**If we clean up first**:
- ✅ Simple, focused daemon implementation
- ✅ Single storage path (SQLite only)
- ✅ Proper file watching from day one
- ✅ Clean separation of concerns
- ✅ Easier to test and debug

### Three-Phase Approach

```
┌─────────────────────────────────────────────────────────────────┐
│ Phase 1: Pre-Daemon Cleanup (3-4 weeks)                         │
│ Remove blockers, simplify architecture, fix critical issues     │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ Phase 2: Daemon Implementation (2-3 weeks)                      │
│ Implement daemon spec on clean foundation                        │
└─────────────────────────────────────────────────────────────────┘
                              ↓
┌─────────────────────────────────────────────────────────────────┐
│ Phase 3: Post-Daemon Polish (1 week)                           │
│ Fix remaining issues that don't affect daemon                   │
└─────────────────────────────────────────────────────────────────┘
```

---

## Phase 1: Pre-Daemon Cleanup (3-4 weeks)

**Goal**: Remove architectural blockers and simplify codebase before daemon work

### Week 1: Storage Cleanup (CRITICAL PATH)

#### Day 1-2: Deprecate JSON Storage
**Priority**: P0 - Blocks daemon implementation
**Issue**: HIGH-3

**Tasks**:
- [ ] Add deprecation warning for `storage_backend: json` in config
- [ ] Update CLI to show migration message
- [ ] Fail fast on JSON storage instantiation
- [ ] Update all documentation to show SQLite only
- [ ] Add `cortex migrate json-to-sqlite` stub command (prints "coming soon")

**Code Changes**:
```go
// internal/indexer/storage_interface.go
func NewJSONStorage(outputDir string) (Storage, error) {
    return nil, fmt.Errorf("JSON storage is deprecated. Use SQLite (default) or run 'cortex migrate json-to-sqlite'")
}

// internal/config/indexer.go
func (c *IndexerConfig) Validate() error {
    if c.StorageBackend == "json" {
        return fmt.Errorf("JSON storage deprecated. Remove 'storage_backend: json' from config")
    }
    // ...
}
```

**Outcome**: No new code uses JSON storage, clear path forward

---

#### Day 3-4: Remove Storage Backend Switching
**Priority**: P0 - Simplifies indexer
**Issue**: HIGH-1, HIGH-4 (blocked)

**Tasks**:
- [ ] Remove all type assertions: `if jsonStorage, ok := idx.storage.(*JSONStorage)`
- [ ] Remove storage backend switch statements (8+ locations)
- [ ] Hard-code SQLite in all indexer constructors
- [ ] Remove `storage_backend` config option
- [ ] Remove `output_dir` config (JSON-specific)
- [ ] Update tests to always use SQLite

**Files to Modify**:
```
internal/indexer/impl.go:57,82-96,149-163,216-228,448-460,591-602,652-676,681-692,698-703
internal/indexer/eviction_test.go
internal/indexer/incremental_test.go
internal/config/indexer.go
```

**Outcome**: Single storage path, eliminates ~200 LOC of switching logic

---

#### Day 5: Remove JSONStorage Implementation
**Priority**: P0 - Dead code removal
**Issue**: HIGH-3

**Tasks**:
- [ ] Delete `JSONStorage` struct and methods
- [ ] Delete `AtomicWriter` (JSON-specific)
- [ ] Delete `writeMetadata()` helper
- [ ] Update Storage interface (remove JSON-specific methods if any)
- [ ] Remove JSON-related test files
- [ ] Update CLAUDE.md to remove JSON references

**Files to Delete/Modify**:
```
internal/indexer/storage_interface.go:33-129 (delete JSONStorage)
internal/indexer/types.go (remove GeneratorMetadata if JSON-only)
```

**Outcome**: ~500 LOC removed, Storage interface simplified

---

### Week 2: File Watcher Cleanup

#### Day 1-3: Remove Chunk File Watcher
**Priority**: P0 - Daemon will watch source files
**Issue**: HIGH-3

**Tasks**:
- [ ] Identify all chunk file watcher usage in MCP server
- [ ] Remove file watcher from `internal/mcp/watcher.go`
- [ ] Remove watcher initialization in `internal/mcp/server.go`
- [ ] Remove debounce logic (was for JSON writes)
- [ ] Keep watcher interface (will be used for source files in daemon)
- [ ] Update tests to not expect file watching

**Files to Modify**:
```
internal/mcp/watcher.go (remove or stub out)
internal/mcp/server.go (remove watcher setup)
internal/mcp/watcher_test.go (update expectations)
internal/mcp/watcher_integration_test.go (remove or adapt)
```

**Outcome**: MCP server no longer watches `.cortex/chunks/`, ready for source file watching

---

#### Day 4-5: Remove JSON Chunk Loading
**Priority**: P0 - MCP will query SQLite
**Issue**: HIGH-3

**Tasks**:
- [ ] Remove `loadFromJSONFiles()` from `internal/mcp/loader.go`
- [ ] Remove `loadJSONFile()` helper
- [ ] Keep `loadFromSQLite()` only
- [ ] Update ChunkManager to only support SQLite
- [ ] Update all loader tests
- [ ] Remove `.cortex/chunks/` references from docs

**Files to Modify**:
```
internal/mcp/loader.go:27-107 (delete JSON functions)
internal/mcp/loader_test.go (SQLite only)
internal/mcp/chunk_manager.go (simplify)
```

**Outcome**: ~300 LOC removed, single loading path

---

### Week 3: Graph Library Fixes + Deduplication

#### Day 1-2: Use Graph Library's Traversal Methods
**Priority**: P1 - Correctness issue
**Issue**: HIGH-2

**Tasks**:
- [ ] Refactor `queryCallers()` to use `graph.DFS()`
- [ ] Refactor `queryCallees()` to use `graph.DFS()`
- [ ] Add cycle detection using `graph.CreatesCycle()`
- [ ] Remove manual recursive traversal code
- [ ] Add tests for cycle detection
- [ ] Benchmark performance (library vs manual)

**Code Changes**:
```go
// internal/graph/searcher.go:384-437
func (s *searcher) queryCallers(target string, depth int) ([]resultWithDepth, error) {
    results := []resultWithDepth{}

    // Use library's DFS with depth tracking
    err := s.graph.DFS(target, func(nodeID string, currentDepth int) bool {
        if currentDepth > depth {
            return true // Stop traversal
        }

        // Add callers from reverse index
        for _, caller := range s.callers[nodeID] {
            results = append(results, resultWithDepth{id: caller, depth: currentDepth})
        }

        return false // Continue
    })

    return results, err
}

// Similar for queryCallees
```

**Outcome**: ~100 LOC removed, proper cycle detection, leverages library

---

#### Day 3: Extract Helper Functions (Graph)
**Priority**: P1 - Code quality
**Issue**: HIGH-5, HIGH-6

**Tasks**:
- [ ] Extract `deduplicateNodes()` helper (used 2x)
- [ ] Extract `resolveInterfaceEmbeddings()` helper
- [ ] Extract context injection helper (used 5x)
- [ ] Extract `getNodesByKind()` helper
- [ ] Update all call sites

**Outcome**: ~150 LOC deduplication, cleaner graph module

---

#### Day 4-5: Extract Helper Functions (MCP + Storage)
**Priority**: P1 - Code quality
**Issue**: HIGH-3 (MCP), CRITICAL-2 (Storage)

**Tasks**:
- [ ] Extract `chunkToChromemMetadata()` helper (used 4x)
- [ ] Extract `chunkToChromemDocument()` helper (used 4x)
- [ ] Create MCP argument parsing utilities
- [ ] Extract embedding serialization to `internal/storage/encoding.go`
- [ ] Update all call sites

**Files to Create**:
```
internal/mcp/helpers.go (metadata, document helpers)
internal/mcp/args.go (argument parsing)
internal/storage/encoding.go (serialization)
```

**Outcome**: ~400 LOC deduplication across 3 modules

---

### Week 4: Security + Critical Fixes

#### Day 1-2: Fix SQL Injection Risk
**Priority**: P0 - Security
**Issue**: CRITICAL-3

**Tasks**:
- [ ] Add runtime assertions for field name validation
- [ ] Use Squirrel's native aggregation support
- [ ] Add integration test for SQL injection attempts
- [ ] Document that validation MUST run before translation
- [ ] Security audit of all SQL generation code

**Outcome**: Security vulnerability closed

---

#### Day 3-4: Extract File Metadata Package
**Priority**: P1 - Reduces indexer complexity
**Issue**: MEDIUM-2

**Tasks**:
- [ ] Create `internal/indexer/metadata/` package
- [ ] Move `collectFileMetadata()` (51 lines)
- [ ] Move `calculateChecksum()` (10 lines)
- [ ] Move `countLines()` (29 lines)
- [ ] Move `isCommentLine()` (12 lines)
- [ ] Move `isTestFile()` (11 lines)
- [ ] Move `extractModulePath()` (7 lines)
- [ ] Update impl.go imports

**Outcome**: impl.go reduced by ~140 lines, cleaner abstraction

---

#### Day 5: Performance Fixes
**Priority**: P2 - Quick wins
**Issue**: MEDIUM-4, MEDIUM-5

**Tasks**:
- [ ] Replace O(n²) edge filtering with map-based O(1) lookups
- [ ] Replace bubble sort with `sort.Slice`
- [ ] Add benchmarks to validate improvements
- [ ] Document performance characteristics

**Outcome**: Better performance, cleaner code

---

### Week 3-4 Summary: Pre-Daemon Checklist

**Must Complete Before Daemon Work**:
- ✅ JSON storage completely removed (~1500 LOC)
- ✅ Single storage path (SQLite only)
- ✅ Chunk file watcher removed
- ✅ Graph library used properly
- ✅ SQL injection fixed
- ✅ Major deduplication done

**Code Health After Phase 1**:
- Lines removed: ~2500 LOC
- Critical issues fixed: 3
- High issues fixed: 6
- Architecture aligned with daemon spec
- **Ready for daemon implementation**

---

## Phase 2: Daemon Implementation (2-3 weeks)

**Goal**: Implement daemon spec on clean foundation

### Week 5: Daemon Phase 1 - Embedded Mode with Auto-Watch

**From Daemon Spec: Phase 1**

**Tasks**:
- [ ] Integrate file watcher into `cortex mcp`
- [ ] Watch source files directly (not chunks)
- [ ] Incremental index updates on file change
- [ ] Periodic write to SQLite (60s + 100 changes)
- [ ] Deprecate `cortex index --watch` (merged into mcp)
- [ ] Update documentation

**Benefits**:
- Eliminates manual `cortex index` requirement
- No daemon complexity yet
- Backward compatible

**New Code**: ~500 LOC
**Files**: `internal/cli/mcp.go`, `internal/indexer/watcher.go`

---

### Week 6: Daemon Phase 2 - Session Registry & Coordination

**From Daemon Spec: Phase 2**

**Tasks**:
- [ ] Implement session registry (`~/.cortex/sessions.json`)
- [ ] File locking for concurrent access
- [ ] Registry CRUD operations (load, save, update)
- [ ] Port allocation logic (5173-5273 range)
- [ ] Health check endpoint
- [ ] Unit tests for registry operations

**Benefits**:
- Foundation for daemon auto-start
- Process coordination infrastructure

**New Code**: ~800 LOC
**Files**: `internal/daemon/registry.go`, `internal/daemon/session.go`

---

### Week 7-8: Daemon Phase 3 - Daemon Mode

**From Daemon Spec: Phase 3**

**Tasks**:
- [ ] HTTP/SSE server implementation
- [ ] MCP JSON-RPC over HTTP
- [ ] SSE event stream for notifications
- [ ] Daemon startup logic (spawn, wait for health)
- [ ] Proxy mode (stdio ↔ HTTP)
- [ ] Auto-detection (check registry, start or connect)
- [ ] Heartbeat protocol (send + monitor, 10s interval, 30s timeout)
- [ ] Graceful shutdown (last session stops daemon)
- [ ] Crash recovery (stale session cleanup)
- [ ] Integration tests (multi-session scenarios)

**Benefits**:
- 65% memory savings (shared state)
- Auto-start/stop (zero config)
- Transparent to user

**New Code**: ~2000 LOC
**Files**:
```
internal/daemon/server.go
internal/daemon/proxy.go
internal/daemon/heartbeat.go
internal/daemon/lifecycle.go
cmd/cortex/daemon.go
```

---

### Phase 2 Validation

**Success Criteria**:
- [ ] Single daemon handles multiple sessions
- [ ] Memory usage: 10-70MB for daemon (not N×50MB)
- [ ] First session starts daemon (~200ms overhead)
- [ ] Subsequent sessions connect instantly (<50ms)
- [ ] Daemon shuts down when last session exits
- [ ] Crash recovery works (stale session cleanup)
- [ ] Source file watching triggers incremental updates
- [ ] All MCP tools work via HTTP/SSE

**Integration Tests**:
```bash
# Test 1: Single session
cortex mcp  # Should start daemon and connect

# Test 2: Multi-session
cortex mcp & cortex mcp & cortex mcp  # Should share daemon

# Test 3: Crash recovery
kill -9 <daemon-pid>  # Next session should restart daemon

# Test 4: File watching
echo "test" >> main.go  # Should trigger incremental update

# Test 5: Graceful shutdown
# Close all sessions → daemon should stop
```

---

## Phase 3: Post-Daemon Polish (1 week)

**Goal**: Fix remaining issues that don't affect daemon core

### Week 9: Final Cleanup

#### Day 1-2: God Object Split (If Time Permits)
**Priority**: P2 - Large refactoring
**Issue**: CRITICAL-1

**Tasks**:
- [ ] Split impl.go into focused files:
  - `indexer_core.go` (orchestration)
  - `indexer_files.go` (processing)
  - `indexer_incremental.go` (incremental logic)
  - `indexer_optimization.go` (branch optimization)
- [ ] Test each component independently
- [ ] Ensure no behavior changes

**Note**: This is a NICE-TO-HAVE. If daemon work runs long, defer to post-v1.0.

**Outcome**: impl.go reduced from 1337 → ~300 lines per file

---

#### Day 3: Configuration Cleanup
**Priority**: P2
**Issue**: MEDIUM-8, MEDIUM-9

**Tasks**:
- [ ] Remove unused Config fields (Endpoint, APIKey, Model)
- [ ] Make endpoint/port configurable in embed
- [ ] Make timeouts configurable
- [ ] Clean up documentation

---

#### Day 4: Code Quality Polish
**Priority**: P3
**Issue**: LOW-1 through LOW-12

**Tasks**:
- [ ] Remove verbose AI-generated comments
- [ ] Remove dead code (getWriter test backdoor, containsAll)
- [ ] Extract magic numbers to constants
- [ ] Standardize error message formatting
- [ ] Add deterministic sorting
- [ ] Extract test helpers
- [ ] Make MockProvider fields private

**Outcome**: ~200 LOC cleaned up, better readability

---

#### Day 5: Documentation & Testing
**Priority**: P3

**Tasks**:
- [ ] Update CLAUDE.md (remove JSON references, add daemon info)
- [ ] Update architecture.md
- [ ] Update all specs to reflect SQLite-only
- [ ] Add daemon architecture diagram
- [ ] Update testing strategy for daemon
- [ ] Write migration guide (JSON → SQLite → Daemon)

---

## Timeline Summary

```
┌────────────────────────────────────────────────────────────┐
│ Week 1: Storage Cleanup           [██████████░░]  5 days   │
├────────────────────────────────────────────────────────────┤
│ Week 2: File Watcher Cleanup      [██████████░░]  5 days   │
├────────────────────────────────────────────────────────────┤
│ Week 3: Graph Fixes + Dedup       [██████████░░]  5 days   │
├────────────────────────────────────────────────────────────┤
│ Week 4: Security + Critical       [██████████░░]  5 days   │
├────────────────────────────────────────────────────────────┤
│                      ⬇ READY FOR DAEMON ⬇                   │
├────────────────────────────────────────────────────────────┤
│ Week 5: Daemon Phase 1 (Embedded) [████████████]  5 days   │
├────────────────────────────────────────────────────────────┤
│ Week 6: Daemon Phase 2 (Registry) [████████████]  5 days   │
├────────────────────────────────────────────────────────────┤
│ Week 7-8: Daemon Phase 3 (HTTP)   [████████████]  10 days  │
├────────────────────────────────────────────────────────────┤
│ Week 9: Polish & Documentation    [████████████]  5 days   │
└────────────────────────────────────────────────────────────┘

Total: 6-8 weeks (depends on team size and parallelization)
```

---

## AI-Powered Parallelization Strategy

### Why AI Development Changes the Timeline

With **go-engineer agents**, we can run **5-10 parallel refactoring tasks** simultaneously, each with full context and test coverage. This changes the timeline from **weeks to days**.

**Traditional Development**:
- 1 human dev = 1 task at a time
- Context switching penalty
- 4 weeks for Phase 1

**AI-Powered Development**:
- 5-10 agents in parallel
- No context switching
- **3-5 days for Phase 1** 🚀

---

### Phase 1: Maximum AI Parallelization (3-5 days)

#### Day 1: Parallel Agent Launch (10 agents)

**Agent 1: Deprecate JSON Storage**
```bash
/talk-to-agent go-engineer
Task: Add deprecation warnings for JSON storage and fail fast on instantiation.
Files: internal/indexer/storage_interface.go, internal/config/indexer.go
Tests: Update all tests to expect deprecation errors
```

**Agent 2: Remove Storage Backend Switching (Indexer)**
```bash
/talk-to-agent go-engineer
Task: Remove all type assertions and storage backend switch statements in impl.go.
Hard-code SQLite in all constructors. Remove storage_backend config.
Files: internal/indexer/impl.go (8+ locations), internal/config/indexer.go
Tests: Ensure all tests pass with SQLite-only
```

**Agent 3: Remove JSONStorage Implementation**
```bash
/talk-to-agent go-engineer
Task: Delete JSONStorage struct, AtomicWriter, and all related methods.
Simplify Storage interface.
Files: internal/indexer/storage_interface.go:33-129
Tests: Remove JSON-specific tests
```

**Agent 4: Remove Chunk File Watcher**
```bash
/talk-to-agent go-engineer
Task: Remove file watcher from MCP server that monitors .cortex/chunks/.
Keep watcher interface for future source file watching.
Files: internal/mcp/watcher.go, internal/mcp/server.go
Tests: Update watcher tests
```

**Agent 5: Remove JSON Chunk Loading**
```bash
/talk-to-agent go-engineer
Task: Remove loadFromJSONFiles() and related JSON loading code.
Keep SQLite loading only.
Files: internal/mcp/loader.go:27-107, internal/mcp/chunk_manager.go
Tests: Update loader tests to SQLite-only
```

**Agent 6: Use Graph Library Traversal**
```bash
/talk-to-agent go-engineer
Task: Refactor queryCallers() and queryCallees() to use graph.DFS().
Add cycle detection using graph.CreatesCycle().
Files: internal/graph/searcher.go:384-437
Tests: Add cycle detection tests, benchmark performance
```

**Agent 7: Extract Graph Helper Functions**
```bash
/talk-to-agent go-engineer
Task: Extract deduplicateNodes(), resolveInterfaceEmbeddings(),
context injection (5x), and getNodesByKind() helpers.
Files: internal/graph/builder.go, internal/graph/searcher.go
Tests: Ensure no behavior changes
```

**Agent 8: Extract MCP Helper Functions**
```bash
/talk-to-agent go-engineer
Task: Extract chunkToChromemMetadata() (4x duplication),
chunkToChromemDocument() (4x), and MCP argument parsing utilities.
Files: internal/mcp/chromem_searcher.go, create internal/mcp/helpers.go
Tests: Update all call sites
```

**Agent 9: Extract Storage Encoding**
```bash
/talk-to-agent go-engineer
Task: Extract SerializeEmbedding/DeserializeEmbedding to internal/storage/encoding.go.
Remove duplication across 3 files.
Files: internal/storage/chunk_writer.go, internal/storage/chunk_reader.go, create encoding.go
Tests: Ensure serialization works correctly
```

**Agent 10: Fix SQL Injection + Performance**
```bash
/talk-to-agent go-engineer
Task: Add runtime assertions for SQL field validation.
Replace O(n²) edge filtering with map lookups.
Replace bubble sort with sort.Slice.
Files: internal/files/translator.go, internal/graph/builder.go, internal/storage/chunk_reader.go
Tests: Add SQL injection tests, benchmarks
```

**Day 1 Outcome**: All 10 agents working in parallel on independent tasks

---

#### Day 2: Agent Coordination + PR Review

**Tasks**:
- [ ] Review 10 PRs from agents
- [ ] Run full test suite on each PR
- [ ] Merge PRs in dependency order:
  1. Storage encoding (CRITICAL-2)
  2. JSON storage deprecation (HIGH-3)
  3. Remove backend switching (HIGH-1)
  4. Remove JSONStorage (HIGH-3)
  5. Remove chunk watcher (HIGH-3)
  6. Remove JSON loading (HIGH-3)
  7. Graph library fixes (HIGH-2)
  8. Extract helpers (HIGH-5, HIGH-6)
  9. Security + performance fixes

**Coordination**:
```bash
# Check for merge conflicts
git checkout main
for branch in agent-*; do
  git merge --no-commit $branch
  if [ $? -ne 0 ]; then
    echo "Conflict in $branch"
    git merge --abort
  fi
done

# Run integration tests
task test:integration
```

**Day 2 Outcome**: All Phase 1 cleanup merged and tested

---

#### Day 3: Extract File Metadata Package

**Agent 11: File Metadata Package**
```bash
/talk-to-agent go-engineer
Task: Create internal/indexer/metadata/ package.
Move collectFileMetadata(), calculateChecksum(), countLines(),
isCommentLine(), isTestFile(), extractModulePath() (~140 lines).
Files: Create internal/indexer/metadata/, update internal/indexer/impl.go
Tests: Ensure metadata collection still works
```

**Day 3 Outcome**: Metadata abstraction complete, impl.go reduced by 140 lines

---

#### Day 4-5: Validation + Documentation

**Tasks**:
- [ ] Run full test suite (unit + integration + race detector)
- [ ] Benchmark performance improvements
- [ ] Update CLAUDE.md (remove JSON references)
- [ ] Update architecture.md
- [ ] Update all specs
- [ ] Write migration guide

**Validation Commands**:
```bash
task test              # Unit tests
task test:integration  # Integration tests
task test:race         # Race detector
task test:coverage     # Coverage report

# Smoke tests
cortex index .         # Full indexing
cortex mcp             # MCP server
# Run semantic search, exact search, graph queries
```

---

### Phase 1 Timeline: AI vs Traditional

| Approach | Timeline | Agents/Devs | Speedup |
|----------|----------|-------------|---------|
| **AI-Powered** | **3-5 days** | 10 agents + 1 human coordinator | **5-8x faster** |
| Traditional (3 devs) | 2.5 weeks | 3 developers | Baseline |
| Traditional (1 dev) | 4 weeks | 1 developer | Reference |

**Why AI is Faster**:
1. **True parallelization**: 10 tasks simultaneously (no human can do this)
2. **No context switching**: Each agent focuses on one task
3. **Consistent quality**: Agents follow patterns, write tests, maintain style
4. **24/7 availability**: Can run overnight if needed

---

### Phase 2: Daemon Implementation (1-2 weeks with AI)

#### Week 1: Daemon Core (Parallel Agents)

**Agent 12: Embedded Mode + File Watcher**
```bash
/talk-to-agent go-engineer
Task: Integrate file watcher into cortex mcp. Watch source files directly.
Implement incremental index updates on file change.
Files: internal/cli/mcp.go, internal/indexer/watcher.go
Spec: specs/2025-10-29_auto-daemon.md Phase 1
```

**Agent 13: Session Registry**
```bash
/talk-to-agent go-engineer
Task: Implement session registry (~/.cortex/sessions.json) with file locking.
Registry CRUD operations, port allocation logic.
Files: internal/daemon/registry.go, internal/daemon/session.go
Spec: specs/2025-10-29_auto-daemon.md Phase 2
```

**Agent 14: Health Check Endpoint**
```bash
/talk-to-agent go-engineer
Task: Implement health check HTTP endpoint for daemon.
Files: internal/daemon/health.go
Tests: Health check returns 200 when healthy
```

**Week 1 Outcome**: Registry + embedded mode working

---

#### Week 2: HTTP/SSE Transport (Parallel Agents)

**Agent 15: HTTP/SSE Server**
```bash
/talk-to-agent go-engineer
Task: Implement HTTP/SSE server for MCP JSON-RPC.
SSE event stream for notifications.
Files: internal/daemon/server.go
Spec: specs/2025-10-29_auto-daemon.md Phase 3 (HTTP/SSE)
```

**Agent 16: Proxy Mode**
```bash
/talk-to-agent go-engineer
Task: Implement proxy mode (stdio ↔ HTTP).
Auto-detection logic (check registry, start or connect).
Files: internal/daemon/proxy.go, cmd/cortex/daemon.go
Spec: specs/2025-10-29_auto-daemon.md Phase 3 (Proxy)
```

**Agent 17: Heartbeat + Lifecycle**
```bash
/talk-to-agent go-engineer
Task: Implement heartbeat protocol (10s interval, 30s timeout).
Graceful shutdown (last session stops daemon).
Crash recovery (stale session cleanup).
Files: internal/daemon/heartbeat.go, internal/daemon/lifecycle.go
Spec: specs/2025-10-29_auto-daemon.md Phase 3 (Heartbeat)
```

**Agent 18: Integration Tests**
```bash
/talk-to-agent go-engineer
Task: Write integration tests for multi-session scenarios.
Test: single session, multi-session, crash recovery, graceful shutdown.
Files: internal/daemon/integration_test.go
```

**Week 2 Outcome**: Full daemon implementation complete

---

### Phase 3: Polish (2-3 days with AI)

**Agent 19: Configuration Cleanup**
```bash
/talk-to-agent go-engineer
Task: Remove unused Config fields. Make timeouts configurable.
Files: internal/embed/factory.go, internal/embed/local.go
Issues: MEDIUM-8, MEDIUM-9, MEDIUM-19
```

**Agent 20: Code Quality Polish**
```bash
/talk-to-agent go-engineer
Task: Remove verbose comments, dead code, extract magic numbers.
Standardize error messages.
Files: Multiple (LOW-1 through LOW-12)
```

**Agent 21: Documentation**
```bash
/talk-to-agent docs-writer
Task: Update all documentation for daemon architecture.
Remove JSON storage references. Add migration guide.
Files: CLAUDE.md, docs/architecture.md, docs/configuration.md
```

---

### Revised Timeline with AI Development

```
┌────────────────────────────────────────────────────────────┐
│ Day 1-2: Phase 1 Cleanup    [████████████] 10 agents      │
├────────────────────────────────────────────────────────────┤
│ Day 3: Metadata Extract      [████████████] 1 agent       │
├────────────────────────────────────────────────────────────┤
│ Day 4-5: Validation + Docs   [████████████] Manual        │
├────────────────────────────────────────────────────────────┤
│                      ⬇ READY FOR DAEMON ⬇                  │
├────────────────────────────────────────────────────────────┤
│ Week 2: Daemon Core          [████████████] 3 agents      │
├────────────────────────────────────────────────────────────┤
│ Week 3: HTTP/SSE + Tests     [████████████] 4 agents      │
├────────────────────────────────────────────────────────────┤
│ Day 16-18: Polish            [████████████] 3 agents      │
└────────────────────────────────────────────────────────────┘

Total: 3-4 weeks (down from 6-8 weeks!)
```

---

## AI Agent Coordination Strategy

### Agent Launch Script

Create `scripts/launch-phase1-agents.sh`:

```bash
#!/bin/bash
# Launch 10 parallel go-engineer agents for Phase 1 cleanup

AGENTS=(
  "deprecate-json-storage"
  "remove-backend-switching"
  "remove-json-storage-impl"
  "remove-chunk-watcher"
  "remove-json-loading"
  "use-graph-library"
  "extract-graph-helpers"
  "extract-mcp-helpers"
  "extract-storage-encoding"
  "fix-security-performance"
)

for agent in "${AGENTS[@]}"; do
  echo "Launching agent: $agent"
  claude-code agent go-engineer \
    --task-file "tasks/$agent.md" \
    --branch "agent/$agent" \
    --output "results/$agent.json" &
done

# Wait for all agents to complete
wait

echo "All agents complete. Review PRs in results/"
```

---

### Agent Task Template

`tasks/deprecate-json-storage.md`:

```markdown
# Task: Deprecate JSON Storage

## Context
- Issue: HIGH-3 (Legacy JSON Storage)
- Files: 37+ references to JSON chunks
- Goal: Fail fast on JSON storage requests

## Requirements
1. Add deprecation error in NewJSONStorage()
2. Add validation error in Config.Validate()
3. Update all tests to expect deprecation
4. Update CLI to show migration message
5. Run full test suite

## Success Criteria
- [ ] JSON storage instantiation returns error
- [ ] Config validation fails on storage_backend: json
- [ ] All tests pass
- [ ] No regressions in SQLite path

## Testing
```bash
task test
task test:integration
```

## PR Checklist
- [ ] Code follows Go conventions
- [ ] Tests added/updated
- [ ] Documentation updated
- [ ] No breaking changes to SQLite path
```

---

### Agent Coordination Dashboard

Track agent progress in real-time:

```bash
# Monitor agent status
watch -n 5 'ls -lh results/*.json | wc -l'

# Check for failures
grep -r "ERROR" results/*.json

# Merge order (dependency-aware)
cat merge-order.txt
```

`merge-order.txt`:
```
1. agent/extract-storage-encoding
2. agent/deprecate-json-storage
3. agent/remove-backend-switching
4. agent/remove-json-storage-impl
5. agent/remove-chunk-watcher
6. agent/remove-json-loading
7. agent/use-graph-library
8. agent/extract-graph-helpers
9. agent/extract-mcp-helpers
10. agent/fix-security-performance
```

---

## Benefits of AI-Powered Approach

### Speed
- **10x parallel tasks** vs 1-3 sequential tasks
- **3-5 days** instead of 3-4 weeks for Phase 1
- **3-4 weeks total** instead of 6-8 weeks

### Quality
- **Consistent code style** across all agents
- **Comprehensive tests** written automatically
- **No context switching fatigue** (human bottleneck eliminated)

### Cost-Effectiveness
- **10 agents × 2 days** = 20 agent-days
- **vs 3 devs × 15 days** = 45 developer-days
- **~50% cost reduction** (even with agent costs)

### Risk Reduction
- **Small, focused PRs** easier to review
- **Independent changes** minimize merge conflicts
- **Automated testing** catches regressions immediately

---

## Human Coordination Role

With AI agents, the human becomes **architect + reviewer**:

### Day 1: Launch Agents
- Review roadmap and task definitions
- Launch 10 agents in parallel
- Monitor progress dashboard

### Day 2: Review & Merge
- Review 10 PRs (30-60 min each = 5-10 hours)
- Run integration tests
- Merge in dependency order
- Resolve any conflicts (rare with good task isolation)

### Day 3: Validation
- Run full test suite
- Smoke test critical paths
- Launch Agent 11 (metadata extraction)

### Day 4-5: Documentation
- Update docs with agent help
- Write migration guide
- Prepare for Phase 2

**Human time commitment**: ~3-4 hours/day (vs 8 hours traditional dev)

---

## Conclusion: AI Changes Everything

**Traditional Approach**:
- 3-4 weeks for Phase 1
- 1-3 developers
- Sequential bottlenecks

**AI-Powered Approach**:
- **3-5 days for Phase 1** ✅
- 10 agents + 1 coordinator
- True parallelization

**The Result**:
- Ship daemon-ready codebase in **1 week** instead of 1 month
- Higher quality (consistent, tested, documented)
- Lower cost (less human time)
- More fun (humans do architecture, agents do grunt work)

---

### Phase 2: Mostly Sequential (1-2 Developers)

**Why Sequential?**
- Daemon is a single architectural component
- Session registry → HTTP server → proxy mode (dependencies)
- High coordination overhead if split

**Possible Parallel Work**:
- Dev 1: Core daemon implementation
- Dev 2: Integration tests + documentation

---

### Phase 3: Fully Parallel (2-3 Developers)

**Dev 1**: God object split (if doing it)
**Dev 2**: Configuration cleanup + code quality
**Dev 3**: Documentation + migration guides

---

## Risk Mitigation

### Risk 1: Phase 1 Takes Longer Than Expected

**Mitigation**:
- Each week is independently valuable
- Can stop after Week 2 and proceed to daemon (partial cleanup)
- Week 3-4 are optimizations, not blockers

**Minimum Viable Cleanup** (2 weeks):
- Week 1: Storage cleanup ✅ MUST DO
- Week 2: Watcher cleanup ✅ MUST DO
- Week 3-4: Skip initially, do post-daemon

---

### Risk 2: Breaking Changes During Cleanup

**Mitigation**:
- Comprehensive test suite (3700+ lines of tests)
- Run full test suite after each day's work
- Integration tests catch regressions
- Keep git commits small and focused

**Testing Protocol**:
```bash
# After each change:
task test              # Unit tests
task test:integration  # Integration tests
task test:race         # Race detector
cortex index .         # Manual smoke test
cortex mcp             # MCP server smoke test
```

---

### Risk 3: Daemon Implementation Reveals Issues

**Mitigation**:
- Daemon spec is detailed and well-thought-out
- Clean Phase 1 code reduces surprises
- Phased daemon implementation (embedded → registry → HTTP)
- Can revert to embedded mode if HTTP/SSE fails

**Rollback Plan**:
- Phase 1 cleanup: Reversible (git revert)
- Daemon Phase 1: Backward compatible (embedded mode)
- Daemon Phase 2-3: Feature flag to disable daemon mode

---

## Success Metrics

### After Phase 1 (Pre-Daemon Cleanup)

- [ ] **Code Health**: 8.0/10 (up from 6.8/10)
- [ ] **Lines Removed**: ~2500 LOC
- [ ] **Critical Issues**: 0 (down from 3)
- [ ] **High Issues**: 8 (down from 14)
- [ ] **Storage Paths**: 1 (down from 2)
- [ ] **File Watchers**: 0 in MCP (down from 1)
- [ ] **Test Suite**: Still passing (100% pass rate)

### After Phase 2 (Daemon Implementation)

- [ ] **Memory Usage**: 10-70MB (down from N×50MB)
- [ ] **Session Startup**: <200ms first, <50ms subsequent
- [ ] **Multi-Session**: Works with 10+ concurrent sessions
- [ ] **Crash Recovery**: Automatic stale cleanup
- [ ] **File Watching**: Incremental updates <500ms
- [ ] **Backward Compat**: Embedded mode still works

### After Phase 3 (Polish)

- [ ] **Code Health**: 8.5/10 (target achieved)
- [ ] **Documentation**: 100% accurate (no JSON references)
- [ ] **All Issues**: 90% resolved (45 → 5 remaining)
- [ ] **Performance**: Benchmarked and optimized
- [ ] **Ready for v1.0**: Production-ready release

---

## Decision Points

### Decision 1: Do We Split the God Object? (Week 9)

**If YES**:
- Pros: Cleaner architecture, easier maintenance
- Cons: 2-3 days of work, high refactoring risk
- When: Only if ahead of schedule

**If NO**:
- Pros: Ship daemon faster
- Cons: Tech debt remains
- When: If daemon work runs long or team is small

**Recommendation**: **Defer to post-v1.0** unless 3+ devs available

---

### Decision 2: How Much Parallelization? (Phase 1)

**Option A: Solo Developer (4 weeks)**
- Work sequentially through tracks
- Lower risk, easier coordination
- Timeline: 4 weeks for Phase 1

**Option B: 2 Developers (3 weeks)**
- Dev 1: Storage + Security
- Dev 2: Watcher + Graph + Dedup
- Moderate coordination
- Timeline: 3 weeks for Phase 1

**Option C: 3 Developers (2.5 weeks)**
- Dev 1: Storage track
- Dev 2: Watcher track
- Dev 3: Graph + Dedup track
- All: Week 4 coordination
- Timeline: 2.5 weeks for Phase 1

**Recommendation**: **Option B** - Best balance of speed and coordination

---

### Decision 3: When to Start Daemon Work?

**Option A: After Full Cleanup (Week 5)**
- Pros: Cleanest foundation, zero tech debt drag
- Cons: 4 weeks before daemon starts
- Risk: If cleanup overruns, daemon is delayed

**Option B: After Minimum Cleanup (Week 3)**
- Pros: Start daemon 2 weeks earlier
- Cons: Some tech debt remains
- Risk: May need to refactor daemon code later

**Recommendation**: **Option A** - The 2 extra weeks are worth it for clean architecture

---

## Conclusion

### Why This Plan Works

1. **Clean Foundation**: Phase 1 removes architectural blockers
2. **Focused Effort**: Each phase has clear goals and deliverables
3. **Incremental Progress**: Each week delivers value independently
4. **Risk Managed**: Small commits, comprehensive tests, rollback plans
5. **Team Flexible**: Works with 1-3 developers via parallelization

### What Success Looks Like

**End of Week 4**:
```go
// Clean, simple indexer
func NewIndexer(config *Config) (Indexer, error) {
    storage, err := NewSQLiteStorage(config.RootDir)  // Only one path
    // ... clean initialization
}

// No more type assertions, no backend switching, no JSON code
```

**End of Week 8**:
```bash
# User runs cortex mcp (first time)
$ cortex mcp
[INFO] Starting daemon on :5173...
[INFO] Daemon started (PID 12345)
[INFO] Connected to daemon
# (subsequent sessions connect instantly)

# Memory usage: 10-70MB total (not N×50MB)
```

**End of Week 9**:
- Production-ready v1.0
- Clean architecture aligned with daemon spec
- Technical debt reduced by 90%
- Ready for future features

---

## Next Steps

1. **Review this plan** with team
2. **Decide on parallelization** (1-3 devs)
3. **Create GitHub issues** from checklist
4. **Set up project board** (Pre-Daemon, Daemon, Polish)
5. **Start Week 1: Storage Cleanup** 🚀

---

**Last Updated**: 2025-11-03
**Status**: PROPOSED
**Approval Required**: Team Lead / Architect
