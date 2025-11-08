---
status: draft
started_at: 2025-11-06T00:00:00Z
completed_at: null
dependencies: []
---

# Asynchronous Embedding Indexing

## Purpose

Improve indexer responsiveness by decoupling slow embedding generation from fast file/graph indexing. This allows keyword search and graph queries to become available in seconds rather than waiting minutes for embeddings to complete.

## Core Concept

**Input**: Changed files requiring indexing

**Process**: Split indexing into two phases separated by graph updates
1. Phase 1: Write file stats + FTS content (fast, ~3s)
2. Graph update (fast, ~3s, happens between phases)
3. Phase 2: Parse, chunk, embed, write chunks (slow, ~60s)

**Output**: Non-vector tools ready in 6s, vector search ready after full completion

## Technology Stack

- **Language**: Go 1.25+
- **Pattern**: Sequential method calls (no goroutines, no WaitGroups)
- **Storage**: Existing SQLite with FTS5 and sqlite-vec

## Architecture

### Current Flow (Sequential)

```
ProcessFiles():
  ├─ Write file stats (3s)
  ├─ Write FTS content (1s)
  ├─ Parse + chunk + embed code files (40s) ← BLOCKS
  ├─ Parse + chunk + embed doc files (20s)  ← BLOCKS
  └─ Write chunks (1s)

Graph update (3s)

Total: 68s until any tool works
```

### New Flow (Decoupled)

```
ProcessFiles():
  ├─ Write file stats (3s)
  └─ Write FTS content (1s)
     → cortex_exact, cortex_files READY

Graph update (3s)
     → cortex_graph READY

ProcessEmbeddings():
  ├─ Parse + chunk + embed code files (40s)
  ├─ Parse + chunk + embed doc files (20s)
  └─ Write chunks (1s)
     → cortex_search READY

Total: 68s, but 90% of tools ready after 6s
```

## Implementation

### Processor Interface

```go
// internal/indexer/storage_interface.go

type Processor interface {
    // ProcessFiles writes file stats and FTS content only
    ProcessFiles(ctx context.Context, files []string) (*Stats, error)

    // ProcessEmbeddings handles parsing, chunking, embedding, and chunk writing
    ProcessEmbeddings(ctx context.Context, files []string) error
}
```

### ProcessFiles (Modified)

```go
// internal/indexer/processor.go

func (p *processor) ProcessFiles(ctx context.Context, files []string) (*Stats, error) {
    // Phase 1: Collect file metadata
    fileStatsList := []FileStats{}
    for _, filePath := range files {
        stats := p.collectFileMetadata(filePath)
        fileStatsList = append(fileStatsList, stats)
    }

    // Phase 2: Write file stats to SQLite
    if err := p.storage.WriteFileStatsBatch(fileStatsList); err != nil {
        return nil, fmt.Errorf("failed to write file stats: %w", err)
    }

    // Phase 3: Write FTS content
    if err := p.writeFTSContent(ctx, files); err != nil {
        return nil, fmt.Errorf("failed to write FTS content: %w", err)
    }

    // No chunk processing here anymore

    return &Stats{FilesProcessed: len(files)}, nil
}
```

### ProcessEmbeddings (New)

```go
// internal/indexer/processor.go

func (p *processor) ProcessEmbeddings(ctx context.Context, files []string) error {
    // Split files by type
    codeFiles, docFiles := p.splitFilesByType(files)

    // Process code files (parse, chunk, embed, write)
    // processCodeFiles is UNCHANGED - still embeds internally
    _, _, _, err := p.processCodeFiles(ctx, codeFiles)
    if err != nil {
        return fmt.Errorf("failed to process code files: %w", err)
    }

    // Process doc files (chunk, embed, write)
    // processDocFiles is UNCHANGED - still embeds internally
    _, err = p.processDocFiles(ctx, docFiles)
    if err != nil {
        return fmt.Errorf("failed to process doc files: %w", err)
    }

    return nil
}
```

### IndexerV2 Orchestration

```go
// internal/indexer/indexer_v2.go

func (idx *IndexerV2) Index(ctx context.Context) error {
    // ... change detection ...

    log.Printf("Processing %d changed files...", len(changedFiles))

    // Phase 1: File stats + FTS (fast)
    stats, err := idx.processor.ProcessFiles(ctx, changedFiles)
    if err != nil {
        return fmt.Errorf("failed to process files: %w", err)
    }

    log.Printf("✓ Indexed %d files", stats.FilesProcessed)
    log.Printf("✓ Keyword search ready (cortex_exact, cortex_files)")

    // Phase 2: Graph update (fast, happens before embeddings)
    if idx.graphUpdater != nil {
        log.Printf("Updating code graph...")
        if err := idx.graphUpdater.Update(ctx, changes); err != nil {
            log.Printf("Warning: graph update failed: %v", err)
        } else {
            log.Printf("✓ Graph updated (cortex_graph ready)")
        }
    }

    // Phase 3: Embeddings (slow)
    log.Printf("Generating embeddings...")
    if err := idx.processor.ProcessEmbeddings(ctx, changedFiles); err != nil {
        return fmt.Errorf("failed to process embeddings: %w", err)
    }

    log.Printf("✓ Embeddings complete (cortex_search ready)")
    log.Printf("✓ All indexing complete")

    return nil
}
```

## User Experience

**Before:**
```
Processing 500 files...
[68 second pause - no output]
✓ Indexed 500 files
```

**After:**
```
Processing 500 changed files...
✓ Indexed 500 files
✓ Keyword search ready (cortex_exact, cortex_files)
Updating code graph...
✓ Graph updated (cortex_graph ready)
Generating embeddings...
[embedding progress logs]
✓ Embeddings complete (cortex_search ready)
✓ All indexing complete
```

## Performance Characteristics

**Tool Availability:**
- cortex_exact: 3s (was 68s) - **22x faster**
- cortex_files: 3s (was 68s) - **22x faster**
- cortex_graph: 6s (was 68s) - **11x faster**
- cortex_search: 68s (unchanged)

**Total indexing time:** Unchanged (~68s)

**Resource usage:** Unchanged (same operations, different order)

## Non-Goals

- Async/parallel embedding generation (out of scope)
- Caching parsed chunks between phases (unnecessary complexity)
- Progress streaming for embeddings (existing progress logging sufficient)

## Testing Strategy

**Unit tests:**
- Verify ProcessFiles writes file stats and FTS only
- Verify ProcessEmbeddings writes chunks with embeddings
- Test error handling in each phase

**Integration tests:**
- Run full Index() flow, verify all tables populated correctly
- Test with branch switches (many files changed)
- Verify graph queries work before embeddings complete

**Manual testing:**
- Index project, immediately try cortex_exact (should work at 3s)
- Index project, immediately try cortex_graph (should work at 6s)
- Verify cortex_search works after full completion

## Migration Path

No breaking changes:
- Processor interface adds new method (backward compatible)
- Existing processCodeFiles/processDocFiles unchanged
- IndexerV2 calls methods in different order (no external API change)

## References

- Existing code: `internal/indexer/processor.go`
- Existing code: `internal/indexer/indexer_v2.go`
- Related: Indexer daemon spec (`2025-11-05_indexer-daemon.md`)
