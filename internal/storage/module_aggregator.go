package storage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
)

// ModuleAggregator handles aggregating module-level statistics from
// files, types, functions, and imports tables.
type ModuleAggregator struct {
	db *sql.DB
}

// NewModuleAggregator creates a ModuleAggregator instance.
// DB must have schema already created via CreateSchema().
func NewModuleAggregator(db *sql.DB) *ModuleAggregator {
	return &ModuleAggregator{db: db}
}

// AggregateModule recalculates statistics for a single module.
// This is an idempotent operation - safe to call multiple times.
// If the module has no files, the module entry is deleted.
// If the module doesn't exist yet, it is created if files are found.
//
// Uses INSERT OR REPLACE to handle both creation and updates.
// Transaction-safe: all aggregations succeed or fail together.
func (a *ModuleAggregator) AggregateModule(modulePath string) error {
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// First check if module has any files
	var fileCount int
	err = tx.QueryRow(`
		SELECT COUNT(*) FROM files WHERE module_path = ?
	`, modulePath).Scan(&fileCount)
	if err != nil {
		return fmt.Errorf("failed to count files for module %s: %w", modulePath, err)
	}

	// If no files, delete module entry and return
	if fileCount == 0 {
		_, err = sq.Delete("modules").
			Where(sq.Eq{"module_path": modulePath}).
			RunWith(tx).
			Exec()
		if err != nil {
			return fmt.Errorf("failed to delete empty module %s: %w", modulePath, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("failed to commit module deletion: %w", err)
		}
		return nil
	}

	// Calculate module depth
	depth := strings.Count(modulePath, "/")

	// Aggregate file statistics
	var lineCountTotal, lineCountCode, testFileCount int
	err = tx.QueryRow(`
		SELECT
			COALESCE(SUM(line_count_total), 0),
			COALESCE(SUM(line_count_code), 0),
			COALESCE(SUM(CASE WHEN is_test THEN 1 ELSE 0 END), 0)
		FROM files
		WHERE module_path = ?
	`, modulePath).Scan(&lineCountTotal, &lineCountCode, &testFileCount)
	if err != nil {
		return fmt.Errorf("failed to aggregate file stats for module %s: %w", modulePath, err)
	}

	// Count types and exported types
	var typeCount, exportedTypeCount int
	err = tx.QueryRow(`
		SELECT
			COALESCE(COUNT(*), 0),
			COALESCE(SUM(CASE WHEN is_exported = 1 THEN 1 ELSE 0 END), 0)
		FROM types
		WHERE module_path = ?
	`, modulePath).Scan(&typeCount, &exportedTypeCount)
	if err != nil {
		return fmt.Errorf("failed to aggregate type stats for module %s: %w", modulePath, err)
	}

	// Count functions and exported functions
	var functionCount, exportedFunctionCount int
	err = tx.QueryRow(`
		SELECT
			COALESCE(COUNT(*), 0),
			COALESCE(SUM(CASE WHEN is_exported = 1 THEN 1 ELSE 0 END), 0)
		FROM functions
		WHERE module_path = ?
	`, modulePath).Scan(&functionCount, &exportedFunctionCount)
	if err != nil {
		return fmt.Errorf("failed to aggregate function stats for module %s: %w", modulePath, err)
	}

	// Count total imports and unique external imports
	var importCount, externalImportCount int
	err = tx.QueryRow(`
		SELECT
			COALESCE(COUNT(*), 0),
			COALESCE(COUNT(DISTINCT CASE WHEN is_external = 1 THEN import_path END), 0)
		FROM imports
		WHERE file_path IN (SELECT file_path FROM files WHERE module_path = ?)
	`, modulePath).Scan(&importCount, &externalImportCount)
	if err != nil {
		return fmt.Errorf("failed to aggregate import stats for module %s: %w", modulePath, err)
	}

	// Insert or replace module entry
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = sq.Insert("modules").
		Columns(
			"module_path",
			"file_count",
			"line_count_total",
			"line_count_code",
			"test_file_count",
			"type_count",
			"function_count",
			"exported_type_count",
			"exported_function_count",
			"import_count",
			"external_import_count",
			"depth",
			"updated_at",
		).
		Values(
			modulePath,
			fileCount,
			lineCountTotal,
			lineCountCode,
			testFileCount,
			typeCount,
			functionCount,
			exportedTypeCount,
			exportedFunctionCount,
			importCount,
			externalImportCount,
			depth,
			now,
		).
		Options("OR REPLACE").
		RunWith(tx).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to insert/update module %s: %w", modulePath, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit module aggregation: %w", err)
	}

	return nil
}

// AggregateAllModules recalculates statistics for all modules.
// This clears the existing modules table and rebuilds it from scratch.
// Transaction-safe: all aggregations succeed or fail together.
func (a *ModuleAggregator) AggregateAllModules() error {
	tx, err := a.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Get all distinct module paths from files table
	rows, err := sq.Select("DISTINCT module_path").
		From("files").
		RunWith(tx).
		Query()
	if err != nil {
		return fmt.Errorf("failed to query module paths: %w", err)
	}
	defer rows.Close()

	var modulePaths []string
	for rows.Next() {
		var modulePath string
		if err := rows.Scan(&modulePath); err != nil {
			return fmt.Errorf("failed to scan module path: %w", err)
		}
		modulePaths = append(modulePaths, modulePath)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("error iterating module paths: %w", err)
	}
	rows.Close()

	// Clear existing modules table
	_, err = sq.Delete("modules").
		RunWith(tx).
		Exec()
	if err != nil {
		return fmt.Errorf("failed to clear modules table: %w", err)
	}

	// Commit the transaction to release locks
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit clear operation: %w", err)
	}

	// Aggregate each module (using separate transactions for each)
	// This is more efficient than one large transaction and allows
	// for better error isolation
	for _, modulePath := range modulePaths {
		if err := a.AggregateModule(modulePath); err != nil {
			return fmt.Errorf("failed to aggregate module %s: %w", modulePath, err)
		}
	}

	return nil
}
