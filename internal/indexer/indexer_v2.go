package indexer

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"path/filepath"
	"time"
)

// IndexerV2Stats contains comprehensive statistics about the indexing operation.
type IndexerV2Stats struct {
	FilesAdded         int
	FilesModified      int
	FilesDeleted       int
	FilesUnchanged     int
	CodeFilesProcessed int
	DocsProcessed      int
	TotalCodeChunks    int
	TotalDocChunks     int
	IndexingTime       time.Duration
}

// IndexerV2 is the new indexer implementation following the refactor spec.
// It orchestrates change detection, processing, and graph updates.
type IndexerV2 struct {
	rootDir        string
	changeDetector ChangeDetector
	processor      Processor
	storage        Storage
	graphUpdater   *GraphUpdater
}

// NewIndexerV2 creates a new v2 indexer instance.
func NewIndexerV2(
	rootDir string,
	changeDetector ChangeDetector,
	processor Processor,
	storage Storage,
	db *sql.DB,
) *IndexerV2 {
	return &IndexerV2{
		rootDir:        rootDir,
		changeDetector: changeDetector,
		processor:      processor,
		storage:        storage,
		graphUpdater:   NewGraphUpdater(db, rootDir),
	}
}

// Index discovers changes and processes them.
// hint: Optional list of files that changed (from watcher). If empty, full discovery.
//
// This method handles both full indexing (hint is nil/empty) and incremental indexing
// (hint contains specific files). The change detector optimizes based on the hint.
//
// The indexing flow:
//  1. Detect changes (read-only, no side effects)
//  2. Delete removed files (cascade deletes chunks via FK)
//  3. Update metadata for unchanged files (mtime drift correction)
//  4. Process changed files (added + modified)
//  5. Update graph (incremental, best-effort)
func (idx *IndexerV2) Index(ctx context.Context, hint []string) (*IndexerV2Stats, error) {
	startTime := time.Now()

	// 1. Detect changes (read-only, no side effects)
	changes, err := idx.changeDetector.DetectChanges(ctx, hint)
	if err != nil {
		return nil, fmt.Errorf("change detection failed: %w", err)
	}

	stats := &IndexerV2Stats{
		FilesAdded:     len(changes.Added),
		FilesModified:  len(changes.Modified),
		FilesDeleted:   len(changes.Deleted),
		FilesUnchanged: len(changes.Unchanged),
	}

	// 2. Handle deletions (cascade deletes chunks via FK)
	if len(changes.Deleted) > 0 {
		for _, deleted := range changes.Deleted {
			if err := idx.storage.DeleteFile(deleted); err != nil {
				log.Printf("Warning: failed to delete file %s: %v", deleted, err)
			}
		}
		log.Printf("✓ Deleted %d files from DB\n", len(changes.Deleted))
	}

	// 3. Update metadata for unchanged files (mtime drift correction)
	if len(changes.Unchanged) > 0 {
		if err := idx.storage.UpdateFileMtimes(changes.Unchanged); err != nil {
			log.Printf("Warning: failed to update mtimes: %v", err)
		}
	}

	// 4. Process changed files (added + modified)
	toProcess := append(changes.Added, changes.Modified...)
	if len(toProcess) == 0 {
		log.Println("No changes detected")
		stats.IndexingTime = time.Since(startTime)
		return stats, nil // Nothing to do
	}

	log.Printf("Processing %d files (%d added, %d modified)\n",
		len(toProcess), len(changes.Added), len(changes.Modified))

	// Convert relative paths to absolute paths for processor
	absFiles := make([]string, len(toProcess))
	for i, relPath := range toProcess {
		absFiles[i] = filepath.Join(idx.rootDir, relPath)
	}

	procStats, err := idx.processor.ProcessFiles(ctx, absFiles)
	if err != nil {
		return nil, fmt.Errorf("processing failed: %w", err)
	}

	stats.CodeFilesProcessed = procStats.CodeFilesProcessed
	stats.DocsProcessed = procStats.DocsProcessed
	stats.TotalCodeChunks = procStats.TotalCodeChunks
	stats.TotalDocChunks = procStats.TotalDocChunks

	// 5. Update graph (incremental based on changes)
	if idx.graphUpdater != nil {
		if err := idx.graphUpdater.Update(ctx, changes); err != nil {
			log.Printf("Warning: graph update failed: %v\n", err)
			// Don't fail indexing if graph fails (supplementary data)
		}
	}

	stats.IndexingTime = time.Since(startTime)
	return stats, nil
}

// Close closes the indexer and releases resources.
func (idx *IndexerV2) Close() error {
	if idx.storage != nil {
		return idx.storage.Close()
	}
	return nil
}

// GetStorage returns the underlying storage implementation.
func (idx *IndexerV2) GetStorage() Storage {
	return idx.storage
}
