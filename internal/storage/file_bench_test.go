package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// openDB creates a file-based SQLite database for benchmarking
func openDB(dbPath string) (*sql.DB, error) {
	return sql.Open("sqlite3", dbPath)
}

func BenchmarkFileWriter_WriteFileStatsBatch(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "bench.db")

	// Setup
	db, err := openDB(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err := CreateSchema(db); err != nil {
		b.Fatal(err)
	}

	// Create test data
	now := time.Now().UTC()
	files := make([]*FileStats, 1000)
	for i := range files {
		files[i] = &FileStats{
			FilePath:       "file" + string(rune(i)) + ".go",
			Language:       "go",
			ModulePath:     "pkg/module",
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash" + string(rune(i)),
			LastModified:   now,
			IndexedAt:      now,
		}
	}

	writer := NewFileWriter(db)
	defer writer.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = writer.WriteFileStatsBatch(files)
	}
}

func BenchmarkFileWriter_UpdateModuleStats(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "bench.db")

	// Setup
	db, err := openDB(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err := CreateSchema(db); err != nil {
		b.Fatal(err)
	}

	writer := NewFileWriter(db)
	defer writer.Close()

	// Write 1000 files across 10 modules
	now := time.Now().UTC()
	files := make([]*FileStats, 1000)
	for i := range files {
		files[i] = &FileStats{
			FilePath:       "module" + string(rune(i%10)) + "/file" + string(rune(i)) + ".go",
			Language:       "go",
			ModulePath:     "module" + string(rune(i%10)),
			LineCountTotal: 100 + i,
			LineCountCode:  80 + i,
			FileHash:       "hash" + string(rune(i)),
			LastModified:   now,
			IndexedAt:      now,
		}
	}

	if err := writer.WriteFileStatsBatch(files); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = writer.UpdateModuleStats()
	}
}

func BenchmarkFileWriter_WriteFileContentBatch(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "bench.db")

	// Setup
	db, err := openDB(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err := CreateSchema(db); err != nil {
		b.Fatal(err)
	}

	// Create test content
	contents := make([]*FileContent, 100)
	for i := range contents {
		contents[i] = &FileContent{
			FilePath: "file" + string(rune(i)) + ".go",
			Content:  "package main\n\ntype Provider interface {\n\tEmbed() error\n}\n\nfunc NewProvider() Provider {\n\treturn nil\n}",
		}
	}

	writer := NewFileWriter(db)
	defer writer.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = writer.WriteFileContentBatch(contents)
	}
}

func BenchmarkFileReader_GetFilesByLanguage(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "bench.db")

	// Setup
	db, err := openDB(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err := CreateSchema(db); err != nil {
		b.Fatal(err)
	}

	writer := NewFileWriter(db)

	// Write 1000 files with different languages
	now := time.Now().UTC()
	files := make([]*FileStats, 1000)
	languages := []string{"go", "typescript", "python", "rust"}
	for i := range files {
		files[i] = &FileStats{
			FilePath:       "file" + string(rune(i)) + ".ext",
			Language:       languages[i%len(languages)],
			ModulePath:     "pkg/module",
			LineCountTotal: 100,
			LineCountCode:  80,
			FileHash:       "hash" + string(rune(i)),
			LastModified:   now,
			IndexedAt:      now,
		}
	}

	if err := writer.WriteFileStatsBatch(files); err != nil {
		b.Fatal(err)
	}
	writer.Close()

	reader := NewFileReader(db)
	defer reader.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = reader.GetFilesByLanguage("go")
	}
}

func BenchmarkFileReader_SearchFileContent(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "bench.db")

	// Setup
	db, err := openDB(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err := CreateSchema(db); err != nil {
		b.Fatal(err)
	}

	writer := NewFileWriter(db)

	// Write files with searchable content
	now := time.Now().UTC()
	files := make([]*FileStats, 100)
	contents := make([]*FileContent, 100)
	for i := range files {
		filePath := "file" + string(rune(i)) + ".go"
		files[i] = &FileStats{
			FilePath:       filePath,
			Language:       "go",
			ModulePath:     "pkg",
			LineCountTotal: 20,
			LineCountCode:  15,
			FileHash:       "hash" + string(rune(i)),
			LastModified:   now,
			IndexedAt:      now,
		}
		contents[i] = &FileContent{
			FilePath: filePath,
			Content:  "package main\n\ntype Provider interface {\n\tEmbed(ctx context.Context) error\n\tClose() error\n}",
		}
	}

	if err := writer.WriteFileStatsBatch(files); err != nil {
		b.Fatal(err)
	}
	if err := writer.WriteFileContentBatch(contents); err != nil {
		b.Fatal(err)
	}
	writer.Close()

	reader := NewFileReader(db)
	defer reader.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = reader.SearchFileContent("Provider AND interface")
	}
}

func BenchmarkFileReader_GetTopModules(b *testing.B) {
	dbPath := filepath.Join(b.TempDir(), "bench.db")

	// Setup
	db, err := openDB(dbPath)
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	if err := CreateSchema(db); err != nil {
		b.Fatal(err)
	}

	writer := NewFileWriter(db)

	// Write files across 50 modules
	now := time.Now().UTC()
	files := make([]*FileStats, 1000)
	for i := range files {
		files[i] = &FileStats{
			FilePath:       "module" + string(rune(i%50)) + "/file" + string(rune(i)) + ".go",
			Language:       "go",
			ModulePath:     "module" + string(rune(i%50)),
			LineCountTotal: 100 + i,
			LineCountCode:  80 + i,
			FileHash:       "hash" + string(rune(i)),
			LastModified:   now,
			IndexedAt:      now,
		}
	}

	if err := writer.WriteFileStatsBatch(files); err != nil {
		b.Fatal(err)
	}
	if err := writer.UpdateModuleStats(); err != nil {
		b.Fatal(err)
	}
	writer.Close()

	reader := NewFileReader(db)
	defer reader.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = reader.GetTopModules(10)
	}
}
