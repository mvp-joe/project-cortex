package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for GraphStorage:
// - Save and load graph data with correct metadata
// - Load non-existent file returns nil without error
// - Atomic write uses temp file and renames to final location
// - Graph metadata includes correct node/edge counts and version

func TestStorage_SaveAndLoad(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Create test graph data
	testData := &GraphData{
		Nodes: []Node{
			{
				ID:        "test.Foo",
				Kind:      NodeFunction,
				File:      "test.go",
				StartLine: 10,
				EndLine:   20,
			},
			{
				ID:        "test.Bar",
				Kind:      NodeFunction,
				File:      "test.go",
				StartLine: 25,
				EndLine:   35,
			},
		},
		Edges: []Edge{
			{
				From: "test.Foo",
				To:   "test.Bar",
				Type: EdgeCalls,
				Location: &Location{
					File: "test.go",
					Line: 15,
				},
			},
		},
	}

	// Save
	err = storage.Save(testData)
	require.NoError(t, err)

	// Verify file exists
	assert.True(t, storage.Exists())

	// Load
	loaded, err := storage.Load()
	require.NoError(t, err)
	require.NotNil(t, loaded)

	// Check metadata was added
	assert.Equal(t, GraphVersion, loaded.Metadata.Version)
	assert.Equal(t, 2, loaded.Metadata.NodeCount)
	assert.Equal(t, 1, loaded.Metadata.EdgeCount)

	// Check nodes
	require.Len(t, loaded.Nodes, 2)
	assert.Equal(t, "test.Foo", loaded.Nodes[0].ID)
	assert.Equal(t, "test.Bar", loaded.Nodes[1].ID)

	// Check edges
	require.Len(t, loaded.Edges, 1)
	assert.Equal(t, "test.Foo", loaded.Edges[0].From)
	assert.Equal(t, "test.Bar", loaded.Edges[0].To)
	assert.Equal(t, EdgeCalls, loaded.Edges[0].Type)
}

func TestStorage_LoadNonExistent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	// Load non-existent file should return nil without error
	loaded, err := storage.Load()
	require.NoError(t, err)
	assert.Nil(t, loaded)

	// Exists should return false
	assert.False(t, storage.Exists())
}

func TestStorage_AtomicWrite(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	graphDir := filepath.Join(tmpDir, "graph")

	storage, err := NewStorage(graphDir)
	require.NoError(t, err)

	testData := &GraphData{
		Nodes: []Node{
			{ID: "test.Foo", Kind: NodeFunction, File: "test.go", StartLine: 1, EndLine: 10},
		},
		Edges: []Edge{},
	}

	// Save should use temp file first
	err = storage.Save(testData)
	require.NoError(t, err)

	// Temp file should not exist after save (renamed to final)
	tempFile := filepath.Join(graphDir, ".tmp", GraphFileName)
	_, err = os.Stat(tempFile)
	assert.True(t, os.IsNotExist(err), "temp file should be renamed")

	// Final file should exist
	finalFile := filepath.Join(graphDir, GraphFileName)
	_, err = os.Stat(finalFile)
	assert.NoError(t, err, "final file should exist")
}
