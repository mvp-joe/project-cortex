package indexer

import (
	"os"
	"testing"

	"github.com/mvp-joe/project-cortex/internal/storage"
)

// TestMain initializes the test environment before running tests.
// This is required to initialize the sqlite-vec extension for all tests
// that use SQLite storage.
func TestMain(m *testing.M) {
	// Initialize sqlite-vec extension globally
	storage.InitVectorExtension()

	// Run all tests
	code := m.Run()

	// Exit with test result code
	os.Exit(code)
}
