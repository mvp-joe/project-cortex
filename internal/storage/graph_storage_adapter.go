package storage

import (
	"fmt"

	"github.com/mvp-joe/project-cortex/internal/graph"
)

// sqliteStorage implements graph.Storage interface using SQLite backend via GraphReader.
// This allows the graph searcher to load data from SQLite instead of JSON files.
type sqliteStorage struct {
	reader *GraphReader
}

// NewSQLiteGraphStorage creates a graph.Storage implementation backed by SQLite.
// The GraphReader must be configured with an active database connection.
func NewSQLiteGraphStorage(reader *GraphReader) graph.Storage {
	return &sqliteStorage{reader: reader}
}

// Load loads graph data from SQLite tables.
func (s *sqliteStorage) Load() (*graph.GraphData, error) {
	return s.reader.ReadGraphData()
}

// Save is not supported for SQLite storage.
// Use GraphWriter directly to write graph data.
func (s *sqliteStorage) Save(data *graph.GraphData) error {
	return fmt.Errorf("save not supported for SQLite storage (use storage.GraphWriter directly)")
}

// Exists checks if graph data exists in SQLite.
func (s *sqliteStorage) Exists() bool {
	data, err := s.reader.ReadGraphData()
	if err != nil {
		return false
	}
	return data != nil && len(data.Nodes) > 0
}
