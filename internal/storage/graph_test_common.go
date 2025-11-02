package storage

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

// openTestDB creates an in-memory SQLite database for testing.
func openTestDB(t *testing.T) *sql.DB {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	return db
}

// setupGraphTestDB creates an in-memory database with schema for graph testing.
// Returns both the DB and a GraphWriter/GraphReader ready to use.
func setupGraphTestDB(t *testing.T) (*sql.DB, *GraphWriter, *GraphReader) {
	db := openTestDB(t)
	require.NoError(t, CreateSchema(db))

	writer := &GraphWriter{db: db}
	reader := &GraphReader{db: db}

	return db, writer, reader
}
