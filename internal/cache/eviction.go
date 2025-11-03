package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// EvictionPolicy controls what gets evicted from the cache.
type EvictionPolicy struct {
	MaxAgeDays      int      // Delete branches older than this (default: 30)
	MaxSizeMB       float64  // Delete oldest until under this (default: 500)
	ProtectBranches []string // Never delete (default: ["main", "master"])
}

// DefaultEvictionPolicy returns the default eviction policy.
func DefaultEvictionPolicy() EvictionPolicy {
	return EvictionPolicy{
		MaxAgeDays:      30,
		MaxSizeMB:       500,
		ProtectBranches: []string{"main", "master"},
	}
}

// EvictionResult contains statistics about an eviction run.
type EvictionResult struct {
	EvictedBranches []string  // Names of evicted branches
	FreedMB         float64   // Total size freed in MB
	RemainingMB     float64   // Total size after eviction
	Duration        time.Duration
}

// EvictStaleBranches removes old/deleted branches from the cache.
// Returns list of evicted branch names and any error.
//
// Eviction criteria (in order):
//  1. Branches deleted in git (no longer exist)
//  2. Branches older than MaxAgeDays (not accessed recently)
//  3. Oldest branches if TotalSize > MaxSizeMB (LRU)
//
// Protected branches (main, master, or custom list) are never evicted.
func EvictStaleBranches(cacheDir, projectPath string, policy EvictionPolicy) (*EvictionResult, error) {
	startTime := time.Now()

	// Load metadata
	metadata, err := LoadMetadata(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load metadata: %w", err)
	}

	// Get list of git branches (local and remote)
	gitBranches, err := GetGitBranches(projectPath)
	if err != nil {
		// If git fails, don't evict (safer to keep data)
		log.Printf("Warning: Could not get git branches: %v", err)
		gitBranches = []string{}
	}

	// Normalize git branch names (strip "* ", "remotes/origin/", etc.)
	gitBranchSet := normalizeGitBranches(gitBranches)

	result := &EvictionResult{
		EvictedBranches: []string{},
		FreedMB:         0,
	}

	// Build list of eviction candidates
	candidates := buildEvictionCandidates(metadata, gitBranchSet, policy, projectPath)

	// Sort by priority (deleted first, then by last access time)
	sort.Slice(candidates, func(i, j int) bool {
		// Deleted branches first
		if candidates[i].deleted != candidates[j].deleted {
			return candidates[i].deleted
		}
		// Then by age (oldest first)
		return candidates[i].lastAccessed.Before(candidates[j].lastAccessed)
	})

	// Evict based on policy
	for _, candidate := range candidates {
		shouldEvict := false

		// Reason 1: Branch deleted in git
		if candidate.deleted {
			shouldEvict = true
		}

		// Reason 2: Branch too old (not accessed in MaxAgeDays)
		if !shouldEvict && policy.MaxAgeDays > 0 {
			age := time.Since(candidate.lastAccessed)
			if age > time.Duration(policy.MaxAgeDays)*24*time.Hour {
				shouldEvict = true
			}
		}

		// Reason 3: Cache too large (evict oldest)
		if !shouldEvict && policy.MaxSizeMB > 0 {
			if metadata.TotalSizeMB > policy.MaxSizeMB {
				shouldEvict = true
			}
		}

		if shouldEvict {
			if err := evictBranch(cacheDir, metadata, candidate.name); err != nil {
				log.Printf("Warning: Failed to evict branch %s: %v", candidate.name, err)
				continue
			}

			result.EvictedBranches = append(result.EvictedBranches, candidate.name)
			result.FreedMB += candidate.sizeMB
		}
	}

	// Update metadata with eviction timestamp
	metadata.LastEviction = time.Now()
	if err := metadata.Save(cacheDir); err != nil {
		return nil, fmt.Errorf("failed to save metadata: %w", err)
	}

	result.RemainingMB = metadata.TotalSizeMB
	result.Duration = time.Since(startTime)

	return result, nil
}

// evictionCandidate represents a branch that might be evicted.
type evictionCandidate struct {
	name         string
	lastAccessed time.Time
	sizeMB       float64
	deleted      bool // True if branch no longer exists in git
}

// buildEvictionCandidates identifies branches that could be evicted.
// Excludes protected branches, immortal branches, and the current branch.
func buildEvictionCandidates(
	metadata *CacheMetadata,
	gitBranches map[string]bool,
	policy EvictionPolicy,
	projectPath string,
) []evictionCandidate {
	candidates := []evictionCandidate{}
	protectedSet := make(map[string]bool)

	// Build protected set
	for _, branch := range policy.ProtectBranches {
		protectedSet[branch] = true
	}

	// Get current branch to protect from eviction
	currentBranch := GetCurrentBranch(projectPath)

	// Check each cached branch
	for branchName, branchMeta := range metadata.Branches {
		// Skip current branch (never evict the branch we're on)
		if branchName == currentBranch {
			continue
		}

		// Skip protected branches
		if protectedSet[branchName] {
			continue
		}

		// Skip immortal branches (main/master)
		if branchMeta.IsImmortal {
			continue
		}

		// Check if branch still exists in git
		deleted := !gitBranches[branchName]

		candidates = append(candidates, evictionCandidate{
			name:         branchName,
			lastAccessed: branchMeta.LastAccessed,
			sizeMB:       branchMeta.SizeMB,
			deleted:      deleted,
		})
	}

	return candidates
}

// evictBranch removes a branch database and updates metadata.
func evictBranch(cacheDir string, metadata *CacheMetadata, branchName string) error {
	// Delete database file
	dbPath := filepath.Join(cacheDir, "branches", branchName+".db")
	if err := os.Remove(dbPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete database: %w", err)
	}

	// Remove from metadata
	metadata.RemoveBranch(branchName)

	return nil
}

// normalizeGitBranches converts git branch output to a set of normalized branch names.
// Strips markers like "* ", "remotes/origin/", etc.
//
// Examples:
//   - "* main" → "main"
//   - "  feature-x" → "feature-x"
//   - "remotes/origin/develop" → "develop"
func normalizeGitBranches(gitBranches []string) map[string]bool {
	branchSet := make(map[string]bool)

	for _, branch := range gitBranches {
		// Strip leading "*" and whitespace
		normalized := strings.TrimSpace(branch)
		normalized = strings.TrimPrefix(normalized, "* ")
		normalized = strings.TrimSpace(normalized)

		// Strip remote prefixes
		if strings.HasPrefix(normalized, "remotes/origin/") {
			normalized = strings.TrimPrefix(normalized, "remotes/origin/")
		} else if strings.HasPrefix(normalized, "remotes/") {
			// Other remotes: skip
			continue
		}

		// Skip HEAD pointers
		if strings.Contains(normalized, "HEAD") {
			continue
		}

		if normalized != "" {
			branchSet[normalized] = true
		}
	}

	return branchSet
}

// GetCurrentBranchDB returns the path to the current branch's database.
// This is a convenience function for protecting the current branch from eviction.
func GetCurrentBranchDB(cacheDir, projectPath string) string {
	currentBranch := GetCurrentBranch(projectPath)
	return filepath.Join(cacheDir, "branches", currentBranch+".db")
}
