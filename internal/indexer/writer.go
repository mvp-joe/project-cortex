package indexer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// AtomicWriter handles atomic file writing using temp → rename pattern.
type AtomicWriter struct {
	outputDir string
	tempDir   string
}

// NewAtomicWriter creates a new atomic writer.
func NewAtomicWriter(outputDir string) (*AtomicWriter, error) {
	tempDir := filepath.Join(outputDir, ".tmp")

	// Ensure directories exist
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create output directory: %w", err)
	}

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Clean up stale temp files
	if err := os.RemoveAll(tempDir); err != nil {
		return nil, fmt.Errorf("failed to clean temp directory: %w", err)
	}

	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to recreate temp directory: %w", err)
	}

	return &AtomicWriter{
		outputDir: outputDir,
		tempDir:   tempDir,
	}, nil
}

// WriteChunkFile writes a chunk file atomically.
func (w *AtomicWriter) WriteChunkFile(filename string, chunkFile *ChunkFile) error {
	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(chunkFile, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal chunk file: %w", err)
	}

	// Write to temp file
	tempPath := filepath.Join(w.tempDir, filename)
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	// Rename to final location (atomic operation)
	finalPath := filepath.Join(w.outputDir, filename)
	if err := os.Rename(tempPath, finalPath); err != nil {
		// Clean up temp file on error
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp file: %w", err)
	}

	return nil
}

// WriteMetadata writes generator metadata atomically.
func (w *AtomicWriter) WriteMetadata(metadata *GeneratorMetadata) error {
	filename := "generator-output.json"

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write to temp file
	tempPath := filepath.Join(w.tempDir, filename)
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write temp metadata file: %w", err)
	}

	// Rename to final location (atomic operation)
	finalPath := filepath.Join(w.outputDir, filename)
	if err := os.Rename(tempPath, finalPath); err != nil {
		// Clean up temp file on error
		os.Remove(tempPath)
		return fmt.Errorf("failed to rename temp metadata file: %w", err)
	}

	return nil
}

// ReadMetadata reads existing generator metadata.
func (w *AtomicWriter) ReadMetadata() (*GeneratorMetadata, error) {
	metadataPath := filepath.Join(w.outputDir, "generator-output.json")

	data, err := os.ReadFile(metadataPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No previous metadata
			return &GeneratorMetadata{
				Version:       "2.0.0",
				FileChecksums: make(map[string]string),
				FileMtimes:    make(map[string]time.Time),
			}, nil
		}
		return nil, fmt.Errorf("failed to read metadata: %w", err)
	}

	var metadata GeneratorMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	// Initialize FileMtimes if nil (backward compatibility with old format)
	if metadata.FileMtimes == nil {
		metadata.FileMtimes = make(map[string]time.Time)
	}

	return &metadata, nil
}

// ReadChunkFile reads an existing chunk file.
func (w *AtomicWriter) ReadChunkFile(filename string) (*ChunkFile, error) {
	filePath := filepath.Join(w.outputDir, filename)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist, return empty chunk file
			return &ChunkFile{
				Chunks: []Chunk{},
			}, nil
		}
		return nil, fmt.Errorf("failed to read chunk file: %w", err)
	}

	var chunkFile ChunkFile
	if err := json.Unmarshal(data, &chunkFile); err != nil {
		return nil, fmt.Errorf("failed to unmarshal chunk file: %w", err)
	}

	return &chunkFile, nil
}
