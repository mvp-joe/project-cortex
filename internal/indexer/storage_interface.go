package indexer

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	"github.com/mvp-joe/project-cortex/internal/cache"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// Storage handles persisting indexed data to disk/database.
// Provides a unified interface for both JSON and SQLite storage backends.
type Storage interface {
	// WriteChunks writes all chunks (full replace)
	WriteChunks(chunks []Chunk) error

	// WriteChunksIncremental updates chunks for specific files
	WriteChunksIncremental(chunks []Chunk) error

	// ReadMetadata reads existing metadata
	ReadMetadata() (*GeneratorMetadata, error)

	// Close releases resources
	Close() error
}

// JSONStorage uses the existing AtomicWriter for JSON files.
// Maintains backward compatibility with existing JSON-based workflow.
type JSONStorage struct {
	writer *AtomicWriter
}

// NewJSONStorage creates JSON-based storage (existing behavior).
func NewJSONStorage(outputDir string) (Storage, error) {
	writer, err := NewAtomicWriter(outputDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create atomic writer: %w", err)
	}
	return &JSONStorage{writer: writer}, nil
}

// WriteChunks writes all chunks to JSON files by chunk type.
func (s *JSONStorage) WriteChunks(chunks []Chunk) error {
	// Group chunks by type
	chunksByType := groupChunksByType(chunks)

	// Write each type to its respective file
	for chunkType, typeChunks := range chunksByType {
		filename := getChunkFilename(chunkType)
		chunkFile := &ChunkFile{
			Chunks: typeChunks,
			// Metadata will be set by writeChunkFiles
		}
		if err := s.writer.WriteChunkFile(filename, chunkFile); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	return nil
}

// WriteChunksIncremental updates chunks for specific files.
// For JSON storage, this requires reading existing chunks, filtering, and merging.
func (s *JSONStorage) WriteChunksIncremental(chunks []Chunk) error {
	// Load existing chunks
	existingChunks := make(map[ChunkType][]Chunk)
	for _, chunkType := range []ChunkType{
		ChunkTypeSymbols,
		ChunkTypeDefinitions,
		ChunkTypeData,
		ChunkTypeDocumentation,
	} {
		filename := getChunkFilename(chunkType)
		chunkFile, err := s.writer.ReadChunkFile(filename)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", filename, err)
		}
		existingChunks[chunkType] = chunkFile.Chunks
	}

	// Build file paths affected by new chunks
	affectedFiles := make(map[string]bool)
	for _, chunk := range chunks {
		if filePath, ok := chunk.Metadata["file_path"].(string); ok {
			affectedFiles[filePath] = true
		}
	}

	// Filter out chunks for affected files
	filteredChunks := make(map[ChunkType][]Chunk)
	for chunkType, typeChunks := range existingChunks {
		filtered := []Chunk{}
		for _, chunk := range typeChunks {
			filePath, ok := chunk.Metadata["file_path"].(string)
			if !ok || !affectedFiles[filePath] {
				filtered = append(filtered, chunk)
			}
		}
		filteredChunks[chunkType] = filtered
	}

	// Merge new chunks
	for _, chunk := range chunks {
		filteredChunks[chunk.ChunkType] = append(filteredChunks[chunk.ChunkType], chunk)
	}

	// Write merged chunks
	for chunkType, typeChunks := range filteredChunks {
		filename := getChunkFilename(chunkType)
		chunkFile := &ChunkFile{
			Chunks: typeChunks,
		}
		if err := s.writer.WriteChunkFile(filename, chunkFile); err != nil {
			return fmt.Errorf("failed to write %s: %w", filename, err)
		}
	}

	return nil
}

// writeMetadata writes generator metadata to JSON (internal method).
// This is not part of the Storage interface but is called internally for JSON storage.
func (s *JSONStorage) writeMetadata(metadata *GeneratorMetadata) error {
	return s.writer.WriteMetadata(metadata)
}

// ReadMetadata reads generator metadata from JSON.
func (s *JSONStorage) ReadMetadata() (*GeneratorMetadata, error) {
	return s.writer.ReadMetadata()
}

// Close releases resources held by JSON storage.
func (s *JSONStorage) Close() error {
	// No resources to release for JSON storage
	return nil
}

// SQLiteStorage uses the new storage package for SQLite-based persistence.
// Stores chunks in branch-specific SQLite databases under ~/.cortex/cache/{cacheKey}/branches/{branch}.db
type SQLiteStorage struct {
	cachePath    string
	branch       string
	db           *sql.DB
	chunkWriter  *storage.ChunkWriter
	// fileWriter is not used in Phase 3 (chunks only)
	// graphWriter is not used in Phase 3 (chunks only)
}

// NewSQLiteStorage creates SQLite-based storage.
// Automatically determines cache location based on project identity and current branch.
func NewSQLiteStorage(projectPath string) (Storage, error) {
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

// Helper functions

// groupChunksByType groups chunks by their chunk type.
func groupChunksByType(chunks []Chunk) map[ChunkType][]Chunk {
	result := make(map[ChunkType][]Chunk)
	for _, chunk := range chunks {
		result[chunk.ChunkType] = append(result[chunk.ChunkType], chunk)
	}
	return result
}

// getChunkFilename returns the JSON filename for a given chunk type.
func getChunkFilename(chunkType ChunkType) string {
	switch chunkType {
	case ChunkTypeSymbols:
		return "code-symbols.json"
	case ChunkTypeDefinitions:
		return "code-definitions.json"
	case ChunkTypeData:
		return "code-data.json"
	case ChunkTypeDocumentation:
		return "doc-chunks.json"
	default:
		return "unknown-chunks.json"
	}
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
