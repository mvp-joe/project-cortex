package storage_test

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/mattn/go-sqlite3"
	"github.com/mvp-joe/project-cortex/internal/storage"
)

// Example_createDatabase demonstrates creating a new SQLite cache database
// with the unified schema.
func Example_createDatabase() {
	// Open in-memory database for demo
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Check if schema exists
	version, err := storage.GetSchemaVersion(db)
	if err != nil {
		log.Fatal(err)
	}

	if version == "0" {
		// New database - create schema
		if err := storage.CreateSchema(db); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Created new schema version 2.0")
	} else {
		fmt.Printf("Existing schema version: %s\n", version)
	}

	// Verify schema
	version, err = storage.GetSchemaVersion(db)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Current schema version: %s\n", version)

	// Output:
	// Created new schema version 2.0
	// Current schema version: 2.0
}

// Example_queryMetadata demonstrates querying cache metadata.
func Example_queryMetadata() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create schema
	if err := storage.CreateSchema(db); err != nil {
		log.Fatal(err)
	}

	// Query all metadata
	rows, err := db.Query("SELECT key, value FROM cache_metadata ORDER BY key")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			log.Fatal(err)
		}
		// Skip last_indexed as it's empty initially
		if key != "last_indexed" {
			fmt.Printf("%s: %s\n", key, value)
		}
	}

	// Output:
	// branch: main
	// embedding_dimensions: 384
	// schema_version: 2.0
}

// Example_insertFile demonstrates inserting a file and querying it.
func Example_insertFile() {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		log.Fatal(err)
	}

	// Create schema
	if err := storage.CreateSchema(db); err != nil {
		log.Fatal(err)
	}

	// Insert a file
	_, err = db.Exec(`
		INSERT INTO files (
			file_path, language, module_path, is_test,
			line_count_total, line_count_code,
			file_hash, last_modified, indexed_at
		) VALUES (
			'internal/storage/schema.go', 'go', 'internal/storage', 0,
			359, 280,
			'abc123', '2025-11-02T10:00:00Z', '2025-11-02T10:05:00Z'
		)
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Query the file
	var filePath, language, modulePath string
	var lineCount int
	err = db.QueryRow(`
		SELECT file_path, language, module_path, line_count_total
		FROM files
		WHERE file_path = 'internal/storage/schema.go'
	`).Scan(&filePath, &language, &modulePath, &lineCount)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("File: %s\n", filePath)
	fmt.Printf("Language: %s\n", language)
	fmt.Printf("Module: %s\n", modulePath)
	fmt.Printf("Lines: %d\n", lineCount)

	// Output:
	// File: internal/storage/schema.go
	// Language: go
	// Module: internal/storage
	// Lines: 359
}
