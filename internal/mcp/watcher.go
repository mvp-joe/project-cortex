package mcp

// Implementation Plan:
// 1. Use fsnotify to watch chunks directory and/or SQLite database file
// 2. Support multiple watch paths (JSON dir + SQLite file)
// 3. Debounce file system events (500ms)
// 4. Trigger reload on debounce timeout
// 5. Handle errors gracefully (keep old state on failure)
// 6. Thread-safe start/stop

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mvp-joe/project-cortex/internal/cache"
)

// Reloadable is an interface for components that can be reloaded.
type Reloadable interface {
	Reload(ctx context.Context) error
}

// FileWatcher watches files/directories for changes and triggers reload.
// Supports watching multiple paths simultaneously (e.g., JSON dir + SQLite file).
type FileWatcher struct {
	reloadable   Reloadable
	watchPaths   []string          // All paths being watched
	watcher      *fsnotify.Watcher
	debounceTime time.Duration
	stopCh       chan struct{}
	doneCh       chan struct{}
	stopOnce     sync.Once
}

// NewFileWatcher creates a new file watcher for the specified directory (legacy method).
// For new code, use NewFileWatcherMulti or NewFileWatcherAuto.
func NewFileWatcher(reloadable Reloadable, watchDir string) (*FileWatcher, error) {
	return NewFileWatcherMulti(reloadable, []string{watchDir})
}

// NewFileWatcherMulti creates a file watcher for multiple paths.
// Use this when you know exactly which paths to watch (JSON dir, SQLite file, etc).
func NewFileWatcherMulti(reloadable Reloadable, watchPaths []string) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create file watcher: %w", err)
	}

	// Add all watch paths
	for _, path := range watchPaths {
		if err := watcher.Add(path); err != nil {
			watcher.Close()
			return nil, fmt.Errorf("failed to watch %s: %w", path, err)
		}
		log.Printf("Watching for changes: %s", path)
	}

	return &FileWatcher{
		reloadable:   reloadable,
		watchPaths:   watchPaths,
		watcher:      watcher,
		debounceTime: 500 * time.Millisecond,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}, nil
}

// NewFileWatcherAuto creates a file watcher with auto-detection of SQLite cache.
// It watches both JSON directory (for legacy support) and SQLite database file (if available).
//
// Strategy:
// - Always watch JSON chunks directory (backward compatibility)
// - Try to detect and watch SQLite database for current branch (preferred)
// - If SQLite DB doesn't exist yet, just watch JSON (indexer will create DB later)
//
// Use cases:
// - New projects: Watches JSON initially, adds SQLite after first index
// - Legacy projects: Watches JSON only
// - Development: Watches both (works regardless of which gets updated)
func NewFileWatcherAuto(reloadable Reloadable, projectPath, chunksDir string) (*FileWatcher, error) {
	paths := []string{chunksDir} // Always watch JSON directory

	// Try to add SQLite database path if it exists
	if projectPath != "" {
		dbPath, err := getSQLiteDatabasePath(projectPath)
		if err == nil && dbPath != "" {
			// Database path is valid, add to watch list
			paths = append(paths, dbPath)
		}
		// Errors are non-fatal - we can still watch JSON
	}

	return NewFileWatcherMulti(reloadable, paths)
}

// getSQLiteDatabasePath determines the SQLite database path for the current branch.
// Returns empty string and error if database doesn't exist or can't be determined.
func getSQLiteDatabasePath(projectPath string) (string, error) {
	// Load cache settings
	settings, err := cache.LoadOrCreateSettings(projectPath)
	if err != nil {
		return "", fmt.Errorf("failed to load cache settings: %w", err)
	}

	// Get current branch
	branch := cache.GetCurrentBranch(projectPath)

	// Construct database path: ~/.cortex/cache/{cacheKey}/branches/{branch}.db
	dbPath := filepath.Join(settings.CacheLocation, "branches", fmt.Sprintf("%s.db", branch))

	// Check if database exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return "", fmt.Errorf("database not found: %s", dbPath)
	}

	return dbPath, nil
}

// Start begins watching for file changes.
func (fw *FileWatcher) Start(ctx context.Context) {
	go fw.watch(ctx)
}

// Stop stops the file watcher.
func (fw *FileWatcher) Stop() {
	fw.stopOnce.Do(func() {
		close(fw.stopCh)
		<-fw.doneCh // Wait for goroutine to finish
		fw.watcher.Close()
	})
}

// watch is the main event loop with debouncing logic.
func (fw *FileWatcher) watch(ctx context.Context) {
	defer close(fw.doneCh)

	var debounceTimer *time.Timer
	reloadCh := make(chan struct{}, 1)

	for {
		select {
		case <-ctx.Done():
			// Context cancellation - clean shutdown
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case <-fw.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-fw.watcher.Events:
			if !ok {
				return
			}

			// Only care about WRITE and CREATE events for JSON files
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				// Reset debounce timer - properly stop and drain
				if debounceTimer != nil {
					// Stop the timer and drain the channel if needed
					if !debounceTimer.Stop() {
						// Timer already fired, drain the channel
						select {
						case <-debounceTimer.C:
						default:
						}
					}
				}
				debounceTimer = time.AfterFunc(fw.debounceTime, func() {
					// Send reload signal (non-blocking)
					select {
					case reloadCh <- struct{}{}:
					default:
					}
				})
			}

		case <-reloadCh:
			// Execute reload
			fw.triggerReload(ctx)

		case err, ok := <-fw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// triggerReload executes a reload of the reloadable component.
func (fw *FileWatcher) triggerReload(ctx context.Context) {
	log.Printf("Reloading...")
	start := time.Now()

	if err := fw.reloadable.Reload(ctx); err != nil {
		log.Printf("Error reloading: %v (keeping old state)", err)
		return
	}

	log.Printf("Reloaded successfully in %v", time.Since(start))
}
