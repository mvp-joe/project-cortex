package git

import (
	"os/exec"
	"strings"
)

// Operations defines the interface for git operations.
// This allows mocking git commands in tests.
type Operations interface {
	// GetCurrentBranch returns the current branch name.
	// For detached HEAD, returns "detached-{short-hash}".
	// Returns "unknown" if all git commands fail.
	GetCurrentBranch(projectPath string) string

	// FindAncestorBranch finds the ancestor branch (main or master).
	// Returns empty string if no ancestor found.
	FindAncestorBranch(projectPath, currentBranch string) string

	// GetBranches returns all local and remote branches.
	// Current branch is prefixed with "* ".
	GetBranches(projectPath string) ([]string, error)

	// GetRemoteURL returns the git remote URL.
	// Tries 'origin' first, then falls back to first available remote.
	// Returns empty string if no remote configured.
	GetRemoteURL(projectPath string) string

	// GetWorktreeRoot returns the git worktree root path.
	// Falls back to projectPath if not a git repository.
	GetWorktreeRoot(projectPath string) string
}

// gitOps is the real implementation using exec.Command.
type gitOps struct{}

// NewOperations returns the default git operations implementation.
func NewOperations() Operations {
	return &gitOps{}
}

func (g *gitOps) GetCurrentBranch(projectPath string) string {
	cmd := exec.Command("git", "branch", "--show-current")
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil || len(strings.TrimSpace(string(output))) == 0 {
		// Might be detached HEAD
		cmd = exec.Command("git", "rev-parse", "--short", "HEAD")
		cmd.Dir = projectPath
		output, err = cmd.Output()
		if err != nil {
			return "unknown"
		}
		return "detached-" + strings.TrimSpace(string(output))
	}
	return strings.TrimSpace(string(output))
}

func (g *gitOps) FindAncestorBranch(projectPath, currentBranch string) string {
	// Try merge-base with main
	cmd := exec.Command("git", "merge-base", currentBranch, "main")
	cmd.Dir = projectPath
	if output, err := cmd.Output(); err == nil && len(output) > 0 {
		return "main"
	}

	// Try merge-base with master
	cmd = exec.Command("git", "merge-base", currentBranch, "master")
	cmd.Dir = projectPath
	if output, err := cmd.Output(); err == nil && len(output) > 0 {
		return "master"
	}

	return ""
}

func (g *gitOps) GetBranches(projectPath string) ([]string, error) {
	cmd := exec.Command("git", "branch", "-a")
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	var branches []string

	for _, line := range lines {
		// Strip leading/trailing whitespace but preserve "*" marker
		branch := strings.TrimSpace(line)
		if len(branch) == 0 {
			continue
		}
		branches = append(branches, branch)
	}

	return branches, nil
}

func (g *gitOps) GetRemoteURL(projectPath string) string {
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

func (g *gitOps) GetWorktreeRoot(projectPath string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = projectPath
	output, err := cmd.Output()
	if err != nil {
		return projectPath
	}
	return strings.TrimSpace(string(output))
}

// Package-level variable for dependency injection.
// Tests can replace this with a mock implementation.
var defaultGitOps Operations = NewOperations()
