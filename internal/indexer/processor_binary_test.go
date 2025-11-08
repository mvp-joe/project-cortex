package indexer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	storagepkg "github.com/mvp-joe/project-cortex/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProcessor_ProcessFiles_BinaryFileHandling verifies that binary files
// are stored without content (content = NULL) while text files have content.
func TestProcessor_ProcessFiles_BinaryFileHandling(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create a text file (Go source)
	goFile := filepath.Join(tempDir, "main.go")
	goContent := `package main

func main() {
	println("Hello, World!")
}
`
	err = os.WriteFile(goFile, []byte(goContent), 0644)
	require.NoError(t, err)

	// Create a binary file (PNG header with null bytes)
	binaryFile := filepath.Join(tempDir, "image.png")
	binaryContent := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG magic bytes
		0x00, 0x00, 0x00, 0x0D, // IHDR chunk with null bytes
		'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, // Width = 1
		0x00, 0x00, 0x00, 0x01, // Height = 1
	}
	err = os.WriteFile(binaryFile, binaryContent, 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{goFile, binaryFile})
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify file stats were written for both files
	fileReader := storagepkg.NewFileReader(db)

	// Check text file (Go)
	goStats, err := fileReader.GetFileStats("main.go")
	require.NoError(t, err)
	assert.Equal(t, "main.go", goStats.FilePath)
	assert.Equal(t, "go", goStats.Language)

	// Verify text file has content stored
	row := db.QueryRow("SELECT content FROM files WHERE file_path = ?", "main.go")
	var content *string
	err = row.Scan(&content)
	require.NoError(t, err)
	require.NotNil(t, content, "text file should have content stored")
	assert.Contains(t, *content, "package main", "content should match file")
	assert.Contains(t, *content, "Hello, World!", "content should match file")

	// Check binary file (PNG)
	pngStats, err := fileReader.GetFileStats("image.png")
	require.NoError(t, err)
	assert.Equal(t, "image.png", pngStats.FilePath)

	// Verify binary file has NULL content
	row = db.QueryRow("SELECT content FROM files WHERE file_path = ?", "image.png")
	var binaryFileContent *string
	err = row.Scan(&binaryFileContent)
	require.NoError(t, err)
	assert.Nil(t, binaryFileContent, "binary file should have NULL content")
}

// TestProcessor_ProcessFiles_EmptyTextFile verifies that empty text files
// get an empty string (not NULL) for content.
func TestProcessor_ProcessFiles_EmptyTextFile(t *testing.T) {
	t.Parallel()

	// Setup
	tempDir := t.TempDir()
	db := storagepkg.NewTestDB(t)

	storage, err := setupProcessorTestStorage(t, db, tempDir)
	require.NoError(t, err)

	// Create an empty text file
	emptyFile := filepath.Join(tempDir, "empty.go")
	err = os.WriteFile(emptyFile, []byte(""), 0644)
	require.NoError(t, err)

	processor := createTestProcessor(t, tempDir, storage)

	// Execute
	ctx := context.Background()
	stats, err := processor.ProcessFiles(ctx, []string{emptyFile})
	require.NoError(t, err)
	require.NotNil(t, stats)

	// Verify empty text file has empty string (not NULL)
	row := db.QueryRow("SELECT content FROM files WHERE file_path = ?", "empty.go")
	var content *string
	err = row.Scan(&content)
	require.NoError(t, err)
	require.NotNil(t, content, "empty text file should have non-NULL content")
	assert.Equal(t, "", *content, "empty text file should have empty string")
}
