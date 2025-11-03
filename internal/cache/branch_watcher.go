package cache

import (
	"fmt"
	"log"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// BranchWatcher watches for git branch changes and triggers callbacks.
// It monitors .git/HEAD file modifications to detect when the user switches branches.
type BranchWatcher struct {
	projectPath   string
	watcher       *fsnotify.Watcher
	onChange      func(oldBranch, newBranch string)
	currentBranch string
	mu            sync.Mutex
	stopChan      chan struct{}
	stopped       bool
}

// NewBranchWatcher creates a new branch watcher.
// onChange callback is called when branch changes (debounced by 100ms).
// The callback may be nil (no-op).
func NewBranchWatcher(projectPath string, onChange func(oldBranch, newBranch string)) (*BranchWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	// Watch .git/HEAD file
	headPath := filepath.Join(projectPath, ".git", "HEAD")
	if err := watcher.Add(headPath); err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to watch .git/HEAD: %w", err)
	}

	bw := &BranchWatcher{
		projectPath:   projectPath,
		watcher:       watcher,
		onChange:      onChange,
		currentBranch: GetCurrentBranch(projectPath),
		stopChan:      make(chan struct{}),
	}

	go bw.watch()

	log.Printf("Branch watcher started (current: %s)", bw.currentBranch)
	return bw, nil
}

// watch runs the file watching loop with debouncing.
func (bw *BranchWatcher) watch() {
	var debounceTimer *time.Timer

	for {
		select {
		case event, ok := <-bw.watcher.Events:
			if !ok {
				return
			}

			// We only care about Write events to .git/HEAD
			if event.Op&fsnotify.Write == fsnotify.Write {
				// Reset debounce timer
				if debounceTimer != nil {
					// For AfterFunc timers, Stop() is sufficient
					// (AfterFunc has no channel to drain - C field is nil)
					debounceTimer.Stop()
				}

				// Start new timer
				debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
					bw.checkBranchChange()
				})
			}

		case err, ok := <-bw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Branch watcher error: %v", err)

		case <-bw.stopChan:
			// Stop debounce timer on shutdown
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return
		}
	}
}

// checkBranchChange detects branch changes and triggers callback.
func (bw *BranchWatcher) checkBranchChange() {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	newBranch := GetCurrentBranch(bw.projectPath)

	if newBranch != bw.currentBranch {
		oldBranch := bw.currentBranch
		bw.currentBranch = newBranch

		log.Printf("Branch changed: %s → %s", oldBranch, newBranch)

		if bw.onChange != nil {
			bw.onChange(oldBranch, newBranch)
		}
	}
}

// Close stops the watcher and releases resources.
// Safe to call multiple times.
func (bw *BranchWatcher) Close() error {
	bw.mu.Lock()
	defer bw.mu.Unlock()

	if bw.stopped {
		return nil
	}

	bw.stopped = true
	close(bw.stopChan)
	return bw.watcher.Close()
}

// GetCurrentBranch returns the currently watched branch.
// Thread-safe.
func (bw *BranchWatcher) GetCurrentBranch() string {
	bw.mu.Lock()
	defer bw.mu.Unlock()
	return bw.currentBranch
}
