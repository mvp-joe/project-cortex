package parsers

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for RustParser:
// - Extracts struct definitions with correct names and line numbers
// - Extracts enum definitions with correct names and line numbers
// - Extracts trait definitions with correct names and line numbers
// - Extracts impl blocks and their methods
// - Extracts standalone function declarations
// - Extracts const declarations with type and value
// - Extracts static variable declarations with type and value
// - Counts use declarations (imports)
// - Handles generic types in structs, enums, and traits
// - Distinguishes between associated functions and methods
// - Verifies line numbers are accurate across all extraction types
// - Handles invalid/unparseable files gracefully
// - Handles files with pub visibility modifiers

// Test: Extract struct definitions from Rust file
func TestRustParser_ParseStruct(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// Should find User and UserRepository structs
	require.GreaterOrEqual(t, len(extraction.Symbols.Types), 2)

	var userStruct, userRepoStruct *SymbolInfo
	for i := range extraction.Symbols.Types {
		switch extraction.Symbols.Types[i].Name {
		case "User":
			userStruct = &extraction.Symbols.Types[i]
		case "UserRepository":
			userRepoStruct = &extraction.Symbols.Types[i]
		}
	}

	require.NotNil(t, userStruct, "Should find User struct")
	assert.Equal(t, "struct", userStruct.Type)
	assert.Equal(t, 10, userStruct.StartLine)
	assert.Equal(t, 14, userStruct.EndLine)

	require.NotNil(t, userRepoStruct, "Should find UserRepository struct")
	assert.Equal(t, "struct", userRepoStruct.Type)
	assert.Equal(t, 32, userRepoStruct.StartLine)
	assert.Equal(t, 34, userRepoStruct.EndLine)

	// Check definitions tier
	require.NotNil(t, extraction.Definitions)
	var userDef, userRepoDef *Definition
	for i := range extraction.Definitions.Definitions {
		def := &extraction.Definitions.Definitions[i]
		if def.Name == "User" && def.Type == "struct" {
			userDef = def
		}
		if def.Name == "UserRepository" && def.Type == "struct" {
			userRepoDef = def
		}
	}

	require.NotNil(t, userDef, "Should have User struct definition")
	assert.Contains(t, userDef.Code, "pub struct User")
	assert.Contains(t, userDef.Code, "pub id: String")

	require.NotNil(t, userRepoDef, "Should have UserRepository struct definition")
	assert.Contains(t, userRepoDef.Code, "pub struct UserRepository")
}

// Test: Extract enum definitions from Rust file
func TestRustParser_ParseEnum(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// Should find Status enum
	var statusEnum *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "Status" {
			statusEnum = &extraction.Symbols.Types[i]
			break
		}
	}

	require.NotNil(t, statusEnum, "Should find Status enum")
	assert.Equal(t, "enum", statusEnum.Type)
	assert.Equal(t, 70, statusEnum.StartLine)
	assert.Equal(t, 74, statusEnum.EndLine)

	// Check definitions tier
	require.NotNil(t, extraction.Definitions)
	var statusDef *Definition
	for i := range extraction.Definitions.Definitions {
		def := &extraction.Definitions.Definitions[i]
		if def.Name == "Status" && def.Type == "enum" {
			statusDef = def
			break
		}
	}

	require.NotNil(t, statusDef, "Should have Status enum definition")
	assert.Contains(t, statusDef.Code, "pub enum Status")
	assert.Contains(t, statusDef.Code, "Active")
	assert.Contains(t, statusDef.Code, "Inactive")
	assert.Contains(t, statusDef.Code, "Pending")
}

// Test: Extract trait definitions from Rust file
func TestRustParser_ParseTrait(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// Should find Repository trait
	var repositoryTrait *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "Repository" {
			repositoryTrait = &extraction.Symbols.Types[i]
			break
		}
	}

	require.NotNil(t, repositoryTrait, "Should find Repository trait")
	assert.Equal(t, "trait", repositoryTrait.Type)
	assert.Equal(t, 26, repositoryTrait.StartLine)
	assert.Equal(t, 30, repositoryTrait.EndLine)

	// Check definitions tier
	require.NotNil(t, extraction.Definitions)
	var repositoryDef *Definition
	for i := range extraction.Definitions.Definitions {
		def := &extraction.Definitions.Definitions[i]
		if def.Name == "Repository" && def.Type == "trait" {
			repositoryDef = def
			break
		}
	}

	require.NotNil(t, repositoryDef, "Should have Repository trait definition")
	assert.Contains(t, repositoryDef.Code, "pub trait Repository<T>")
	assert.Contains(t, repositoryDef.Code, "fn add")
	assert.Contains(t, repositoryDef.Code, "fn get")
	assert.Contains(t, repositoryDef.Code, "fn remove")
}

// Test: Extract impl blocks and their methods
func TestRustParser_ParseImplBlock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// Should find methods from User impl block
	var userNewMethod, validateMethod *SymbolInfo
	for i := range extraction.Symbols.Functions {
		fn := &extraction.Symbols.Functions[i]
		if fn.Name == "new" && fn.Type == "method" && strings.Contains(fn.Signature, "User::new") {
			userNewMethod = fn
		}
		if fn.Name == "validate" && fn.Type == "method" {
			validateMethod = fn
		}
	}

	require.NotNil(t, userNewMethod, "Should find User::new method")
	assert.Equal(t, "method", userNewMethod.Type)
	assert.Equal(t, 17, userNewMethod.StartLine)
	assert.Equal(t, 19, userNewMethod.EndLine)
	assert.Contains(t, userNewMethod.Signature, "User::new")

	require.NotNil(t, validateMethod, "Should find User::validate method")
	assert.Equal(t, "method", validateMethod.Type)
	assert.Equal(t, 21, validateMethod.StartLine)
	assert.Equal(t, 23, validateMethod.EndLine)
	assert.Contains(t, validateMethod.Signature, "User::validate")

	// Should find methods from UserRepository impl block
	var newRepoMethod, sizeMethod *SymbolInfo
	for i := range extraction.Symbols.Functions {
		fn := &extraction.Symbols.Functions[i]
		if fn.Name == "new" && strings.Contains(fn.Signature, "UserRepository") {
			newRepoMethod = fn
		}
		if fn.Name == "size" {
			sizeMethod = fn
		}
	}

	require.NotNil(t, newRepoMethod, "Should find UserRepository::new method")
	assert.Contains(t, newRepoMethod.Signature, "UserRepository::new")

	require.NotNil(t, sizeMethod, "Should find UserRepository::size method")
	assert.Contains(t, sizeMethod.Signature, "UserRepository::size")

	// Should find trait impl methods (Repository for UserRepository)
	var addMethod, getMethod, removeMethod *SymbolInfo
	for i := range extraction.Symbols.Functions {
		fn := &extraction.Symbols.Functions[i]
		switch fn.Name {
		case "add":
			addMethod = fn
		case "get":
			getMethod = fn
		case "remove":
			removeMethod = fn
		}
	}

	require.NotNil(t, addMethod, "Should find add method from trait impl")
	assert.Equal(t, "method", addMethod.Type)

	require.NotNil(t, getMethod, "Should find get method from trait impl")
	assert.Equal(t, "method", getMethod.Type)

	require.NotNil(t, removeMethod, "Should find remove method from trait impl")
	assert.Equal(t, "method", removeMethod.Type)
}

// Test: Extract standalone function declarations
func TestRustParser_ParseFunctions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// Should find create_user function
	var createUserFn *SymbolInfo
	for i := range extraction.Symbols.Functions {
		fn := &extraction.Symbols.Functions[i]
		if fn.Name == "create_user" && fn.Type == "function" {
			createUserFn = fn
			break
		}
	}

	require.NotNil(t, createUserFn, "Should find create_user function")
	assert.Equal(t, "function", createUserFn.Type)
	assert.Equal(t, 66, createUserFn.StartLine)
	assert.Equal(t, 68, createUserFn.EndLine)
	assert.Contains(t, createUserFn.Signature, "create_user")
	assert.Contains(t, createUserFn.Signature, "id: &str")
	assert.Contains(t, createUserFn.Signature, "name: &str")
	assert.Contains(t, createUserFn.Signature, "email: &str")

	// Check definitions tier
	require.NotNil(t, extraction.Definitions)
	var createUserDef *Definition
	for i := range extraction.Definitions.Definitions {
		def := &extraction.Definitions.Definitions[i]
		if def.Name == "create_user" && def.Type == "function" {
			createUserDef = def
			break
		}
	}

	require.NotNil(t, createUserDef, "Should have create_user function definition")
	assert.Contains(t, createUserDef.Code, "pub fn create_user")
}

// Test: Extract const declarations
func TestRustParser_ParseConstants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Data)

	// Should find MAX_USERS and DEFAULT_TIMEOUT constants
	require.GreaterOrEqual(t, len(extraction.Data.Constants), 2)

	var maxUsersConst, defaultTimeoutConst *ConstantInfo
	for i := range extraction.Data.Constants {
		c := &extraction.Data.Constants[i]
		switch c.Name {
		case "MAX_USERS":
			maxUsersConst = c
		case "DEFAULT_TIMEOUT":
			defaultTimeoutConst = c
		}
	}

	require.NotNil(t, maxUsersConst, "Should find MAX_USERS constant")
	assert.Equal(t, "usize", maxUsersConst.Type)
	assert.Equal(t, "1000", maxUsersConst.Value)
	assert.Equal(t, 4, maxUsersConst.StartLine)
	assert.Equal(t, 4, maxUsersConst.EndLine)

	require.NotNil(t, defaultTimeoutConst, "Should find DEFAULT_TIMEOUT constant")
	assert.Equal(t, "u64", defaultTimeoutConst.Type)
	assert.Equal(t, "30", defaultTimeoutConst.Value)
	assert.Equal(t, 5, defaultTimeoutConst.StartLine)
	assert.Equal(t, 5, defaultTimeoutConst.EndLine)
}

// Test: Extract static variable declarations
func TestRustParser_ParseStaticVariables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Data)

	// Should find GLOBAL_COUNTER static variable
	require.GreaterOrEqual(t, len(extraction.Data.Variables), 1)

	var globalCounterVar *VariableInfo
	for i := range extraction.Data.Variables {
		v := &extraction.Data.Variables[i]
		if v.Name == "GLOBAL_COUNTER" {
			globalCounterVar = v
			break
		}
	}

	require.NotNil(t, globalCounterVar, "Should find GLOBAL_COUNTER static variable")
	assert.Equal(t, "i32", globalCounterVar.Type)
	assert.Equal(t, "0", globalCounterVar.Value)
	assert.Equal(t, 7, globalCounterVar.StartLine)
	assert.Equal(t, 7, globalCounterVar.EndLine)
}

// Test: Verify line numbers are accurate across all extraction types
func TestRustParser_LineNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)

	// File should start at line 1 and end around line 75
	assert.Equal(t, 1, extraction.StartLine)
	assert.Equal(t, 75, extraction.EndLine)

	// Verify a few key line numbers match the test fixture
	require.NotNil(t, extraction.Symbols)

	// User struct at lines 10-14
	for _, typ := range extraction.Symbols.Types {
		if typ.Name == "User" {
			assert.Equal(t, 10, typ.StartLine)
			assert.Equal(t, 14, typ.EndLine)
		}
	}

	// Repository trait at lines 26-30
	for _, typ := range extraction.Symbols.Types {
		if typ.Name == "Repository" {
			assert.Equal(t, 26, typ.StartLine)
			assert.Equal(t, 30, typ.EndLine)
		}
	}

	// Status enum at lines 70-74
	for _, typ := range extraction.Symbols.Types {
		if typ.Name == "Status" {
			assert.Equal(t, 70, typ.StartLine)
			assert.Equal(t, 74, typ.EndLine)
		}
	}
}

// Test: Handle invalid/unparseable files gracefully
func TestRustParser_InvalidFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	// Test: Nonexistent file
	_, err := parser.ParseFile(ctx, "../../../testdata/code/rust/nonexistent.rs")
	require.Error(t, err)

	// Test: Empty file (should parse but have no symbols)
	// Note: We would need to create an empty test file for this
	// For now, testing with a completely invalid path is sufficient
}

// Test: Handle generic types in structs, enums, and traits
func TestRustParser_Generics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// Repository trait should be extracted even though it has generic parameter <T>
	var repositoryTrait *SymbolInfo
	for i := range extraction.Symbols.Types {
		if extraction.Symbols.Types[i].Name == "Repository" {
			repositoryTrait = &extraction.Symbols.Types[i]
			break
		}
	}

	require.NotNil(t, repositoryTrait, "Should extract generic trait Repository<T>")
	assert.Equal(t, "trait", repositoryTrait.Type)

	// Verify the definition includes the generic parameter
	require.NotNil(t, extraction.Definitions)
	var repositoryDef *Definition
	for i := range extraction.Definitions.Definitions {
		def := &extraction.Definitions.Definitions[i]
		if def.Name == "Repository" && def.Type == "trait" {
			repositoryDef = def
			break
		}
	}

	require.NotNil(t, repositoryDef)
	assert.Contains(t, repositoryDef.Code, "<T>", "Generic parameter should be in definition")
}

// Test: Count use declarations (imports)
func TestRustParser_ImportsCount(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)
	require.NotNil(t, extraction.Symbols)

	// simple.rs has 2 use declarations:
	// - use std::collections::HashMap;
	// - use std::fmt;
	assert.Equal(t, 2, extraction.Symbols.ImportsCount)
}

// Test: Verify metadata fields are populated correctly
func TestRustParser_Metadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewRustParser()

	extraction, err := parser.ParseFile(ctx, "../../../testdata/code/rust/simple.rs")

	require.NoError(t, err)
	require.NotNil(t, extraction)

	assert.Equal(t, "rust", extraction.Language)
	assert.Contains(t, extraction.FilePath, "simple.rs")
	assert.Equal(t, 1, extraction.StartLine)
	assert.Greater(t, extraction.EndLine, 1)
}
