package mcp

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// SearcherCoordinator coordinates reload operations across multiple searchers.
// It ensures chunks are loaded once and updates are applied in parallel.
type SearcherCoordinator struct {
	chunkManager   *ChunkManager
	chromemSearcher *chromemSearcher // Vector search
	exactSearcher  ExactSearcher    // Text search
	metrics        *ReloadMetrics

	mu sync.Mutex // Protects reload operation (not queries)
}

// NewSearcherCoordinator creates a new coordinator with the given searchers.
func NewSearcherCoordinator(
	chunkManager *ChunkManager,
	chromemSearcher *chromemSearcher,
	exactSearcher ExactSearcher,
) *SearcherCoordinator {
	return &SearcherCoordinator{
		chunkManager:    chunkManager,
		chromemSearcher: chromemSearcher,
		exactSearcher:   exactSearcher,
		metrics:         NewReloadMetrics(),
	}
}

// Reload reloads chunks and updates both searchers incrementally.
// This is the single coordinated reload entry point.
// Implements Reloadable interface for FileWatcher compatibility.
func (sc *SearcherCoordinator) Reload(ctx context.Context) error {
	// Only one reload at a time (but queries can proceed concurrently)
	sc.mu.Lock()
	defer sc.mu.Unlock()

	startTime := time.Now()

	// 1. Load chunks ONCE (shared across both searchers)
	newSet, err := sc.chunkManager.Load(ctx)
	if err != nil {
		duration := time.Since(startTime)
		sc.metrics.RecordReload(duration, err, 0)
		return fmt.Errorf("failed to load chunks: %w", err)
	}

	// 2. Detect what changed
	added, updated, deleted := sc.chunkManager.DetectChanges(newSet)

	log.Printf("Change detection: %d added, %d updated, %d deleted",
		len(added), len(updated), len(deleted))

	// 3. Update both indexes in PARALLEL
	var wg sync.WaitGroup
	var chromemErr, exactErr error

	wg.Add(2)

	// Update chromem vector DB incrementally
	go func() {
		defer wg.Done()
		chromemErr = sc.chromemSearcher.UpdateIncremental(ctx, added, updated, deleted)
		if chromemErr != nil {
			log.Printf("chromem update failed: %v", chromemErr)
		}
	}()

	// Update bleve text index incrementally
	go func() {
		defer wg.Done()
		exactErr = sc.exactSearcher.UpdateIncremental(ctx, added, updated, deleted)
		if exactErr != nil {
			log.Printf("bleve update failed: %v", exactErr)
		}
	}()

	wg.Wait()

	// 4. Handle errors (eventual consistency model)
	// If one fails, the other may have succeeded - system self-heals on next reload
	if chromemErr != nil {
		duration := time.Since(startTime)
		sc.metrics.RecordReload(duration, chromemErr, 0)
		return fmt.Errorf("chromem index update failed: %w", chromemErr)
	}
	if exactErr != nil {
		duration := time.Since(startTime)
		sc.metrics.RecordReload(duration, exactErr, 0)
		return fmt.Errorf("bleve index update failed: %w", exactErr)
	}

	// 5. Swap chunk manager reference (atomic)
	sc.chunkManager.Update(newSet, time.Now())

	// Record successful reload
	duration := time.Since(startTime)
	chunkCount := newSet.Len()
	sc.metrics.RecordReload(duration, nil, chunkCount)

	log.Printf("✓ Reloaded %d chunks in %v", chunkCount, duration)

	return nil
}

// GetChromemSearcher returns the chromem searcher (for MCP tool registration).
func (sc *SearcherCoordinator) GetChromemSearcher() ContextSearcher {
	return sc.chromemSearcher
}

// GetExactSearcher returns the exact searcher (for MCP tool registration).
func (sc *SearcherCoordinator) GetExactSearcher() ExactSearcher {
	return sc.exactSearcher
}

// GetMetrics returns current reload operation metrics.
func (sc *SearcherCoordinator) GetMetrics() MetricsSnapshot {
	return sc.metrics.GetMetrics()
}

// Close releases resources held by all searchers.
func (sc *SearcherCoordinator) Close() error {
	var errs []error

	if sc.chromemSearcher != nil {
		if err := sc.chromemSearcher.Close(); err != nil {
			errs = append(errs, fmt.Errorf("chromem close failed: %w", err))
		}
	}

	if sc.exactSearcher != nil {
		if err := sc.exactSearcher.Close(); err != nil {
			errs = append(errs, fmt.Errorf("exact close failed: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}