package mcp

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ChunkManager coordinates chunk loading across multiple searchers.
// It provides a shared abstraction for loading, tracking, and managing
// code/documentation chunks, eliminating duplicate chunk loading/deserialization.
//
// All chunks are loaded from SQLite cache (no legacy JSON support).
type ChunkManager struct {
	projectPath    string    // Project root path (for SQLite cache lookup)
	current        *ChunkSet // Read-only after creation
	lastReloadTime time.Time
	mu             sync.RWMutex // Protects current and lastReloadTime
}

// ChunkSet is an immutable collection of chunks with fast lookups.
// Once created, it should not be modified.
type ChunkSet struct {
	chunks []*ContextChunk            // All chunks
	byID   map[string]*ContextChunk   // Fast lookup by ID
	byFile map[string][]*ContextChunk // Fast lookup by file path
}

// NewChunkManager creates a new chunk manager for the specified project.
// All chunks are loaded from SQLite cache.
func NewChunkManager(projectPath string) *ChunkManager {
	return &ChunkManager{
		projectPath:    projectPath,
		lastReloadTime: time.Time{}, // Zero time forces full load on first call
	}
}

// Load reads all chunks from SQLite cache and returns a new ChunkSet.
// Thread-safe for concurrent calls (each gets independent ChunkSet).
func (cm *ChunkManager) Load(ctx context.Context) (*ChunkSet, error) {
	// Check cancellation before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Load chunks from SQLite cache
	allChunks, err := LoadChunksFromSQLite(cm.projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load chunks from SQLite: %w", err)
	}

	// Build indexes
	byID := make(map[string]*ContextChunk, len(allChunks))
	byFile := make(map[string][]*ContextChunk)

	for _, chunk := range allChunks {
		byID[chunk.ID] = chunk

		// Extract file path from metadata
		if filePath, ok := chunk.Metadata["file_path"].(string); ok {
			byFile[filePath] = append(byFile[filePath], chunk)
		}
	}

	// Return immutable ChunkSet
	return &ChunkSet{
		chunks: allChunks,
		byID:   byID,
		byFile: byFile,
	}, nil
}

// GetCurrent returns the current ChunkSet (thread-safe read).
// Returns nil if no chunks have been loaded yet.
func (cm *ChunkManager) GetCurrent() *ChunkSet {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.current
}

// Update atomically swaps to new ChunkSet and updates reload time.
// Old ChunkSet becomes eligible for GC once no references remain.
func (cm *ChunkManager) Update(newSet *ChunkSet, reloadTime time.Time) {
	cm.mu.Lock()
	cm.current = newSet
	cm.lastReloadTime = reloadTime
	cm.mu.Unlock()
}

// GetLastReloadTime returns when chunks were last loaded (thread-safe).
func (cm *ChunkManager) GetLastReloadTime() time.Time {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.lastReloadTime
}

// DetectChanges compares new ChunkSet against current to find modifications.
// Uses timestamp-based detection: chunks with UpdatedAt > lastReloadTime are considered updated.
// Returns: added chunks (new IDs), updated chunks (modified since last reload), deleted chunk IDs.
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

// ChunkSet methods

// GetByID retrieves a chunk by ID (O(1) lookup).
// Returns nil if chunk not found.
func (cs *ChunkSet) GetByID(id string) *ContextChunk {
	if cs == nil {
		return nil
	}
	return cs.byID[id]
}

// GetByFile retrieves all chunks for a file (O(1) lookup).
// Returns nil if no chunks found for the file.
func (cs *ChunkSet) GetByFile(filePath string) []*ContextChunk {
	if cs == nil {
		return nil
	}
	return cs.byFile[filePath]
}

// All returns all chunks (read-only slice).
// The returned slice should not be modified.
func (cs *ChunkSet) All() []*ContextChunk {
	if cs == nil {
		return nil
	}
	return cs.chunks
}

// Len returns total chunk count.
func (cs *ChunkSet) Len() int {
	if cs == nil {
		return 0
	}
	return len(cs.chunks)
}
