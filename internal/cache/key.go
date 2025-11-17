package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/mvp-joe/project-cortex/internal/git"
)

// GetCacheKey returns the cache key for the project.
// The key combines git remote and worktree path to uniquely identify a project.
// Format: {remoteHash}-{worktreeHash} where each hash is 8 chars.
func GetCacheKey(projectPath string) (string, error) {
	return GetCacheKeyWithGitOps(projectPath, git.NewOperations())
}

// GetCacheKeyWithGitOps returns the cache key using the provided git operations.
func GetCacheKeyWithGitOps(projectPath string, gitOps git.Operations) (string, error) {
	remoteHash := getRemoteHashWithGitOps(projectPath, gitOps)
	worktreeHash := getWorktreeHashWithGitOps(projectPath, gitOps)
	return remoteHash + "-" + worktreeHash, nil
}

// getRemoteHash returns an 8-char hash of the git remote URL.
// Returns "00000000" if no remote is configured.
func getRemoteHash(projectPath string) string {
	return getRemoteHashWithGitOps(projectPath, git.NewOperations())
}

// getRemoteHashWithGitOps returns an 8-char hash using provided git operations.
func getRemoteHashWithGitOps(projectPath string, gitOps git.Operations) string {
	remote := gitOps.GetRemoteURL(projectPath)
	if remote == "" {
		return "00000000"
	}
	normalized := normalizeRemoteURL(remote)
	return hashString(normalized)[:8]
}

// normalizeRemoteURL normalizes git remote URLs to a canonical form.
// Strips protocols, converts SSH format to path format, removes .git suffix.
// Examples:
//   - https://github.com/user/repo.git -> github.com/user/repo
//   - git@github.com:user/repo.git -> github.com/user/repo
func normalizeRemoteURL(remote string) string {
	// Trim whitespace first
	remote = strings.TrimSpace(remote)

	// Strip protocols
	remote = strings.TrimPrefix(remote, "https://")
	remote = strings.TrimPrefix(remote, "http://")
	remote = strings.TrimPrefix(remote, "ssh://")
	remote = strings.TrimPrefix(remote, "git://")

	// Strip .git suffix before handling git@ to avoid issues
	remote = strings.TrimSuffix(remote, ".git")

	// Convert SSH format (git@github.com:user/repo) to path format
	if strings.HasPrefix(remote, "git@") {
		remote = strings.TrimPrefix(remote, "git@")
		// Replace first colon with slash
		remote = strings.Replace(remote, ":", "/", 1)
	}

	return remote
}

// getWorktreeHash returns an 8-char hash of the worktree root path.
func getWorktreeHash(projectPath string) string {
	return getWorktreeHashWithGitOps(projectPath, git.NewOperations())
}

// getWorktreeHashWithGitOps returns an 8-char hash using provided git operations.
func getWorktreeHashWithGitOps(projectPath string, gitOps git.Operations) string {
	root := gitOps.GetWorktreeRoot(projectPath)
	return hashString(root)[:8]
}

// hashString returns SHA-256 hash of the input string as hex.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
