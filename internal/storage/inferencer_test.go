package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInferImplementations_EmptyInterface tests that empty interfaces match all structs.
func TestInferImplementations_EmptyInterface(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Create empty interface
	insertType(t, db, Type{
		ID:         "test::EmptyInterface",
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "EmptyInterface",
		Kind:       "interface",
		StartLine:  1,
		EndLine:    1,
		IsExported: true,
	})

	// Create two structs
	insertType(t, db, Type{
		ID:         "test::Struct1",
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Struct1",
		Kind:       "struct",
		StartLine:  3,
		EndLine:    5,
		IsExported: true,
	})
	insertType(t, db, Type{
		ID:         "test::Struct2",
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Struct2",
		Kind:       "struct",
		StartLine:  7,
		EndLine:    9,
		IsExported: true,
	})

	// Run inference
	inferencer := NewInterfaceInferencer(db)
	err := inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify both structs implement the empty interface
	rels := readRelationships(t, db)
	require.Len(t, rels, 2)

	assert.Equal(t, "test::Struct1", rels[0].FromTypeID)
	assert.Equal(t, "test::EmptyInterface", rels[0].ToTypeID)
	assert.Equal(t, "implements", rels[0].RelationshipType)

	assert.Equal(t, "test::Struct2", rels[1].FromTypeID)
	assert.Equal(t, "test::EmptyInterface", rels[1].ToTypeID)
	assert.Equal(t, "implements", rels[1].RelationshipType)
}

// TestInferImplementations_ExactMethodMatch tests exact method signature matching.
func TestInferImplementations_ExactMethodMatch(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Create interface with one method: Read(p []byte) (n int, err error)
	interfaceID := "test::Reader"
	insertType(t, db, Type{
		ID:         interfaceID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Reader",
		Kind:       "interface",
		StartLine:  1,
		EndLine:    3,
		IsExported: true,
	})
	insertTypeField(t, db, TypeField{
		ID:          "test::Reader::Read",
		TypeID:      interfaceID,
		Name:        "Read",
		IsMethod:    true,
		ParamCount:  intPtr(1), // p []byte
		ReturnCount: intPtr(2), // n int, err error
	})

	// Create struct with matching method
	structID := "test::FileReader"
	insertType(t, db, Type{
		ID:         structID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "FileReader",
		Kind:       "struct",
		StartLine:  5,
		EndLine:    7,
		IsExported: true,
	})
	insertFunction(t, db, Function{
		ID:               "test::FileReader::Read",
		FilePath:         "test.go",
		ModulePath:       "test",
		Name:             "Read",
		IsMethod:         true,
		ReceiverTypeID:   &structID,
		ReceiverTypeName: stringPtr("FileReader"),
		ParamCount:       1,
		ReturnCount:      2,
	})

	// Run inference
	inferencer := NewInterfaceInferencer(db)
	err := inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify struct implements interface
	rels := readRelationships(t, db)
	require.Len(t, rels, 1)

	assert.Equal(t, structID, rels[0].FromTypeID)
	assert.Equal(t, interfaceID, rels[0].ToTypeID)
	assert.Equal(t, "implements", rels[0].RelationshipType)
}

// TestInferImplementations_MissingMethod tests that structs without all required methods don't match.
func TestInferImplementations_MissingMethod(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Create interface with two methods
	interfaceID := "test::TwoMethods"
	insertType(t, db, Type{
		ID:         interfaceID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "TwoMethods",
		Kind:       "interface",
		StartLine:  1,
		EndLine:    4,
		IsExported: true,
	})
	insertTypeField(t, db, TypeField{
		ID:          "test::TwoMethods::Foo",
		TypeID:      interfaceID,
		Name:        "Foo",
		IsMethod:    true,
		ParamCount:  intPtr(0),
		ReturnCount: intPtr(0),
	})
	insertTypeField(t, db, TypeField{
		ID:          "test::TwoMethods::Bar",
		TypeID:      interfaceID,
		Name:        "Bar",
		IsMethod:    true,
		ParamCount:  intPtr(0),
		ReturnCount: intPtr(0),
	})

	// Create struct with only one method
	structID := "test::Incomplete"
	insertType(t, db, Type{
		ID:         structID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Incomplete",
		Kind:       "struct",
		StartLine:  6,
		EndLine:    8,
		IsExported: true,
	})
	insertFunction(t, db, Function{
		ID:               "test::Incomplete::Foo",
		FilePath:         "test.go",
		ModulePath:       "test",
		Name:             "Foo",
		IsMethod:         true,
		ReceiverTypeID:   &structID,
		ReceiverTypeName: stringPtr("Incomplete"),
		ParamCount:       0,
		ReturnCount:      0,
	})

	// Run inference
	inferencer := NewInterfaceInferencer(db)
	err := inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify NO implementation relationship (missing Bar method)
	rels := readRelationships(t, db)
	assert.Len(t, rels, 0)
}

// TestInferImplementations_ExtraMethodsOnStruct tests that extra methods don't prevent matching.
func TestInferImplementations_ExtraMethodsOnStruct(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Create interface with one method
	interfaceID := "test::SingleMethod"
	insertType(t, db, Type{
		ID:         interfaceID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "SingleMethod",
		Kind:       "interface",
		StartLine:  1,
		EndLine:    3,
		IsExported: true,
	})
	insertTypeField(t, db, TypeField{
		ID:          "test::SingleMethod::Foo",
		TypeID:      interfaceID,
		Name:        "Foo",
		IsMethod:    true,
		ParamCount:  intPtr(0),
		ReturnCount: intPtr(0),
	})

	// Create struct with two methods (one extra)
	structID := "test::Extended"
	insertType(t, db, Type{
		ID:         structID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Extended",
		Kind:       "struct",
		StartLine:  5,
		EndLine:    7,
		IsExported: true,
	})
	insertFunction(t, db, Function{
		ID:               "test::Extended::Foo",
		FilePath:         "test.go",
		ModulePath:       "test",
		Name:             "Foo",
		IsMethod:         true,
		ReceiverTypeID:   &structID,
		ReceiverTypeName: stringPtr("Extended"),
		ParamCount:       0,
		ReturnCount:      0,
	})
	insertFunction(t, db, Function{
		ID:               "test::Extended::Bar",
		FilePath:         "test.go",
		ModulePath:       "test",
		Name:             "Bar",
		IsMethod:         true,
		ReceiverTypeID:   &structID,
		ReceiverTypeName: stringPtr("Extended"),
		ParamCount:       1,
		ReturnCount:      1,
	})

	// Run inference
	inferencer := NewInterfaceInferencer(db)
	err := inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify struct implements interface (extra method is OK)
	rels := readRelationships(t, db)
	require.Len(t, rels, 1)

	assert.Equal(t, structID, rels[0].FromTypeID)
	assert.Equal(t, interfaceID, rels[0].ToTypeID)
	assert.Equal(t, "implements", rels[0].RelationshipType)
}

// TestInferImplementations_EmbeddedFields tests embedded field detection.
func TestInferImplementations_EmbeddedFields(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Create two types
	handlerID := "test::Handler"
	serverID := "test::Server"

	insertType(t, db, Type{
		ID:         handlerID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Handler",
		Kind:       "struct",
		StartLine:  1,
		EndLine:    3,
		IsExported: true,
	})
	insertType(t, db, Type{
		ID:         serverID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Server",
		Kind:       "struct",
		StartLine:  5,
		EndLine:    8,
		IsExported: true,
	})

	// Create embedded field (empty name)
	insertTypeField(t, db, TypeField{
		ID:        "test::Server::Handler",
		TypeID:    serverID,
		Name:      "", // Empty name indicates embedded field
		FieldType: "Handler",
		IsMethod:  false,
	})

	// Run inference
	inferencer := NewInterfaceInferencer(db)
	err := inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify embeds relationship
	rels := readRelationships(t, db)
	require.Len(t, rels, 1)

	assert.Equal(t, serverID, rels[0].FromTypeID)
	assert.Equal(t, handlerID, rels[0].ToTypeID)
	assert.Equal(t, "embeds", rels[0].RelationshipType)
}

// TestInferImplementations_BulkWriteTransaction tests transactional writes.
func TestInferImplementations_BulkWriteTransaction(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Create interface
	interfaceID := "test::Interface"
	insertType(t, db, Type{
		ID:         interfaceID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Interface",
		Kind:       "interface",
		StartLine:  1,
		EndLine:    3,
		IsExported: true,
	})
	insertTypeField(t, db, TypeField{
		ID:          "test::Interface::Method",
		TypeID:      interfaceID,
		Name:        "Method",
		IsMethod:    true,
		ParamCount:  intPtr(0),
		ReturnCount: intPtr(0),
	})

	// Create first struct implementing interface
	struct1ID := "test::Struct1"
	insertType(t, db, Type{
		ID:         struct1ID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Struct1",
		Kind:       "struct",
		StartLine:  5,
		EndLine:    7,
		IsExported: true,
	})
	insertFunction(t, db, Function{
		ID:               "test::Struct1::Method",
		FilePath:         "test.go",
		ModulePath:       "test",
		Name:             "Method",
		IsMethod:         true,
		ReceiverTypeID:   &struct1ID,
		ReceiverTypeName: stringPtr("Struct1"),
		ParamCount:       0,
		ReturnCount:      0,
	})

	// Run first inference
	inferencer := NewInterfaceInferencer(db)
	err := inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify one relationship
	rels := readRelationships(t, db)
	require.Len(t, rels, 1)

	// Add second struct
	struct2ID := "test::Struct2"
	insertType(t, db, Type{
		ID:         struct2ID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Struct2",
		Kind:       "struct",
		StartLine:  9,
		EndLine:    11,
		IsExported: true,
	})
	insertFunction(t, db, Function{
		ID:               "test::Struct2::Method",
		FilePath:         "test.go",
		ModulePath:       "test",
		Name:             "Method",
		IsMethod:         true,
		ReceiverTypeID:   &struct2ID,
		ReceiverTypeName: stringPtr("Struct2"),
		ParamCount:       0,
		ReturnCount:      0,
	})

	// Run second inference
	err = inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify old relationships cleared and new ones written (2 total)
	rels = readRelationships(t, db)
	require.Len(t, rels, 2)

	// Both structs should implement interface
	fromIDs := []string{rels[0].FromTypeID, rels[1].FromTypeID}
	assert.Contains(t, fromIDs, struct1ID)
	assert.Contains(t, fromIDs, struct2ID)
	assert.Equal(t, interfaceID, rels[0].ToTypeID)
	assert.Equal(t, interfaceID, rels[1].ToTypeID)
}

// TestInferImplementations_SignatureMismatch tests method signature mismatches.
func TestInferImplementations_SignatureMismatch(t *testing.T) {
	t.Parallel()

	db := setupTestDB(t)
	defer db.Close()

	// Create interface with Read(p []byte) (n int, err error)
	interfaceID := "test::Reader"
	insertType(t, db, Type{
		ID:         interfaceID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "Reader",
		Kind:       "interface",
		StartLine:  1,
		EndLine:    3,
		IsExported: true,
	})
	insertTypeField(t, db, TypeField{
		ID:          "test::Reader::Read",
		TypeID:      interfaceID,
		Name:        "Read",
		IsMethod:    true,
		ParamCount:  intPtr(1),
		ReturnCount: intPtr(2),
	})

	// Create struct with Read() error (different signature)
	structID := "test::BadReader"
	insertType(t, db, Type{
		ID:         structID,
		FilePath:   "test.go",
		ModulePath: "test",
		Name:       "BadReader",
		Kind:       "struct",
		StartLine:  5,
		EndLine:    7,
		IsExported: true,
	})
	insertFunction(t, db, Function{
		ID:               "test::BadReader::Read",
		FilePath:         "test.go",
		ModulePath:       "test",
		Name:             "Read",
		IsMethod:         true,
		ReceiverTypeID:   &structID,
		ReceiverTypeName: stringPtr("BadReader"),
		ParamCount:       0, // DIFFERENT: no params
		ReturnCount:      1, // DIFFERENT: one return
	})

	// Run inference
	inferencer := NewInterfaceInferencer(db)
	err := inferencer.InferImplementations(context.Background())
	require.NoError(t, err)

	// Verify NO implementation relationship (signature mismatch)
	rels := readRelationships(t, db)
	assert.Len(t, rels, 0)
}

// Helper functions for test setup

func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db := NewTestDB(t)

	// Insert test.go file to satisfy foreign key constraints
	_, err := db.Exec(`
		INSERT INTO files (
			file_path, module_path, language, is_test,
			line_count_total, line_count_code, line_count_comment, line_count_blank,
			size_bytes, file_hash, last_modified, indexed_at
		)
		VALUES ('test.go', 'test', 'go', 0, 100, 80, 10, 10, 1024, 'test-hash', '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')
	`)
	require.NoError(t, err)

	return db
}

func insertType(t *testing.T, db *sql.DB, typ Type) {
	t.Helper()

	_, err := db.Exec(`
		INSERT INTO types
		(type_id, file_path, module_path, name, kind, start_line, end_line, is_exported, field_count, method_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, typ.ID, typ.FilePath, typ.ModulePath, typ.Name, typ.Kind,
		typ.StartLine, typ.EndLine, typ.IsExported, typ.FieldCount, typ.MethodCount)
	require.NoError(t, err)
}

func insertTypeField(t *testing.T, db *sql.DB, field TypeField) {
	t.Helper()

	_, err := db.Exec(`
		INSERT INTO type_fields
		(field_id, type_id, name, field_type, position, is_method, is_exported, param_count, return_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, field.ID, field.TypeID, field.Name, field.FieldType, field.Position,
		field.IsMethod, field.IsExported, field.ParamCount, field.ReturnCount)
	require.NoError(t, err)
}

func insertFunction(t *testing.T, db *sql.DB, fn Function) {
	t.Helper()

	_, err := db.Exec(`
		INSERT INTO functions
		(function_id, file_path, module_path, name, start_line, end_line, line_count,
		 is_exported, is_method, receiver_type_id, receiver_type_name, param_count, return_count)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, fn.ID, fn.FilePath, fn.ModulePath, fn.Name, fn.StartLine, fn.EndLine, fn.LineCount,
		fn.IsExported, fn.IsMethod, fn.ReceiverTypeID, fn.ReceiverTypeName, fn.ParamCount, fn.ReturnCount)
	require.NoError(t, err)
}

func readRelationships(t *testing.T, db *sql.DB) []TypeRelationship {
	t.Helper()

	rows, err := db.Query(`
		SELECT relationship_id, from_type_id, to_type_id, relationship_type, source_file_path, source_line
		FROM type_relationships
		ORDER BY from_type_id, to_type_id
	`)
	require.NoError(t, err)
	defer rows.Close()

	var rels []TypeRelationship
	for rows.Next() {
		var rel TypeRelationship
		err := rows.Scan(&rel.ID, &rel.FromTypeID, &rel.ToTypeID, &rel.RelationshipType, &rel.SourceFilePath, &rel.SourceLine)
		require.NoError(t, err)
		rels = append(rels, rel)
	}
	require.NoError(t, rows.Err())

	return rels
}

// Use strPtr from models_test.go test helpers
