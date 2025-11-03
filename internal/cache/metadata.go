package cache

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CacheMetadata tracks branch usage and size for eviction decisions.
// Stored in ~/.cortex/cache/{cacheKey}/metadata.json
type CacheMetadata struct {
	Version      string                     `json:"version"`       // Schema version "1.0.0"
	ProjectKey   string                     `json:"project_key"`   // Cache key from GetCacheKey()
	Branches     map[string]*BranchMetadata `json:"branches"`      // branch name → stats
	TotalSizeMB  float64                    `json:"total_size_mb"` // Total cache size
	LastEviction time.Time                  `json:"last_eviction"` // Last eviction run
}

// BranchMetadata tracks stats for a single branch database.
type BranchMetadata struct {
	LastAccessed time.Time `json:"last_accessed"` // Last index or MCP query
	SizeMB       float64   `json:"size_mb"`       // Database file size
	ChunkCount   int       `json:"chunk_count"`   // Number of chunks
	IsImmortal   bool      `json:"is_immortal"`   // main/master never evicted
}

// LoadMetadata reads metadata from ~/.cortex/cache/{cacheKey}/metadata.json.
// Returns empty metadata if file doesn't exist or is invalid.
func LoadMetadata(cacheDir string) (*CacheMetadata, error) {
	metadataPath := filepath.Join(cacheDir, "metadata.json")

	// If file doesn't exist, return empty metadata
	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			return newEmptyMetadata(cacheDir), nil
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata CacheMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		// If unmarshal fails, return empty metadata (graceful degradation)
		return newEmptyMetadata(cacheDir), nil
	}

	// Initialize branches map if nil
	if metadata.Branches == nil {
		metadata.Branches = make(map[string]*BranchMetadata)
	}

	return &metadata, nil
}

// newEmptyMetadata creates a new empty metadata object.
func newEmptyMetadata(cacheDir string) *CacheMetadata {
	// Extract cache key from path (last component)
	cacheKey := filepath.Base(cacheDir)

	return &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: cacheKey,
		Branches:   make(map[string]*BranchMetadata),
	}
}

// Save writes metadata to disk using atomic write (temp + rename).
func (m *CacheMetadata) Save(cacheDir string) error {
	metadataPath := filepath.Join(cacheDir, "metadata.json")

	// Ensure cache directory exists
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal with pretty printing for readability
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Atomic write: write to temp file, then rename
	tmpPath := metadataPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp metadata: %w", err)
	}

	if err := os.Rename(tmpPath, metadataPath); err != nil {
		// Clean up temp file on failure
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename metadata: %w", err)
	}

	return nil
}

// UpdateBranchStats updates or creates metadata for a branch.
// Marks main/master branches as immortal automatically.
func (m *CacheMetadata) UpdateBranchStats(branch string, sizeMB float64, chunkCount int) {
	// Ensure branches map is initialized
	if m.Branches == nil {
		m.Branches = make(map[string]*BranchMetadata)
	}

	// Get or create branch metadata
	branchMeta, exists := m.Branches[branch]
	if !exists {
		branchMeta = &BranchMetadata{}
		m.Branches[branch] = branchMeta
	}

	// Update stats
	branchMeta.LastAccessed = time.Now()
	branchMeta.SizeMB = sizeMB
	branchMeta.ChunkCount = chunkCount

	// Mark main/master as immortal
	if branch == "main" || branch == "master" {
		branchMeta.IsImmortal = true
	}

	// Recalculate total size
	m.recalculateTotalSize()
}

// recalculateTotalSize recalculates total cache size from all branches.
func (m *CacheMetadata) recalculateTotalSize() {
	total := 0.0
	for _, branchMeta := range m.Branches {
		total += branchMeta.SizeMB
	}
	m.TotalSizeMB = total
}

// RemoveBranch removes a branch from metadata and recalculates total size.
func (m *CacheMetadata) RemoveBranch(branch string) {
	delete(m.Branches, branch)
	m.recalculateTotalSize()
}

// GetBranchStats returns metadata for a branch, or nil if not found.
func (m *CacheMetadata) GetBranchStats(branch string) *BranchMetadata {
	return m.Branches[branch]
}

// GetBranchDBSize returns the size of a branch database file in MB.
// Returns 0 if file doesn't exist.
func GetBranchDBSize(cacheDir, branch string) float64 {
	dbPath := filepath.Join(cacheDir, "branches", branch+".db")
	info, err := os.Stat(dbPath)
	if err != nil {
		return 0
	}
	return float64(info.Size()) / (1024 * 1024) // bytes to MB
}
