package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests for real GitOperations implementation.
// These tests use actual git commands and run sequentially (NO t.Parallel()).

func TestGitOpsIntegration(t *testing.T) {
	// NO t.Parallel() - these tests run sequentially to avoid resource exhaustion

	gitOps := NewOperations()

	t.Run("GetCurrentBranch on main", func(t *testing.T) {
		dir := createTestGitRepo(t)
		branch := gitOps.GetCurrentBranch(dir)
		assert.Equal(t, "main", branch)
	})

	t.Run("GetCurrentBranch on feature branch", func(t *testing.T) {
		dir := createTestGitRepo(t)
		runGitCmd(t, dir, "checkout", "-b", "feature/test")
		branch := gitOps.GetCurrentBranch(dir)
		assert.Equal(t, "feature/test", branch)
	})

	t.Run("GetCurrentBranch detached HEAD", func(t *testing.T) {
		dir := createTestGitRepo(t)
		runGitCmd(t, dir, "checkout", "HEAD~0")
		branch := gitOps.GetCurrentBranch(dir)
		assert.Contains(t, branch, "detached-")
	})

	t.Run("GetCurrentBranch non-git directory", func(t *testing.T) {
		dir := t.TempDir()
		branch := gitOps.GetCurrentBranch(dir)
		assert.Equal(t, "unknown", branch)
	})

	t.Run("FindAncestorBranch finds main", func(t *testing.T) {
		dir := createTestGitRepo(t)
		runGitCmd(t, dir, "checkout", "-b", "feature/test")
		ancestor := gitOps.FindAncestorBranch(dir, "feature/test")
		assert.Equal(t, "main", ancestor)
	})

	t.Run("FindAncestorBranch on main", func(t *testing.T) {
		dir := createTestGitRepo(t)
		ancestor := gitOps.FindAncestorBranch(dir, "main")
		// On main branch, merge-base with main succeeds
		assert.Equal(t, "main", ancestor)
	})

	t.Run("FindAncestorBranch no common ancestor", func(t *testing.T) {
		dir := createTestGitRepo(t)
		// Create orphan branch (no common history)
		runGitCmd(t, dir, "checkout", "--orphan", "orphan-branch")
		ancestor := gitOps.FindAncestorBranch(dir, "orphan-branch")
		assert.Equal(t, "", ancestor)
	})

	t.Run("GetBranches single branch", func(t *testing.T) {
		dir := createTestGitRepo(t)
		branches, err := gitOps.GetBranches(dir)
		require.NoError(t, err)
		assert.Equal(t, []string{"* main"}, branches)
	})

	t.Run("GetBranches multiple branches", func(t *testing.T) {
		dir := createTestGitRepo(t)
		runGitCmd(t, dir, "checkout", "-b", "feature/auth")
		runGitCmd(t, dir, "checkout", "main")
		branches, err := gitOps.GetBranches(dir)
		require.NoError(t, err)
		// Branches could be in any order, just check both exist
		branchStr := ""
		for _, b := range branches {
			branchStr += b + "|"
		}
		assert.Contains(t, branchStr, "* main")
		assert.Contains(t, branchStr, "feature/auth")
	})

	t.Run("GetBranches non-git directory", func(t *testing.T) {
		dir := t.TempDir()
		_, err := gitOps.GetBranches(dir)
		assert.Error(t, err)
	})

	t.Run("GetRemoteURL with origin", func(t *testing.T) {
		dir := createTestGitRepo(t)
		runGitCmd(t, dir, "remote", "add", "origin", "https://github.com/user/repo.git")
		url := gitOps.GetRemoteURL(dir)
		assert.Equal(t, "https://github.com/user/repo.git", url)
	})

	t.Run("GetRemoteURL prefers origin over others", func(t *testing.T) {
		dir := createTestGitRepo(t)
		runGitCmd(t, dir, "remote", "add", "upstream", "https://github.com/upstream/repo.git")
		runGitCmd(t, dir, "remote", "add", "origin", "https://github.com/user/repo.git")
		url := gitOps.GetRemoteURL(dir)
		assert.Equal(t, "https://github.com/user/repo.git", url)
	})

	t.Run("GetRemoteURL falls back to first remote", func(t *testing.T) {
		dir := createTestGitRepo(t)
		runGitCmd(t, dir, "remote", "add", "upstream", "https://github.com/upstream/repo.git")
		url := gitOps.GetRemoteURL(dir)
		assert.Equal(t, "https://github.com/upstream/repo.git", url)
	})

	t.Run("GetRemoteURL no remote", func(t *testing.T) {
		dir := createTestGitRepo(t)
		url := gitOps.GetRemoteURL(dir)
		assert.Equal(t, "", url)
	})

	t.Run("GetWorktreeRoot from repo root", func(t *testing.T) {
		dir := createTestGitRepo(t)
		root := gitOps.GetWorktreeRoot(dir)
		// macOS: /var/folders is symlinked to /private/var/folders
		// Use filepath.EvalSymlinks to resolve
		dirResolved, _ := filepath.EvalSymlinks(dir)
		rootResolved, _ := filepath.EvalSymlinks(root)
		assert.Equal(t, dirResolved, rootResolved)
	})

	t.Run("GetWorktreeRoot from subdirectory", func(t *testing.T) {
		dir := createTestGitRepo(t)
		subdir := filepath.Join(dir, "subdir")
		require.NoError(t, os.MkdirAll(subdir, 0755))
		root := gitOps.GetWorktreeRoot(subdir)
		// Resolve symlinks for comparison
		dirResolved, _ := filepath.EvalSymlinks(dir)
		rootResolved, _ := filepath.EvalSymlinks(root)
		assert.Equal(t, dirResolved, rootResolved)
	})

	t.Run("GetWorktreeRoot non-git directory", func(t *testing.T) {
		dir := t.TempDir()
		root := gitOps.GetWorktreeRoot(dir)
		assert.Equal(t, dir, root)
	})
}

// Test helpers

func createTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Initialize repo
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	require.NoError(t, cmd.Run(), "git init failed")

	// Configure git identity
	runGitCmd(t, dir, "config", "user.email", "test@example.com")
	runGitCmd(t, dir, "config", "user.name", "Test User")

	// Create initial commit
	testFile := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(testFile, []byte("# Test\n"), 0644))
	runGitCmd(t, dir, "add", "README.md")
	runGitCmd(t, dir, "commit", "-m", "Initial commit")

	return dir
}

func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(output))
}
