package indexer

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// IndexerWatcher watches the root directory for file changes and triggers incremental reindexing.
type IndexerWatcher struct {
	indexer      *indexer
	rootDir      string
	watcher      *fsnotify.Watcher
	debounceTime time.Duration
	stopCh       chan struct{}
	doneCh       chan struct{}
	stopOnce     sync.Once
}

// NewIndexerWatcher creates a new file watcher for the indexer.
func NewIndexerWatcher(idx Indexer, rootDir string) (*IndexerWatcher, error) {
	// Type assert to get the concrete indexer implementation
	indexerImpl, ok := idx.(*indexer)
	if !ok {
		// This shouldn't happen in practice, but handle it gracefully
		log.Printf("Warning: expected *indexer, got %T", idx)
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	iw := &IndexerWatcher{
		indexer:      indexerImpl,
		rootDir:      rootDir,
		watcher:      watcher,
		debounceTime: 500 * time.Millisecond,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}

	// Add directories to watcher recursively
	if err := iw.addDirectoriesRecursively(rootDir); err != nil {
		watcher.Close()
		return nil, err
	}

	return iw, nil
}

// Start begins watching for file changes.
func (iw *IndexerWatcher) Start(ctx context.Context) {
	go iw.watch(ctx)
}

// Stop stops the file watcher.
func (iw *IndexerWatcher) Stop() {
	iw.stopOnce.Do(func() {
		close(iw.stopCh)
		<-iw.doneCh // Wait for goroutine to finish
		iw.watcher.Close()
	})
}

// watch is the main event loop with debouncing logic.
func (iw *IndexerWatcher) watch(ctx context.Context) {
	defer close(iw.doneCh)

	var debounceTimer *time.Timer
	reindexCh := make(chan struct{}, 1)
	changedFiles := make(map[string]bool) // Track changed files for logging

	for {
		select {
		case <-ctx.Done():
			// Context cancellation - clean shutdown
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case <-iw.stopCh:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-iw.watcher.Events:
			if !ok {
				return
			}

			// Filter events - only process relevant file operations
			if !iw.shouldProcessEvent(event) {
				continue
			}

			// Track changed file
			relPath, _ := filepath.Rel(iw.rootDir, event.Name)
			changedFiles[relPath] = true

			// Handle new directories - add them to watcher
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					if iw.shouldWatchDirectory(event.Name) {
						if err := iw.addDirectoriesRecursively(event.Name); err != nil {
							log.Printf("Warning: failed to watch new directory %s: %v", event.Name, err)
						}
					}
				}
			}

			// Reset debounce timer - properly stop and drain
			if debounceTimer != nil {
				if !debounceTimer.Stop() {
					// Timer already fired, drain the channel
					select {
					case <-debounceTimer.C:
					default:
					}
				}
			}

			// Create new timer that will trigger reindexing
			debounceTimer = time.AfterFunc(iw.debounceTime, func() {
				// Send reindex signal (non-blocking)
				select {
				case reindexCh <- struct{}{}:
				default:
				}
			})

		case <-reindexCh:
			// Execute incremental reindex
			iw.triggerReindex(ctx, changedFiles)
			// Clear changed files map for next batch
			changedFiles = make(map[string]bool)

		case err, ok := <-iw.watcher.Errors:
			if !ok {
				return
			}
			log.Printf("File watcher error: %v", err)
		}
	}
}

// triggerReindex executes an incremental reindex.
func (iw *IndexerWatcher) triggerReindex(ctx context.Context, changedFiles map[string]bool) {
	if len(changedFiles) == 0 {
		return
	}

	fileList := make([]string, 0, len(changedFiles))
	for file := range changedFiles {
		fileList = append(fileList, file)
	}

	log.Printf("Reindexing due to changes in %d file(s)...", len(fileList))
	start := time.Now()

	stats, err := iw.indexer.IndexIncremental(ctx)
	if err != nil {
		log.Printf("Error during incremental reindex: %v", err)
		return
	}

	log.Printf("Reindex complete in %v (%d code chunks, %d doc chunks)",
		time.Since(start), stats.TotalCodeChunks, stats.TotalDocChunks)
}

// shouldProcessEvent checks if an event should trigger reindexing.
func (iw *IndexerWatcher) shouldProcessEvent(event fsnotify.Event) bool {
	// Only care about WRITE, CREATE, and REMOVE events
	if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
		return false
	}

	// Get relative path for pattern matching
	relPath, err := filepath.Rel(iw.rootDir, event.Name)
	if err != nil {
		return false
	}

	// Normalize path separators for glob matching
	relPath = filepath.ToSlash(relPath)

	// Check if path should be ignored
	if iw.indexer.discovery.shouldIgnore(relPath) {
		return false
	}

	// Check if it matches code or docs patterns
	if iw.indexer.discovery.matchesAnyPattern(relPath, iw.indexer.discovery.codePatterns) {
		return true
	}
	if iw.indexer.discovery.matchesAnyPattern(relPath, iw.indexer.discovery.docsPatterns) {
		return true
	}

	return false
}

// shouldWatchDirectory checks if a directory should be watched.
func (iw *IndexerWatcher) shouldWatchDirectory(path string) bool {
	relPath, err := filepath.Rel(iw.rootDir, path)
	if err != nil {
		return false
	}

	// Normalize path separators for glob matching
	relPath = filepath.ToSlash(relPath)

	// Don't watch if it matches ignore patterns
	return !iw.indexer.discovery.shouldIgnore(relPath)
}

// addDirectoriesRecursively adds all directories in the tree to the watcher.
func (iw *IndexerWatcher) addDirectoriesRecursively(rootPath string) error {
	return filepath.Walk(rootPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// Log but continue - don't fail the entire watch for one directory
			log.Printf("Warning: error accessing %s: %v", path, err)
			return nil
		}

		// Only add directories
		if !info.IsDir() {
			return nil
		}

		// Check if directory should be watched
		if !iw.shouldWatchDirectory(path) {
			return filepath.SkipDir
		}

		// Add directory to watcher
		if err := iw.watcher.Add(path); err != nil {
			log.Printf("Warning: failed to watch directory %s: %v", path, err)
			return nil // Continue anyway
		}

		return nil
	})
}
