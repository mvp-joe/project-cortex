package mcp

// This file provides the unified chunk loading interface.
// All chunks are loaded from SQLite cache (no legacy JSON support).
//
// Implementation moved to loader_sqlite.go:
// - LoadChunksFromSQLite: Load chunks from SQLite cache
// - deriveTags: Derive tags from chunk type and file path
// - updateBranchAccessTime: Update branch metadata after loading
