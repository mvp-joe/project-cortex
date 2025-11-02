package cache

// Test Plan for Branch Detection:
// - GetCurrentBranch returns "main" for main branch
// - GetCurrentBranch returns correct name for feature branches
// - GetCurrentBranch returns "detached-{short-hash}" for detached HEAD state
// - GetCurrentBranch returns "unknown" for non-git directories
// - GetCurrentBranch handles empty git repo (no commits) gracefully
// - FindAncestorBranch finds "main" as ancestor for feature branches
// - FindAncestorBranch finds "master" as ancestor when main doesn't exist
// - FindAncestorBranch returns "main" when queried for main branch itself
// - FindAncestorBranch returns empty string when no main/master branch exists
// - FindAncestorBranch returns empty string for detached HEAD state
// - FindAncestorBranch returns empty string for non-git directories
// - GetGitBranches returns list with single branch for new repos
// - GetGitBranches returns all branches with current branch marked with "*"
// - GetGitBranches preserves whitespace and markers in branch names
// - GetGitBranches includes exactly one branch marked with "*" (current branch)
// - GetGitBranches lists branches correctly in detached HEAD state
// - GetGitBranches returns error for non-git directories
// - Integration: creating feature branch and finding ancestor works end-to-end
// - Integration: nested feature branches correctly find main as common ancestor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// createTestGitRepo creates a test git repository with an initial commit on main
func createTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize repo
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git init failed")

	// Configure git identity
	cmd = exec.Command("git", "config", "user.email", "test@example.com")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git config user.email failed")

	cmd = exec.Command("git", "config", "user.name", "Test User")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git config user.name failed")

	// Create initial commit
	testFile := filepath.Join(dir, "test.txt")
	require.NoError(t, os.WriteFile(testFile, []byte("test content"), 0644))

	cmd = exec.Command("git", "add", ".")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git add failed")

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git commit failed")

	// Rename to main (modern git default)
	cmd = exec.Command("git", "branch", "-M", "main")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git branch -M main failed")

	return dir
}

// createBranch creates a new branch from the current HEAD
func createBranch(t *testing.T, repoPath, branchName string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run(), "git checkout -b failed")
}

// checkoutBranch switches to an existing branch
func checkoutBranch(t *testing.T, repoPath, branchName string) {
	t.Helper()
	cmd := exec.Command("git", "checkout", branchName)
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run(), "git checkout failed")
}

// createDetachedHead creates a detached HEAD state
func createDetachedHead(t *testing.T, repoPath string) string {
	t.Helper()
	// Get current commit hash
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoPath
	output, err := cmd.Output()
	require.NoError(t, err, "git rev-parse HEAD failed")
	commitHash := strings.TrimSpace(string(output))

	// Checkout the commit directly (creates detached HEAD)
	cmd = exec.Command("git", "checkout", commitHash)
	cmd.Dir = repoPath
	require.NoError(t, cmd.Run(), "git checkout commit failed")

	// Get short hash
	cmd = exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = repoPath
	output, err = cmd.Output()
	require.NoError(t, err, "git rev-parse --short HEAD failed")

	return strings.TrimSpace(string(output))
}

func TestGetCurrentBranch(t *testing.T) {
	t.Parallel()

	t.Run("main branch", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		branch := GetCurrentBranch(repo)
		assert.Equal(t, "main", branch)
	})

	t.Run("feature branch", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature-x")

		branch := GetCurrentBranch(repo)
		assert.Equal(t, "feature-x", branch)
	})

	t.Run("detached HEAD", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		shortHash := createDetachedHead(t, repo)

		branch := GetCurrentBranch(repo)
		assert.Equal(t, "detached-"+shortHash, branch)
	})

	t.Run("non-git directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		branch := GetCurrentBranch(dir)
		assert.Equal(t, "unknown", branch)
	})

	t.Run("empty git repo (no commits)", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		cmd := exec.Command("git", "init")
		cmd.Dir = dir
		require.NoError(t, cmd.Run())

		// Modern git initializes with a default branch name (usually "main")
		// even before first commit, so git branch --show-current returns it
		branch := GetCurrentBranch(dir)
		// Could be "main" or "master" depending on git config, or "unknown" on very old git
		assert.Contains(t, []string{"main", "master", "unknown"}, branch)
	})
}

func TestFindAncestorBranch(t *testing.T) {
	t.Parallel()

	t.Run("feature branch from main", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature-x")

		ancestor := FindAncestorBranch(repo, "feature-x")
		assert.Equal(t, "main", ancestor)
	})

	t.Run("feature branch from master", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		// Rename main to master
		cmd := exec.Command("git", "branch", "-M", "master")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())

		createBranch(t, repo, "feature-y")

		ancestor := FindAncestorBranch(repo, "feature-y")
		assert.Equal(t, "master", ancestor)
	})

	t.Run("main branch itself", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		// main can't have itself as ancestor in merge-base
		ancestor := FindAncestorBranch(repo, "main")
		// Merge-base of main with main returns the commit, so this returns "main"
		assert.Equal(t, "main", ancestor)
	})

	t.Run("no main or master branch", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		// Rename main to develop
		cmd := exec.Command("git", "branch", "-M", "develop")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())

		createBranch(t, repo, "feature-z")

		ancestor := FindAncestorBranch(repo, "feature-z")
		assert.Equal(t, "", ancestor, "should return empty string when no main/master exists")
	})

	t.Run("detached HEAD", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		shortHash := createDetachedHead(t, repo)

		ancestor := FindAncestorBranch(repo, "detached-"+shortHash)
		// Merge-base will fail for non-existent branch name
		assert.Equal(t, "", ancestor)
	})

	t.Run("non-git directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		ancestor := FindAncestorBranch(dir, "any-branch")
		assert.Equal(t, "", ancestor)
	})
}

func TestGetGitBranches(t *testing.T) {
	t.Parallel()

	t.Run("single branch", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		branches, err := GetGitBranches(repo)
		require.NoError(t, err)
		assert.Equal(t, []string{"* main"}, branches)
	})

	t.Run("multiple branches", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature-a")
		checkoutBranch(t, repo, "main")
		createBranch(t, repo, "feature-b")
		checkoutBranch(t, repo, "main")

		branches, err := GetGitBranches(repo)
		require.NoError(t, err)

		// Should contain all branches
		assert.Contains(t, branches, "* main")
		assert.Contains(t, branches, "feature-a")
		assert.Contains(t, branches, "feature-b")
		assert.Len(t, branches, 3)
	})

	t.Run("strips whitespace preserves markers", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature-x")
		checkoutBranch(t, repo, "main")

		branches, err := GetGitBranches(repo)
		require.NoError(t, err)

		// All branches should be present, current branch marked with "*"
		for _, branch := range branches {
			assert.NotEmpty(t, branch, "branch name should not be empty")
		}
		// Should have one branch with "*" marker (current branch)
		hasCurrentMarker := false
		for _, branch := range branches {
			if strings.HasPrefix(branch, "* ") {
				hasCurrentMarker = true
				break
			}
		}
		assert.True(t, hasCurrentMarker, "should have current branch marker")
	})

	t.Run("detached HEAD state", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)
		createBranch(t, repo, "feature-x")
		checkoutBranch(t, repo, "main")
		createDetachedHead(t, repo)

		branches, err := GetGitBranches(repo)
		require.NoError(t, err)

		// Should still list branches even in detached HEAD
		assert.Contains(t, branches, "main")
		assert.Contains(t, branches, "feature-x")
	})

	t.Run("non-git directory", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()

		branches, err := GetGitBranches(dir)
		assert.Error(t, err, "should return error for non-git directory")
		assert.Nil(t, branches)
	})
}

func TestBranchDetectionIntegration(t *testing.T) {
	t.Parallel()

	t.Run("workflow: create feature branch and find ancestor", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		// Verify we're on main
		branch := GetCurrentBranch(repo)
		assert.Equal(t, "main", branch)

		// Create feature branch
		createBranch(t, repo, "feature-awesome")
		branch = GetCurrentBranch(repo)
		assert.Equal(t, "feature-awesome", branch)

		// Find ancestor should return main
		ancestor := FindAncestorBranch(repo, "feature-awesome")
		assert.Equal(t, "main", ancestor)

		// List branches should show both
		branches, err := GetGitBranches(repo)
		require.NoError(t, err)
		assert.Contains(t, branches, "* feature-awesome")
		assert.Contains(t, branches, "main")
	})

	t.Run("workflow: nested feature branches", func(t *testing.T) {
		t.Parallel()
		repo := createTestGitRepo(t)

		// Create feature-1 from main
		createBranch(t, repo, "feature-1")

		// Add a commit to feature-1
		testFile := filepath.Join(repo, "feature1.txt")
		require.NoError(t, os.WriteFile(testFile, []byte("feature 1"), 0644))
		cmd := exec.Command("git", "add", ".")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())
		cmd = exec.Command("git", "commit", "-m", "feature 1 work")
		cmd.Dir = repo
		require.NoError(t, cmd.Run())

		// Create feature-2 from feature-1
		createBranch(t, repo, "feature-2")

		// feature-2's ancestor should still be main (merge-base finds common ancestor)
		ancestor := FindAncestorBranch(repo, "feature-2")
		assert.Equal(t, "main", ancestor)

		// But feature-1's ancestor is also main
		ancestor = FindAncestorBranch(repo, "feature-1")
		assert.Equal(t, "main", ancestor)
	})
}
