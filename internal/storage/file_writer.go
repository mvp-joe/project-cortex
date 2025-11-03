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

// WriteFileStats writes or updates a single file's statistics.
// Uses INSERT OR REPLACE to handle updates.
func (w *FileWriter) WriteFileStats(stats *FileStats) error {
	_, err := sq.Insert("files").
		Columns(
			"file_path", "language", "module_path", "is_test",
			"line_count_total", "line_count_code", "line_count_comment", "line_count_blank",
			"size_bytes", "file_hash", "last_modified", "indexed_at",
		).
		Values(
			stats.FilePath,
			stats.Language,
			stats.ModulePath,
			stats.IsTest,
			stats.LineCountTotal,
			stats.LineCountCode,
			stats.LineCountComment,
			stats.LineCountBlank,
			stats.SizeBytes,
			stats.FileHash,
			stats.LastModified.Format(time.RFC3339),
			stats.IndexedAt.Format(time.RFC3339),
		).
		Options("OR REPLACE").
		RunWith(w.db).
		Exec()

	if err != nil {
		return fmt.Errorf("failed to write file stats for %s: %w", stats.FilePath, err)
	}

	return nil
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
// Replaces existing content if file already indexed.
func (w *FileWriter) WriteFileContent(content *FileContent) error {
	tx, err := w.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete existing entry
	_, err = sq.Delete("files_fts").
		Where(sq.Eq{"file_path": content.FilePath}).
		RunWith(tx).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to delete old FTS entry for %s: %w", content.FilePath, err)
	}

	// Insert new content
	_, err = sq.Insert("files_fts").
		Columns("file_path", "content").
		Values(content.FilePath, content.Content).
		RunWith(tx).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to insert FTS content for %s: %w", content.FilePath, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit FTS write: %w", err)
	}

	return nil
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
