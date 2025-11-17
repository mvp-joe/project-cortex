package indexer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/git"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// BranchSynchronizer prepares branch databases and optionally copies chunks from ancestor branches.
// It detects the ancestor branch (typically main/master) and copies chunks for files that haven't
// changed, avoiding expensive re-indexing.
type BranchSynchronizer interface {
	// PrepareDB ensures a branch database exists and is optimized.
	// If the branch has an ancestor with overlapping files, copies unchanged chunks.
	// Safe for concurrent calls.
	PrepareDB(ctx context.Context, branch string) error
}

// branchSynchronizer is the concrete implementation of BranchSynchronizer.
type branchSynchronizer struct {
	projectPath string
	cache       *cache.Cache
	cachePath   string
	gitOps      git.Operations

	// Mutex to protect concurrent PrepareDB calls for the same branch
	mu sync.Mutex
	// Track which branches are currently being prepared
	preparing map[string]*sync.Mutex
}

// NewBranchSynchronizer creates a new BranchSynchronizer.
func NewBranchSynchronizer(projectPath string, gitOps git.Operations, c *cache.Cache) (BranchSynchronizer, error) {
	// Get cache path
	cachePath, err := c.EnsureCacheLocation(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache location: %w", err)
	}

	return &branchSynchronizer{
		projectPath: projectPath,
		cache:       c,
		cachePath:   cachePath,
		gitOps:      gitOps,
		preparing:   make(map[string]*sync.Mutex),
	}, nil
}

// PrepareDB implements BranchSynchronizer.PrepareDB.
//
// Algorithm:
//  1. Check if branch DB exists
//  2. If exists: verify schema version and return
//  3. If not exists: create DB with schema
//  4. Detect ancestor branch using git merge-base
//  5. If ancestor found and ancestor DB exists:
//     a. Copy chunks for files with matching hashes
//     b. Log optimization stats
//  6. Return (DB ready for indexing)
func (bs *branchSynchronizer) PrepareDB(ctx context.Context, branch string) error {
	// Get or create mutex for this branch to ensure only one prepare at a time
	branchMutex := bs.getBranchMutex(branch)
	branchMutex.Lock()
	defer branchMutex.Unlock()

	// Construct DB path
	dbPath := bs.getBranchDBPath(branch)

	// Check if DB already exists
	if fileExistsHelper(dbPath) {
		// Verify schema version
		db, err := sql.Open("sqlite3", dbPath)
		if err != nil {
			return fmt.Errorf("failed to open existing database: %w", err)
		}
		defer db.Close()

		version, err := storage.GetSchemaVersion(db)
		if err != nil {
			return fmt.Errorf("failed to check schema version: %w", err)
		}

		if version == "0" {
			return fmt.Errorf("database exists but has no schema (corrupted?)")
		}

		// DB exists with valid schema - nothing to do
		log.Printf("Branch database already exists: %s (schema version: %s)\n", branch, version)
		return nil
	}

	// Create new database with schema
	log.Printf("Creating new database for branch: %s\n", branch)
	db, err := bs.createDatabaseWithSchema(dbPath)
	if err != nil {
		return fmt.Errorf("failed to create database: %w", err)
	}
	defer db.Close()

	// Try to optimize by copying chunks from ancestor
	ancestorBranch := bs.findAncestor(branch)
	if ancestorBranch == "" {
		log.Printf("No ancestor branch found for %s (full indexing required)\n", branch)
		return nil
	}

	ancestorDBPath := bs.getBranchDBPath(ancestorBranch)
	if !fileExistsHelper(ancestorDBPath) {
		log.Printf("Ancestor branch database not found: %s (full indexing required)\n", ancestorBranch)
		return nil
	}

	// Copy chunks from ancestor
	copiedCount, skippedCount, err := bs.copyFromAncestor(ctx, db, ancestorDBPath)
	if err != nil {
		// Don't fail PrepareDB if copying fails - just log and continue
		log.Printf("Warning: failed to copy chunks from ancestor: %v\n", err)
		return nil
	}

	if copiedCount > 0 {
		log.Printf("✓ Optimization: copied %d chunks from %s, %d files need reprocessing\n",
			copiedCount, ancestorBranch, skippedCount)
	} else {
		log.Printf("No chunks copied from ancestor (all files changed or no overlap)\n")
	}

	return nil
}

// getBranchMutex returns a mutex for a specific branch, creating it if needed.
// This ensures concurrent PrepareDB calls for the same branch are serialized.
func (bs *branchSynchronizer) getBranchMutex(branch string) *sync.Mutex {
	bs.mu.Lock()
	defer bs.mu.Unlock()

	if bs.preparing[branch] == nil {
		bs.preparing[branch] = &sync.Mutex{}
	}

	return bs.preparing[branch]
}

// getBranchDBPath returns the full path to a branch's database file.
func (bs *branchSynchronizer) getBranchDBPath(branch string) string {
	return filepath.Join(bs.cachePath, "branches", fmt.Sprintf("%s.db", branch))
}

// createDatabaseWithSchema creates a new SQLite database with the current schema.
func (bs *branchSynchronizer) createDatabaseWithSchema(dbPath string) (*sql.DB, error) {
	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	// Create database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create database: %w", err)
	}

	// Initialize vector extension
	storage.InitVectorExtension()

	// Create schema
	if err := storage.CreateSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return db, nil
}

// findAncestor detects the ancestor branch for the given branch.
// Returns empty string if no ancestor found.
func (bs *branchSynchronizer) findAncestor(branch string) string {
	// Don't try to find ancestor for main/master (they are the base)
	if branch == "main" || branch == "master" {
		return ""
	}

	// Use git operations to find ancestor branch
	return bs.gitOps.FindAncestorBranch(bs.projectPath, branch)
}

// copyFromAncestor copies chunks from ancestor database to current database.
// Only copies chunks for files that exist on disk and have matching hashes.
// Returns:
//   - copiedCount: Number of chunks successfully copied
//   - skippedCount: Number of unique files that need reprocessing
//   - error: Any error encountered
func (bs *branchSynchronizer) copyFromAncestor(ctx context.Context, currentDB *sql.DB, ancestorDBPath string) (int, int, error) {
	// Open ancestor database (read-only)
	ancestorDB, err := sql.Open("sqlite3", ancestorDBPath+"?mode=ro")
	if err != nil {
		return 0, 0, fmt.Errorf("failed to open ancestor database: %w", err)
	}
	defer ancestorDB.Close()

	// Load file hashes from ancestor
	ancestorFileHashes, err := bs.loadFileHashes(ancestorDB)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to load ancestor file hashes: %w", err)
	}

	if len(ancestorFileHashes) == 0 {
		// Ancestor DB is empty or has no files table
		return 0, 0, nil
	}

	// Identify which files are unchanged (exist on disk with matching hash)
	unchangedFiles := make(map[string]string) // filePath -> hash
	skippedFiles := 0

	for filePath, ancestorHash := range ancestorFileHashes {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return 0, 0, ctx.Err()
		default:
		}

		// Check if file exists on disk
		fullPath := filepath.Join(bs.projectPath, filePath)
		if !fileExistsHelper(fullPath) {
			skippedFiles++
			continue
		}

		// Calculate current hash
		currentHash, err := calculateFileHash(fullPath)
		if err != nil {
			// File not readable - skip
			skippedFiles++
			continue
		}

		if currentHash == ancestorHash {
			// File unchanged - can copy chunks
			unchangedFiles[filePath] = ancestorHash
		} else {
			// File changed - needs reprocessing
			skippedFiles++
		}
	}

	if len(unchangedFiles) == 0 {
		return 0, skippedFiles, nil
	}

	// Copy file metadata and chunks for unchanged files
	copiedCount, err := bs.copyFilesAndChunks(ctx, ancestorDB, currentDB, unchangedFiles)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to copy chunks: %w", err)
	}

	return copiedCount, skippedFiles, nil
}

// loadFileHashes loads file path -> hash mapping from database.
func (bs *branchSynchronizer) loadFileHashes(db *sql.DB) (map[string]string, error) {
	// Check if files table exists
	var tableExists int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='files'").Scan(&tableExists)
	if err != nil {
		return nil, fmt.Errorf("failed to check files table existence: %w", err)
	}
	if tableExists == 0 {
		// No files table - return empty map
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

// copyFilesAndChunks copies file metadata and chunks from ancestor to current database.
// Uses a transaction for atomicity. Respects context cancellation.
func (bs *branchSynchronizer) copyFilesAndChunks(ctx context.Context, ancestorDB, currentDB *sql.DB, unchangedFiles map[string]string) (int, error) {
	tx, err := currentDB.Begin()
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Prepare statements
	fileInsertStmt, err := tx.Prepare(`
		INSERT OR REPLACE INTO files (file_path, language, module_path, is_test, line_count_total, line_count_code,
		                               line_count_comment, line_count_blank, size_bytes, file_hash, last_modified, indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, fmt.Errorf("failed to prepare file insert statement: %w", err)
	}
	defer fileInsertStmt.Close()

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

	// Copy each file's metadata and chunks
	for filePath := range unchangedFiles {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		// Copy file metadata
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
			// File metadata missing - skip
			log.Printf("Warning: file metadata missing for %s in ancestor database\n", filePath)
			continue
		}
		if err != nil {
			return 0, fmt.Errorf("failed to query file metadata for %s: %w", filePath, err)
		}

		// Insert file metadata
		_, err = fileInsertStmt.Exec(filePath, language, modulePath, isTest, lineCountTotal, lineCountCode,
			lineCountComment, lineCountBlank, sizeBytes, fileHash, lastModified, indexedAt)
		if err != nil {
			return 0, fmt.Errorf("failed to insert file metadata for %s: %w", filePath, err)
		}

		// Copy chunks for this file
		rows, err := ancestorDB.Query(`
			SELECT chunk_id, file_path, chunk_type, title, text, embedding, start_line, end_line, created_at
			FROM chunks
			WHERE file_path = ?
		`, filePath)
		if err != nil {
			return 0, fmt.Errorf("failed to query chunks for %s: %w", filePath, err)
		}

		for rows.Next() {
			// Check context cancellation during chunk copying
			select {
			case <-ctx.Done():
				rows.Close()
				return 0, ctx.Err()
			default:
			}

			var chunkID, chunkFilePath, chunkType, title, text string
			var embedding []byte
			var startLine, endLine sql.NullInt64
			var createdAt string

			err := rows.Scan(&chunkID, &chunkFilePath, &chunkType, &title, &text, &embedding, &startLine, &endLine, &createdAt)
			if err != nil {
				rows.Close()
				return 0, fmt.Errorf("failed to scan chunk: %w", err)
			}

			// Insert chunk with updated timestamp
			_, err = chunkInsertStmt.Exec(chunkID, chunkFilePath, chunkType, title, text, embedding, startLine, endLine, createdAt, now)
			if err != nil {
				rows.Close()
				return 0, fmt.Errorf("failed to insert chunk %s: %w", chunkID, err)
			}

			copiedCount++
		}
		rows.Close()
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return copiedCount, nil
}

// fileExistsHelper checks if a file exists.
func fileExistsHelper(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// calculateFileHash calculates SHA-256 hash of a file.
// Uses the same hash calculation as the indexer (calculateChecksum) for consistency.
func calculateFileHash(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}
