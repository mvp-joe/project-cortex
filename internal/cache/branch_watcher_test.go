package cache

// Test Plan for Branch Watcher:
// - NewBranchWatcher creates watcher successfully
// - Watcher detects branch changes via .git/HEAD modifications
// - onChange callback is triggered on branch change
// - Debouncing works (100ms delay before callback)
// - Close() stops watcher cleanly
// - Multiple rapid changes are debounced to single callback
// - Non-git directory returns error
// - GetCurrentBranch returns correct branch
// - Detached HEAD is detected correctly

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchWatcher_DetectChange(t *testing.T) {
	t.Parallel()

	// Create test git repo
	dir := createTestGitRepo(t)

	// Track callback invocations
	var oldBranch, newBranch string
	called := make(chan bool, 1)

	watcher, err := NewBranchWatcher(dir, func(old, new string) {
		oldBranch = old
		newBranch = new
		called <- true
	})
	require.NoError(t, err)
	defer watcher.Close()

	// Initial branch should be main
	assert.Equal(t, "main", watcher.GetCurrentBranch())

	// Simulate branch change by modifying .git/HEAD
	headPath := filepath.Join(dir, ".git", "HEAD")
	err = os.WriteFile(headPath, []byte("ref: refs/heads/feature\n"), 0644)
	require.NoError(t, err)

	// Wait for callback (with timeout)
	select {
	case <-called:
		assert.Equal(t, "main", oldBranch)
		assert.Equal(t, "feature", newBranch)
		assert.Equal(t, "feature", watcher.GetCurrentBranch())
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Callback not triggered")
	}
}

func TestBranchWatcher_Debouncing(t *testing.T) {
	t.Parallel()

	// Create test git repo
	dir := createTestGitRepo(t)

	// Track callback invocations
	callCount := 0
	var lastBranch string
	called := make(chan bool, 10)

	watcher, err := NewBranchWatcher(dir, func(old, new string) {
		callCount++
		lastBranch = new
		called <- true
	})
	require.NoError(t, err)
	defer watcher.Close()

	headPath := filepath.Join(dir, ".git", "HEAD")

	// Simulate rapid branch changes (3 changes within 50ms)
	for i, branch := range []string{"feature1", "feature2", "feature3"} {
		content := "ref: refs/heads/" + branch + "\n"
		err = os.WriteFile(headPath, []byte(content), 0644)
		require.NoError(t, err)

		// Small delay between writes (less than debounce time)
		if i < 2 {
			time.Sleep(20 * time.Millisecond)
		}
	}

	// Wait for debounced callback
	select {
	case <-called:
		// Should get exactly one callback for the final state
		assert.Equal(t, "feature3", lastBranch)
		assert.Equal(t, "feature3", watcher.GetCurrentBranch())

		// No additional callbacks should arrive
		select {
		case <-called:
			t.Fatal("Received multiple callbacks (debouncing failed)")
		case <-time.After(200 * time.Millisecond):
			// Good - no extra callbacks
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Callback not triggered")
	}
}

func TestBranchWatcher_NoChangeNoCallback(t *testing.T) {
	t.Parallel()

	// Create test git repo
	dir := createTestGitRepo(t)

	// Track callback invocations
	called := make(chan bool, 1)

	watcher, err := NewBranchWatcher(dir, func(old, new string) {
		called <- true
	})
	require.NoError(t, err)
	defer watcher.Close()

	headPath := filepath.Join(dir, ".git", "HEAD")

	// Write the same branch again
	err = os.WriteFile(headPath, []byte("ref: refs/heads/main\n"), 0644)
	require.NoError(t, err)

	// Should not trigger callback (branch didn't change)
	select {
	case <-called:
		t.Fatal("Callback triggered despite no branch change")
	case <-time.After(300 * time.Millisecond):
		// Good - no callback
	}
}

func TestBranchWatcher_DetachedHEAD(t *testing.T) {
	t.Parallel()

	// Create test git repo
	dir := createTestGitRepo(t)

	// Get the current commit hash
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	output, err := cmd.Output()
	require.NoError(t, err)
	commitHash := string(output)

	// Track callback invocations
	var oldBranch, newBranch string
	called := make(chan bool, 1)

	watcher, err := NewBranchWatcher(dir, func(old, new string) {
		oldBranch = old
		newBranch = new
		called <- true
	})
	require.NoError(t, err)
	defer watcher.Close()

	headPath := filepath.Join(dir, ".git", "HEAD")

	// Manually write detached HEAD (commit hash instead of ref)
	// This simulates the state after `git checkout --detach`
	err = os.WriteFile(headPath, []byte(commitHash), 0644)
	require.NoError(t, err)

	// Wait for callback
	select {
	case <-called:
		assert.Equal(t, "main", oldBranch)
		// NewBranch will be "detached-{short-hash}" (see GetCurrentBranch implementation)
		assert.Contains(t, newBranch, "detached-")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Callback not triggered")
	}
}

func TestBranchWatcher_Close(t *testing.T) {
	t.Parallel()

	// Create test git repo
	dir := createTestGitRepo(t)

	called := make(chan bool, 1)

	watcher, err := NewBranchWatcher(dir, func(old, new string) {
		called <- true
	})
	require.NoError(t, err)

	// Close watcher
	err = watcher.Close()
	require.NoError(t, err)

	// Multiple closes should be safe
	err = watcher.Close()
	require.NoError(t, err)

	headPath := filepath.Join(dir, ".git", "HEAD")

	// Modify HEAD after closing
	err = os.WriteFile(headPath, []byte("ref: refs/heads/feature\n"), 0644)
	require.NoError(t, err)

	// Should not trigger callback (watcher stopped)
	select {
	case <-called:
		t.Fatal("Callback triggered after Close()")
	case <-time.After(300 * time.Millisecond):
		// Good - no callback
	}
}

func TestBranchWatcher_NonGitDirectory(t *testing.T) {
	t.Parallel()

	// Create temp directory without .git
	dir := t.TempDir()

	_, err := NewBranchWatcher(dir, func(old, new string) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to watch")
}

func TestBranchWatcher_NilCallback(t *testing.T) {
	t.Parallel()

	// Create test git repo
	dir := createTestGitRepo(t)

	// Should accept nil callback (no-op)
	watcher, err := NewBranchWatcher(dir, nil)
	require.NoError(t, err)
	defer watcher.Close()

	headPath := filepath.Join(dir, ".git", "HEAD")

	// Should not panic when callback is nil
	err = os.WriteFile(headPath, []byte("ref: refs/heads/feature\n"), 0644)
	require.NoError(t, err)

	// Wait to ensure no panic
	time.Sleep(200 * time.Millisecond)
}
