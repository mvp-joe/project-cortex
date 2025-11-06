package watcher

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for GitWatcher:
// - Start monitors .git/HEAD for changes
// - Detects branch switch from one branch to another
// - Handles initial branch (no "old" branch, use empty string)
// - Handles detached HEAD (returns hash or "detached")
// - Stop() cleanup (no goroutine leaks)
// - Context cancellation (clean shutdown)
// - Rapid branch switching (should fire callback for each)
// - .git/HEAD deleted/recreated (error handling)
// - Callback errors don't crash watcher
// - Concurrent Stop() calls are safe (sync.Once)
// - Parse symbolic refs correctly (ref: refs/heads/main)
// - Parse detached HEAD correctly (40 char SHA-1)

// Test: Detect branch switch from main to feature
func TestGitWatcher_BranchSwitch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))

	// Initial state: main branch
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	// Track callback invocations
	var mu sync.Mutex
	var invocations []struct{ old, new string }
	callback := func(oldBranch, newBranch string) {
		mu.Lock()
		invocations = append(invocations, struct{ old, new string }{oldBranch, newBranch})
		mu.Unlock()
	}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)
	require.NotNil(t, watcher)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = watcher.Start(ctx, callback)
	require.NoError(t, err)
	defer watcher.Stop()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Switch to feature branch
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/feature\n"), 0644))

	// Wait for fsnotify to detect change
	time.Sleep(200 * time.Millisecond)

	// Verify callback was invoked
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, invocations, 1, "Should have one callback invocation")
	assert.Equal(t, "main", invocations[0].old)
	assert.Equal(t, "feature", invocations[0].new)
}

// Test: Handle initial branch (no "old" branch, use empty string)
func TestGitWatcher_InitialBranch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))

	// Initial state: main branch
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	// Track callback invocations
	var mu sync.Mutex
	var invocations []struct{ old, new string }
	callback := func(oldBranch, newBranch string) {
		mu.Lock()
		invocations = append(invocations, struct{ old, new string }{oldBranch, newBranch})
		mu.Unlock()
	}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)
	defer watcher.Stop()

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Verify no callback on initial start (no branch change)
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, invocations, 0, "Should have no callback invocations on start")
}

// Test: Handle detached HEAD
func TestGitWatcher_DetachedHEAD(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))

	// Initial state: main branch
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	// Track callback invocations
	var mu sync.Mutex
	var invocations []struct{ old, new string }
	callback := func(oldBranch, newBranch string) {
		mu.Lock()
		invocations = append(invocations, struct{ old, new string }{oldBranch, newBranch})
		mu.Unlock()
	}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)
	defer watcher.Stop()

	time.Sleep(100 * time.Millisecond)

	// Switch to detached HEAD
	commitHash := "a1b2c3d4e5f6789012345678901234567890abcd"
	require.NoError(t, os.WriteFile(headFile, []byte(commitHash+"\n"), 0644))

	time.Sleep(200 * time.Millisecond)

	// Verify callback was invoked with detached HEAD
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, invocations, 1)
	assert.Equal(t, "main", invocations[0].old)
	assert.Equal(t, "detached", invocations[0].new)
}

// Test: Stop() cleanup (no goroutine leaks)
func TestGitWatcher_Stop_NoGoroutineLeaks(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	callback := func(oldBranch, newBranch string) {}

	// Count goroutines before
	goroutinesBefore := runtime.NumGoroutine()

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Stop watcher
	err = watcher.Stop()
	require.NoError(t, err)

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	// Count goroutines after - should be same or less (some cleanup may have happened)
	goroutinesAfter := runtime.NumGoroutine()
	assert.LessOrEqual(t, goroutinesAfter, goroutinesBefore+1,
		"Should have no goroutine leaks (before: %d, after: %d)", goroutinesBefore, goroutinesAfter)
}

// Test: Context cancellation (clean shutdown)
func TestGitWatcher_ContextCancellation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	callback := func(oldBranch, newBranch string) {}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Cancel context and measure shutdown time
	start := time.Now()
	cancel()
	err = watcher.Stop()
	require.NoError(t, err)
	shutdownTime := time.Since(start)

	// Should stop within 200ms
	assert.Less(t, shutdownTime, 200*time.Millisecond, "Watcher should stop quickly on context cancellation")
}

// Test: Rapid branch switching (should fire callback for each)
func TestGitWatcher_RapidBranchSwitching(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	// Track callback invocations
	var mu sync.Mutex
	var invocations []struct{ old, new string }
	callback := func(oldBranch, newBranch string) {
		mu.Lock()
		invocations = append(invocations, struct{ old, new string }{oldBranch, newBranch})
		mu.Unlock()
	}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)
	defer watcher.Stop()

	time.Sleep(100 * time.Millisecond)

	// Rapid branch switches
	branches := []string{"feature-1", "feature-2", "feature-3"}
	for _, branch := range branches {
		require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/"+branch+"\n"), 0644))
		time.Sleep(50 * time.Millisecond)
	}

	// Wait for all events to be processed
	time.Sleep(500 * time.Millisecond)

	// Verify all callbacks were invoked
	mu.Lock()
	defer mu.Unlock()

	require.Len(t, invocations, 3, "Should have three callback invocations")
	assert.Equal(t, "main", invocations[0].old)
	assert.Equal(t, "feature-1", invocations[0].new)
	assert.Equal(t, "feature-1", invocations[1].old)
	assert.Equal(t, "feature-2", invocations[1].new)
	assert.Equal(t, "feature-2", invocations[2].old)
	assert.Equal(t, "feature-3", invocations[2].new)
}

// Test: .git/HEAD deleted/recreated (error handling)
func TestGitWatcher_HEADDeleted(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	// Track callback invocations
	var mu sync.Mutex
	var invocations []struct{ old, new string }
	callback := func(oldBranch, newBranch string) {
		mu.Lock()
		invocations = append(invocations, struct{ old, new string }{oldBranch, newBranch})
		mu.Unlock()
	}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)
	defer watcher.Stop()

	time.Sleep(100 * time.Millisecond)

	// Delete HEAD file
	require.NoError(t, os.Remove(headFile))
	time.Sleep(200 * time.Millisecond)

	// Recreate HEAD file with different branch
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/develop\n"), 0644))
	time.Sleep(200 * time.Millisecond)

	// Verify watcher handled the deletion and recreation
	mu.Lock()
	defer mu.Unlock()

	// Should have detected the change when recreated
	assert.GreaterOrEqual(t, len(invocations), 1, "Should detect branch change after HEAD recreation")
	if len(invocations) > 0 {
		assert.Equal(t, "develop", invocations[len(invocations)-1].new)
	}
}

// Test: Callback errors don't crash watcher
func TestGitWatcher_CallbackPanic(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	// Callback that panics (with proper synchronization)
	var mu sync.Mutex
	panicCount := 0
	callback := func(oldBranch, newBranch string) {
		mu.Lock()
		panicCount++
		shouldPanic := panicCount == 1
		mu.Unlock()

		if shouldPanic {
			panic("test panic")
		}
	}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)
	defer watcher.Stop()

	time.Sleep(100 * time.Millisecond)

	// Trigger first callback (will panic)
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/feature\n"), 0644))
	time.Sleep(200 * time.Millisecond)

	// Trigger second callback (should still work)
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/develop\n"), 0644))
	time.Sleep(200 * time.Millisecond)

	// Watcher should still be running
	mu.Lock()
	finalCount := panicCount
	mu.Unlock()
	assert.Equal(t, 2, finalCount, "Both callbacks should have been called despite first panic")
}

// Test: Concurrent Stop() calls are safe (sync.Once)
func TestGitWatcher_ConcurrentStop(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")
	headFile := filepath.Join(gitDir, "HEAD")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	require.NoError(t, os.WriteFile(headFile, []byte("ref: refs/heads/main\n"), 0644))

	callback := func(oldBranch, newBranch string) {}

	watcher, err := NewGitWatcher(gitDir)
	require.NoError(t, err)

	ctx := context.Background()
	err = watcher.Start(ctx, callback)
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)

	// Call Stop multiple times concurrently
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = watcher.Stop()
		}()
	}

	wg.Wait()

	// Should not panic or deadlock
}

// Test: Parse symbolic refs correctly (ref: refs/heads/main)
func TestGitWatcher_ParseSymbolicRef(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "Main branch",
			content:  "ref: refs/heads/main\n",
			expected: "main",
		},
		{
			name:     "Feature branch",
			content:  "ref: refs/heads/feature/new-feature\n",
			expected: "feature/new-feature",
		},
		{
			name:     "No newline",
			content:  "ref: refs/heads/develop",
			expected: "develop",
		},
		{
			name:     "Extra whitespace",
			content:  "ref: refs/heads/main  \n",
			expected: "main",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			branch := parseBranch([]byte(tc.content))
			assert.Equal(t, tc.expected, branch)
		})
	}
}

// Test: Parse detached HEAD correctly (40 char SHA-1)
func TestGitWatcher_ParseDetachedHEAD(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		content  string
		expected string
	}{
		{
			name:     "Full SHA-1",
			content:  "a1b2c3d4e5f6789012345678901234567890abcd\n",
			expected: "detached",
		},
		{
			name:     "No newline",
			content:  "a1b2c3d4e5f6789012345678901234567890abcd",
			expected: "detached",
		},
		{
			name:     "SHA-1 with extra whitespace",
			content:  "a1b2c3d4e5f6789012345678901234567890abcd  \n",
			expected: "detached",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			branch := parseBranch([]byte(tc.content))
			assert.Equal(t, tc.expected, branch)
		})
	}
}

// Test: NewGitWatcher returns error for non-existent git directory
func TestGitWatcher_InvalidGitDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, "nonexistent")

	watcher, err := NewGitWatcher(gitDir)
	assert.Error(t, err)
	assert.Nil(t, watcher)
}

// Test: NewGitWatcher returns error when .git/HEAD doesn't exist
func TestGitWatcher_MissingHEAD(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gitDir := filepath.Join(tempDir, ".git")

	require.NoError(t, os.MkdirAll(gitDir, 0755))
	// Don't create HEAD file

	watcher, err := NewGitWatcher(gitDir)
	assert.Error(t, err)
	assert.Nil(t, watcher)
}
