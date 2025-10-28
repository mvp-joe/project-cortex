package graph

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// GraphFileName is the name of the graph data file
	GraphFileName = "code-graph.json"
	// GraphVersion is the current version of the graph format
	GraphVersion = "1.0"
)

// Storage handles reading and writing graph data to disk.
type Storage interface {
	// Load loads the graph from disk. Returns nil if file doesn't exist.
	Load() (*GraphData, error)

	// Save saves the graph to disk using atomic write pattern.
	Save(data *GraphData) error

	// Exists checks if the graph file exists.
	Exists() bool
}

// storage implements Storage with atomic write support.
type storage struct {
	graphDir string // Directory containing graph file (.cortex/graph/)
}

// NewStorage creates a new graph storage instance.
func NewStorage(graphDir string) (Storage, error) {
	// Ensure graph directory exists
	if err := os.MkdirAll(graphDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create graph directory: %w", err)
	}

	// Ensure temp directory exists for atomic writes
	tempDir := filepath.Join(graphDir, ".tmp")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp directory: %w", err)
	}

	return &storage{graphDir: graphDir}, nil
}

// Load loads the graph data from disk.
func (s *storage) Load() (*GraphData, error) {
	filePath := s.graphFilePath()

	// Check if file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, nil // Not an error, just no graph yet
	}

	// Read file
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read graph file: %w", err)
	}

	// Parse JSON
	var graphData GraphData
	if err := json.Unmarshal(data, &graphData); err != nil {
		return nil, fmt.Errorf("failed to parse graph JSON: %w", err)
	}

	return &graphData, nil
}

// Save saves the graph data to disk using atomic write pattern.
func (s *storage) Save(data *GraphData) error {
	// Update metadata
	data.Metadata.Version = GraphVersion
	data.Metadata.GeneratedAt = time.Now()
	data.Metadata.NodeCount = len(data.Nodes)
	data.Metadata.EdgeCount = len(data.Edges)

	// Marshal to JSON with indentation for readability
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal graph data: %w", err)
	}

	// Write to temp file first
	tempPath := filepath.Join(s.graphDir, ".tmp", GraphFileName)
	if err := os.WriteFile(tempPath, jsonData, 0644); err != nil {
		return fmt.Errorf("failed to write temp graph file: %w", err)
	}

	// Atomic rename (POSIX guarantees atomicity)
	finalPath := s.graphFilePath()
	if err := os.Rename(tempPath, finalPath); err != nil {
		return fmt.Errorf("failed to rename temp graph file: %w", err)
	}

	return nil
}

// Exists checks if the graph file exists.
func (s *storage) Exists() bool {
	_, err := os.Stat(s.graphFilePath())
	return err == nil
}

// graphFilePath returns the full path to the graph file.
func (s *storage) graphFilePath() string {
	return filepath.Join(s.graphDir, GraphFileName)
}
