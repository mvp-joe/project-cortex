package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ChunkManager coordinates chunk loading across multiple searchers.
// It provides a shared abstraction for loading, tracking, and managing
// code/documentation chunks, eliminating duplicate chunk loading/deserialization.
type ChunkManager struct {
	projectPath    string       // Project root path (for SQLite cache lookup)
	chunksDir      string       // Legacy JSON chunks directory (fallback)
	current        *ChunkSet    // Read-only after creation
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

// NewChunkManager creates a new chunk manager for the specified chunks directory.
// DEPRECATED: Use NewChunkManagerWithProject for SQLite support.
func NewChunkManager(chunksDir string) *ChunkManager {
	return &ChunkManager{
		projectPath:    "", // Empty = JSON-only mode
		chunksDir:      chunksDir,
		lastReloadTime: time.Time{}, // Zero time forces full load on first call
	}
}

// NewChunkManagerWithProject creates a new chunk manager with SQLite support.
// Prefers SQLite cache, falls back to JSON if not available.
func NewChunkManagerWithProject(projectPath, chunksDir string) *ChunkManager {
	return &ChunkManager{
		projectPath:    projectPath,
		chunksDir:      chunksDir,
		lastReloadTime: time.Time{}, // Zero time forces full load on first call
	}
}

// Load reads all chunk files and returns a new ChunkSet.
// Thread-safe for concurrent calls (each gets independent ChunkSet).
//
// Loading strategy:
// - If projectPath is set: Try SQLite first, fallback to JSON
// - If projectPath is empty: JSON-only mode (backward compatibility)
func (cm *ChunkManager) Load(ctx context.Context) (*ChunkSet, error) {
	// Check cancellation before starting
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	var allChunks []*ContextChunk
	var err error

	// Decide loading strategy based on whether projectPath is set
	if cm.projectPath != "" {
		// SQLite-first mode (with JSON fallback)
		allChunks, err = LoadChunksAuto(cm.projectPath, cm.chunksDir)
		if err != nil {
			return nil, fmt.Errorf("failed to load chunks: %w", err)
		}
	} else {
		// JSON-only mode (legacy)
		allChunks, err = cm.loadChunksFromJSON(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to load chunks from JSON: %w", err)
		}
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

// loadChunksFromJSON loads chunks from individual JSON files (legacy method).
// Used when projectPath is not set or as fallback for SQLite.
func (cm *ChunkManager) loadChunksFromJSON(ctx context.Context) ([]*ContextChunk, error) {
	// Load all chunk files
	symbolChunks, err := cm.loadChunkFile(ctx, "code-symbols.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load symbols: %w", err)
	}

	defChunks, err := cm.loadChunkFile(ctx, "code-definitions.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load definitions: %w", err)
	}

	dataChunks, err := cm.loadChunkFile(ctx, "code-data.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load data: %w", err)
	}

	docChunks, err := cm.loadChunkFile(ctx, "doc-chunks.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load documentation: %w", err)
	}

	// Combine all chunks
	allChunks := make([]*ContextChunk, 0, len(symbolChunks)+len(defChunks)+len(dataChunks)+len(docChunks))
	allChunks = append(allChunks, symbolChunks...)
	allChunks = append(allChunks, defChunks...)
	allChunks = append(allChunks, dataChunks...)
	allChunks = append(allChunks, docChunks...)

	return allChunks, nil
}

// loadChunkFile loads a single chunk file from the chunks directory.
// Returns empty slice if file doesn't exist (valid for new projects).
// Returns error if file is corrupted or invalid.
func (cm *ChunkManager) loadChunkFile(ctx context.Context, filename string) ([]*ContextChunk, error) {
	// Check cancellation before each file
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	path := filepath.Join(cm.chunksDir, filename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing file is valid for new projects
			return []*ContextChunk{}, nil
		}
		return nil, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	var wrapper ChunkFileWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("corrupted chunk file %s: %w", filename, err)
	}

	return wrapper.Chunks, nil
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
