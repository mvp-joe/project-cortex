package indexer

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/git"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// BranchOptimizer handles copying chunks from ancestor branches to optimize indexing.
// When switching to a new branch, most files are unchanged from the ancestor (e.g., main).
// Instead of re-indexing everything, we copy chunks for unchanged files from the ancestor's database.
type BranchOptimizer struct {
	projectPath    string
	currentBranch  string
	ancestorBranch string
	cache          *cache.Cache
	cachePath      string
	gitOps         git.Operations
}

// FileInfo contains information about a file for change detection.
type FileInfo struct {
	Path     string    // Relative path from project root
	Hash     string    // SHA-256 checksum
	ModTime  time.Time // Last modification time
	Language string    // Programming language
}

// NewBranchOptimizer creates a BranchOptimizer.
// Returns nil if no optimization is possible (no ancestor branch or not using SQLite).
func NewBranchOptimizer(projectPath string, gitOps git.Operations, c *cache.Cache) (*BranchOptimizer, error) {
	// Get current branch
	currentBranch := gitOps.GetCurrentBranch(projectPath)

	// Skip optimization for main/master branches (they are the base)
	if currentBranch == "main" || currentBranch == "master" {
		return nil, nil
	}

	// Find ancestor branch
	ancestorBranch := gitOps.FindAncestorBranch(projectPath, currentBranch)
	if ancestorBranch == "" {
		// No ancestor found - can't optimize
		return nil, nil
	}

	// Get cache path
	cachePath, err := c.EnsureCacheLocation(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache location: %w", err)
	}

	return &BranchOptimizer{
		projectPath:    projectPath,
		currentBranch:  currentBranch,
		ancestorBranch: ancestorBranch,
		cache:          c,
		cachePath:      cachePath,
		gitOps:         gitOps,
	}, nil
}

// CopyUnchangedChunks copies chunks for files that haven't changed from ancestor branch.
// Returns:
//   - copiedCount: Number of chunks copied
//   - skippedFiles: Files that need full indexing (changed or new)
//   - error: Any error encountered
//
// Strategy:
//  1. Open ancestor branch database
//  2. Compare file hashes between current and ancestor
//  3. For unchanged files: copy chunks from ancestor DB to current DB
//  4. Return list of files that still need indexing
func (bo *BranchOptimizer) CopyUnchangedChunks(currentFiles map[string]FileInfo) (int, []string, error) {
	if bo == nil {
		// Optimization not available
		return 0, allFilePaths(currentFiles), nil
	}

	// Check if ancestor database exists
	ancestorDBPath := filepath.Join(bo.cachePath, "branches", fmt.Sprintf("%s.db", bo.ancestorBranch))
	if !fileExists(ancestorDBPath) {
		log.Printf("Ancestor branch database not found: %s (full indexing required)\n", ancestorDBPath)
		return 0, allFilePaths(currentFiles), nil
	}

	// Open ancestor database (read-only)
	ancestorDB, err := sql.Open("sqlite3", ancestorDBPath+"?mode=ro")
	if err != nil {
		return 0, nil, fmt.Errorf("failed to open ancestor database: %w", err)
	}
	defer ancestorDB.Close()

	// Open current branch database (read-write)
	currentDBPath := filepath.Join(bo.cachePath, "branches", fmt.Sprintf("%s.db", bo.currentBranch))
	currentDB, err := sql.Open("sqlite3", currentDBPath)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to open current database: %w", err)
	}
	defer currentDB.Close()

	// Enable foreign keys
	if _, err := currentDB.Exec("PRAGMA foreign_keys = ON"); err != nil {
		return 0, nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Ensure schema exists in current DB
	version, err := storage.GetSchemaVersion(currentDB)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to check schema version: %w", err)
	}
	if version == "0" {
		storage.InitVectorExtension()
		if err := storage.CreateSchema(currentDB); err != nil {
			return 0, nil, fmt.Errorf("failed to create schema: %w", err)
		}
	}

	// Load file hashes from ancestor database
	ancestorFileHashes, err := loadFileHashes(ancestorDB)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to load ancestor file hashes: %w", err)
	}

	// Identify unchanged files
	unchangedFiles := make(map[string]bool)
	changedFiles := []string{}

	for relPath, fileInfo := range currentFiles {
		ancestorHash, exists := ancestorFileHashes[relPath]
		if exists && ancestorHash == fileInfo.Hash {
			// File unchanged - can copy chunks
			unchangedFiles[relPath] = true
		} else {
			// File changed or new - needs indexing
			changedFiles = append(changedFiles, relPath)
		}
	}

	if len(unchangedFiles) == 0 {
		log.Println("No unchanged files found - full indexing required")
		return 0, allFilePaths(currentFiles), nil
	}

	log.Printf("Found %d unchanged files (can copy from %s), %d changed files (need indexing)\n",
		len(unchangedFiles), bo.ancestorBranch, len(changedFiles))

	// Copy file metadata and chunks for unchanged files
	copiedCount, err := bo.copyFilesAndChunks(ancestorDB, currentDB, unchangedFiles)
	if err != nil {
		return 0, nil, fmt.Errorf("failed to copy chunks: %w", err)
	}

	log.Printf("Copied %d chunks from ancestor branch %s\n", copiedCount, bo.ancestorBranch)

	return copiedCount, changedFiles, nil
}

// copyFilesAndChunks copies file metadata and chunks for specific files from ancestor to current database.
// Uses a transaction for atomic copying.
// Updates chunk timestamps to current time when copying.
func (bo *BranchOptimizer) copyFilesAndChunks(ancestorDB, currentDB *sql.DB, unchangedFiles map[string]bool) (int, error) {
	tx, err := currentDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// First, copy file metadata (required due to FK constraint)
	fileInsertStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO files (file_path, language, module_path, is_test, line_count_total, line_count_code,
		                               line_count_comment, line_count_blank, size_bytes, file_hash, last_modified, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare file insert statement: %w", err)
	}
	defer fileInsertStmt.Close()

	for filePath := range unchangedFiles {
		// Query file metadata from ancestor
		var language, modulePath, fileHash, lastModified, indexedAt string
		var isTest, lineCountTotal, lineCountCode, lineCountComment, lineCountBlank, sizeBytes int
		err := ancestorDB.QueryRow(`
			SELECT language, module_path, is_test, line_count_total, line_count_code,
			       line_count_comment, line_count_blank, size_bytes, file_hash, last_modified, indexed_at
			FROM files
			WHERE file_path = ?
		`, filePath).Scan(&language, &modulePath, &isTest, &lineCountTotal, &lineCountCode,
			&lineCountComment, &lineCountBlank, &sizeBytes, &fileHash, &lastModified, &indexedAt)

		if err == sql.ErrNoRows {
			// File metadata missing in ancestor - skip this file
			log.Printf("Warning: file metadata missing for %s in ancestor database, skipping\n", filePath)
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("failed to query file metadata for %s: %w", filePath, err)
		}

		// Insert file metadata into current database
		_, err = fileInsertStmt.Exec(filePath, language, modulePath, isTest, lineCountTotal, lineCountCode,
			lineCountComment, lineCountBlank, sizeBytes, fileHash, lastModified, indexedAt)
		if err != nil {
			return 0, fmt.Errorf("failed to insert file metadata for %s: %w", filePath, err)
		}
	}

	// Now copy chunks
	chunkInsertStmt, err := tx.Prepare(`
		INSERT INTO chunks (chunk_id, file_path, chunk_type, title, text, embedding, start_line, end_line, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare chunk insert statement: %w", err)
	}
	defer chunkInsertStmt.Close()

	copiedCount := 0
	now := time.Now().UTC().Format(time.RFC3339)

	// Query chunks from ancestor for unchanged files
	for filePath := range unchangedFiles {
		rows, err := ancestorDB.Query(`
			SELECT chunk_id, file_path, chunk_type, title, text, embedding, start_line, end_line, created_at
			FROM chunks
			WHERE file_path = ?
		`, filePath)
		if err != nil {
			return 0, fmt.Errorf("failed to query chunks for %s: %w", filePath, err)
		}

		// Copy each chunk
		for rows.Next() {
			var chunkID, chunkFilePath, chunkType, title, text string
			var embedding []byte
			var startLine, endLine sql.NullInt64
			var createdAt string

			err := rows.Scan(&chunkID, &chunkFilePath, &chunkType, &title, &text, &embedding, &startLine, &endLine, &createdAt)
			if err != nil {
				rows.Close()
				return 0, fmt.Errorf("failed to scan chunk: %w", err)
			}

			// Insert into current database with updated timestamp
			_, err = chunkInsertStmt.Exec(chunkID, chunkFilePath, chunkType, title, text, embedding, startLine, endLine, createdAt, now)
			if err != nil {
				rows.Close()
				return 0, fmt.Errorf("failed to insert chunk %s: %w", chunkID, err)
			}

			copiedCount++
		}
		rows.Close()
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return copiedCount, nil
}

// loadFileHashes loads file path -> hash mapping from database.
// This is used for comparing file checksums between branches.
func loadFileHashes(db *sql.DB) (map[string]string, error) {
	// Check if files table exists
	var tableExists int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='files'").Scan(&tableExists)
	if err != nil {
		return nil, fmt.Errorf("failed to check files table existence: %w", err)
	}
	if tableExists == 0 {
		// No files table - return empty map (full indexing needed)
		return make(map[string]string), nil
	}

	rows, err := db.Query("SELECT file_path, file_hash FROM files")
	if err != nil {
		return nil, fmt.Errorf("failed to query file hashes: %w", err)
	}
	defer rows.Close()

	fileHashes := make(map[string]string)
	for rows.Next() {
		var filePath, fileHash string
		if err := rows.Scan(&filePath, &fileHash); err != nil {
			return nil, fmt.Errorf("failed to scan file hash: %w", err)
		}
		fileHashes[filePath] = fileHash
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating file hashes: %w", err)
	}

	return fileHashes, nil
}

// allFilePaths extracts all file paths from FileInfo map.
func allFilePaths(files map[string]FileInfo) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	return paths
}

// fileExists checks if a file exists.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
