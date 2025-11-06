package watcher

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// gitWatcher is the concrete implementation of GitWatcher.
type gitWatcher struct {
	gitDir      string
	headPath    string
	watcher     *fsnotify.Watcher
	lastBranch  string
	stopCh      chan struct{}
	doneCh      chan struct{}
	stopOnce    sync.Once
	mu          sync.RWMutex // Protects lastBranch
}

// NewGitWatcher creates a new GitWatcher for the given git directory.
// gitDir should be the path to .git directory.
// Returns error if .git/HEAD doesn't exist or cannot be accessed.
func NewGitWatcher(gitDir string) (GitWatcher, error) {
	headPath := filepath.Join(gitDir, "HEAD")

	// Verify HEAD file exists
	if _, err := os.Stat(headPath); err != nil {
		return nil, fmt.Errorf("cannot access .git/HEAD: %w", err)
	}

	// Create fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create watcher: %w", err)
	}

	// Read initial branch
	initialBranch, err := readBranch(headPath)
	if err != nil {
		watcher.Close()
		return nil, fmt.Errorf("failed to read initial branch: %w", err)
	}

	return &gitWatcher{
		gitDir:     gitDir,
		headPath:   headPath,
		watcher:    watcher,
		lastBranch: initialBranch,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
	}, nil
}

// Start begins monitoring .git/HEAD for changes.
func (gw *gitWatcher) Start(ctx context.Context, callback func(oldBranch, newBranch string)) error {
	// Watch the .git directory instead of the HEAD file directly
	// This ensures we catch the file even if it's deleted and recreated
	if err := gw.watcher.Add(gw.gitDir); err != nil {
		return fmt.Errorf("failed to watch .git directory: %w", err)
	}

	// Start watch goroutine
	go gw.watch(ctx, callback)

	return nil
}

// Stop stops the watcher and cleans up resources.
func (gw *gitWatcher) Stop() error {
	var err error
	gw.stopOnce.Do(func() {
		close(gw.stopCh)
		<-gw.doneCh // Wait for goroutine to finish
		err = gw.watcher.Close()
	})
	return err
}

// watch is the main event loop.
func (gw *gitWatcher) watch(ctx context.Context, callback func(oldBranch, newBranch string)) {
	defer close(gw.doneCh)

	for {
		select {
		case <-ctx.Done():
			return

		case <-gw.stopCh:
			return

		case event, ok := <-gw.watcher.Events:
			if !ok {
				return
			}

			// Only process events for the HEAD file
			if event.Name != gw.headPath {
				continue
			}

			// Only care about WRITE, CREATE, and REMOVE events
			// (CREATE happens when HEAD is recreated after deletion)
			// (REMOVE happens when HEAD is deleted - we'll wait for recreation)
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
				continue
			}

			// If file was removed, skip this event (wait for recreation)
			if event.Op&fsnotify.Remove != 0 {
				continue
			}

			// Read new branch
			newBranch, err := readBranch(gw.headPath)
			if err != nil {
				log.Printf("Warning: failed to read .git/HEAD: %v", err)
				continue
			}

			// Check if branch changed
			gw.mu.RLock()
			oldBranch := gw.lastBranch
			gw.mu.RUnlock()

			if newBranch != oldBranch {
				// Update last known branch
				gw.mu.Lock()
				gw.lastBranch = newBranch
				gw.mu.Unlock()

				// Fire callback with panic recovery
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("Warning: git watcher callback panic: %v", r)
						}
					}()
					callback(oldBranch, newBranch)
				}()
			}

		case err, ok := <-gw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("Git watcher error: %v", err)
		}
	}
}

// readBranch reads and parses the current branch from .git/HEAD.
func readBranch(headPath string) (string, error) {
	content, err := os.ReadFile(headPath)
	if err != nil {
		return "", err
	}
	return parseBranch(content), nil
}

// parseBranch parses branch name from HEAD file content.
// Returns branch name, or "detached" for detached HEAD.
func parseBranch(content []byte) string {
	line := strings.TrimSpace(string(content))

	// Check for symbolic ref: "ref: refs/heads/branch-name"
	if strings.HasPrefix(line, "ref: refs/heads/") {
		branchName := strings.TrimPrefix(line, "ref: refs/heads/")
		return strings.TrimSpace(branchName)
	}

	// If it's a 40-character hex string, it's a detached HEAD
	if len(line) == 40 && isHexString(line) {
		return "detached"
	}

	// Fallback: treat as branch name (shouldn't normally happen)
	return strings.TrimSpace(line)
}

// isHexString checks if a string contains only hexadecimal characters.
func isHexString(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
