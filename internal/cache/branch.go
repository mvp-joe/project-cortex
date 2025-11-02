// Package cache provides utilities for managing SQLite-based chunk storage.
package cache

import (
	"os/exec"
	"strings"
)

// GetCurrentBranch returns the current git branch name for the given project path.
// For detached HEAD state, returns "detached-{short-hash}".
// If all git commands fail, returns "unknown".
func GetCurrentBranch(projectPath string) string {
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

// FindAncestorBranch attempts to find the ancestor branch for the current branch.
// It tries "main" first, then "master". Returns empty string if no ancestor found.
// This is used for optimization: if a feature branch derives from main, we can
// copy unchanged chunks from main's DB instead of re-indexing.
func FindAncestorBranch(projectPath string, currentBranch string) string {
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

// GetGitBranches returns all local and remote branches for the given project path.
// Branch names are stripped of leading/trailing whitespace.
// The current branch is prefixed with "* " (e.g., "* main").
func GetGitBranches(projectPath string) ([]string, error) {
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
