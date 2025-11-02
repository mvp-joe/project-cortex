package cache

// Test Plan for Cache Key Calculation:
// - normalizeRemoteURL handles HTTPS format with and without .git suffix
// - normalizeRemoteURL handles SSH format (git@github.com:) with and without .git
// - normalizeRemoteURL handles HTTP format with .git suffix
// - normalizeRemoteURL handles already normalized URLs
// - normalizeRemoteURL handles GitLab and self-hosted SSH URLs
// - normalizeRemoteURL handles empty strings and whitespace
// - hashString produces valid SHA-256 hex strings (64 characters, lowercase)
// - hashString is deterministic (same input produces same hash)
// - GetCacheKey generates keys in format {remote-hash}-{worktree-hash} for repos with remotes
// - GetCacheKey uses placeholder 00000000 for remote hash when no remote exists
// - GetCacheKey is deterministic (same repo produces same key)
// - GetCacheKey produces different keys for different worktrees with same remote
// - GetCacheKey produces same remote hash for different worktrees with same remote
// - getRemoteHash returns placeholder "00000000" when no remote exists
// - getRemoteHash returns 8-character hex hash when remote exists
// - getGitRemote prefers "origin" remote when multiple remotes exist
// - getGitRemote falls back to first remote when no origin exists
// - getGitRemote returns empty string when no remotes configured
// - getWorktreeHash returns 8-character hex hash of absolute worktree path
// - getWorktreeRoot returns git root directory from subdirectories
// - getWorktreeRoot falls back to projectPath for non-git directories

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeRemoteURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "HTTPS with .git suffix",
			input:    "https://github.com/user/repo.git",
			expected: "github.com/user/repo",
		},
		{
			name:     "HTTPS without .git suffix",
			input:    "https://github.com/user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "SSH format (git@)",
			input:    "git@github.com:user/repo.git",
			expected: "github.com/user/repo",
		},
		{
			name:     "SSH format without .git",
			input:    "git@github.com:user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "HTTP with .git",
			input:    "http://github.com/user/repo.git",
			expected: "github.com/user/repo",
		},
		{
			name:     "Already normalized",
			input:    "github.com/user/repo",
			expected: "github.com/user/repo",
		},
		{
			name:     "GitLab SSH",
			input:    "git@gitlab.com:group/project.git",
			expected: "gitlab.com/group/project",
		},
		{
			name:     "Self-hosted SSH",
			input:    "git@git.company.com:team/repo.git",
			expected: "git.company.com/team/repo",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Whitespace",
			input:    "  https://github.com/user/repo.git  ",
			expected: "github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := normalizeRemoteURL(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHashString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "Empty string",
			input: "",
		},
		{
			name:  "Simple string",
			input: "test",
		},
		{
			name:  "GitHub URL",
			input: "github.com/user/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := hashString(tt.input)
			// Check that result is a valid SHA-256 hex string
			assert.True(t, len(result) == 64, "hash should be 64 characters (256 bits)")
			assert.Regexp(t, `^[0-9a-f]{64}$`, result, "hash should be lowercase hex")
		})
	}
}

func TestHashStringConsistency(t *testing.T) {
	t.Parallel()

	input := "github.com/user/repo"
	hash1 := hashString(input)
	hash2 := hashString(input)

	assert.Equal(t, hash1, hash2, "hash should be deterministic")
	assert.True(t, len(hash1) > 8, "hash should be full SHA-256 hex")
}

func TestGetCacheKeyWithGitRepo(t *testing.T) {
	t.Parallel()

	// Create temporary git repository
	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")

	// Add remote
	runGitCmd(t, tmpDir, "remote", "add", "origin", "https://github.com/user/repo.git")

	// Get cache key
	cacheKey, err := GetCacheKey(tmpDir)
	require.NoError(t, err)

	// Verify format: {remote-hash}-{worktree-hash}
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{8}$`, cacheKey)

	// Verify remote hash is not the placeholder
	remoteHash := cacheKey[:8]
	assert.NotEqual(t, "00000000", remoteHash)
}

func TestGetCacheKeyWithoutRemote(t *testing.T) {
	t.Parallel()

	// Create temporary git repository without remote
	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")

	// Get cache key
	cacheKey, err := GetCacheKey(tmpDir)
	require.NoError(t, err)

	// Verify format with placeholder remote hash
	assert.Regexp(t, `^00000000-[0-9a-f]{8}$`, cacheKey)
}

func TestGetCacheKeyConsistency(t *testing.T) {
	t.Parallel()

	// Create temporary git repository
	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")
	runGitCmd(t, tmpDir, "remote", "add", "origin", "git@github.com:user/repo.git")

	// Get cache key twice
	key1, err1 := GetCacheKey(tmpDir)
	key2, err2 := GetCacheKey(tmpDir)

	require.NoError(t, err1)
	require.NoError(t, err2)
	assert.Equal(t, key1, key2, "cache key should be deterministic")
}

func TestGetCacheKeyDifferentWorktrees(t *testing.T) {
	t.Parallel()

	// Create two temporary git repositories with same remote
	tmpDir1 := t.TempDir()
	tmpDir2 := t.TempDir()

	remote := "https://github.com/user/repo.git"

	// Setup first repo
	runGitCmd(t, tmpDir1, "init")
	runGitCmd(t, tmpDir1, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir1, "config", "user.email", "test@example.com")
	runGitCmd(t, tmpDir1, "remote", "add", "origin", remote)

	// Setup second repo
	runGitCmd(t, tmpDir2, "init")
	runGitCmd(t, tmpDir2, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir2, "config", "user.email", "test@example.com")
	runGitCmd(t, tmpDir2, "remote", "add", "origin", remote)

	// Get cache keys
	key1, err1 := GetCacheKey(tmpDir1)
	key2, err2 := GetCacheKey(tmpDir2)

	require.NoError(t, err1)
	require.NoError(t, err2)

	// Same remote, different worktrees = different keys
	assert.NotEqual(t, key1, key2)

	// But remote hash should be the same
	remoteHash1 := key1[:8]
	remoteHash2 := key2[:8]
	assert.Equal(t, remoteHash1, remoteHash2)
}

func TestGetRemoteHash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")

	t.Run("no remote returns placeholder", func(t *testing.T) {
		hash := getRemoteHash(tmpDir)
		assert.Equal(t, "00000000", hash)
	})

	t.Run("with remote returns hash", func(t *testing.T) {
		runGitCmd(t, tmpDir, "remote", "add", "origin", "https://github.com/user/repo.git")
		hash := getRemoteHash(tmpDir)
		assert.Regexp(t, `^[0-9a-f]{8}$`, hash)
		assert.NotEqual(t, "00000000", hash)
	})
}

func TestGetGitRemote(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")
	runGitCmd(t, tmpDir, "config", "user.name", "Test User")
	runGitCmd(t, tmpDir, "config", "user.email", "test@example.com")

	t.Run("no remotes", func(t *testing.T) {
		remote := getGitRemote(tmpDir)
		assert.Empty(t, remote)
	})

	t.Run("origin remote", func(t *testing.T) {
		expected := "https://github.com/user/repo.git"
		runGitCmd(t, tmpDir, "remote", "add", "origin", expected)

		remote := getGitRemote(tmpDir)
		assert.Equal(t, expected, remote)
	})

	t.Run("fallback to first remote", func(t *testing.T) {
		// Remove origin
		runGitCmd(t, tmpDir, "remote", "remove", "origin")

		// Add non-origin remote
		expected := "https://github.com/other/repo.git"
		runGitCmd(t, tmpDir, "remote", "add", "upstream", expected)

		remote := getGitRemote(tmpDir)
		assert.Equal(t, expected, remote)
	})
}

func TestGetWorktreeHash(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")

	hash := getWorktreeHash(tmpDir)
	assert.Regexp(t, `^[0-9a-f]{8}$`, hash)
	assert.NotEmpty(t, hash)
}

func TestGetWorktreeRoot(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Initialize git repo
	runGitCmd(t, tmpDir, "init")

	// Create subdirectory
	subDir := filepath.Join(tmpDir, "subdir", "nested")
	err := os.MkdirAll(subDir, 0755)
	require.NoError(t, err)

	// Get worktree root from subdirectory
	root := getWorktreeRoot(subDir)

	// Should return the git root (may be symlink-resolved on macOS)
	// Use filepath.EvalSymlinks to compare correctly
	expectedRoot, err := filepath.EvalSymlinks(tmpDir)
	require.NoError(t, err)
	actualRoot, err := filepath.EvalSymlinks(root)
	require.NoError(t, err)

	assert.Equal(t, expectedRoot, actualRoot)
}

func TestGetWorktreeRootNonGitDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Not a git repository - should fallback to projectPath
	root := getWorktreeRoot(tmpDir)
	assert.Equal(t, tmpDir, root)
}

// Helper function to run git commands in tests
func runGitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("git %v failed: %s\nOutput: %s", args, err, output)
		require.NoError(t, err)
	}
}
