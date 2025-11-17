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
// - GetCacheKey generates keys using git operations (mocked)
// - getRemoteHash returns placeholder "00000000" when remote is empty
// - getRemoteHash returns 8-character hex hash when remote exists
// - getWorktreeHash returns 8-character hex hash of worktree path

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mvp-joe/project-cortex/internal/git"
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

func TestGetCacheKeyWithRemote(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	// Initialize real git repo with remote
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())

	// Create initial commit
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "README.md"), []byte("# Test\n"), 0644))
	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())

	// Add remote
	cmd = exec.Command("git", "remote", "add", "origin", "https://github.com/user/repo.git")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())

	cacheKey, err := GetCacheKey(projectPath)
	require.NoError(t, err)

	// Verify format: {remote-hash}-{worktree-hash}
	assert.Regexp(t, `^[0-9a-f]{8}-[0-9a-f]{8}$`, cacheKey)

	// Verify remote hash is not the placeholder
	remoteHash := cacheKey[:8]
	assert.NotEqual(t, "00000000", remoteHash)
}

func TestGetCacheKeyWithoutRemote(t *testing.T) {
	t.Parallel()

	mock := git.NewMockGitOps()
	mock.RemoteURL = ""
	mock.WorktreeRoot = "/home/user/projects/repo"

	cacheKey, err := GetCacheKeyWithGitOps("/any/path", mock)
	require.NoError(t, err)

	// Verify format with placeholder remote hash
	assert.Regexp(t, `^00000000-[0-9a-f]{8}$`, cacheKey)
}

func TestGetCacheKeyConsistency(t *testing.T) {
	t.Parallel()

	mock := git.NewMockGitOps()
	mock.RemoteURL = "https://github.com/user/repo.git"
	mock.WorktreeRoot = "/home/user/projects/repo"

	key1, err1 := GetCacheKeyWithGitOps("/any/path", mock)
	require.NoError(t, err1)

	key2, err2 := GetCacheKeyWithGitOps("/any/path", mock)
	require.NoError(t, err2)

	assert.Equal(t, key1, key2, "cache key should be deterministic")
}

func TestGetCacheKeyDifferentWorktrees(t *testing.T) {
	t.Parallel()

	mock := git.NewMockGitOps()
	mock.RemoteURL = "https://github.com/user/repo.git"

	// Same remote, different worktrees
	mock.WorktreeRoot = "/home/user/projects/repo1"
	key1, err1 := GetCacheKeyWithGitOps("/any/path1", mock)
	require.NoError(t, err1)

	mock.WorktreeRoot = "/home/user/projects/repo2"
	key2, err2 := GetCacheKeyWithGitOps("/any/path2", mock)
	require.NoError(t, err2)

	// Different keys because worktree differs
	assert.NotEqual(t, key1, key2)

	// But remote hash should be the same
	assert.Equal(t, key1[:8], key2[:8], "remote hash should match")
}

func TestGetRemoteHashWithRemote(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	projectPath := filepath.Join(tmpDir, "project")
	require.NoError(t, os.MkdirAll(projectPath, 0755))

	// Initialize real git repo with remote
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())

	// Create initial commit
	require.NoError(t, os.WriteFile(filepath.Join(projectPath, "README.md"), []byte("# Test\n"), 0644))
	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())
	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())

	// Add remote
	cmd = exec.Command("git", "remote", "add", "origin", "https://github.com/user/repo.git")
	cmd.Dir = projectPath
	require.NoError(t, cmd.Run())

	hash := getRemoteHash(projectPath)

	assert.Regexp(t, `^[0-9a-f]{8}$`, hash)
	assert.NotEqual(t, "00000000", hash)
}

func TestGetRemoteHashWithoutRemote(t *testing.T) {
	t.Parallel()

	mock := git.NewMockGitOps()
	mock.RemoteURL = ""

	hash := getRemoteHashWithGitOps("/any/path", mock)

	assert.Equal(t, "00000000", hash, "should return placeholder when no remote")
}

func TestGetWorktreeHash(t *testing.T) {
	t.Parallel()

	mock := git.NewMockGitOps()
	mock.WorktreeRoot = "/home/user/projects/repo"

	hash := getWorktreeHashWithGitOps("/any/path", mock)

	assert.Regexp(t, `^[0-9a-f]{8}$`, hash)
}
