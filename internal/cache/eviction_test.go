package cache

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan:
// 1. Test DefaultEvictionPolicy returns expected values
// 2. Test normalizeGitBranches handles various formats
// 3. Test buildEvictionCandidates excludes protected/immortal branches
// 4. Test buildEvictionCandidates identifies deleted branches
// 5. Test evictBranch removes database and updates metadata
// 6. Test EvictStaleBranches removes deleted branches
// 7. Test EvictStaleBranches removes old branches (MaxAgeDays)
// 8. Test EvictStaleBranches enforces size limit (MaxSizeMB)
// 9. Test EvictStaleBranches protects main/master
// 10. Test EvictStaleBranches protects custom branches
// 11. Test EvictStaleBranches handles git command failure gracefully
// 12. Test EvictStaleBranches returns correct statistics
// 13. Test GetCurrentBranchDB returns correct path

func TestDefaultEvictionPolicy(t *testing.T) {
	t.Parallel()

	policy := DefaultEvictionPolicy()
	assert.Equal(t, 30, policy.MaxAgeDays)
	assert.Equal(t, 500.0, policy.MaxSizeMB)
	assert.Equal(t, []string{"main", "master"}, policy.ProtectBranches)
}

func TestNormalizeGitBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected map[string]bool
	}{
		{
			name: "current branch marker",
			input: []string{
				"* main",
				"  feature-x",
			},
			expected: map[string]bool{
				"main":      true,
				"feature-x": true,
			},
		},
		{
			name: "remote branches",
			input: []string{
				"main",
				"remotes/origin/develop",
				"remotes/origin/feature-y",
			},
			expected: map[string]bool{
				"main":      true,
				"develop":   true,
				"feature-y": true,
			},
		},
		{
			name: "skip HEAD pointers",
			input: []string{
				"main",
				"remotes/origin/HEAD -> origin/main",
			},
			expected: map[string]bool{
				"main": true,
			},
		},
		{
			name: "skip non-origin remotes",
			input: []string{
				"main",
				"remotes/upstream/develop",
			},
			expected: map[string]bool{
				"main": true,
			},
		},
		{
			name:     "empty list",
			input:    []string{},
			expected: map[string]bool{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := normalizeGitBranches(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildEvictionCandidates_ExcludesProtected(t *testing.T) {
	t.Parallel()

	metadata := &CacheMetadata{
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: time.Now(),
				SizeMB:       10.0,
				IsImmortal:   true,
			},
			"master": {
				LastAccessed: time.Now(),
				SizeMB:       8.0,
				IsImmortal:   true,
			},
			"develop": {
				LastAccessed: time.Now(),
				SizeMB:       5.0,
				IsImmortal:   false,
			},
			"feature-x": {
				LastAccessed: time.Now(),
				SizeMB:       3.0,
				IsImmortal:   false,
			},
		},
	}

	gitBranches := map[string]bool{
		"main":      true,
		"master":    true,
		"develop":   true,
		"feature-x": true,
	}

	policy := EvictionPolicy{
		ProtectBranches: []string{"develop"},
	}

	candidates := buildEvictionCandidates(metadata, gitBranches, policy)

	// Should only include feature-x (main/master immortal, develop protected)
	assert.Len(t, candidates, 1)
	assert.Equal(t, "feature-x", candidates[0].name)
}

func TestBuildEvictionCandidates_IdentifiesDeleted(t *testing.T) {
	t.Parallel()

	now := time.Now()
	metadata := &CacheMetadata{
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.0,
				IsImmortal:   true,
			},
			"feature-old": {
				LastAccessed: now.Add(-60 * 24 * time.Hour),
				SizeMB:       5.0,
				IsImmortal:   false,
			},
			"feature-deleted": {
				LastAccessed: now.Add(-10 * 24 * time.Hour),
				SizeMB:       3.0,
				IsImmortal:   false,
			},
		},
	}

	// feature-deleted doesn't exist in git
	gitBranches := map[string]bool{
		"main":        true,
		"feature-old": true,
	}

	policy := DefaultEvictionPolicy()
	candidates := buildEvictionCandidates(metadata, gitBranches, policy)

	// Should have 2 candidates (feature-old and feature-deleted)
	assert.Len(t, candidates, 2)

	// Find the deleted one
	var deletedCandidate *evictionCandidate
	for i := range candidates {
		if candidates[i].name == "feature-deleted" {
			deletedCandidate = &candidates[i]
			break
		}
	}

	require.NotNil(t, deletedCandidate)
	assert.True(t, deletedCandidate.deleted)
}

func TestEvictBranch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Create test database
	dbPath := filepath.Join(branchesDir, "feature-x.db")
	require.NoError(t, os.WriteFile(dbPath, []byte("test data"), 0644))

	metadata := &CacheMetadata{
		Branches: map[string]*BranchMetadata{
			"feature-x": {
				LastAccessed: time.Now(),
				SizeMB:       5.0,
				ChunkCount:   50,
			},
		},
		TotalSizeMB: 5.0,
	}

	// Evict the branch
	err := evictBranch(cacheDir, metadata, "feature-x")
	require.NoError(t, err)

	// Verify database deleted
	assert.NoFileExists(t, dbPath)

	// Verify metadata updated
	assert.Nil(t, metadata.Branches["feature-x"])
	assert.Equal(t, 0.0, metadata.TotalSizeMB)
}

func TestEvictStaleBranches_DeletedBranches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Initialize git repo with main branch
	setupGitRepo(t, projectPath, []string{"main"})

	// Create metadata with deleted branch
	now := time.Now()
	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.0,
				ChunkCount:   100,
				IsImmortal:   true,
			},
			"deleted-branch": {
				LastAccessed: now.Add(-5 * 24 * time.Hour),
				SizeMB:       5.0,
				ChunkCount:   50,
				IsImmortal:   false,
			},
		},
		TotalSizeMB: 15.0,
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Create database files
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "main.db"), []byte("main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "deleted-branch.db"), []byte("deleted"), 0644))

	// Run eviction
	policy := DefaultEvictionPolicy()
	result, err := EvictStaleBranches(cacheDir, projectPath, policy)
	require.NoError(t, err)

	// Verify deleted branch was evicted
	assert.Contains(t, result.EvictedBranches, "deleted-branch")
	assert.Equal(t, 5.0, result.FreedMB)
	assert.NoFileExists(t, filepath.Join(branchesDir, "deleted-branch.db"))

	// Verify main branch preserved
	assert.FileExists(t, filepath.Join(branchesDir, "main.db"))

	// Verify metadata updated
	reloadedMeta, err := LoadMetadata(cacheDir)
	require.NoError(t, err)
	assert.Nil(t, reloadedMeta.Branches["deleted-branch"])
	assert.NotNil(t, reloadedMeta.Branches["main"])
}

func TestEvictStaleBranches_OldBranches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Initialize git repo with branches
	setupGitRepo(t, projectPath, []string{"main", "old-branch", "recent-branch"})

	// Create metadata with old branch (40 days old)
	now := time.Now()
	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.0,
				IsImmortal:   true,
			},
			"old-branch": {
				LastAccessed: now.Add(-40 * 24 * time.Hour), // 40 days old
				SizeMB:       5.0,
				IsImmortal:   false,
			},
			"recent-branch": {
				LastAccessed: now.Add(-5 * 24 * time.Hour), // 5 days old
				SizeMB:       3.0,
				IsImmortal:   false,
			},
		},
		TotalSizeMB: 18.0,
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Create database files
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "main.db"), []byte("main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "old-branch.db"), []byte("old"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "recent-branch.db"), []byte("recent"), 0644))

	// Run eviction with 30-day policy
	policy := EvictionPolicy{
		MaxAgeDays:      30,
		MaxSizeMB:       0, // Disable size limit for this test
		ProtectBranches: []string{"main", "master"},
	}
	result, err := EvictStaleBranches(cacheDir, projectPath, policy)
	require.NoError(t, err)

	// Verify old branch evicted
	assert.Contains(t, result.EvictedBranches, "old-branch")
	assert.NoFileExists(t, filepath.Join(branchesDir, "old-branch.db"))

	// Verify recent branch preserved
	assert.FileExists(t, filepath.Join(branchesDir, "recent-branch.db"))
}

func TestEvictStaleBranches_SizeLimit(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Initialize git repo with branches
	setupGitRepo(t, projectPath, []string{"main", "feature-1", "feature-2", "feature-3"})

	// Create metadata with total size over limit
	now := time.Now()
	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.0,
				IsImmortal:   true,
			},
			"feature-1": {
				LastAccessed: now.Add(-30 * 24 * time.Hour), // Oldest
				SizeMB:       5.0,
				IsImmortal:   false,
			},
			"feature-2": {
				LastAccessed: now.Add(-20 * 24 * time.Hour), // Middle
				SizeMB:       4.0,
				IsImmortal:   false,
			},
			"feature-3": {
				LastAccessed: now.Add(-10 * 24 * time.Hour), // Newest
				SizeMB:       3.0,
				IsImmortal:   false,
			},
		},
		TotalSizeMB: 22.0, // Over 20 MB limit
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Create database files
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "main.db"), []byte("main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "feature-1.db"), []byte("f1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "feature-2.db"), []byte("f2"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "feature-3.db"), []byte("f3"), 0644))

	// Run eviction with 20 MB size limit
	policy := EvictionPolicy{
		MaxAgeDays:      0, // Disable age limit for this test
		MaxSizeMB:       20.0,
		ProtectBranches: []string{"main", "master"},
	}
	result, err := EvictStaleBranches(cacheDir, projectPath, policy)
	require.NoError(t, err)

	// Verify oldest branches evicted until under limit
	assert.Contains(t, result.EvictedBranches, "feature-1")
	assert.True(t, result.RemainingMB <= 20.0)
}

func TestEvictStaleBranches_ProtectsImmortalBranches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Initialize git repo
	setupGitRepo(t, projectPath, []string{"main", "master"})

	// Create metadata with old main/master branches
	now := time.Now()
	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now.Add(-100 * 24 * time.Hour), // Very old
				SizeMB:       10.0,
				IsImmortal:   true,
			},
			"master": {
				LastAccessed: now.Add(-100 * 24 * time.Hour), // Very old
				SizeMB:       8.0,
				IsImmortal:   true,
			},
		},
		TotalSizeMB: 18.0,
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Create database files
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "main.db"), []byte("main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "master.db"), []byte("master"), 0644))

	// Run eviction with aggressive policy
	policy := EvictionPolicy{
		MaxAgeDays:      1, // 1 day (main/master are 100 days old!)
		MaxSizeMB:       1.0,
		ProtectBranches: []string{"main", "master"},
	}
	result, err := EvictStaleBranches(cacheDir, projectPath, policy)
	require.NoError(t, err)

	// Verify no branches evicted (immortal protection)
	assert.Empty(t, result.EvictedBranches)
	assert.FileExists(t, filepath.Join(branchesDir, "main.db"))
	assert.FileExists(t, filepath.Join(branchesDir, "master.db"))
}

func TestEvictStaleBranches_CustomProtectedBranches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Initialize git repo
	setupGitRepo(t, projectPath, []string{"main", "develop", "staging"})

	// Create metadata with old branches
	now := time.Now()
	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now.Add(-60 * 24 * time.Hour),
				SizeMB:       10.0,
				IsImmortal:   true,
			},
			"develop": {
				LastAccessed: now.Add(-60 * 24 * time.Hour),
				SizeMB:       8.0,
				IsImmortal:   false,
			},
			"staging": {
				LastAccessed: now.Add(-60 * 24 * time.Hour),
				SizeMB:       5.0,
				IsImmortal:   false,
			},
		},
		TotalSizeMB: 23.0,
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Create database files
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "main.db"), []byte("main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "develop.db"), []byte("develop"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "staging.db"), []byte("staging"), 0644))

	// Run eviction with custom protected branches
	policy := EvictionPolicy{
		MaxAgeDays:      30,
		MaxSizeMB:       0,
		ProtectBranches: []string{"main", "develop", "staging"},
	}
	result, err := EvictStaleBranches(cacheDir, projectPath, policy)
	require.NoError(t, err)

	// Verify no branches evicted
	assert.Empty(t, result.EvictedBranches)
}

func TestEvictStaleBranches_GitFailure(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	nonGitDir := filepath.Join(tmpDir, "non-git")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))
	require.NoError(t, os.MkdirAll(nonGitDir, 0755))

	// Create metadata
	now := time.Now()
	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.0,
				IsImmortal:   true,
			},
		},
		TotalSizeMB: 10.0,
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Run eviction on non-git directory (should not error)
	policy := DefaultEvictionPolicy()
	result, err := EvictStaleBranches(cacheDir, nonGitDir, policy)
	require.NoError(t, err)

	// Should not evict anything (safer to keep data when git fails)
	assert.Empty(t, result.EvictedBranches)
}

func TestEvictStaleBranches_Statistics(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")
	branchesDir := filepath.Join(cacheDir, "branches")
	require.NoError(t, os.MkdirAll(branchesDir, 0755))

	// Initialize git repo
	setupGitRepo(t, projectPath, []string{"main"})

	// Create metadata with deleted branches
	now := time.Now()
	metadata := &CacheMetadata{
		Version:    "1.0.0",
		ProjectKey: "test",
		Branches: map[string]*BranchMetadata{
			"main": {
				LastAccessed: now,
				SizeMB:       10.0,
				IsImmortal:   true,
			},
			"deleted-1": {
				LastAccessed: now,
				SizeMB:       5.0,
				IsImmortal:   false,
			},
			"deleted-2": {
				LastAccessed: now,
				SizeMB:       3.0,
				IsImmortal:   false,
			},
		},
		TotalSizeMB: 18.0,
	}
	require.NoError(t, metadata.Save(cacheDir))

	// Create database files
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "main.db"), []byte("main"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "deleted-1.db"), []byte("d1"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(branchesDir, "deleted-2.db"), []byte("d2"), 0644))

	// Run eviction
	policy := DefaultEvictionPolicy()
	result, err := EvictStaleBranches(cacheDir, projectPath, policy)
	require.NoError(t, err)

	// Verify statistics
	assert.Len(t, result.EvictedBranches, 2)
	assert.Equal(t, 8.0, result.FreedMB) // 5 + 3
	assert.Equal(t, 10.0, result.RemainingMB)
	assert.Greater(t, result.Duration, time.Duration(0))
}

func TestGetCurrentBranchDB(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	cacheDir := filepath.Join(tmpDir, "cache")

	// Initialize git repo on main branch
	setupGitRepo(t, projectPath, []string{"main"})

	dbPath := GetCurrentBranchDB(cacheDir, projectPath)
	expected := filepath.Join(cacheDir, "branches", "main.db")
	assert.Equal(t, expected, dbPath)
}

// Helper function to set up a git repo with branches
func setupGitRepo(t *testing.T, repoPath string, branches []string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(repoPath, 0755))

	// Initialize git repo
	cmd := exec.Command("git", "init")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	// Configure git
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	// Create initial commit
	testFile := filepath.Join(repoPath, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test"), 0644))

	cmd = exec.Command("git", "add", "test.txt")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run())

	// Create branches
	for _, branch := range branches {
		if branch != "main" {
			cmd = exec.Command("git", "branch", branch)
			cmd.Dir = repoPath
			require.NoError(t, cmd.Run())
		}
	}
}
