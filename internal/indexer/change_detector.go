package indexer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mvp-joe/project-cortex/internal/storage"
)

// ChangeDetector compares filesystem state to database state and returns what changed.
type ChangeDetector interface {
	// DetectChanges compares disk to DB and returns files needing processing.
	// If hint is non-empty, only checks those files (optimization from watcher).
	// If hint is empty, discovers all files and compares.
	DetectChanges(ctx context.Context, hint []string) (*ChangeSet, error)
}

// ChangeSet contains the result of change detection.
type ChangeSet struct {
	Added     []string // New files not in DB
	Modified  []string // Files with different hash than DB
	Deleted   []string // Files in DB but not on disk
	Unchanged []string // Files with same hash (mtime may have drifted)
}

// changeDetector implements ChangeDetector interface.
type changeDetector struct {
	rootDir   string
	storage   Storage
	discovery *FileDiscovery
}

// NewChangeDetector creates a new change detector.
func NewChangeDetector(rootDir string, storage Storage, discovery *FileDiscovery) ChangeDetector {
	return &changeDetector{
		rootDir:   rootDir,
		storage:   storage,
		discovery: discovery,
	}
}

// DetectChanges implements the change detection algorithm with mtime optimization.
//
// Algorithm:
// 1. If hint provided, only check those files; if empty, discover all files from disk
// 2. For each file:
//    a. Read mtime from disk
//    b. Query DB for (file_path, mtime, hash)
//    c. If DB mtime == disk mtime: Unchanged (fast path, skip hash)
//    d. If DB mtime != disk mtime:
//       - Calculate hash, compare to DB hash
//       - If hash same: Unchanged (mtime drift, needs DB update)
//       - If hash different: Modified
//    e. If file not in DB: Added
// 3. Files in DB but not on disk: Deleted
func (cd *changeDetector) DetectChanges(ctx context.Context, hint []string) (*ChangeSet, error) {
	changes := &ChangeSet{
		Added:     []string{},
		Modified:  []string{},
		Deleted:   []string{},
		Unchanged: []string{},
	}

	// Step 1: Determine which files to check
	var filesToCheck []string
	if len(hint) == 0 {
		// Full discovery: scan all code/doc files
		codeFiles, docFiles, err := cd.discovery.DiscoverFiles()
		if err != nil {
			return nil, fmt.Errorf("failed to discover files: %w", err)
		}
		filesToCheck = append(codeFiles, docFiles...)
	} else {
		// Hint optimization: only check specified files
		filesToCheck = hint
	}

	// Convert absolute paths to relative for DB lookup
	relativeFiles := make([]string, 0, len(filesToCheck))
	for _, file := range filesToCheck {
		var relPath string
		if filepath.IsAbs(file) {
			rel, err := filepath.Rel(cd.rootDir, file)
			if err != nil {
				return nil, fmt.Errorf("failed to get relative path for %s: %w", file, err)
			}
			relPath = rel
		} else {
			relPath = file
		}
		relativeFiles = append(relativeFiles, relPath)
	}

	// Step 2: Load all files from DB to detect deletions
	reader := storage.NewFileReader(cd.storage.GetDB())
	dbFiles, err := reader.GetAllFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to read files from database: %w", err)
	}

	// Create map of DB files for quick lookup
	dbFileMap := make(map[string]*storage.FileStats)
	for _, file := range dbFiles {
		dbFileMap[file.FilePath] = file
	}

	// Step 3: Check each file on disk
	for _, relPath := range relativeFiles {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		absPath := filepath.Join(cd.rootDir, relPath)

		// Check if file exists on disk
		fileInfo, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				// File doesn't exist on disk - skip (might be in deleted list later)
				continue
			}
			return nil, fmt.Errorf("failed to stat file %s: %w", relPath, err)
		}

		// Get DB record if exists
		dbFile, existsInDB := dbFileMap[relPath]

		if !existsInDB {
			// File not in DB → Added
			changes.Added = append(changes.Added, relPath)
			continue
		}

		// File exists in both disk and DB - check if changed
		diskMtime := fileInfo.ModTime()
		dbMtime := dbFile.LastModified

		// Mtime fast-path: if mtime same, skip hash calculation
		if diskMtime.Equal(dbMtime) {
			changes.Unchanged = append(changes.Unchanged, relPath)
			delete(dbFileMap, relPath) // Mark as seen
			continue
		}

		// Mtime differs - need to calculate hash
		diskHash, err := calculateHashForFile(absPath)
		if err != nil {
			return nil, fmt.Errorf("failed to calculate hash for %s: %w", relPath, err)
		}

		dbHash := dbFile.FileHash

		if diskHash == dbHash {
			// Hash same but mtime different → Unchanged (mtime drift)
			changes.Unchanged = append(changes.Unchanged, relPath)
		} else {
			// Hash different → Modified
			changes.Modified = append(changes.Modified, relPath)
		}

		delete(dbFileMap, relPath) // Mark as seen
	}

	// Step 4: Files remaining in dbFileMap are deleted (in DB but not checked/found on disk)
	// Only detect deletions if we did full discovery (no hint)
	if len(hint) == 0 {
		for filePath := range dbFileMap {
			changes.Deleted = append(changes.Deleted, filePath)
		}
	}

	return changes, nil
}

// calculateHashForFile calculates SHA-256 hash of a file.
func calculateHashForFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
