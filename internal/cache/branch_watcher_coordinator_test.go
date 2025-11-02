package cache

// Simple test to verify branch watcher integration with coordinator pattern
// This test doesn't spin up the full MCP stack, just verifies the callback mechanism

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBranchWatcher_CallbackIntegration(t *testing.T) {
	t.Parallel()

	// Create test git repo
	dir := createTestGitRepo(t)

	// Track callback invocations
	var oldBranch, newBranch string
	callbackCalled := make(chan bool, 1)

	onBranchChange := func(old, new string) {
		oldBranch = old
		newBranch = new
		select {
		case callbackCalled <- true:
		default:
		}
	}

	// Create watcher
	watcher, err := NewBranchWatcher(dir, onBranchChange)
	require.NoError(t, err)
	defer watcher.Close()

	// Initial branch should be main
	assert.Equal(t, "main", watcher.GetCurrentBranch())

	// Simulate branch change
	headPath := filepath.Join(dir, ".git", "HEAD")
	err = os.WriteFile(headPath, []byte("ref: refs/heads/feature\n"), 0644)
	require.NoError(t, err)

	// Wait for callback
	select {
	case <-callbackCalled:
		assert.Equal(t, "main", oldBranch)
		assert.Equal(t, "feature", newBranch)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Callback not triggered")
	}
}

func TestBranchWatcher_MultipleChanges(t *testing.T) {
	t.Parallel()

	dir := createTestGitRepo(t)

	callCount := 0
	var lastOld, lastNew string
	callbackCalled := make(chan bool, 10)

	onBranchChange := func(old, new string) {
		callCount++
		lastOld = old
		lastNew = new
		callbackCalled <- true
	}

	watcher, err := NewBranchWatcher(dir, onBranchChange)
	require.NoError(t, err)
	defer watcher.Close()

	headPath := filepath.Join(dir, ".git", "HEAD")

	// Change 1: main -> feature
	err = os.WriteFile(headPath, []byte("ref: refs/heads/feature\n"), 0644)
	require.NoError(t, err)

	select {
	case <-callbackCalled:
		assert.Equal(t, "main", lastOld)
		assert.Equal(t, "feature", lastNew)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("First callback not triggered")
	}

	// Change 2: feature -> develop
	err = os.WriteFile(headPath, []byte("ref: refs/heads/develop\n"), 0644)
	require.NoError(t, err)

	select {
	case <-callbackCalled:
		assert.Equal(t, "feature", lastOld)
		assert.Equal(t, "develop", lastNew)
		assert.Equal(t, 2, callCount)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Second callback not triggered")
	}
}
