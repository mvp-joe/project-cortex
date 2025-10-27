package mcp

// Implementation Plan:
// 1. Use fsnotify to watch chunks directory
// 2. Debounce file system events (500ms)
// 3. Trigger reload on debounce timeout
// 4. Handle errors gracefully (keep old state on failure)
// 5. Thread-safe start/stop

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// FileWatcher watches a directory for changes and triggers reload.
type FileWatcher struct {
	searcher     ContextSearcher
	watcher      *fsnotify.Watcher
	debounceTime time.Duration
	stopCh       chan struct{}
	doneCh       chan struct{}
	stopOnce     sync.Once
}

// NewFileWatcher creates a new file watcher for the specified directory.
func NewFileWatcher(searcher ContextSearcher, chunksDir string) (*FileWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(chunksDir); err != nil {
		watcher.Close()
		return nil, err
	}

	return &FileWatcher{
		searcher:     searcher,
		watcher:      watcher,
		debounceTime: 500 * time.Millisecond,
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}, nil
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

// triggerReload executes a reload of the chunk database.
func (fw *FileWatcher) triggerReload(ctx context.Context) {
	log.Printf("Reloading chunks...")
	start := time.Now()

	if err := fw.searcher.Reload(ctx); err != nil {
		log.Printf("Error reloading chunks: %v (keeping old state)", err)
		return
	}

	log.Printf("Chunks reloaded successfully in %v", time.Since(start))
}
