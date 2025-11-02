package mcp

// Implementation Plan:
// 1. Read all JSON files from chunks directory
// 2. Parse each file as array of ContextChunk
// 3. Validate chunk structure and embeddings
// 4. Return combined list of chunks
// 5. Handle errors gracefully (skip malformed files, log warnings)

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

// LoadChunks reads and parses all chunk files from the specified directory.
// It returns all valid chunks and logs warnings for any malformed files.
func LoadChunks(chunksDir string) ([]*ContextChunk, error) {
	// Check if directory exists
	info, err := os.Stat(chunksDir)
	if err != nil {
		return nil, fmt.Errorf("chunks directory not accessible: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("chunks path is not a directory: %s", chunksDir)
	}

	// Find all JSON files
	pattern := filepath.Join(chunksDir, "*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to list chunk files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no chunk files found in %s", chunksDir)
	}

	// Load chunks from all files
	var allChunks []*ContextChunk
	for _, file := range files {
		chunks, err := loadChunkFile(file)
		if err != nil {
			log.Printf("Warning: skipping malformed chunk file %s: %v", file, err)
			continue
		}
		allChunks = append(allChunks, chunks...)
	}

	if len(allChunks) == 0 {
		return nil, fmt.Errorf("no valid chunks found in %s", chunksDir)
	}

	return allChunks, nil
}

// ChunkFileWrapper represents the chunk file format with metadata.
type ChunkFileWrapper struct {
	Metadata ChunkFileMetadata `json:"_metadata"`
	Chunks   []*ContextChunk   `json:"chunks"`
}

// ChunkFileMetadata contains metadata about the chunk file.
type ChunkFileMetadata struct {
	Model      string `json:"model"`
	Dimensions int    `json:"dimensions"`
	ChunkType  string `json:"chunk_type"`
	Generated  string `json:"generated"` // time as string
	Version    string `json:"version"`
}

// loadChunkFile reads and parses a single chunk file.
// It handles the wrapped format: {"_metadata": {...}, "chunks": [...]}
func loadChunkFile(path string) ([]*ContextChunk, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var wrapper ChunkFileWrapper
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	// Validate chunks
	for i, chunk := range wrapper.Chunks {
		if err := validateChunk(chunk, wrapper.Metadata.Dimensions); err != nil {
			return nil, fmt.Errorf("chunk %d invalid: %w", i, err)
		}
	}

	return wrapper.Chunks, nil
}

// validateChunk checks if a chunk has required fields and valid embeddings.
func validateChunk(chunk *ContextChunk, expectedDims int) error {
	if chunk.ID == "" {
		return fmt.Errorf("missing ID")
	}
	if chunk.Text == "" {
		return fmt.Errorf("missing text content")
	}
	if len(chunk.Embedding) == 0 {
		return fmt.Errorf("missing embedding")
	}
	// Validate embedding dimensions from metadata
	if len(chunk.Embedding) != expectedDims {
		return fmt.Errorf("invalid embedding dimensions: expected %d, got %d", expectedDims, len(chunk.Embedding))
	}
	return nil
}

// LoadChunksAuto attempts to load chunks from SQLite, falls back to JSON.
// This provides backward compatibility while preferring SQLite when available.
//
// Strategy:
// 1. Try SQLite first (preferred method)
// 2. If SQLite fails (database doesn't exist), fallback to JSON
// 3. If JSON also fails, return error
//
// Use cases:
// - New projects: SQLite after initial indexing
// - Legacy projects: JSON until they re-index with SQLite support
// - Development: Both methods work transparently
func LoadChunksAuto(projectPath, chunksDir string) ([]*ContextChunk, error) {
	// Try SQLite first
	chunks, err := LoadChunksFromSQLite(projectPath)
	if err == nil {
		return chunks, nil
	}

	// Log SQLite failure and fallback
	log.Printf("SQLite cache not available (%v), falling back to JSON", err)

	// Fallback to JSON
	chunks, jsonErr := LoadChunks(chunksDir)
	if jsonErr != nil {
		return nil, fmt.Errorf("failed to load chunks from both SQLite and JSON: SQLite: %v, JSON: %v", err, jsonErr)
	}

	log.Printf("✓ Loaded %d chunks from legacy JSON format", len(chunks))
	return chunks, nil
}
