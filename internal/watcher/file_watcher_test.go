package watcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for FileWatcher:
// - NewFileWatcher creates watcher successfully with valid directories
// - NewFileWatcher returns error with invalid directory
// - Single file change fires callback after debounce
// - Multiple file changes are batched into one callback
// - Debouncing works (rapid changes coalesced into single callback)
// - Pause/Resume behavior (accumulate during pause, fire on resume)
// - File created triggers callback
// - File deleted triggers callback
// - File renamed triggers callback (may appear as delete + create)
// - Directory added triggers recursive watch
// - Stop() cleanup (no goroutine leaks)
// - Context cancellation stops watcher
// - Extension filtering (only monitored extensions trigger callback)
// - Deduplication (same file modified twice appears once in batch)
// - Concurrent Stop() calls are safe

// Test: NewFileWatcher creates watcher successfully with valid directories
func TestNewFileWatcher_Success(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go", ".ts", ".md"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	require.NotNil(t, watcher)

	// Clean up
	require.NoError(t, watcher.Stop())
}

// Test: NewFileWatcher returns error with invalid directory
func TestNewFileWatcher_InvalidDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nonexistent := filepath.Join(tempDir, "nonexistent")

	watcher, err := NewFileWatcher([]string{nonexistent}, []string{".go"})
	assert.Error(t, err)
	assert.Nil(t, watcher)
}

// Test: Single file change fires callback after debounce
func TestFileWatcher_SingleFileChange(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	// Track callback invocations
	var callbackMu sync.Mutex
	var callbackFiles []string
	callbackCalled := make(chan struct{})

	callback := func(files []string) {
		callbackMu.Lock()
		callbackFiles = files
		callbackMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))

	// Wait for watcher to initialize
	time.Sleep(100 * time.Millisecond)

	// Create a file
	testFile := filepath.Join(tempDir, "test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0644))

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called after timeout")
	}

	// Verify callback received the file
	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.Equal(t, 1, len(callbackFiles))
	assert.Contains(t, callbackFiles, testFile)
}

// Test: Multiple file changes are batched into one callback
func TestFileWatcher_MultipleFileChanges(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	var callbackMu sync.Mutex
	var callbackFiles []string
	callbackCalled := make(chan struct{})

	callback := func(files []string) {
		callbackMu.Lock()
		callbackFiles = files
		callbackMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Create multiple files rapidly (within debounce window)
	file1 := filepath.Join(tempDir, "file1.go")
	file2 := filepath.Join(tempDir, "file2.go")
	file3 := filepath.Join(tempDir, "file3.go")

	require.NoError(t, os.WriteFile(file1, []byte("package main"), 0644))
	time.Sleep(50 * time.Millisecond) // Less than debounce time
	require.NoError(t, os.WriteFile(file2, []byte("package main"), 0644))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(file3, []byte("package main"), 0644))

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called after timeout")
	}

	// Verify all files are in the batch
	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.Equal(t, 3, len(callbackFiles))
	assert.Contains(t, callbackFiles, file1)
	assert.Contains(t, callbackFiles, file2)
	assert.Contains(t, callbackFiles, file3)
}

// Test: Debouncing works (rapid changes coalesced into single callback)
func TestFileWatcher_Debouncing(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	// Reduce debounce time for faster test
	fw := watcher.(*fileWatcher)
	fw.debounceTime = 200 * time.Millisecond

	callbackCount := 0
	var countMu sync.Mutex
	callbackCalled := make(chan struct{}, 10) // Buffered to not block

	callback := func(files []string) {
		countMu.Lock()
		callbackCount++
		countMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Modify same file rapidly (should coalesce into one callback)
	testFile := filepath.Join(tempDir, "test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n// v1"), 0644))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n// v2"), 0644))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n// v3"), 0644))

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called after timeout")
	}

	// Wait a bit more to ensure no additional callbacks
	time.Sleep(500 * time.Millisecond)

	// Should only have one callback despite multiple changes
	countMu.Lock()
	defer countMu.Unlock()
	assert.Equal(t, 1, callbackCount, "Should have exactly one callback due to debouncing")
}

// Test: Pause/Resume behavior (accumulate during pause, fire on resume)
func TestFileWatcher_PauseResume(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	var callbackMu sync.Mutex
	var callbackFiles []string
	callbackCalled := make(chan struct{}, 10)

	callback := func(files []string) {
		callbackMu.Lock()
		callbackFiles = append(callbackFiles, files...)
		callbackMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Pause watching
	watcher.Pause()

	// Create file while paused
	pausedFile := filepath.Join(tempDir, "paused.go")
	require.NoError(t, os.WriteFile(pausedFile, []byte("package main"), 0644))

	// Wait beyond debounce period - callback should NOT fire
	time.Sleep(1 * time.Second)

	callbackMu.Lock()
	countWhilePaused := len(callbackFiles)
	callbackMu.Unlock()
	assert.Equal(t, 0, countWhilePaused, "No callbacks should fire while paused")

	// Resume - should fire callback immediately with accumulated events
	watcher.Resume()

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Callback not called after Resume()")
	}

	// Verify callback received the file
	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.Contains(t, callbackFiles, pausedFile)
}

// Test: File created triggers callback
func TestFileWatcher_FileCreated(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	callbackCalled := make(chan struct{})
	var receivedFile string

	callback := func(files []string) {
		if len(files) > 0 {
			receivedFile = files[0]
			callbackCalled <- struct{}{}
		}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Create file
	newFile := filepath.Join(tempDir, "new.go")
	require.NoError(t, os.WriteFile(newFile, []byte("package main"), 0644))

	// Wait for callback
	select {
	case <-callbackCalled:
		assert.Equal(t, newFile, receivedFile)
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called after file creation")
	}
}

// Test: File deleted triggers callback
func TestFileWatcher_FileDeleted(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	// Create initial file
	testFile := filepath.Join(tempDir, "test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main"), 0644))

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	callbackCalled := make(chan struct{})
	var receivedFile string

	callback := func(files []string) {
		if len(files) > 0 {
			receivedFile = files[0]
			callbackCalled <- struct{}{}
		}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Delete file
	require.NoError(t, os.Remove(testFile))

	// Wait for callback
	select {
	case <-callbackCalled:
		assert.Equal(t, testFile, receivedFile)
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called after file deletion")
	}
}

// Test: File renamed triggers callback
func TestFileWatcher_FileRenamed(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	// Create initial file
	oldFile := filepath.Join(tempDir, "old.go")
	require.NoError(t, os.WriteFile(oldFile, []byte("package main"), 0644))

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	var callbackMu sync.Mutex
	var callbackFiles []string
	callbackCalled := make(chan struct{})

	callback := func(files []string) {
		callbackMu.Lock()
		callbackFiles = files
		callbackMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Rename file (may trigger RENAME or CREATE event)
	newFile := filepath.Join(tempDir, "new.go")
	require.NoError(t, os.Rename(oldFile, newFile))

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success - at least one event should be detected
		callbackMu.Lock()
		assert.NotEmpty(t, callbackFiles)
		callbackMu.Unlock()
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called after file rename")
	}
}

// Test: Directory added triggers recursive watch
func TestFileWatcher_DirectoryAdded(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	var callbackMu sync.Mutex
	var allCallbackFiles []string
	callbackCalled := make(chan struct{}, 10)

	callback := func(files []string) {
		callbackMu.Lock()
		allCallbackFiles = append(allCallbackFiles, files...)
		callbackMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Create new directory
	newDir := filepath.Join(tempDir, "newdir")
	require.NoError(t, os.Mkdir(newDir, 0755))

	// Wait for directory to be added to watcher
	time.Sleep(300 * time.Millisecond)

	// Create file in new directory
	fileInNewDir := filepath.Join(newDir, "test.go")
	require.NoError(t, os.WriteFile(fileInNewDir, []byte("package main"), 0644))

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called for file in new directory")
	}

	// Verify file in new directory was detected
	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.Contains(t, allCallbackFiles, fileInNewDir)
}

// Test: Stop() cleanup (no goroutine leaks)
func TestFileWatcher_StopCleanup(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)

	callback := func(files []string) {}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Stop should complete without blocking
	start := time.Now()
	require.NoError(t, watcher.Stop())
	elapsed := time.Since(start)

	// Should stop quickly
	assert.Less(t, elapsed, 500*time.Millisecond)

	// Calling Stop() again should be safe
	require.NoError(t, watcher.Stop())
}

// Test: Context cancellation stops watcher
func TestFileWatcher_ContextCancellation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	callback := func(files []string) {}

	ctx, cancel := context.WithCancel(context.Background())
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	start := time.Now()
	cancel()

	// Wait for watcher to stop
	fw := watcher.(*fileWatcher)
	<-fw.doneCh
	elapsed := time.Since(start)

	// Should stop quickly
	assert.Less(t, elapsed, 500*time.Millisecond)
}

// Test: Extension filtering (only monitored extensions trigger callback)
func TestFileWatcher_ExtensionFiltering(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go", ".md"} // Only .go and .md

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	var callbackMu sync.Mutex
	var callbackFiles []string
	callbackCalled := make(chan struct{}, 10)

	callback := func(files []string) {
		callbackMu.Lock()
		callbackFiles = append(callbackFiles, files...)
		callbackMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Create files with different extensions
	goFile := filepath.Join(tempDir, "test.go")
	mdFile := filepath.Join(tempDir, "README.md")
	txtFile := filepath.Join(tempDir, "notes.txt")
	jsFile := filepath.Join(tempDir, "app.js")

	require.NoError(t, os.WriteFile(goFile, []byte("package main"), 0644))
	require.NoError(t, os.WriteFile(mdFile, []byte("# Title"), 0644))
	require.NoError(t, os.WriteFile(txtFile, []byte("notes"), 0644))
	require.NoError(t, os.WriteFile(jsFile, []byte("console.log()"), 0644))

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called")
	}

	// Verify only .go and .md files are detected
	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.Contains(t, callbackFiles, goFile)
	assert.Contains(t, callbackFiles, mdFile)
	assert.NotContains(t, callbackFiles, txtFile)
	assert.NotContains(t, callbackFiles, jsFile)
}

// Test: Deduplication (same file modified twice appears once in batch)
func TestFileWatcher_Deduplication(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)
	defer watcher.Stop()

	var callbackMu sync.Mutex
	var callbackFiles []string
	callbackCalled := make(chan struct{})

	callback := func(files []string) {
		callbackMu.Lock()
		callbackFiles = files
		callbackMu.Unlock()
		callbackCalled <- struct{}{}
	}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Modify same file multiple times rapidly
	testFile := filepath.Join(tempDir, "test.go")
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n// v1"), 0644))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n// v2"), 0644))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, os.WriteFile(testFile, []byte("package main\n// v3"), 0644))

	// Wait for callback
	select {
	case <-callbackCalled:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Callback not called")
	}

	// Should only appear once in the list
	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.Equal(t, 1, len(callbackFiles), "File should appear only once despite multiple modifications")
	assert.Equal(t, testFile, callbackFiles[0])
}

// Test: Concurrent Stop() calls are safe
func TestFileWatcher_ConcurrentStop(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	extensions := []string{".go"}

	watcher, err := NewFileWatcher([]string{tempDir}, extensions)
	require.NoError(t, err)

	callback := func(files []string) {}

	ctx := context.Background()
	require.NoError(t, watcher.Start(ctx, callback))
	time.Sleep(100 * time.Millisecond)

	// Call Stop() concurrently from multiple goroutines
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			watcher.Stop()
		}()
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Should not panic or deadlock
}
