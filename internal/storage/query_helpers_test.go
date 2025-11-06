package storage

// Test Plan for Query Helpers (TDD Workflow):
//
// 1. LoadInterfacesWithMethods:
//    - Loads interfaces with multiple methods
//    - Preserves method order by position
//    - Handles empty interface (no methods)
//    - Returns empty slice when no interfaces exist
//    - Groups methods correctly by type_id
//
// 2. LoadStructsWithMethods:
//    - Loads structs with methods from functions table
//    - Groups methods by receiver_type_id
//    - Handles structs with no methods
//    - Excludes non-struct types (interfaces)
//    - Returns empty slice when no structs exist
//
// 3. LoadEmbeddedFields:
//    - Finds embedded fields by empty name
//    - Excludes interface methods (is_method = false)
//    - Converts to TypeModelRelationship with "embeds"
//    - Resolves same-package embeds correctly
//    - Handles cross-package embeds
//    - Returns empty slice when no embedded fields
//
// 4. BulkInsertRelationships:
//    - Inserts multiple relationships in single transaction
//    - Uses prepared statement (performance)
//    - Generates unique relationship IDs
//    - Handles empty input gracefully
//    - Validates foreign key constraints
//
// 5. Scanning helpers:
//    - scanTypesWithFields groups correctly
//    - scanTypesWithMethods groups correctly
//    - scanEmbedRelationships converts correctly
//    - All handle NULL values from LEFT JOIN
//
// Edge cases:
// - NULL values in optional fields
// - Multiple types with same name in different modules
// - Complex embedded type names (e.g., "pkg.Type")

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadInterfacesWithMethods(t *testing.T) {
	t.Parallel()

	t.Run("loads interfaces with multiple methods", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert test data
		setupInterfaceTestData(t, db)

		// Test: Load interfaces
		interfaces, err := LoadInterfacesWithMethods(db)
		require.NoError(t, err)
		require.Len(t, interfaces, 2)

		// Verify Provider interface
		provider := findTypeByName(interfaces, "Provider")
		require.NotNil(t, provider)
		assert.Equal(t, "interface", provider.Kind)
		assert.Equal(t, "internal/embed/provider.go", provider.FilePath)
		assert.Len(t, provider.Fields, 3)

		// Verify method order and signatures
		embedMethod := findFieldByName(provider.Fields, "Embed")
		require.NotNil(t, embedMethod)
		assert.Equal(t, intPtr(2), embedMethod.ParamCount)
		assert.Equal(t, intPtr(2), embedMethod.ReturnCount)
		assert.Equal(t, 0, embedMethod.Position)

		dimensionsMethod := findFieldByName(provider.Fields, "Dimensions")
		require.NotNil(t, dimensionsMethod)
		assert.Equal(t, intPtr(0), dimensionsMethod.ParamCount)
		assert.Equal(t, intPtr(1), dimensionsMethod.ReturnCount)
		assert.Equal(t, 1, dimensionsMethod.Position)
	})

	t.Run("handles empty interface", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert interface with no methods
		_, err := db.Exec(`
			INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
			VALUES ('test.go', 'go', 'test', 0, 'hash123', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
		`)
		require.NoError(t, err)

		_, err = db.Exec(`
			INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
			VALUES ('test::Empty', 'test.go', 'test', 'Empty', 'interface', 10, 11, 1, 0, 0)
		`)
		require.NoError(t, err)

		// Test: Load should return interface with empty Fields slice
		interfaces, err := LoadInterfacesWithMethods(db)
		require.NoError(t, err)
		require.Len(t, interfaces, 1)
		assert.Equal(t, "Empty", interfaces[0].Name)
		assert.Empty(t, interfaces[0].Fields)
	})

	t.Run("returns empty slice when no interfaces", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		interfaces, err := LoadInterfacesWithMethods(db)
		require.NoError(t, err)
		assert.Empty(t, interfaces)
	})

	t.Run("groups methods correctly by type_id", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		setupInterfaceTestData(t, db)

		interfaces, err := LoadInterfacesWithMethods(db)
		require.NoError(t, err)

		// Verify Searcher interface has exactly 1 method
		searcher := findTypeByName(interfaces, "Searcher")
		require.NotNil(t, searcher)
		assert.Len(t, searcher.Fields, 1)
		assert.Equal(t, "Search", searcher.Fields[0].Name)
	})
}

func TestLoadStructsWithMethods(t *testing.T) {
	t.Parallel()

	t.Run("loads structs with methods from functions table", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert test data
		setupStructTestData(t, db)

		// Test: Load structs
		structs, err := LoadStructsWithMethods(db)
		require.NoError(t, err)
		require.Len(t, structs, 1)

		// Verify localProvider struct
		localProvider := structs[0]
		assert.Equal(t, "localProvider", localProvider.Name)
		assert.Equal(t, "struct", localProvider.Kind)
		assert.Len(t, localProvider.Fields, 3)

		// Verify methods are populated from functions table
		embedMethod := findFieldByName(localProvider.Fields, "Embed")
		require.NotNil(t, embedMethod)
		assert.Equal(t, intPtr(2), embedMethod.ParamCount)
		assert.Equal(t, intPtr(2), embedMethod.ReturnCount)
	})

	t.Run("handles structs with no methods", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert struct with no methods
		_, err := db.Exec(`
			INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
			VALUES ('test.go', 'go', 'test', 0, 'hash123', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
		`)
		require.NoError(t, err)

		_, err = db.Exec(`
			INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
			VALUES ('test::Config', 'test.go', 'test', 'Config', 'struct', 10, 15, 1, 2, 0)
		`)
		require.NoError(t, err)

		// Test: Load should return struct with empty Fields slice
		structs, err := LoadStructsWithMethods(db)
		require.NoError(t, err)
		require.Len(t, structs, 1)
		assert.Equal(t, "Config", structs[0].Name)
		assert.Empty(t, structs[0].Fields)
	})

	t.Run("excludes non-struct types", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert interface (should be excluded)
		setupInterfaceTestData(t, db)

		// Test: Should return empty (no structs)
		structs, err := LoadStructsWithMethods(db)
		require.NoError(t, err)
		assert.Empty(t, structs)
	})

	t.Run("returns empty slice when no structs exist", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		structs, err := LoadStructsWithMethods(db)
		require.NoError(t, err)
		assert.Empty(t, structs)
	})
}

func TestLoadEmbeddedFields(t *testing.T) {
	t.Parallel()

	t.Run("finds embedded field relationships", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert struct with embedded field
		setupEmbeddedFieldTestData(t, db)

		// Test: Load embedded relationships
		embeds, err := LoadEmbeddedFields(db)
		require.NoError(t, err)
		require.Len(t, embeds, 1)

		embed := embeds[0]
		assert.Equal(t, "test::Server", embed.FromTypeID)
		assert.Equal(t, "test::http.Handler", embed.ToTypeID)
		assert.Equal(t, "embeds", embed.RelationshipType)
		assert.Equal(t, "server.go", embed.SourceFilePath)
		assert.Equal(t, 10, embed.SourceLine)
	})

	t.Run("excludes interface methods", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert type with method (empty name but is_method = 1)
		_, err := db.Exec(`
			INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
			VALUES ('test.go', 'go', 'test', 0, 'hash123', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
		`)
		require.NoError(t, err)

		_, err = db.Exec(`
			INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
			VALUES ('test::Iface', 'test.go', 'test', 'Iface', 'interface', 10, 15, 1, 0, 1)
		`)
		require.NoError(t, err)

		_, err = db.Exec(`
			INSERT INTO type_fields (field_id, type_id, name, field_type, position, is_method, is_exported)
			VALUES ('field1', 'test::Iface', '', 'void', 0, 1, 1)
		`)
		require.NoError(t, err)

		// Test: Should not return method as embedded field
		embeds, err := LoadEmbeddedFields(db)
		require.NoError(t, err)
		assert.Empty(t, embeds)
	})

	t.Run("returns empty slice when no embedded fields", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		embeds, err := LoadEmbeddedFields(db)
		require.NoError(t, err)
		assert.Empty(t, embeds)
	})

	t.Run("handles cross-package embeds with double colon", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert struct with cross-package embedded field (fully qualified)
		_, err := db.Exec(`
			INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
			VALUES ('server.go', 'go', 'test', 0, 'hash789', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
		`)
		require.NoError(t, err)

		_, err = db.Exec(`
			INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
			VALUES ('test::Server', 'server.go', 'test', 'Server', 'struct', 10, 20, 1, 1, 0)
		`)
		require.NoError(t, err)

		// field_type contains fully qualified type_id
		_, err = db.Exec(`
			INSERT INTO type_fields (field_id, type_id, name, field_type, position, is_method, is_exported)
			VALUES ('test::Server::embedded', 'test::Server', '', 'external::Handler', 0, 0, 1)
		`)
		require.NoError(t, err)

		embeds, err := LoadEmbeddedFields(db)
		require.NoError(t, err)
		require.Len(t, embeds, 1)
		assert.Equal(t, "external::Handler", embeds[0].ToTypeID)
	})
}

func TestBulkInsertRelationships(t *testing.T) {
	t.Parallel()

	t.Run("inserts multiple relationships", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Insert types for FK constraints
		setupTypesForRelationships(t, db)

		relationships := []TypeRelationship{
			{
				FromTypeID:       "test::Struct1",
				ToTypeID:         "test::Interface1",
				RelationshipType: "implements",
				SourceFilePath:   "test.go",
				SourceLine:       10,
			},
			{
				FromTypeID:       "test::Struct2",
				ToTypeID:         "test::Interface1",
				RelationshipType: "implements",
				SourceFilePath:   "test.go",
				SourceLine:       20,
			},
			{
				FromTypeID:       "test::Struct1",
				ToTypeID:         "test::Struct2",
				RelationshipType: "embeds",
				SourceFilePath:   "test.go",
				SourceLine:       12,
			},
		}

		// Test: Bulk insert
		tx, err := db.Begin()
		require.NoError(t, err)
		defer tx.Rollback()

		err = BulkInsertRelationships(tx, relationships)
		require.NoError(t, err)

		err = tx.Commit()
		require.NoError(t, err)

		// Verify: Check inserted relationships
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM type_relationships").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		// Verify specific relationship
		var relType string
		err = db.QueryRow(`
			SELECT relationship_type FROM type_relationships
			WHERE from_type_id = 'test::Struct1' AND to_type_id = 'test::Interface1'
		`).Scan(&relType)
		require.NoError(t, err)
		assert.Equal(t, "implements", relType)
	})

	t.Run("generates unique IDs when not provided", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		setupTypesForRelationships(t, db)

		relationships := []TypeRelationship{
			{
				// No ID provided - should be generated
				FromTypeID:       "test::Struct1",
				ToTypeID:         "test::Interface1",
				RelationshipType: "implements",
				SourceFilePath:   "test.go",
				SourceLine:       10,
			},
			{
				// No ID provided - should be generated
				FromTypeID:       "test::Struct2",
				ToTypeID:         "test::Interface1",
				RelationshipType: "implements",
				SourceFilePath:   "test.go",
				SourceLine:       20,
			},
		}

		tx, err := db.Begin()
		require.NoError(t, err)
		defer tx.Rollback()

		err = BulkInsertRelationships(tx, relationships)
		require.NoError(t, err)
		tx.Commit()

		// Verify: All IDs are unique and non-empty
		rows, err := db.Query("SELECT relationship_id FROM type_relationships")
		require.NoError(t, err)
		defer rows.Close()

		ids := make(map[string]bool)
		for rows.Next() {
			var id string
			require.NoError(t, rows.Scan(&id))
			assert.NotEmpty(t, id, "Generated ID should not be empty")
			assert.False(t, ids[id], "Duplicate relationship ID: %s", id)
			ids[id] = true
		}
		assert.Len(t, ids, 2)
	})

	t.Run("uses provided IDs when available", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		setupTypesForRelationships(t, db)

		customID := "custom-relationship-id-12345"
		relationships := []TypeRelationship{
			{
				ID:               customID,
				FromTypeID:       "test::Struct1",
				ToTypeID:         "test::Interface1",
				RelationshipType: "implements",
				SourceFilePath:   "test.go",
				SourceLine:       10,
			},
		}

		tx, err := db.Begin()
		require.NoError(t, err)
		defer tx.Rollback()

		err = BulkInsertRelationships(tx, relationships)
		require.NoError(t, err)
		tx.Commit()

		// Verify: Custom ID was used
		var id string
		err = db.QueryRow("SELECT relationship_id FROM type_relationships").Scan(&id)
		require.NoError(t, err)
		assert.Equal(t, customID, id)
	})

	t.Run("handles empty input", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		tx, err := db.Begin()
		require.NoError(t, err)
		defer tx.Rollback()

		err = BulkInsertRelationships(tx, []TypeRelationship{})
		require.NoError(t, err)

		err = tx.Commit()
		require.NoError(t, err)

		// Verify: No rows inserted
		var count int
		err = db.QueryRow("SELECT COUNT(*) FROM type_relationships").Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("validates foreign key constraints", func(t *testing.T) {
		t.Parallel()
		db := NewTestDB(t)

		// Setup: Only insert file, no types
		_, err := db.Exec(`
			INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
			VALUES ('test.go', 'go', 'test', 0, 'hash999', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
		`)
		require.NoError(t, err)

		relationships := []TypeRelationship{
			{
				FromTypeID:       "nonexistent::Type1",
				ToTypeID:         "nonexistent::Type2",
				RelationshipType: "implements",
				SourceFilePath:   "test.go",
				SourceLine:       10,
			},
		}

		tx, err := db.Begin()
		require.NoError(t, err)
		defer tx.Rollback()

		// Test: Should fail due to FK constraint (types don't exist)
		err = BulkInsertRelationships(tx, relationships)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "insert relationship")
	})
}

func TestQueryHelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("resolveEmbeddedTypeID with same-package embed", func(t *testing.T) {
		result := resolveEmbeddedTypeID("test::Server", "http.Handler")
		assert.Equal(t, "test::http.Handler", result)
	})

	t.Run("resolveEmbeddedTypeID with cross-package embed", func(t *testing.T) {
		result := resolveEmbeddedTypeID("test::Server", "external::Handler")
		assert.Equal(t, "external::Handler", result)
	})

	t.Run("extractModulePathFromTypeID from type_id", func(t *testing.T) {
		assert.Equal(t, "test", extractModulePathFromTypeID("test::Server"))
		assert.Equal(t, "internal/embed", extractModulePathFromTypeID("internal/embed::Provider"))
		assert.Equal(t, "", extractModulePathFromTypeID("NoDoubleColon"))
	})

	t.Run("containsColonColon detects delimiter", func(t *testing.T) {
		assert.True(t, containsColonColon("test::Type"))
		assert.True(t, containsColonColon("a::b::c"))
		assert.False(t, containsColonColon("NoDelimiter"))
		assert.False(t, containsColonColon("Single:Colon"))
	})
}

// Test helpers

func setupInterfaceTestData(t *testing.T, db *sql.DB) {
	t.Helper()

	// Insert file
	_, err := db.Exec(`
		INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
		VALUES ('internal/embed/provider.go', 'go', 'internal/embed', 0, 'hash123', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
	`)
	require.NoError(t, err)

	// Insert Provider interface
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
		VALUES ('embed::Provider', 'internal/embed/provider.go', 'internal/embed', 'Provider', 'interface', 10, 20, 1, 0, 3)
	`)
	require.NoError(t, err)

	// Insert interface methods
	methods := []struct {
		name        string
		paramCount  int
		returnCount int
		position    int
	}{
		{"Embed", 2, 2, 0},
		{"Dimensions", 0, 1, 1},
		{"Close", 0, 1, 2},
	}

	for _, m := range methods {
		_, err = db.Exec(`
			INSERT INTO type_fields (field_id, type_id, name, field_type, position, is_method, is_exported, param_count, return_count)
			VALUES (?, 'embed::Provider', ?, 'method', ?, 1, 1, ?, ?)
		`, "embed::Provider::"+m.name, m.name, m.position, m.paramCount, m.returnCount)
		require.NoError(t, err)
	}

	// Insert another interface for testing multiple interfaces
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
		VALUES ('embed::Searcher', 'internal/embed/provider.go', 'internal/embed', 'Searcher', 'interface', 25, 30, 1, 0, 1)
	`)
	require.NoError(t, err)

	_, err = db.Exec(`
		INSERT INTO type_fields (field_id, type_id, name, field_type, position, is_method, is_exported, param_count, return_count)
		VALUES ('embed::Searcher::Search', 'embed::Searcher', 'Search', 'method', 0, 1, 1, 1, 2)
	`)
	require.NoError(t, err)
}

func setupStructTestData(t *testing.T, db *sql.DB) {
	t.Helper()

	// Insert file
	_, err := db.Exec(`
		INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
		VALUES ('internal/embed/local.go', 'go', 'internal/embed', 0, 'hash456', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
	`)
	require.NoError(t, err)

	// Insert struct type
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
		VALUES ('embed::localProvider', 'internal/embed/local.go', 'internal/embed', 'localProvider', 'struct', 10, 20, 0, 2, 3)
	`)
	require.NoError(t, err)

	// Insert methods as functions with receiver_type_id
	methods := []struct {
		name        string
		paramCount  int
		returnCount int
	}{
		{"Embed", 2, 2},
		{"Dimensions", 0, 1},
		{"Close", 0, 1},
	}

	for _, m := range methods {
		receiverID := "embed::localProvider"
		receiverName := "localProvider"
		_, err = db.Exec(`
			INSERT INTO functions (function_id, file_path, module_path, name, start_line, end_line, line_count, is_exported, is_method, receiver_type_id, receiver_type_name, param_count, return_count)
			VALUES (?, 'internal/embed/local.go', 'internal/embed', ?, 25, 30, 5, 1, 1, ?, ?, ?, ?)
		`, "embed::localProvider::"+m.name, m.name, receiverID, receiverName, m.paramCount, m.returnCount)
		require.NoError(t, err)
	}
}

func setupEmbeddedFieldTestData(t *testing.T, db *sql.DB) {
	t.Helper()

	// Insert file
	_, err := db.Exec(`
		INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
		VALUES ('server.go', 'go', 'test', 0, 'hash789', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
	`)
	require.NoError(t, err)

	// Insert struct type
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
		VALUES ('test::Server', 'server.go', 'test', 'Server', 'struct', 10, 20, 1, 2, 0)
	`)
	require.NoError(t, err)

	// Insert embedded type (for FK reference)
	_, err = db.Exec(`
		INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
		VALUES ('test::http.Handler', 'server.go', 'test', 'http.Handler', 'interface', 5, 8, 1, 0, 1)
	`)
	require.NoError(t, err)

	// Insert embedded field (empty name)
	_, err = db.Exec(`
		INSERT INTO type_fields (field_id, type_id, name, field_type, position, is_method, is_exported)
		VALUES ('test::Server::embedded', 'test::Server', '', 'http.Handler', 0, 0, 1)
	`)
	require.NoError(t, err)

	// Insert normal field for comparison
	_, err = db.Exec(`
		INSERT INTO type_fields (field_id, type_id, name, field_type, position, is_method, is_exported)
		VALUES ('test::Server::config', 'test::Server', 'config', 'Config', 1, 0, 0)
	`)
	require.NoError(t, err)
}

func setupTypesForRelationships(t *testing.T, db *sql.DB) {
	t.Helper()

	// Insert file
	_, err := db.Exec(`
		INSERT INTO files (file_path, language, module_path, is_test, file_hash, last_modified, indexed_at)
		VALUES ('test.go', 'go', 'test', 0, 'hash999', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
	`)
	require.NoError(t, err)

	// Insert types
	types := []string{"Interface1", "Struct1", "Struct2"}
	for _, name := range types {
		kind := "interface"
		if name != "Interface1" {
			kind = "struct"
		}
		_, err = db.Exec(`
			INSERT INTO types (type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
			VALUES (?, 'test.go', 'test', ?, ?, 10, 20, 1, 0, 0)
		`, "test::"+name, name, kind)
		require.NoError(t, err)
	}
}

func findTypeByName(types []Type, name string) *Type {
	for i := range types {
		if types[i].Name == name {
			return &types[i]
		}
	}
	return nil
}

func findFieldByName(fields []TypeField, name string) *TypeField {
	for i := range fields {
		if fields[i].Name == name {
			return &fields[i]
		}
	}
	return nil
}
