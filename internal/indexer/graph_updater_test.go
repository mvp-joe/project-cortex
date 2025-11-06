package indexer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraphUpdater_Update_SingleFileAdd(t *testing.T) {
	t.Parallel()

	// Setup: Create temp database
	db := setupTestDB(t)
	defer db.Close()

	// Setup: Create temp project directory with a Go file
	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "main.go")
	writeGoFile(t, testFile, `package main

type Server struct {
	Port int
}

func (s *Server) Start() error {
	return nil
}

func main() {
	s := &Server{Port: 8080}
	s.Start()
}
`)

	// Create GraphUpdater
	updater := NewGraphUpdater(db, rootDir)

	// Create ChangeSet with added file
	changes := &ChangeSet{
		Added: []string{"main.go"},
	}

	// Execute update
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify types were inserted
	var typeCount int
	err = db.QueryRow("SELECT COUNT(*) FROM types WHERE file_path = ?", "main.go").Scan(&typeCount)
	require.NoError(t, err)
	assert.Equal(t, 1, typeCount, "should have 1 type (Server)")

	// Verify functions were inserted (main + Start method)
	var functionCount int
	err = db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "main.go").Scan(&functionCount)
	require.NoError(t, err)
	assert.Equal(t, 2, functionCount, "should have 2 functions (main + Start)")

	// Verify at least one function call was recorded (s.Start())
	var callCount int
	err = db.QueryRow("SELECT COUNT(*) FROM function_calls WHERE source_file_path = ?", "main.go").Scan(&callCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, callCount, 1, "should have at least 1 function call")
}

func TestGraphUpdater_Update_SingleFileModify(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "handler.go")

	// Initial version: 1 type, 1 method
	writeGoFile(t, testFile, `package handler

type Handler struct{}

func (h *Handler) Handle() {}
`)

	updater := NewGraphUpdater(db, rootDir)

	// First update: add file
	changes := &ChangeSet{Added: []string{"handler.go"}}
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify initial state
	var typeCount, functionCount int
	db.QueryRow("SELECT COUNT(*) FROM types WHERE file_path = ?", "handler.go").Scan(&typeCount)
	db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "handler.go").Scan(&functionCount)
	assert.Equal(t, 1, typeCount)
	assert.Equal(t, 1, functionCount)

	// Modified version: 1 type, 2 methods
	writeGoFile(t, testFile, `package handler

type Handler struct{}

func (h *Handler) Handle() {}
func (h *Handler) Process() {}
`)

	// Second update: modify file
	changes = &ChangeSet{Modified: []string{"handler.go"}}
	err = updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify updated state
	db.QueryRow("SELECT COUNT(*) FROM types WHERE file_path = ?", "handler.go").Scan(&typeCount)
	db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "handler.go").Scan(&functionCount)
	assert.Equal(t, 1, typeCount, "should still have 1 type")
	assert.Equal(t, 2, functionCount, "should now have 2 functions")
}

func TestGraphUpdater_Update_SingleFileDelete(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "temp.go")

	writeGoFile(t, testFile, `package temp

type Temp struct{}

func (t *Temp) Method() {}
`)

	updater := NewGraphUpdater(db, rootDir)

	// Add file
	changes := &ChangeSet{Added: []string{"temp.go"}}
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify data exists
	var typeCount, functionCount int
	db.QueryRow("SELECT COUNT(*) FROM types WHERE file_path = ?", "temp.go").Scan(&typeCount)
	db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "temp.go").Scan(&functionCount)
	assert.Equal(t, 1, typeCount)
	assert.Equal(t, 1, functionCount)

	// Delete file
	changes = &ChangeSet{Deleted: []string{"temp.go"}}
	err = updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify data was deleted
	db.QueryRow("SELECT COUNT(*) FROM types WHERE file_path = ?", "temp.go").Scan(&typeCount)
	db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "temp.go").Scan(&functionCount)
	assert.Equal(t, 0, typeCount, "types should be deleted")
	assert.Equal(t, 0, functionCount, "functions should be deleted")
}

func TestGraphUpdater_Update_MultipleFiles(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()

	// Create multiple files
	file1 := filepath.Join(rootDir, "file1.go")
	file2 := filepath.Join(rootDir, "file2.go")
	file3 := filepath.Join(rootDir, "file3.go")

	writeGoFile(t, file1, `package pkg
type Type1 struct{}
`)
	writeGoFile(t, file2, `package pkg
type Type2 struct{}
`)
	writeGoFile(t, file3, `package pkg
type Type3 struct{}
`)

	updater := NewGraphUpdater(db, rootDir)

	// Update all files at once
	changes := &ChangeSet{
		Added: []string{"file1.go", "file2.go", "file3.go"},
	}
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify all types were inserted
	var totalTypes int
	err = db.QueryRow("SELECT COUNT(*) FROM types").Scan(&totalTypes)
	require.NoError(t, err)
	assert.Equal(t, 3, totalTypes, "should have 3 types")
}

func TestGraphUpdater_Update_SkipsNonGoFiles(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()

	updater := NewGraphUpdater(db, rootDir)

	// Create ChangeSet with non-Go files
	changes := &ChangeSet{
		Added: []string{
			"README.md",
			"config.json",
			"script.sh",
			"main.go",
		},
	}

	// Create only main.go
	mainFile := filepath.Join(rootDir, "main.go")
	writeGoFile(t, mainFile, `package main
type App struct{}
`)

	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify only main.go was processed
	var typeCount int
	err = db.QueryRow("SELECT COUNT(*) FROM types").Scan(&typeCount)
	require.NoError(t, err)
	assert.Equal(t, 1, typeCount, "should only process .go files")
}

func TestGraphUpdater_Update_ReInferenceTrigger(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()

	// Create interface and struct
	interfaceFile := filepath.Join(rootDir, "iface.go")
	structFile := filepath.Join(rootDir, "impl.go")

	writeGoFile(t, interfaceFile, `package pkg

type Handler interface {
	Handle()
}
`)

	writeGoFile(t, structFile, `package pkg

type MyHandler struct{}

func (m *MyHandler) Handle() {}
`)

	updater := NewGraphUpdater(db, rootDir)

	// Add both files
	changes := &ChangeSet{
		Added: []string{"iface.go", "impl.go"},
	}
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify interface implementation was inferred
	var relCount int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM type_relationships
		WHERE relationship_type = ?
	`, "implements").Scan(&relCount)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, relCount, 1, "should have at least 1 implements relationship")
}

func TestGraphUpdater_Update_CascadeDelete(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "cascade.go")

	writeGoFile(t, testFile, `package cascade

type Service struct {
	Name string
}

func (s *Service) Run() error {
	return nil
}

func New() *Service {
	return &Service{}
}
`)

	updater := NewGraphUpdater(db, rootDir)

	// Add file
	changes := &ChangeSet{Added: []string{"cascade.go"}}
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify child records exist (type_fields, function_parameters)
	var fieldCount, paramCount int
	db.QueryRow("SELECT COUNT(*) FROM type_fields").Scan(&fieldCount)
	db.QueryRow("SELECT COUNT(*) FROM function_parameters").Scan(&paramCount)

	// At least the struct field should exist
	assert.GreaterOrEqual(t, fieldCount, 1, "should have type fields")

	// Delete file
	changes = &ChangeSet{Deleted: []string{"cascade.go"}}
	err = updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify CASCADE deleted child records
	db.QueryRow("SELECT COUNT(*) FROM type_fields").Scan(&fieldCount)
	db.QueryRow("SELECT COUNT(*) FROM function_parameters").Scan(&paramCount)
	assert.Equal(t, 0, fieldCount, "type_fields should be cascade deleted")
	assert.Equal(t, 0, paramCount, "function_parameters should be cascade deleted")
}

func TestGraphUpdater_Update_ErrorHandling(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()

	updater := NewGraphUpdater(db, rootDir)

	// Try to process a file that doesn't exist
	changes := &ChangeSet{
		Added: []string{"nonexistent.go"},
	}

	err := updater.Update(context.Background(), changes)
	assert.Error(t, err, "should error on non-existent file")
	assert.Contains(t, err.Error(), "extract", "error should mention extraction failure")
}

func TestGraphUpdater_Update_NoTypeChanges(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "util.go")

	// File with only functions, no types
	writeGoFile(t, testFile, `package util

func Helper() string {
	return "help"
}

func Another() int {
	return 42
}
`)

	updater := NewGraphUpdater(db, rootDir)

	// Add file (no types, so no re-inference should happen)
	changes := &ChangeSet{Added: []string{"util.go"}}
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify functions were added
	var functionCount int
	err = db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "util.go").Scan(&functionCount)
	require.NoError(t, err)
	assert.Equal(t, 2, functionCount)

	// No relationships should exist (no types)
	var relCount int
	err = db.QueryRow("SELECT COUNT(*) FROM type_relationships").Scan(&relCount)
	require.NoError(t, err)
	assert.Equal(t, 0, relCount)
}

func TestGraphUpdater_deleteCodeStructure(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	rootDir := t.TempDir()
	testFile := filepath.Join(rootDir, "delete.go")

	writeGoFile(t, testFile, `package delete

type Widget struct{}
func (w *Widget) Do() {}
`)

	updater := NewGraphUpdater(db, rootDir)

	// Add file
	changes := &ChangeSet{Added: []string{"delete.go"}}
	err := updater.Update(context.Background(), changes)
	require.NoError(t, err)

	// Verify data exists
	var count int
	db.QueryRow("SELECT COUNT(*) FROM types WHERE file_path = ?", "delete.go").Scan(&count)
	assert.Equal(t, 1, count)

	// Call deleteCodeStructure directly
	err = updater.deleteCodeStructure(context.Background(), "delete.go")
	require.NoError(t, err)

	// Verify all data was deleted
	var typeCount, functionCount, importCount int
	db.QueryRow("SELECT COUNT(*) FROM types WHERE file_path = ?", "delete.go").Scan(&typeCount)
	db.QueryRow("SELECT COUNT(*) FROM functions WHERE file_path = ?", "delete.go").Scan(&functionCount)
	db.QueryRow("SELECT COUNT(*) FROM imports WHERE file_path = ?", "delete.go").Scan(&importCount)

	assert.Equal(t, 0, typeCount)
	assert.Equal(t, 0, functionCount)
	assert.Equal(t, 0, importCount)
}

// Helper functions

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Create in-memory database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)

	// Enable foreign keys
	_, err = db.Exec("PRAGMA foreign_keys = ON")
	require.NoError(t, err)

	// Create schema
	err = storage.CreateSchema(db)
	require.NoError(t, err)

	return db
}

func writeGoFile(t *testing.T, path string, content string) {
	t.Helper()

	// Ensure directory exists
	dir := filepath.Dir(path)
	err := os.MkdirAll(dir, 0755)
	require.NoError(t, err)

	// Write file
	err = os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
}
