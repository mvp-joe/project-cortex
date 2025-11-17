package indexer

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mvp-joe/project-cortex/internal/storage"
)

// Storage handles persisting indexed data to SQLite database.
type Storage interface {
	// WriteChunks writes all chunks (full replace)
	WriteChunks(chunks []Chunk) error

	// WriteChunksIncremental updates chunks for specific files
	WriteChunksIncremental(chunks []Chunk) error

	// ReadMetadata reads existing metadata
	ReadMetadata() (*GeneratorMetadata, error)

	// GetDB returns the underlying database connection
	GetDB() *sql.DB

	// GetCachePath returns the cache directory path
	GetCachePath() string

	// DeleteFile deletes a file and all its associated chunks
	DeleteFile(filePath string) error

	// UpdateFileMtimes updates last_modified timestamps for unchanged files (mtime drift correction)
	UpdateFileMtimes(filePaths []string) error

	// Close releases resources
	Close() error
}

// SQLiteStorage uses the storage package for SQLite-based persistence.
// Stores chunks in SQLite database managed by the cache package.
type SQLiteStorage struct {
	cacheRootPath string // Cache root directory (e.g., ~/.cortex/cache/{cacheKey})
	projectPath   string // Project root directory
	db            *sql.DB
	chunkWriter   *storage.ChunkWriter
	// fileWriter is not used in Phase 3 (chunks only)
	// graphWriter is not used in Phase 3 (chunks only)
}

// NewSQLiteStorage creates SQLite-based storage using a pre-opened database connection.
// The caller is responsible for opening the database via cache.OpenDatabase().
// The storage will initialize the schema if needed but does not manage the connection lifecycle.
//
// Parameters:
//   db - Pre-opened database connection from cache.OpenDatabase()
//   cacheRootPath - Cache root directory (e.g., ~/.cortex/cache/{cacheKey})
//   projectPath - Project root directory for git operations
func NewSQLiteStorage(db *sql.DB, cacheRootPath, projectPath string) (Storage, error) {
	if db == nil {
		return nil, fmt.Errorf("database connection is required")
	}
	if cacheRootPath == "" {
		return nil, fmt.Errorf("cache root path is required")
	}
	if projectPath == "" {
		return nil, fmt.Errorf("project path is required")
	}

	// Initialize sqlite-vec extension globally before any operations
	storage.InitVectorExtension()

	// Create schema if not exists
	version, err := storage.GetSchemaVersion(db)
	if err != nil {
		return nil, fmt.Errorf("failed to check schema version: %w", err)
	}

	if version == "0" {
		if err := storage.CreateSchema(db); err != nil {
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}
	}

	// Create chunk writer using the shared connection
	chunkWriter := storage.NewChunkWriterWithDB(db)

	return &SQLiteStorage{
		cacheRootPath: cacheRootPath,
		projectPath:   projectPath,
		db:            db,
		chunkWriter:   chunkWriter,
	}, nil
}

// WriteChunks writes all chunks to SQLite (full replace).
func (s *SQLiteStorage) WriteChunks(chunks []Chunk) error {
	storageChunks := convertToStorageChunks(chunks)
	return s.chunkWriter.WriteChunks(storageChunks)
}

// WriteChunksIncremental updates chunks for specific files in SQLite.
func (s *SQLiteStorage) WriteChunksIncremental(chunks []Chunk) error {
	storageChunks := convertToStorageChunks(chunks)
	return s.chunkWriter.WriteChunksIncremental(storageChunks)
}

// ReadMetadata reads generator metadata from SQLite files table.
// Reconstructs GeneratorMetadata from files.file_hash and files.last_modified.
func (s *SQLiteStorage) ReadMetadata() (*GeneratorMetadata, error) {
	reader := storage.NewFileReader(s.db)
	files, err := reader.GetAllFiles()
	if err != nil {
		return nil, fmt.Errorf("failed to read files from database: %w", err)
	}

	metadata := &GeneratorMetadata{
		Version:       "3.0.0",
		Dimensions:    384,
		GeneratedAt:   time.Now(),
		FileChecksums: make(map[string]string),
		FileMtimes:    make(map[string]time.Time),
		Stats:         ProcessingStats{}, // Empty stats (not stored in DB)
	}

	for _, file := range files {
		metadata.FileChecksums[file.FilePath] = file.FileHash
		metadata.FileMtimes[file.FilePath] = file.LastModified
	}

	return metadata, nil
}

// GetDB returns the underlying database connection.
func (s *SQLiteStorage) GetDB() *sql.DB {
	return s.db
}

// GetCachePath returns the cache root directory path.
// This is where metadata.json should be saved.
func (s *SQLiteStorage) GetCachePath() string {
	return s.cacheRootPath
}

// DeleteFile deletes a file and all its associated chunks from the database.
func (s *SQLiteStorage) DeleteFile(filePath string) error {
	fileWriter := storage.NewFileWriter(s.db)
	return fileWriter.DeleteFile(filePath)
}

// UpdateFileMtimes updates last_modified timestamps for files (mtime drift correction).
// This handles the case where a file's mtime changed but content didn't (e.g., git checkout, touch).
func (s *SQLiteStorage) UpdateFileMtimes(filePaths []string) error {
	if s.db == nil {
		return fmt.Errorf("no database connection available")
	}
	if len(filePaths) == 0 {
		return nil
	}

	// Read current mtimes from disk
	for _, relPath := range filePaths {
		absPath := filepath.Join(s.projectPath, relPath)
		fileInfo, err := os.Stat(absPath)
		if err != nil {
			// File may have been deleted, skip
			continue
		}

		// Update mtime in database (store as RFC3339 string)
		_, err = s.db.Exec(
			"UPDATE files SET last_modified = ? WHERE file_path = ?",
			fileInfo.ModTime().Format(time.RFC3339),
			relPath,
		)
		if err != nil {
			return fmt.Errorf("failed to update mtime for %s: %w", relPath, err)
		}
	}

	return nil
}

// Close releases resources held by SQLite storage.
func (s *SQLiteStorage) Close() error {
	if s.chunkWriter != nil {
		if err := s.chunkWriter.Close(); err != nil {
			return fmt.Errorf("failed to close chunk writer: %w", err)
		}
	}
	// Don't close the DB - it's managed by the caller
	return nil
}

// convertToStorageChunks converts indexer.Chunk to storage.Chunk.
func convertToStorageChunks(chunks []Chunk) []*storage.Chunk {
	result := make([]*storage.Chunk, len(chunks))
	for i, c := range chunks {
		filePath := ""
		if fp, ok := c.Metadata["file_path"].(string); ok {
			filePath = fp
		}

		startLine := 0
		if sl, ok := c.Metadata["start_line"].(int); ok {
			startLine = sl
		}

		endLine := 0
		if el, ok := c.Metadata["end_line"].(int); ok {
			endLine = el
		}

		result[i] = &storage.Chunk{
			ID:        c.ID,
			FilePath:  filePath,
			ChunkType: string(c.ChunkType),
			Title:     c.Title,
			Text:      c.Text,
			Embedding: c.Embedding,
			StartLine: startLine,
			EndLine:   endLine,
			CreatedAt: c.CreatedAt,
			UpdatedAt: c.UpdatedAt,
		}
	}
	return result
}
