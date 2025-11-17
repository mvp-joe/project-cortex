package watcher

import "context"

// FileWatcher monitors source files for changes with debouncing and pause/resume support.
type FileWatcher interface {
	// Start begins watching source directories, calling callback with debounced file changes.
	Start(ctx context.Context, callback func(files []string)) error

	// Stop stops the file watcher and cleans up resources.
	Stop() error

	// Pause stops firing callbacks but continues accumulating events.
	Pause()

	// Resume resumes firing callbacks. If events accumulated during pause, fires immediately.
	Resume()
}

// BranchSynchronizer prepares branch databases, optionally copying chunks from ancestor.
type BranchSynchronizer interface {
	// PrepareDB ensures branch database exists and is optimized.
	// If branch has ancestor with overlapping files, copies unchanged chunks.
	PrepareDB(ctx context.Context, branch string) error
}

// IndexerInterface defines the minimal interface needed for the new Indexer.
type IndexerInterface interface {
	// Index discovers changes and processes them.
	// hint: Optional list of files that changed (from watcher). If empty, full discovery.
	Index(ctx context.Context, hint []string) (*IndexStats, error)

	// SwitchBranch reconnects to different branch database.
	SwitchBranch(branch string) error
}

// IndexStats contains statistics about indexing operations.
type IndexStats struct {
	FilesProcessed int
	CodeChunks     int
	DocChunks      int
}
