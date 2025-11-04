---
status: ready-for-implementation
started_at: 2025-11-04T00:00:00Z
completed_at: null
dependencies: []
---

# Indexer Architecture Refactor

## Problem Statement

The current indexer architecture has accumulated complexity from attempting to support multiple modes (full vs incremental) and optimization strategies (branch copying) within a single implementation. This has led to:

1. **Broken Incremental Indexing**: Always re-embeds all files instead of only changed files
2. **Unclear Responsibilities**: `processFiles()` helper tries to do too much (metadata collection + processing + graph building)
3. **Hidden Side Effects**: Change detection logic buried inside processing logic
4. **Difficult Testing**: Tightly coupled components make unit testing hard
5. **Poor Maintainability**: Three entry points (Index, IndexIncremental, Watch) with overlapping logic

### Root Cause Analysis

**Incremental Indexing Bug:**
- `processFiles(codeFiles, docFiles, allFiles, deletedFiles)` receives:
  - `codeFiles`/`docFiles`: Files to process chunks for (subset - changed files only)
  - `allFiles`: All discovered files (used for graph building)
- Line 141: `allFilesToProcess := append(codeFiles, docFiles...)`
- **Bug**: Only collects metadata for changed files, not all files
- **Effect**: Next run, `ReadMetadata()` returns incomplete checksums → all files look new

**Architecture Problems:**
- Change detection mixed with processing
- No clear separation between "what changed" vs "process changes"
- Multiple code paths for same operation (Index vs IndexIncremental)
- Watch mode duplicates change detection logic

## Proposed Architecture

### Core Principle

**There is only one operation: Index**

Whether triggered by CLI (`cortex index`) or file watcher, the job is the same:
1. Detect what changed (disk vs database comparison)
2. Process what changed (parse → chunk → embed → write)
3. Update auxiliary data (graph, metadata)

### Component Separation

```
┌─────────────────────────────────────────────────────────────┐
│                        Indexer                              │
│  Index(ctx, hint []string) → Stats                          │
│                                                              │
│  1. ChangeDetector.DetectChanges(hint)                      │
│  2. Storage.DeleteFiles(deleted)                            │
│  3. Storage.UpdateFileMtimes(unchanged)                     │
│  4. Processor.ProcessFiles(added + modified)                │
│  5. GraphBuilder.UpdateGraph(changes)                       │
└─────────────────────────────────────────────────────────────┘
         ▲
         │
    ┌────┴────┐
    │         │
┌───┴───┐  ┌─┴──────┐
│  CLI  │  │ Watch  │
│ index │  │  Mode  │
└───────┘  └────────┘
              │
     ┌────────┴────────┐
     │                 │
┌────┴────┐      ┌─────┴──────┐
│   Git   │      │   File     │
│ Watcher │      │  Watcher   │
└─────────┘      └────────────┘
     │                 │
     └────────┬────────┘
              │
      ┌───────┴────────┐
      │ WatchCoordinator│
      └────────────────┘
```

## Component Interfaces

### 1. ChangeDetector

**Responsibility**: Compare filesystem state to database state, return what changed.

**Interface:**
```go
type ChangeDetector interface {
    // DetectChanges compares disk to DB and returns files needing processing.
    // If hint is non-empty, only checks those files (optimization from watcher).
    // If hint is empty, discovers all files and compares.
    DetectChanges(ctx context.Context, hint []string) (*ChangeSet, error)
}

type ChangeSet struct {
    Added     []string // New files not in DB
    Modified  []string // Files with different hash than DB
    Deleted   []string // Files in DB but not on disk
    Unchanged []string // Files with same hash (mtime may have drifted)
}
```

**Implementation Logic:**
```
1. If hint provided:
   - Only check those specific files
2. If hint empty:
   - Discover all code/doc files from disk

3. For each file:
   a. Read mtime from disk
   b. Query DB for (file_path, mtime, hash)
   c. If DB mtime == disk mtime:
      → Unchanged (fast path, skip hash calculation)
   d. If DB mtime != disk mtime:
      → Calculate hash, compare to DB hash
      → If hash same: Unchanged (mtime drift, needs DB update)
      → If hash different: Modified
   e. If file not in DB:
      → Added

4. Files in DB but not on disk:
   → Deleted
```

**Key Properties:**
- **Read-only**: No database writes, no side effects
- **Idempotent**: Same inputs always return same results
- **Fast path**: Mtime comparison avoids hash calculation when possible
- **Handles mtime drift**: Detects unchanged files even when mtime changed (git operations)

### 2. Processor

**Responsibility**: Parse → chunk → embed → write to database.

**Interface:**
```go
type Processor interface {
    // ProcessFiles parses, chunks, embeds, and writes files to database.
    // Returns statistics about what was processed.
    ProcessFiles(ctx context.Context, files []string) (*Stats, error)
}

type Stats struct {
    CodeFilesProcessed int
    DocsProcessed      int
    TotalCodeChunks    int
    TotalDocChunks     int
    ProcessingTime     time.Duration
}
```

**Implementation:**
```
For each file in files:
  1. Parse with tree-sitter → CodeExtraction
  2. Generate chunks (symbols, definitions, data)
  3. Format chunks for embedding
  4. Batch embed chunks (passage mode)
  5. Write to DB:
     - File stats (INSERT OR REPLACE)
     - File content for FTS5
     - Chunks with embeddings
     - Code structures (types, functions, imports)
```

**Key Properties:**
- **No change detection**: Just processes what it's given
- **No branching logic**: Doesn't know about incremental vs full
- **No metadata tracking**: Doesn't manage checksums/mtimes
- **Focused**: Single responsibility (processing pipeline)

### 3. GitWatcher

**Responsibility**: Monitor `.git/HEAD` for branch changes.

**Interface:**
```go
type GitWatcher interface {
    // Start monitoring .git/HEAD, call callback on branch switch.
    Start(ctx context.Context, callback func(oldBranch, newBranch string)) error

    // Stop watching.
    Stop() error
}
```

**Implementation:**
```
1. Use fsnotify to watch .git/HEAD
2. On change event:
   a. Read new HEAD content
   b. Parse branch name
   c. Compare to last known branch
   d. If different, fire callback(old, new)
```

**Key Properties:**
- **Focused**: Only cares about git branch
- **No file watching**: Doesn't monitor source files
- **Simple callback**: Just notifies, doesn't coordinate

### 4. FileWatcher

**Responsibility**: Monitor source files for modifications with debouncing and pause/resume.

**Interface:**
```go
type FileWatcher interface {
    // Start monitoring source files, call callback with debounced changes.
    Start(ctx context.Context, callback func(files []string)) error

    // Stop watching.
    Stop() error

    // Pause watching (accumulate events but don't fire callback).
    Pause()

    // Resume watching (fire callback for accumulated events).
    Resume()
}
```

**Implementation:**
```
1. Use fsnotify to watch source directories
2. Filter events (only .go, .ts, .md, etc.)
3. Accumulate changes in buffer
4. Debounce (wait 500ms of quiet time)
5. If not paused:
   → Fire callback with list of changed files
6. If paused:
   → Keep accumulating, fire on Resume()
```

**Key Properties:**
- **Debouncing**: Coalesces rapid changes into single event
- **Pause/Resume**: Supports coordination during branch switch
- **No git awareness**: Doesn't know about branches
- **Batching**: Fires callback with multiple files at once

### 5. BranchSynchronizer

**Responsibility**: Prepare branch databases, optionally copying chunks from ancestor.

**Interface:**
```go
type BranchSynchronizer interface {
    // PrepareDB ensures branch database exists and is optimized.
    // If branch has ancestor with overlapping files, copies unchanged chunks.
    PrepareDB(ctx context.Context, branch string) error
}
```

**Implementation:**
```
1. Check if branch.db exists
   - If yes, verify schema version, return
   - If no, create new DB with schema

2. Detect ancestor branch:
   - Find merge-base with main/master
   - Check if ancestor branch DB exists

3. If ancestor found:
   a. Query ancestor DB for all chunks
   b. For each chunk, check if file still exists and unchanged
   c. Copy chunk if file hash matches
   d. Log how many chunks copied vs need reprocessing

4. Return (ready for indexing)
```

**Key Properties:**
- **Pure logic**: No watching, no coordination
- **Optimization**: Copies chunks when possible
- **Fallback**: Gracefully handles missing ancestor
- **Transparent**: Caller just gets prepared DB

### 6. WatchCoordinator

**Responsibility**: Coordinate GitWatcher and FileWatcher, route events to Indexer.

**Interface:**
```go
type WatchCoordinator struct {
    git        GitWatcher
    files      FileWatcher
    branchSync BranchSynchronizer
    indexer    *Indexer
}

func (c *WatchCoordinator) Start(ctx context.Context) error
```

**Implementation:**
```
Start():
  1. git.Start(ctx, c.handleBranchSwitch)
  2. files.Start(ctx, c.handleFileChange)
  3. Block until ctx.Done()
  4. Cleanup watchers

handleBranchSwitch(old, new string):
  1. files.Pause() → Stop reacting to file events
  2. branchSync.PrepareDB(new) → Copy chunks if possible
  3. indexer.SwitchBranch(new) → Reconnect to new DB
  4. files.Resume() → Start reacting again

handleFileChange(files []string):
  1. indexer.Index(ctx, files) → Process with hint
```

**Key Properties:**
- **Event routing**: Connects watchers to indexer
- **Coordination**: Pauses file watching during branch switch
- **Clean shutdown**: Stops watchers on context cancel
- **Error handling**: Logs errors but doesn't crash

### 7. Indexer (New Core)

**Responsibility**: Orchestrate change detection, processing, and graph updates.

**Interface:**
```go
type Indexer struct {
    changeDetector ChangeDetector
    processor      Processor
    storage        Storage
    graphBuilder   GraphBuilder
}

// Index discovers changes and processes them.
// hint: Optional list of files that changed (from watcher). If empty, full discovery.
func (idx *Indexer) Index(ctx context.Context, hint []string) (*Stats, error)

// SwitchBranch reconnects to different branch database.
func (idx *Indexer) SwitchBranch(branch string) error
```

**Implementation:**
```go
func (idx *Indexer) Index(ctx context.Context, hint []string) (*Stats, error) {
    // 1. Detect changes (read-only, no side effects)
    changes, err := idx.changeDetector.DetectChanges(ctx, hint)
    if err != nil {
        return nil, fmt.Errorf("change detection failed: %w", err)
    }

    // 2. Handle deletions (cascade deletes chunks via FK)
    if len(changes.Deleted) > 0 {
        for _, deleted := range changes.Deleted {
            idx.storage.DeleteFile(deleted)
        }
        log.Printf("✓ Deleted %d files from DB\n", len(changes.Deleted))
    }

    // 3. Update metadata for unchanged files (mtime drift correction)
    // SQLite: UPDATE files SET last_modified = ? WHERE file_path IN (...)
    if len(changes.Unchanged) > 0 {
        idx.storage.UpdateFileMtimes(changes.Unchanged)
    }

    // 4. Process changed files (added + modified)
    toProcess := append(changes.Added, changes.Modified...)
    if len(toProcess) == 0 {
        log.Println("No changes detected")
        return &Stats{}, nil // Nothing to do
    }

    log.Printf("Processing %d files (%d added, %d modified)\n",
        len(toProcess), len(changes.Added), len(changes.Modified))

    stats, err := idx.processor.ProcessFiles(ctx, toProcess)
    if err != nil {
        return nil, fmt.Errorf("processing failed: %w", err)
    }

    // 5. Update graph (incremental based on changes)
    err = idx.graphBuilder.UpdateGraph(ctx, changes.Added, changes.Modified, changes.Deleted)
    if err != nil {
        log.Printf("Warning: graph update failed: %v\n", err)
        // Don't fail indexing if graph fails (supplementary data)
    }

    return stats, nil
}
```

**Key Properties:**
- **Single entry point**: No Index vs IndexIncremental
- **Clear flow**: Detect → Delete → Update → Process → Graph
- **Explicit**: Each step is a clear method call
- **Error handling**: Graceful degradation for graph failures
- **Logging**: Progress visibility

## Data Flow

### CLI Index Flow

```
$ cortex index

CLI:
  indexer.Index(ctx, nil) // No hint

Indexer.Index():
  1. changeDetector.DetectChanges(ctx, nil)
     → Discovers all files, compares to DB
     → Returns ChangeSet

  2. storage.DeleteFile() for each deleted

  3. storage.UpdateFileMtimes() for unchanged

  4. processor.ProcessFiles(added + modified)
     → Parse, chunk, embed, write
     → Returns Stats

  5. graphBuilder.UpdateGraph()
     → Incremental graph update

  6. Return Stats to CLI
```

### Watch Mode Flow

```
$ cortex index --watch

CLI:
  coordinator.Start(ctx)

WatchCoordinator.Start():
  1. gitWatcher.Start(ctx, handleBranchSwitch)
  2. fileWatcher.Start(ctx, handleFileChange)
  3. <block until ctx cancelled>

Event: File change (src/foo.go modified)
  → FileWatcher detects, debounces (500ms)
  → Fires callback(["src/foo.go"])
  → coordinator.handleFileChange(["src/foo.go"])
  → indexer.Index(ctx, ["src/foo.go"]) // With hint!
  → ChangeDetector only checks hinted files
  → Process only if hash changed

Event: Branch switch (main → feature)
  → GitWatcher detects .git/HEAD change
  → Parses new branch name
  → Fires callback("main", "feature")
  → coordinator.handleBranchSwitch("main", "feature")
  → fileWatcher.Pause() // Stop reacting
  → branchSync.PrepareDB("feature") // Copy chunks
  → indexer.SwitchBranch("feature") // Reconnect DB
  → fileWatcher.Resume() // Start reacting
```

## Migration Strategy

### Create New, Replace Old Pattern

**Phase 1: Create New Components (Parallel)**
- Implement all 7 components in new files
- Use `_v2` suffix to avoid conflicts (e.g., `indexer_v2.go`)
- Full test coverage for each component
- **No changes to existing code**

**Phase 2: Wire Up New Implementation**
- Add feature flag or build tag to switch between old/new
- Update `internal/cli/index.go` to use new indexer when enabled
- Update `internal/cli/watch.go` to use WatchCoordinator when enabled
- Integration testing

**Phase 3: Remove Old Code**
- Delete old `IndexIncremental()` method
- Delete old `processFiles()` helper
- Delete old watcher implementation
- Remove feature flag (new is default)
- Clean up unused interfaces

**Benefits:**
- Minimize risk (old code still works during development)
- Easy rollback if problems found
- Parallel development (multiple agents can work independently)
- Gradual migration (can ship feature-flagged initially)

## Implementation Plan

Following `specs/specs.md` workflow for multi-agent parallel implementation.

### Prerequisites

- SQLite migration complete ✅
- Current tests passing ✅
- Spec approved ✅

### Phase 1: Parallel Component Implementation (7 agents)

Each agent creates new file(s), implements interface, writes tests. No cross-dependencies during Phase 1.

#### Agent 1: ChangeDetector
**Files:**
- `internal/indexer/change_detector.go`
- `internal/indexer/change_detector_test.go`

**Dependencies:** None (uses existing Storage interface)

**Interface:**
```go
type ChangeDetector interface {
    DetectChanges(ctx context.Context, hint []string) (*ChangeSet, error)
}

type ChangeSet struct {
    Added     []string
    Modified  []string
    Deleted   []string
    Unchanged []string
}
```

**Implementation Requirements:**
- Mtime fast-path optimization
- Hash calculation only when mtime differs
- Handle empty hint (full discovery)
- Handle non-empty hint (check specific files)
- Detect deleted files (in DB, not on disk)

**Tests:**
- No changes detected
- New file added
- File modified (content changed)
- File deleted
- Mtime drift (content unchanged, mtime changed)
- Hint optimization (only checks hinted files)
- Large file sets (performance)

**Success Criteria:**
- All tests pass
- Performance: <1s for 10K files with 10% changed

---

#### Agent 2: Processor
**Files:**
- `internal/indexer/processor.go`
- `internal/indexer/processor_test.go`

**Dependencies:** None (uses existing Parser, Chunker, Formatter, Provider, Storage)

**Interface:**
```go
type Processor interface {
    ProcessFiles(ctx context.Context, files []string) (*Stats, error)
}

type Stats struct {
    CodeFilesProcessed int
    DocsProcessed      int
    TotalCodeChunks    int
    TotalDocChunks     int
    ProcessingTime     time.Duration
}
```

**Implementation Requirements:**
- Parse code files (tree-sitter)
- Parse doc files (markdown)
- Generate chunks (symbols, definitions, data, docs)
- Batch embed chunks
- Write to DB (file stats, chunks, code structures)
- Progress reporting via existing ProgressReporter
- Context cancellation support

**Tests:**
- Process single Go file
- Process single Markdown file
- Process multiple files (batch)
- Empty file list (no-op)
- Context cancellation (graceful stop)
- Invalid file (error handling)
- Embedding failure (error propagation)

**Success Criteria:**
- All tests pass
- Matches existing processing quality
- Cancellation works cleanly

---

#### Agent 3: GitWatcher
**Files:**
- `internal/watcher/git_watcher.go`
- `internal/watcher/git_watcher_test.go`

**Dependencies:** None (uses fsnotify)

**Interface:**
```go
type GitWatcher interface {
    Start(ctx context.Context, callback func(oldBranch, newBranch string)) error
    Stop() error
}
```

**Implementation Requirements:**
- Watch `.git/HEAD` with fsnotify
- Parse HEAD to extract branch name
- Detect branch changes
- Fire callback with old/new branch names
- Handle symbolic refs (ref: refs/heads/main)
- Handle detached HEAD
- Graceful shutdown on Stop()

**Tests:**
- Detect branch switch
- Handle initial branch (no "old" branch)
- Handle detached HEAD
- Stop() cleanup
- Context cancellation
- Rapid branch switching (debounce?)
- .git/HEAD deleted/recreated

**Success Criteria:**
- All tests pass
- No goroutine leaks
- Clean shutdown

---

#### Agent 4: FileWatcher
**Files:**
- `internal/watcher/file_watcher.go`
- `internal/watcher/file_watcher_test.go`

**Dependencies:** None (uses fsnotify)

**Interface:**
```go
type FileWatcher interface {
    Start(ctx context.Context, callback func(files []string)) error
    Stop() error
    Pause()
    Resume()
}
```

**Implementation Requirements:**
- Watch source directories with fsnotify
- Filter events (only .go, .ts, .md, .py, .rs, etc.)
- Debounce (500ms of quiet time)
- Accumulate changes during debounce window
- Pause/Resume support (accumulate but don't fire)
- Fire callback with batched file list
- Handle recursive directory watching

**Tests:**
- Single file change
- Multiple file changes (batched)
- Debouncing (coalesce rapid changes)
- Pause/Resume behavior
- File created
- File deleted
- File renamed
- Directory added (recursive watch)
- Stop() cleanup
- Context cancellation

**Success Criteria:**
- All tests pass
- Debouncing works (500ms window)
- Pause/Resume correct
- No goroutine leaks

---

#### Agent 5: BranchSynchronizer
**Files:**
- `internal/indexer/branch_synchronizer.go`
- `internal/indexer/branch_synchronizer_test.go`

**Dependencies:** Existing `internal/cache` and `internal/indexer/branch_optimizer.go`

**Interface:**
```go
type BranchSynchronizer interface {
    PrepareDB(ctx context.Context, branch string) error
}
```

**Implementation Requirements:**
- Check if branch DB exists
- Create DB with schema if missing
- Detect ancestor branch (merge-base)
- Copy chunks from ancestor if available
- Log optimization results
- Fallback gracefully if ancestor missing

**Tests:**
- New branch (no ancestor)
- Branch with ancestor (copy chunks)
- Branch with ancestor (no common files)
- Ancestor DB missing (fallback)
- Schema version mismatch (handle)
- Concurrent branch switches (safety)

**Success Criteria:**
- All tests pass
- Matches existing branch optimization behavior
- Safe for concurrent calls

---

#### Agent 6: WatchCoordinator
**Files:**
- `internal/watcher/coordinator.go`
- `internal/watcher/coordinator_test.go`

**Dependencies:** GitWatcher (Agent 3), FileWatcher (Agent 4), BranchSynchronizer (Agent 5)

**Interface:**
```go
type WatchCoordinator struct {
    git        GitWatcher
    files      FileWatcher
    branchSync BranchSynchronizer
    indexer    *Indexer
}

func (c *WatchCoordinator) Start(ctx context.Context) error
```

**Implementation Requirements:**
- Start both watchers
- Route git events to handleBranchSwitch
- Route file events to handleFileChange
- Coordinate pause/resume during branch switch
- Block until context cancelled
- Clean shutdown

**Tests:**
- File change event (triggers index)
- Branch switch event (pauses, syncs, switches, resumes)
- Context cancellation (clean shutdown)
- File change during branch switch (queued until resume)
- Multiple file changes (batched)
- Error handling (indexer fails, watcher fails)

**Success Criteria:**
- All tests pass
- Coordination works correctly
- No goroutine leaks
- Error handling robust

---

#### Agent 7: Indexer Core
**Files:**
- `internal/indexer/indexer_v2.go`
- `internal/indexer/indexer_v2_test.go`

**Dependencies:** ChangeDetector (Agent 1), Processor (Agent 2), BranchSynchronizer (Agent 5)

**Interface:**
```go
type Indexer struct {
    changeDetector ChangeDetector
    processor      Processor
    storage        Storage
    graphBuilder   GraphBuilder
}

func (idx *Indexer) Index(ctx context.Context, hint []string) (*Stats, error)
func (idx *Indexer) SwitchBranch(branch string) error
```

**Implementation Requirements:**
- Orchestrate: Detect → Delete → Update → Process → Graph
- Handle empty hint (full discovery)
- Handle non-empty hint (optimization)
- Update mtimes for unchanged files
- Delete files via storage
- Process changed files
- Update graph incrementally
- Graceful graph failure (log, continue)

**Tests:**
- Full index (no hint, all files new)
- Incremental (hint, some files changed)
- No changes (empty result)
- File added
- File modified
- File deleted
- Multiple operations (add + modify + delete)
- Context cancellation
- Graph failure (doesn't fail index)
- SwitchBranch (reconnect DB)

**Success Criteria:**
- All tests pass
- Fixes incremental indexing bug
- Single method handles all cases
- Error handling correct

---

### Phase 2: Integration

**Agent 8: CLI Integration**
**Files:**
- `internal/cli/index.go` (modify)
- `internal/cli/watch.go` (modify)

**Tasks:**
1. Add feature flag (`--use-v2-indexer` or env var)
2. Create factory function for v1 vs v2 indexer
3. Update `cortex index` to use new indexer when enabled
4. Update `cortex index --watch` to use WatchCoordinator when enabled
5. Integration tests for both CLI commands

**Success Criteria:**
- Both v1 and v2 work via feature flag
- All integration tests pass
- No breaking changes to CLI interface

---

### Phase 3: Cleanup

**Agent 9: Remove Old Code**
**Tasks:**
1. Remove old `IndexIncremental()` method
2. Remove old `processFiles()` helper
3. Remove old watcher implementation
4. Remove feature flag (v2 becomes default)
5. Rename `indexer_v2.go` → `indexer.go`
6. Update all references
7. Delete unused code

**Success Criteria:**
- All tests still pass
- No dead code remains
- Clean git history

---

## Testing Strategy

### Unit Tests (Per Component)
- Each component has comprehensive unit tests
- Mock dependencies where needed
- Test happy path + error cases
- Test edge cases (empty inputs, large inputs, cancellation)

### Integration Tests
- End-to-end CLI flows (`cortex index`, `cortex index --watch`)
- Cross-component interactions (coordinator → indexer → storage)
- Real filesystem operations (create/modify/delete files, watch events)
- Real git operations (branch switches)

### Performance Tests
- Benchmark change detection (10K files)
- Benchmark processing (1K files)
- Benchmark incremental updates (1% changed)
- Memory profiling (watch mode, long running)

### Compatibility Tests
- Existing projects still work
- Existing DBs still readable
- No breaking API changes
- Backward compatibility with v1 behavior

## Success Criteria

### Functional Requirements
✅ Single `Index()` method handles all cases
✅ Incremental indexing works (only processes changed files)
✅ Full indexing works (processes all files when hint empty)
✅ Watch mode works (auto-reindex on file changes)
✅ Branch switching works (auto-sync, reconnect DB)
✅ File watching pauses during branch switch
✅ Mtime optimization works (skip hash for unchanged mtime)
✅ Hash verification works (detect mtime drift)

### Non-Functional Requirements
✅ All tests pass (unit + integration)
✅ No goroutine leaks
✅ Clean shutdown (all watchers stop)
✅ Error handling robust (no panics)
✅ Performance equivalent or better than v1
✅ Code coverage ≥ 90% for new code

### Quality Requirements
✅ Clear separation of concerns
✅ Single responsibility per component
✅ Testable components (mockable interfaces)
✅ No hidden side effects
✅ Clean error messages
✅ Good logging (progress visibility)

## Rollout Plan

### Step 1: Feature Flag Release
- Ship v2 behind feature flag
- Early adopters test with `--use-v2-indexer`
- Collect feedback, fix bugs
- Monitor performance, memory usage

### Step 2: Default to v2
- Make v2 default, keep v1 accessible via flag
- Wider testing
- Address any regressions

### Step 3: Remove v1
- Delete old code
- Remove feature flag
- v2 is the only implementation

### Step 4: Performance Tuning
- Profile and optimize hot paths
- Reduce memory allocations
- Improve concurrency if needed

## Open Questions

1. **Debounce timing**: Is 500ms the right debounce window for FileWatcher?
2. **Pause queue size**: Should FileWatcher have a max queue size during pause?
3. **Branch switch cancellation**: Should in-progress indexing be cancelled on branch switch?
4. **Concurrent indexing**: Should we prevent concurrent Index() calls, or allow them?
5. **Graph rebuild**: When should we do full graph rebuild vs incremental update?

## References

- Current implementation: `internal/indexer/impl.go`
- SQLite storage: `specs/2025-10-30_sqlite-cache-storage.md`
- Existing interfaces: `internal/indexer/indexer.go`, `internal/indexer/storage_interface.go`
- Watch implementation: `internal/indexer/watcher.go`
