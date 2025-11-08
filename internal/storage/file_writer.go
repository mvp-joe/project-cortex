package storage

import (
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
)

// FileWriter handles writing file statistics and content to SQLite.
type FileWriter struct {
	db *sql.DB
}

// FileStats represents file-level statistics for storage.
type FileStats struct {
	FilePath         string
	Language         string
	ModulePath       string
	IsTest           bool
	LineCountTotal   int
	LineCountCode    int
	LineCountComment int
	LineCountBlank   int
	SizeBytes        int64
	FileHash         string // SHA-256
	LastModified     time.Time
	IndexedAt        time.Time
}

// FileContent represents full file content for FTS5 indexing.
type FileContent struct {
	FilePath string
	Content  string // Full file content for keyword search
}

// NewFileWriter creates a FileWriter instance.
// DB must have schema already created via CreateSchema().
func NewFileWriter(db *sql.DB) *FileWriter {
	return &FileWriter{db: db}
}

// WriteFile writes or updates a file's statistics and content atomically.
// The unified API supports binary file detection via pointer semantics:
//   - content = nil → binary file (files.content = NULL, no FTS entry)
//   - content = &"" → empty text file (files.content = "", FTS entry created)
//   - content = &"package main..." → text file (files.content populated, FTS entry created)
//
// FTS sync happens automatically via database triggers (see createFTSTriggers).
// This method replaces the two-method pattern (WriteFileStats + WriteFileContent).
func (w *FileWriter) WriteFile(file *FileStats, content *string) error {
	// Convert *string to interface{} for SQL NULL handling
	var contentVal interface{}
	if content != nil {
		contentVal = *content
	} else {
		contentVal = nil // SQL NULL for binary files
	}

	_, err := sq.Insert("files").
		Columns(
			"file_path", "language", "module_path", "is_test",
			"line_count_total", "line_count_code", "line_count_comment", "line_count_blank",
			"size_bytes", "file_hash", "last_modified", "indexed_at",
			"content",
		).
		Values(
			file.FilePath,
			file.Language,
			file.ModulePath,
			file.IsTest,
			file.LineCountTotal,
			file.LineCountCode,
			file.LineCountComment,
			file.LineCountBlank,
			file.SizeBytes,
			file.FileHash,
			file.LastModified.Format(time.RFC3339),
			file.IndexedAt.Format(time.RFC3339),
			contentVal,
		).
		Options("OR REPLACE").
		RunWith(w.db).
		Exec()

	if err != nil {
		return fmt.Errorf("failed to write file for %s: %w", file.FilePath, err)
	}

	return nil
}

// WriteFileStats writes or updates a single file's statistics without content.
//
// Deprecated: Use WriteFile(file, nil) instead, which handles both stats and content atomically.
// This method will be removed in v3.0.
func (w *FileWriter) WriteFileStats(stats *FileStats) error {
	// Delegate to WriteFile with nil content (binary file)
	return w.WriteFile(stats, nil)
}

// WriteFileStatsBatch writes multiple file statistics in a single transaction.
// More efficient than individual writes for bulk updates.
func (w *FileWriter) WriteFileStatsBatch(stats []*FileStats) error {
	if len(stats) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Build the query once with Squirrel, then get SQL and args for preparation
	builder := sq.Insert("files").
		Columns(
			"file_path", "language", "module_path", "is_test",
			"line_count_total", "line_count_code", "line_count_comment", "line_count_blank",
			"size_bytes", "file_hash", "last_modified", "indexed_at",
		).
		Options("OR REPLACE")

	// Get SQL string for preparation (use a dummy values call)
	sqlStr, _, err := builder.Values("", "", "", false, 0, 0, 0, 0, 0, "", "", "").ToSql()
	if err != nil {
		return fmt.Errorf("failed to build SQL: %w", err)
	}

	stmt, err := tx.Prepare(sqlStr)
	if err != nil {
		return fmt.Errorf("failed to prepare statement: %w", err)
	}
	defer stmt.Close()

	for _, s := range stats {
		_, err := stmt.Exec(
			s.FilePath,
			s.Language,
			s.ModulePath,
			s.IsTest,
			s.LineCountTotal,
			s.LineCountCode,
			s.LineCountComment,
			s.LineCountBlank,
			s.SizeBytes,
			s.FileHash,
			s.LastModified.Format(time.RFC3339),
			s.IndexedAt.Format(time.RFC3339),
		)
		if err != nil {
			return fmt.Errorf("failed to insert file %s: %w", s.FilePath, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit batch: %w", err)
	}

	return nil
}

// WriteFileContent writes full file content to FTS5 table for keyword search.
//
// Deprecated: Use WriteFile(file, &content) instead, which handles both stats and content atomically.
// This method will be removed in v3.0.
//
// Migration guide:
//   OLD: writer.WriteFileStats(stats); writer.WriteFileContent(&FileContent{path, content})
//   NEW: writer.WriteFile(stats, &content)
func (w *FileWriter) WriteFileContent(content *FileContent) error {
	// This method is deprecated and should not be used for new code.
	// Existing callers should migrate to WriteFile.
	return fmt.Errorf("WriteFileContent is deprecated, use WriteFile(file, &content) instead")
}

// WriteFileContentBatch writes multiple file contents to FTS5 in a transaction.
func (w *FileWriter) WriteFileContentBatch(contents []*FileContent) error {
	if len(contents) == 0 {
		return nil
	}

	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Build SQL for delete and insert statements
	deleteSql, _, err := sq.Delete("files_fts").
		Where(sq.Eq{"file_path": ""}).
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build delete SQL: %w", err)
	}

	insertSql, _, err := sq.Insert("files_fts").
		Columns("file_path", "content").
		Values("", "").
		ToSql()
	if err != nil {
		return fmt.Errorf("failed to build insert SQL: %w", err)
	}

	deleteStmt, err := tx.Prepare(deleteSql)
	if err != nil {
		return fmt.Errorf("failed to prepare delete statement: %w", err)
	}
	defer deleteStmt.Close()

	insertStmt, err := tx.Prepare(insertSql)
	if err != nil {
		return fmt.Errorf("failed to prepare insert statement: %w", err)
	}
	defer insertStmt.Close()

	for _, c := range contents {
		// Delete old entry
		if _, err := deleteStmt.Exec(c.FilePath); err != nil {
			return fmt.Errorf("failed to delete FTS entry for %s: %w", c.FilePath, err)
		}

		// Insert new content
		if _, err := insertStmt.Exec(c.FilePath, c.Content); err != nil {
			return fmt.Errorf("failed to insert FTS content for %s: %w", c.FilePath, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit FTS batch: %w", err)
	}

	return nil
}

// DeleteFile removes a file from the database.
// CASCADE deletes propagate to types, functions, chunks, etc.
func (w *FileWriter) DeleteFile(filePath string) error {
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete from files table (cascades to related tables via FK constraints)
	_, err = sq.Delete("files").
		Where(sq.Eq{"file_path": filePath}).
		RunWith(tx).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to delete file %s: %w", filePath, err)
	}

	// Delete from FTS5 table
	_, err = sq.Delete("files_fts").
		Where(sq.Eq{"file_path": filePath}).
		RunWith(tx).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to delete FTS entry for %s: %w", filePath, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit delete: %w", err)
	}

	return nil
}

// Close releases resources held by the writer.
// The underlying DB connection is NOT closed (caller owns it).
func (w *FileWriter) Close() error {
	// No resources to clean up currently (no prepared statements cached)
	// DB is owned by caller, not closed here
	return nil
}
