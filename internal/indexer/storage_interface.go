package indexer

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mvp-joe/project-cortex/internal/cache"
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

	// GetBranch returns the current branch name
	GetBranch() string

	// Close releases resources
	Close() error
}

// SQLiteStorage uses the storage package for SQLite-based persistence.
// Stores chunks in branch-specific SQLite databases under ~/.cortex/cache/{cacheKey}/branches/{branch}.db
type SQLiteStorage struct {
	cachePath   string
	branch      string
	db          *sql.DB
	chunkWriter *storage.ChunkWriter
	// fileWriter is not used in Phase 3 (chunks only)
	// graphWriter is not used in Phase 3 (chunks only)
}

// NewSQLiteStorage creates SQLite-based storage.
// Automatically determines cache location based on project identity and current branch.
func NewSQLiteStorage(projectPath string) (Storage, error) {
	// Initialize sqlite-vec extension globally before any database operations
	storage.InitVectorExtension()

	// 1. Get cache key and ensure cache location
	_, err := cache.GetCacheKey(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get cache key: %w", err)
	}

	cachePath, err := cache.EnsureCacheLocation(projectPath)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure cache location: %w", err)
	}

	// 2. Get current branch
	branch := cache.GetCurrentBranch(projectPath)

	// 3. Open/create SQLite database for this branch
	dbPath := filepath.Join(cachePath, "branches", fmt.Sprintf("%s.db", branch))

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Create schema if not exists
	version, err := storage.GetSchemaVersion(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to check schema version: %w", err)
	}

	if version == "0" {
		if err := storage.CreateSchema(db); err != nil {
			db.Close()
			return nil, fmt.Errorf("failed to create schema: %w", err)
		}
	}

	// Create chunk writer using the shared connection
	chunkWriter := storage.NewChunkWriterWithDB(db)

	return &SQLiteStorage{
		cachePath:   cachePath,
		branch:      branch,
		db:          db,
		chunkWriter: chunkWriter,
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
		Version:       "2.0.0",
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

// GetCachePath returns the cache directory path.
func (s *SQLiteStorage) GetCachePath() string {
	return s.cachePath
}

// GetBranch returns the current branch name.
func (s *SQLiteStorage) GetBranch() string {
	return s.branch
}

// Close releases resources held by SQLite storage.
func (s *SQLiteStorage) Close() error {
	if s.chunkWriter != nil {
		if err := s.chunkWriter.Close(); err != nil {
			return fmt.Errorf("failed to close chunk writer: %w", err)
		}
	}
	if s.db != nil {
		if err := s.db.Close(); err != nil {
			return fmt.Errorf("failed to close database: %w", err)
		}
	}
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
