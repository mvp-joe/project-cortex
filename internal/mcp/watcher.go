package mcp

// DEPRECATED: This file watcher implementation will be replaced with a source file watcher
// in the daemon phase. Currently stubbed out as SQLite-only storage means we no longer
// need to watch `.cortex/chunks/` JSON files.
//
// Future implementation will watch source files directly for incremental reindexing.

import (
	"context"
	"sync"
)

// Reloadable is an interface for components that can be reloaded.
// This interface is preserved for future use with source file watching.
type Reloadable interface {
	Reload(ctx context.Context) error
}

// FileWatcher is a stub implementation that does nothing.
// Preserved for interface compatibility but will be reimplemented for source file watching.
type FileWatcher struct {
	stopOnce sync.Once
	doneCh   chan struct{}
}

// NewFileWatcher creates a stub file watcher that does nothing.
// Preserved for backward compatibility during migration to SQLite-only storage.
func NewFileWatcher(reloadable Reloadable, watchDir string) (*FileWatcher, error) {
	return &FileWatcher{
		doneCh: make(chan struct{}),
	}, nil
}

// NewFileWatcherMulti creates a stub file watcher that does nothing.
// Preserved for backward compatibility during migration to SQLite-only storage.
func NewFileWatcherMulti(reloadable Reloadable, watchPaths []string) (*FileWatcher, error) {
	return &FileWatcher{
		doneCh: make(chan struct{}),
	}, nil
}

// NewFileWatcherAuto creates a stub file watcher that does nothing.
// Preserved for backward compatibility during migration to SQLite-only storage.
func NewFileWatcherAuto(reloadable Reloadable, projectPath, chunksDir string) (*FileWatcher, error) {
	return &FileWatcher{
		doneCh: make(chan struct{}),
	}, nil
}

// Start does nothing (stub implementation).
func (fw *FileWatcher) Start(ctx context.Context) {
	// Close immediately to signal completion
	close(fw.doneCh)
}

// Stop stops the file watcher (idempotent).
func (fw *FileWatcher) Stop() {
	fw.stopOnce.Do(func() {
		// Ensure doneCh is closed
		select {
		case <-fw.doneCh:
			// Already closed
		default:
			close(fw.doneCh)
		}
	})
}
