package watcher

import (
	"context"
	"log"
)

// WatchCoordinator coordinates GitWatcher and FileWatcher, routing events to Indexer.
type WatchCoordinator struct {
	git        GitWatcher
	files      FileWatcher
	branchSync BranchSynchronizer
	indexer    IndexerInterface
}

// NewWatchCoordinator creates a new watch coordinator.
func NewWatchCoordinator(
	git GitWatcher,
	files FileWatcher,
	branchSync BranchSynchronizer,
	indexer IndexerInterface,
) *WatchCoordinator {
	return &WatchCoordinator{
		git:        git,
		files:      files,
		branchSync: branchSync,
		indexer:    indexer,
	}
}

// Start begins coordinating watchers and routing events to the indexer.
// Blocks until context is cancelled.
func (c *WatchCoordinator) Start(ctx context.Context) error {
	// Create error channels for startup failures
	gitErr := make(chan error, 1)
	filesErr := make(chan error, 1)

	// Start git watcher
	go func() {
		if err := c.git.Start(ctx, c.handleBranchSwitch); err != nil {
			gitErr <- err
		}
	}()

	// Start file watcher
	go func() {
		if err := c.files.Start(ctx, c.handleFileChange); err != nil {
			filesErr <- err
		}
	}()

	// Check for immediate startup errors with a small timeout
	// This catches configuration errors (e.g., invalid directory)
	select {
	case err := <-gitErr:
		c.cleanup()
		return err
	case err := <-filesErr:
		c.cleanup()
		return err
	case <-ctx.Done():
		c.cleanup()
		return ctx.Err()
	}
}

// cleanup stops both watchers.
func (c *WatchCoordinator) cleanup() {
	if err := c.git.Stop(); err != nil {
		log.Printf("Warning: git watcher stop failed: %v", err)
	}

	if err := c.files.Stop(); err != nil {
		log.Printf("Warning: file watcher stop failed: %v", err)
	}
}

// handleBranchSwitch coordinates the branch switch operation:
// 1. Pause file watching
// 2. Sync branch DB (copy chunks if possible)
// 3. Switch indexer to new branch
// 4. Resume file watching
func (c *WatchCoordinator) handleBranchSwitch(oldBranch, newBranch string) {
	log.Printf("Branch switch detected: %s → %s", oldBranch, newBranch)

	// 1. Pause file watching (accumulate events but don't fire callback)
	c.files.Pause()
	defer c.files.Resume() // Ensure resume even on error

	// 2. Prepare branch database (copy chunks if possible)
	ctx := context.Background() // Branch sync needs its own context
	if err := c.branchSync.PrepareDB(ctx, newBranch); err != nil {
		log.Printf("Warning: branch sync failed: %v", err)
		// Continue anyway - indexer will work with empty DB
	}

	// 3. Switch indexer to new branch database
	if err := c.indexer.SwitchBranch(newBranch); err != nil {
		log.Printf("Error: failed to switch indexer branch: %v", err)
		return
	}

	log.Printf("✓ Switched to branch: %s", newBranch)
}

// handleFileChange processes file change events from the file watcher.
func (c *WatchCoordinator) handleFileChange(files []string) {
	if len(files) == 0 {
		return
	}

	log.Printf("Processing %d file change(s)...", len(files))

	// Index with hint (optimization - only check these files)
	ctx := context.Background() // File change processing needs its own context
	stats, err := c.indexer.Index(ctx, files)
	if err != nil {
		log.Printf("Error: indexing failed: %v", err)
		return
	}

	log.Printf("✓ Indexed %d file(s) (%d code chunks, %d doc chunks)",
		stats.FilesProcessed, stats.CodeChunks, stats.DocChunks)
}
