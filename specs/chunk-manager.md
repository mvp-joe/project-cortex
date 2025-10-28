---
status: planned
dependencies: [indexer]
---

# Chunk Manager Specification

## Purpose

The ChunkManager provides a shared abstraction for loading, tracking, and managing code/documentation chunks across multiple search implementations (vector search, full-text search). It eliminates duplicate chunk loading/deserialization and provides efficient change detection for incremental updates.

## Core Concept

**Problem**: Multiple searchers (chromem vector DB, bleve text search) need access to the same chunk data. Loading and deserializing chunks separately for each searcher wastes CPU (~30ms) and creates consistency issues during hot reload.

**Solution**: Single ChunkManager loads chunks once, provides read-only ChunkSet to all searchers, tracks changes for incremental updates.

```
┌─────────────────┐
│  Chunk Files    │  .cortex/chunks/*.json
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│  ChunkManager   │  Load once, share everywhere
└────────┬────────┘
         │
         ├──────────────┬──────────────┐
         ▼              ▼
┌──────────────┐ ┌──────────────┐
│   chromem    │ │    bleve     │
│   Searcher   │ │   Searcher   │
└──────────────┘ └──────────────┘
```

**Note**: Graph searcher uses separate data (`.cortex/graph/code-graph.json`), not chunks.

## Technology Stack

- **Language**: Go 1.25+
- **Concurrency**: `sync.RWMutex` for atomic swap
- **File I/O**: Standard library `encoding/json`
- **Change Detection**: Timestamp-based using `updated_at` field

## Architecture

### Core Types

```go
// ChunkManager coordinates chunk loading across multiple searchers
type ChunkManager struct {
    chunksDir      string
    current        *ChunkSet    // Read-only after creation
    lastReloadTime time.Time
    mu             sync.RWMutex // Protects current and lastReloadTime
}

// ChunkSet is an immutable collection of chunks
type ChunkSet struct {
    chunks []*ContextChunk         // All chunks
    byID   map[string]*ContextChunk // Fast lookup by ID
    byFile map[string][]*ContextChunk // Fast lookup by file path
}

// ContextChunk represents a searchable code or documentation chunk
// (Defined in existing codebase, included here for reference)
type ContextChunk struct {
    ID        string                 `json:"id"`
    Title     string                 `json:"title"`
    Text      string                 `json:"text"`
    ChunkType string                 `json:"chunk_type"` // symbols, definitions, data, documentation
    Embedding []float32              `json:"embedding,omitempty"`
    Tags      []string               `json:"tags,omitempty"`
    Metadata  map[string]interface{} `json:"metadata,omitempty"`
    CreatedAt time.Time              `json:"created_at"`
    UpdatedAt time.Time              `json:"updated_at"`
}
```

### Public Interface

```go
// NewChunkManager creates a new chunk manager
func NewChunkManager(chunksDir string) *ChunkManager {
    return &ChunkManager{
        chunksDir:      chunksDir,
        lastReloadTime: time.Time{}, // Zero time forces full load on first call
    }
}

// Load reads all chunk files and returns a new ChunkSet
// Thread-safe for concurrent calls (each gets independent ChunkSet)
func (cm *ChunkManager) Load(ctx context.Context) (*ChunkSet, error)

// GetCurrent returns the current ChunkSet (thread-safe read)
func (cm *ChunkManager) GetCurrent() *ChunkSet

// Update atomically swaps to new ChunkSet and updates reload time
func (cm *ChunkManager) Update(newSet *ChunkSet, reloadTime time.Time)

// DetectChanges compares new ChunkSet against current to find modifications
// Returns: added, updated, deleted chunks
func (cm *ChunkManager) DetectChanges(newSet *ChunkSet) (
    added []*ContextChunk,
    updated []*ContextChunk,
    deleted []string,
)

// GetLastReloadTime returns when chunks were last loaded (thread-safe)
func (cm *ChunkManager) GetLastReloadTime() time.Time
```

### ChunkSet Methods

```go
// GetByID retrieves chunk by ID (O(1) lookup)
func (cs *ChunkSet) GetByID(id string) *ContextChunk

// GetByFile retrieves all chunks for a file (O(1) lookup)
func (cs *ChunkSet) GetByFile(filePath string) []*ContextChunk

// All returns all chunks (read-only slice)
func (cs *ChunkSet) All() []*ContextChunk

// Len returns total chunk count
func (cs *ChunkSet) Len() int
```

## Loading Strategy

### Initial Load

```go
func (cm *ChunkManager) Load(ctx context.Context) (*ChunkSet, error) {
    // 1. Read all chunk files
    symbolChunks, err := loadChunkFile(filepath.Join(cm.chunksDir, "code-symbols.json"))
    defChunks, err := loadChunkFile(filepath.Join(cm.chunksDir, "code-definitions.json"))
    dataChunks, err := loadChunkFile(filepath.Join(cm.chunksDir, "code-data.json"))
    docChunks, err := loadChunkFile(filepath.Join(cm.chunksDir, "doc-chunks.json"))

    // 2. Combine all chunks
    allChunks := append(symbolChunks, defChunks...)
    allChunks = append(allChunks, dataChunks...)
    allChunks = append(allChunks, docChunks...)

    // 3. Build indexes
    byID := make(map[string]*ContextChunk, len(allChunks))
    byFile := make(map[string][]*ContextChunk)

    for _, chunk := range allChunks {
        byID[chunk.ID] = chunk

        // Extract file path from metadata
        if filePath, ok := chunk.Metadata["file_path"].(string); ok {
            byFile[filePath] = append(byFile[filePath], chunk)
        }
    }

    // 4. Return immutable ChunkSet
    return &ChunkSet{
        chunks: allChunks,
        byID:   byID,
        byFile: byFile,
    }, nil
}

// Helper: load single chunk file
func loadChunkFile(path string) ([]*ContextChunk, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        if os.IsNotExist(err) {
            return nil, nil // Empty set if file doesn't exist yet
        }
        return nil, fmt.Errorf("failed to read %s: %w", path, err)
    }

    var wrapper struct {
        Chunks []*ContextChunk `json:"chunks"`
    }

    if err := json.Unmarshal(data, &wrapper); err != nil {
        return nil, fmt.Errorf("failed to parse %s: %w", path, err)
    }

    return wrapper.Chunks, nil
}
```

### Atomic Update

```go
func (cm *ChunkManager) Update(newSet *ChunkSet, reloadTime time.Time) {
    cm.mu.Lock()
    cm.current = newSet
    cm.lastReloadTime = reloadTime
    cm.mu.Unlock()
    // Old ChunkSet eligible for GC once no references remain
}
```

**Key properties**:
- Lock held for ~1µs (just pointer swap)
- No expensive operations inside critical section
- Old ChunkSet freed by GC once searchers release references

### Thread-Safe Read

```go
func (cm *ChunkManager) GetCurrent() *ChunkSet {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    return cm.current
}

func (cm *ChunkManager) GetLastReloadTime() time.Time {
    cm.mu.RLock()
    defer cm.mu.RUnlock()
    return cm.lastReloadTime
}
```

## Change Detection

### Timestamp-Based Detection

```go
func (cm *ChunkManager) DetectChanges(newSet *ChunkSet) (
    added []*ContextChunk,
    updated []*ContextChunk,
    deleted []string,
) {
    cm.mu.RLock()
    oldSet := cm.current
    lastReload := cm.lastReloadTime
    cm.mu.RUnlock()

    // If no old set, everything is new
    if oldSet == nil {
        return newSet.All(), nil, nil
    }

    // Build set of new IDs for quick lookup
    newIDs := make(map[string]bool, len(newSet.byID))
    for id := range newSet.byID {
        newIDs[id] = true
    }

    // Find added and updated chunks
    for _, newChunk := range newSet.All() {
        oldChunk := oldSet.GetByID(newChunk.ID)

        if oldChunk == nil {
            // New chunk
            added = append(added, newChunk)
        } else if newChunk.UpdatedAt.After(lastReload) {
            // Chunk was modified since last reload
            updated = append(updated, newChunk)
        }
        // else: unchanged (UpdatedAt <= lastReload)
    }

    // Find deleted chunks
    for id := range oldSet.byID {
        if !newIDs[id] {
            deleted = append(deleted, id)
        }
    }

    return added, updated, deleted
}
```

**Why timestamp-based?**
- ✅ O(n) complexity (linear scan, fast)
- ✅ No embedding comparison (avoiding 384-float comparison per chunk)
- ✅ Leverages existing `UpdatedAt` field from chunks
- ✅ Simple and correct

**Alternative considered (rejected)**:
- Content hashing: Requires computing hash over text + embedding (~10ms for 10K chunks)
- Embedding comparison: Expensive (384 floats × 10K chunks = 3.84M comparisons)
- Checksum field: Would require adding new field to chunk schema

## Integration with Searchers

### Pattern: Searcher Owns Its Index

```go
type VectorSearcher struct {
    chunkManager *ChunkManager
    collection   *chromem.Collection // chromem-go vector index
    mu           sync.RWMutex
}

func (vs *VectorSearcher) Reload(ctx context.Context) error {
    // 1. Load chunks (shared operation, ~30ms)
    newSet, err := vs.chunkManager.Load(ctx)
    if err != nil {
        return fmt.Errorf("failed to load chunks: %w", err)
    }

    // 2. Detect changes (~5ms)
    added, updated, deleted := vs.chunkManager.DetectChanges(newSet)

    // 3. Update THIS searcher's index incrementally (~10ms)
    newCollection, err := vs.updateVectorIndex(ctx, added, updated, deleted)
    if err != nil {
        return fmt.Errorf("failed to update vector index: %w", err)
    }

    // 4. Atomic swap (both ChunkManager and searcher's index)
    vs.chunkManager.Update(newSet, time.Now())

    vs.mu.Lock()
    vs.collection = newCollection
    vs.mu.Unlock()

    return nil
}
```

### Pattern: Coordinated Multi-Searcher Reload

```go
type SearchCoordinator struct {
    chunkManager   *ChunkManager
    vectorSearcher *VectorSearcher
    textSearcher   *TextSearcher
}

func (sc *SearchCoordinator) Reload(ctx context.Context) error {
    // 1. Load chunks ONCE (shared)
    newSet, err := sc.chunkManager.Load(ctx)
    if err != nil {
        return fmt.Errorf("failed to load chunks: %w", err)
    }

    // 2. Detect changes ONCE
    added, updated, deleted := sc.chunkManager.DetectChanges(newSet)

    // 3. Update both indexes SEQUENTIALLY (simpler error handling)
    if err := sc.vectorSearcher.UpdateIncremental(ctx, added, updated, deleted); err != nil {
        return fmt.Errorf("vector index update failed: %w", err)
    }

    if err := sc.textSearcher.UpdateIncremental(ctx, added, updated, deleted); err != nil {
        return fmt.Errorf("text index update failed: %w", err)
        // Note: vectorSearcher already updated - eventual consistency is acceptable
        // Next reload will sync both to consistent state
    }

    // 4. Atomic swap ChunkManager reference
    sc.chunkManager.Update(newSet, time.Now())

    return nil
}
```

**Eventual consistency model**:
- If text index fails after vector succeeds, indexes are briefly inconsistent
- Next reload (triggered by file watcher) will sync both
- Acceptable for development tool with debounced reload (500ms)
- No transactional rollback needed

## Memory Management

### ChunkSet Lifecycle

```
┌──────────────────┐
│ Load() called    │
│ Deserialize JSON │  Allocates ~10MB for 10K chunks
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ ChunkSet created │  Immutable, heap-allocated
│ (read-only)      │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Update() swaps   │  Old ChunkSet reference replaced
│ ChunkManager ref │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Searchers query  │  May hold temporary references during query
│ new ChunkSet     │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│ Old ChunkSet GC  │  Freed when no references remain
│                  │  (typically <1 second after swap)
└──────────────────┘
```

**No memory leaks because**:
- ChunkSet is read-only (no internal mutation)
- Searchers don't cache chunks (reconstruct from indexes)
- Query references are stack-local (freed when function returns)
- Go GC automatically frees unreferenced ChunkSets

### Memory Overhead

| Component | Memory per 10K chunks |
|-----------|----------------------|
| ChunkSet structs | ~10MB |
| byID map | ~1MB |
| byFile map | ~1MB |
| **Total** | **~12MB** |

During reload, briefly holds two ChunkSets (~24MB), drops to 12MB after GC.

## Performance Characteristics

### Timing Breakdown (10K chunks)

| Operation | Time | Notes |
|-----------|------|-------|
| Load 4 JSON files | ~20ms | Disk I/O + deserialization |
| Build indexes (byID, byFile) | ~10ms | Hash map construction |
| Detect changes | ~5ms | Timestamp comparison |
| Atomic swap | ~1µs | Pointer assignment |
| **Total reload** | **~35ms** | 10x faster than rebuilding indexes |

### Scalability

| Chunks | Load Time | Memory | Change Detection |
|--------|-----------|--------|------------------|
| 1K     | ~5ms      | ~1MB   | <1ms             |
| 10K    | ~35ms     | ~12MB  | ~5ms             |
| 100K   | ~250ms    | ~120MB | ~50ms            |

### Comparison to Full Rebuild

**Without ChunkManager** (old approach):
- chromem loads chunks: 30ms
- chromem rebuilds vector index: 200ms
- bleve loads chunks: 30ms
- bleve rebuilds text index: 100ms
- **Total: ~360ms** (serial), or ~230ms (parallel with duplicate loading)

**With ChunkManager** (new approach):
- Load chunks once: 35ms
- Detect changes: 5ms
- chromem incremental update: 10ms
- bleve incremental update: 10ms
- **Total: ~60ms** (6x faster)

## Error Handling

### Load Failures

```go
func (cm *ChunkManager) Load(ctx context.Context) (*ChunkSet, error) {
    // Missing file: return empty set (valid for new projects)
    if os.IsNotExist(err) {
        return &ChunkSet{
            chunks: []*ContextChunk{},
            byID:   make(map[string]*ContextChunk),
            byFile: make(map[string][]*ContextChunk),
        }, nil
    }

    // JSON parse error: return error (don't proceed with corrupted data)
    if err := json.Unmarshal(data, &wrapper); err != nil {
        return nil, fmt.Errorf("corrupted chunk file %s: %w", path, err)
    }

    return chunkSet, nil
}
```

### Partial Reload Failures

**Scenario**: Vector index updates successfully, text index fails.

**Behavior**:
- Vector searcher serves new data
- Text searcher serves old data
- System logs error
- Next reload (triggered by file watcher or manual) syncs both

**Rationale**:
- Eventual consistency acceptable for development tool
- No rollback complexity
- Self-healing on next reload

### Context Cancellation

```go
func (cm *ChunkManager) Load(ctx context.Context) (*ChunkSet, error) {
    // Check cancellation before each file
    select {
    case <-ctx.Done():
        return nil, ctx.Err()
    default:
    }

    chunks, err := loadChunkFile(path)
    // ...
}
```

## Testing Strategy

### Unit Tests

```go
func TestChunkManager_Load(t *testing.T) {
    // Test loading all chunk types
    // Test missing files (should return empty set)
    // Test corrupted JSON (should return error)
}

func TestChunkManager_DetectChanges(t *testing.T) {
    // Test added chunks (new IDs)
    // Test updated chunks (timestamp after lastReload)
    // Test deleted chunks (ID in old, not in new)
    // Test unchanged chunks (timestamp before lastReload)
}

func TestChunkManager_ThreadSafety(t *testing.T) {
    // Concurrent GetCurrent() calls
    // Concurrent Load() + GetCurrent()
    // Update() during GetCurrent()
}
```

### Integration Tests

```go
func TestChunkManager_WithSearchers(t *testing.T) {
    // Load chunks
    // Create two searchers (vector + text)
    // Both use same ChunkManager
    // Verify no duplicate loading
    // Update chunks
    // Reload via ChunkManager
    // Verify both searchers see new data
}
```

### Benchmark Tests

```go
func BenchmarkChunkManager_Load(b *testing.B) {
    // Measure load time for 1K, 10K, 100K chunks
}

func BenchmarkChunkManager_DetectChanges(b *testing.B) {
    // Measure change detection for various sizes
}
```

## Configuration

No configuration needed—ChunkManager uses existing chunk file format and locations.

## Future Enhancements

### Phase 2: Optional Optimizations

1. **Incremental file loading**: Only reload modified chunk files (requires file watcher integration)
2. **Memory-mapped files**: Use mmap for large chunk files (>100MB)
3. **Concurrent file loading**: Load 4 chunk files in parallel (4x speedup)
4. **Chunk compression**: Store chunks compressed, decompress in memory (trade CPU for disk I/O)

### Non-Goals

- **Persistent cache**: ChunkManager doesn't cache to disk (chunks already on disk)
- **Query caching**: Searchers own their query caching strategies
- **Distributed loading**: Single-machine design (MCP server is local)

## Integration with MCP Server

### Hot Reload Flow

```
1. File watcher detects chunk file change
   ↓
2. Debounce 500ms (wait for all files to finish writing)
   ↓
3. Call SearchCoordinator.Reload()
   ↓
4. ChunkManager.Load() - deserialize once
   ↓
5. ChunkManager.DetectChanges() - find diffs
   ↓
6. VectorSearcher.UpdateIncremental() - update chromem
   ↓
7. TextSearcher.UpdateIncremental() - update bleve
   ↓
8. ChunkManager.Update() - atomic swap
   ↓
9. MCP server continues serving queries with new data
```

### File Watcher Integration

```go
func (w *FileWatcher) onChunksChanged() {
    // Existing debounce logic (500ms)
    w.debounceTimer.Reset(500 * time.Millisecond)
}

func (w *FileWatcher) reload() {
    // Instead of reloading each searcher separately:
    // OLD: w.vectorSearcher.Reload(); w.textSearcher.Reload()

    // NEW: Single coordinated reload
    if err := w.coordinator.Reload(context.Background()); err != nil {
        log.Printf("Reload failed: %v", err)
        w.metrics.RecordReloadError(err)
        return
    }

    log.Printf("✓ Reloaded %d chunks", w.chunkManager.GetCurrent().Len())
}
```

## Dependencies

- **Upstream**: Indexer writes chunk files
- **Downstream**: Vector searcher (chromem), text searcher (bleve)

## Success Metrics

- ✅ Single chunk deserialization per reload (verify via metrics)
- ✅ <50ms reload time for 10K chunks (vs 360ms without ChunkManager)
- ✅ No memory leaks (monitor heap size over time)
- ✅ Thread-safe (pass race detector: `go test -race`)

## References

- Current implementation: `internal/mcp/chromem_searcher.go` (existing reload pattern)
- Chunk format: `specs/indexer.md` (defines ContextChunk schema)
- MCP server: `specs/mcp-server.md` (hot reload architecture)
