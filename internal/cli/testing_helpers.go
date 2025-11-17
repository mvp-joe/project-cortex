package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// initGitRepo initializes a proper git repository with a main branch.
// This is only used by tests that explicitly need a real git repository.
// For most tests, prefer using git.NewMockGitOps() instead.
func initGitRepo(t *testing.T, dir string) {
	t.Helper()

	// Create directory if it doesn't exist
	if err := os.MkdirAll(dir, 0755); err != nil {
		require.NoError(t, err)
	}

	// Initialize git repo with main branch
	cmd := exec.Command("git", "init", "-b", "main")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	// Configure git to avoid warnings
	configCmd := exec.Command("git", "config", "user.name", "Test User")
	configCmd.Dir = dir
	_ = configCmd.Run()

	configCmd = exec.Command("git", "config", "user.email", "test@example.com")
	configCmd.Dir = dir
	_ = configCmd.Run()

	// Create initial commit
	readmeFile := filepath.Join(dir, "README.md")
	require.NoError(t, os.WriteFile(readmeFile, []byte("# Test\n"), 0644))

	cmd = exec.Command("git", "add", "README.md")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())

	cmd = exec.Command("git", "commit", "-m", "Initial commit")
	cmd.Dir = dir
	require.NoError(t, cmd.Run())
}
