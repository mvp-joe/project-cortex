//go:build fts5 || sqlite_fts5

// Package storage enables FTS5 support for SQLite full-text search.
// This file should be compiled with build tags: -tags="fts5" or -tags="sqlite_fts5"
//
// Note: mattn/go-sqlite3 will automatically enable FTS5 when these build tags are present.
// See: github.com/mattn/go-sqlite3/sqlite3_opt_fts5.go
package storage

import (
	_ "github.com/mattn/go-sqlite3"
)
