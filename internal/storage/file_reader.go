package storage

import (
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
)

// FileReader handles reading file statistics and content from SQLite.
type FileReader struct {
	db *sql.DB
}

// ModuleStats represents aggregated module-level statistics.
type ModuleStats struct {
	ModulePath            string
	FileCount             int
	LineCountTotal        int
	LineCountCode         int
	TestFileCount         int
	TypeCount             int
	FunctionCount         int
	ExportedTypeCount     int
	ExportedFunctionCount int
	ImportCount           int
	ExternalImportCount   int
	Depth                 int
	UpdatedAt             time.Time
}

// NewFileReader creates a FileReader instance.
// DB should have schema already created.
func NewFileReader(db *sql.DB) *FileReader {
	return &FileReader{db: db}
}

// GetFileStats retrieves statistics for a single file.
// Returns (nil, nil) if file not found.
func (r *FileReader) GetFileStats(filePath string) (*FileStats, error) {
	stats := &FileStats{}
	var lastModified, indexedAt string

	err := sq.Select(
		"file_path", "language", "module_path", "is_test",
		"line_count_total", "line_count_code", "line_count_comment", "line_count_blank",
		"size_bytes", "file_hash", "last_modified", "indexed_at",
	).
		From("files").
		Where(sq.Eq{"file_path": filePath}).
		RunWith(r.db).
		QueryRow().
		Scan(
			&stats.FilePath,
			&stats.Language,
			&stats.ModulePath,
			&stats.IsTest,
			&stats.LineCountTotal,
			&stats.LineCountCode,
			&stats.LineCountComment,
			&stats.LineCountBlank,
			&stats.SizeBytes,
			&stats.FileHash,
			&lastModified,
			&indexedAt,
		)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get file stats for %s: %w", filePath, err)
	}

	// Parse timestamps
	stats.LastModified, _ = time.Parse(time.RFC3339, lastModified)
	stats.IndexedAt, _ = time.Parse(time.RFC3339, indexedAt)

	return stats, nil
}

// GetAllFiles retrieves all file statistics.
func (r *FileReader) GetAllFiles() ([]*FileStats, error) {
	rows, err := sq.Select(
		"file_path", "language", "module_path", "is_test",
		"line_count_total", "line_count_code", "line_count_comment", "line_count_blank",
		"size_bytes", "file_hash", "last_modified", "indexed_at",
	).
		From("files").
		OrderBy("file_path").
		RunWith(r.db).
		Query()

	if err != nil {
		return nil, fmt.Errorf("failed to query all files: %w", err)
	}
	defer rows.Close()

	return r.scanFileStats(rows)
}

// GetFilesByLanguage retrieves all files for a specific language.
func (r *FileReader) GetFilesByLanguage(language string) ([]*FileStats, error) {
	rows, err := sq.Select(
		"file_path", "language", "module_path", "is_test",
		"line_count_total", "line_count_code", "line_count_comment", "line_count_blank",
		"size_bytes", "file_hash", "last_modified", "indexed_at",
	).
		From("files").
		Where(sq.Eq{"language": language}).
		OrderBy("file_path").
		RunWith(r.db).
		Query()

	if err != nil {
		return nil, fmt.Errorf("failed to query files by language %s: %w", language, err)
	}
	defer rows.Close()

	return r.scanFileStats(rows)
}

// GetFilesByModule retrieves all files for a specific module.
func (r *FileReader) GetFilesByModule(modulePath string) ([]*FileStats, error) {
	rows, err := sq.Select(
		"file_path", "language", "module_path", "is_test",
		"line_count_total", "line_count_code", "line_count_comment", "line_count_blank",
		"size_bytes", "file_hash", "last_modified", "indexed_at",
	).
		From("files").
		Where(sq.Eq{"module_path": modulePath}).
		OrderBy("file_path").
		RunWith(r.db).
		Query()

	if err != nil {
		return nil, fmt.Errorf("failed to query files by module %s: %w", modulePath, err)
	}
	defer rows.Close()

	return r.scanFileStats(rows)
}

// GetModuleStats retrieves aggregated statistics for a module.
// Returns (nil, nil) if module not found.
func (r *FileReader) GetModuleStats(modulePath string) (*ModuleStats, error) {
	stats := &ModuleStats{}
	var updatedAt string

	err := sq.Select(
		"module_path", "file_count", "line_count_total", "line_count_code",
		"test_file_count", "type_count", "function_count",
		"exported_type_count", "exported_function_count",
		"import_count", "external_import_count", "depth", "updated_at",
	).
		From("modules").
		Where(sq.Eq{"module_path": modulePath}).
		RunWith(r.db).
		QueryRow().
		Scan(
			&stats.ModulePath,
			&stats.FileCount,
			&stats.LineCountTotal,
			&stats.LineCountCode,
			&stats.TestFileCount,
			&stats.TypeCount,
			&stats.FunctionCount,
			&stats.ExportedTypeCount,
			&stats.ExportedFunctionCount,
			&stats.ImportCount,
			&stats.ExternalImportCount,
			&stats.Depth,
			&updatedAt,
		)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get module stats for %s: %w", modulePath, err)
	}

	stats.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return stats, nil
}

// GetAllModules retrieves all module statistics.
func (r *FileReader) GetAllModules() ([]*ModuleStats, error) {
	rows, err := sq.Select(
		"module_path", "file_count", "line_count_total", "line_count_code",
		"test_file_count", "type_count", "function_count",
		"exported_type_count", "exported_function_count",
		"import_count", "external_import_count", "depth", "updated_at",
	).
		From("modules").
		OrderBy("module_path").
		RunWith(r.db).
		Query()

	if err != nil {
		return nil, fmt.Errorf("failed to query all modules: %w", err)
	}
	defer rows.Close()

	return r.scanModuleStats(rows)
}

// GetTopModules retrieves top N modules by line count (code).
func (r *FileReader) GetTopModules(limit int) ([]*ModuleStats, error) {
	rows, err := sq.Select(
		"module_path", "file_count", "line_count_total", "line_count_code",
		"test_file_count", "type_count", "function_count",
		"exported_type_count", "exported_function_count",
		"import_count", "external_import_count", "depth", "updated_at",
	).
		From("modules").
		OrderBy("line_count_code DESC").
		Limit(uint64(limit)).
		RunWith(r.db).
		Query()

	if err != nil {
		return nil, fmt.Errorf("failed to query top modules: %w", err)
	}
	defer rows.Close()

	return r.scanModuleStats(rows)
}

// GetFileContent retrieves full file content from FTS5 table.
// Returns empty string if file not found.
func (r *FileReader) GetFileContent(filePath string) (string, error) {
	var content string

	err := sq.Select("content").
		From("files_fts").
		Where(sq.Eq{"file_path": filePath}).
		RunWith(r.db).
		QueryRow().
		Scan(&content)

	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("failed to get file content for %s: %w", filePath, err)
	}

	return content, nil
}

// SearchFileContent performs FTS5 full-text search on file contents.
// Returns file statistics for matching files, ordered by relevance (rank).
func (r *FileReader) SearchFileContent(query string) ([]*FileStats, error) {
	rows, err := sq.Select(
		"f.file_path", "f.language", "f.module_path", "f.is_test",
		"f.line_count_total", "f.line_count_code", "f.line_count_comment", "f.line_count_blank",
		"f.size_bytes", "f.file_hash", "f.last_modified", "f.indexed_at",
	).
		From("files_fts fts").
		Join("files f ON fts.file_path = f.file_path").
		Where(sq.Expr("fts.content MATCH ?", query)).
		OrderBy("rank").
		RunWith(r.db).
		Query()

	if err != nil {
		return nil, fmt.Errorf("failed to search file content: %w", err)
	}
	defer rows.Close()

	return r.scanFileStats(rows)
}

// Close releases resources held by the reader.
// DB connection is NOT closed (caller owns it).
func (r *FileReader) Close() error {
	// No resources to clean up currently
	// DB is owned by caller, not closed here
	return nil
}

// scanFileStats is a helper to scan multiple FileStats rows.
func (r *FileReader) scanFileStats(rows *sql.Rows) ([]*FileStats, error) {
	var results []*FileStats

	for rows.Next() {
		stats := &FileStats{}
		var lastModified, indexedAt string

		err := rows.Scan(
			&stats.FilePath,
			&stats.Language,
			&stats.ModulePath,
			&stats.IsTest,
			&stats.LineCountTotal,
			&stats.LineCountCode,
			&stats.LineCountComment,
			&stats.LineCountBlank,
			&stats.SizeBytes,
			&stats.FileHash,
			&lastModified,
			&indexedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan file stats: %w", err)
		}

		stats.LastModified, _ = time.Parse(time.RFC3339, lastModified)
		stats.IndexedAt, _ = time.Parse(time.RFC3339, indexedAt)

		results = append(results, stats)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating file stats: %w", err)
	}

	return results, nil
}

// scanModuleStats is a helper to scan multiple ModuleStats rows.
func (r *FileReader) scanModuleStats(rows *sql.Rows) ([]*ModuleStats, error) {
	var results []*ModuleStats

	for rows.Next() {
		stats := &ModuleStats{}
		var updatedAt string

		err := rows.Scan(
			&stats.ModulePath,
			&stats.FileCount,
			&stats.LineCountTotal,
			&stats.LineCountCode,
			&stats.TestFileCount,
			&stats.TypeCount,
			&stats.FunctionCount,
			&stats.ExportedTypeCount,
			&stats.ExportedFunctionCount,
			&stats.ImportCount,
			&stats.ExternalImportCount,
			&stats.Depth,
			&updatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan module stats: %w", err)
		}

		stats.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

		results = append(results, stats)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating module stats: %w", err)
	}

	return results, nil
}
