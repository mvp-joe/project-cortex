package files

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// TestMain is run once before all tests in this package.
// Registers the SQLite driver for all tests.
func TestMain(m *testing.M) {
	// SQLite driver is registered via blank import above
	m.Run()
}
