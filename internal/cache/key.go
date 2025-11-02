package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"os/exec"
	"strings"
)

// GetCacheKey returns the cache key for the project.
// The key combines git remote and worktree path to uniquely identify a project.
// Format: {remoteHash}-{worktreeHash} where each hash is 8 chars.
func GetCacheKey(projectPath string) (string, error) {
	remoteHash := getRemoteHash(projectPath)
	worktreeHash := getWorktreeHash(projectPath)
	return remoteHash + "-" + worktreeHash, nil
}

// getRemoteHash returns an 8-char hash of the git remote URL.
// Returns "00000000" if no remote is configured.
func getRemoteHash(projectPath string) string {
	remote := getGitRemote(projectPath)
	if remote == "" {
		return "00000000"
	}
	normalized := normalizeRemoteURL(remote)
	return hashString(normalized)[:8]
}

// getGitRemote returns the git remote URL for the project.
// Tries 'origin' first, then falls back to the first available remote.
func getGitRemote(projectPath string) string {
	// Try 'origin' first
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(output))
	}

	// Fallback: first remote
	cmd = exec.Command("git", "remote")
	cmd.Dir = projectPath
	output, err = cmd.Output()
	if err != nil {
		return ""
	}

	remotes := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(remotes) > 0 && remotes[0] != "" {
		cmd = exec.Command("git", "remote", "get-url", remotes[0])
		cmd.Dir = projectPath
		output, _ = cmd.Output()
		return strings.TrimSpace(string(output))
	}

	return ""
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
	root := getWorktreeRoot(projectPath)
	return hashString(root)[:8]
}

// getWorktreeRoot returns the git worktree root path.
// Falls back to projectPath if not a git repository.
func getWorktreeRoot(projectPath string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil {
		return projectPath
	}
	return strings.TrimSpace(string(output))
}

// hashString returns SHA-256 hash of the input string as hex.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
