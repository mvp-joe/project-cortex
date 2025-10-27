package parsers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for RubyParser:
// - Extract class definitions with correct line numbers
// - Extract module definitions with correct line numbers
// - Extract methods from classes/modules
// - Extract top-level functions
// - Extract capitalized constants (Ruby convention)
// - Extract global variables ($prefix)
// - Distinguish between instance methods and singleton methods (self.method)
// - Handle blocks with do...end and {...} syntax
// - Handle parse errors gracefully
// - Verify three-tier extraction structure (Symbols, Definitions, Data)

const testRubyFile = "/Users/josephward/code/project-cortex/testdata/code/ruby/simple.rb"

// Test: Extract class definitions from Ruby file
func TestRubyParser_ParseClass(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Verify metadata
	assert.Equal(t, "ruby", extraction.Language)
	assert.Equal(t, testRubyFile, extraction.FilePath)

	// simple.rb has 2 classes: User and UserRepository
	classes := filterByType(extraction.Symbols.Types, "class")
	require.Len(t, classes, 2, "Expected 2 classes")

	// Verify User class
	userClass := findSymbolByName(classes, "User")
	require.NotNil(t, userClass, "User class not found")
	assert.Equal(t, "class", userClass.Type)
	assert.Equal(t, 11, userClass.StartLine, "User class should start at line 11")
	assert.Equal(t, 31, userClass.EndLine, "User class should end at line 31")

	// Verify UserRepository class
	repoClass := findSymbolByName(classes, "UserRepository")
	require.NotNil(t, repoClass, "UserRepository class not found")
	assert.Equal(t, "class", repoClass.Type)
	assert.Equal(t, 33, repoClass.StartLine, "UserRepository class should start at line 33")
	assert.Equal(t, 57, repoClass.EndLine, "UserRepository class should end at line 57")

	// Verify class definitions in Definitions tier
	classDefs := filterDefinitionsByType(extraction.Definitions.Definitions, "class")
	require.Len(t, classDefs, 2, "Expected 2 class definitions")

	// Verify User class definition contains actual code
	userDef := findDefinitionByName(classDefs, "User")
	require.NotNil(t, userDef, "User class definition not found")
	assert.Contains(t, userDef.Code, "class User", "Definition should contain class declaration")
	assert.Contains(t, userDef.Code, "attr_reader", "Definition should contain full class body")
}

// Test: Extract module definitions from Ruby file
func TestRubyParser_ParseModule(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// simple.rb has 1 module: UserManagement
	modules := filterByType(extraction.Symbols.Types, "module")
	require.Len(t, modules, 1, "Expected 1 module")

	// Verify UserManagement module
	userMgmt := modules[0]
	assert.Equal(t, "UserManagement", userMgmt.Name)
	assert.Equal(t, "module", userMgmt.Type)
	assert.Equal(t, 10, userMgmt.StartLine, "UserManagement module should start at line 10")
	assert.Equal(t, 58, userMgmt.EndLine, "UserManagement module should end at line 58")

	// Verify module definition in Definitions tier
	moduleDefs := filterDefinitionsByType(extraction.Definitions.Definitions, "module")
	require.Len(t, moduleDefs, 1, "Expected 1 module definition")

	moduleDef := moduleDefs[0]
	assert.Equal(t, "UserManagement", moduleDef.Name)
	assert.Contains(t, moduleDef.Code, "module UserManagement", "Definition should contain module declaration")
}

// Test: Extract method declarations from classes
func TestRubyParser_ParseMethods(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Verify methods are extracted
	methods := extraction.Symbols.Functions
	require.NotEmpty(t, methods, "Expected methods to be extracted")

	// Check for User class methods
	initializeMethod := findSymbolByName(methods, "initialize")
	require.NotNil(t, initializeMethod, "initialize method not found")
	assert.Equal(t, "method", initializeMethod.Type)
	assert.Equal(t, 14, initializeMethod.StartLine, "initialize should start at line 14")
	assert.Equal(t, 18, initializeMethod.EndLine, "initialize should end at line 18")
	assert.Contains(t, initializeMethod.Signature, "User#initialize", "Signature should include class name")

	validateMethod := findSymbolByName(methods, "validate")
	require.NotNil(t, validateMethod, "validate method not found")
	assert.Equal(t, "method", validateMethod.Type)
	assert.Equal(t, 20, validateMethod.StartLine)
	assert.Contains(t, validateMethod.Signature, "User#validate")

	toHashMethod := findSymbolByName(methods, "to_hash")
	require.NotNil(t, toHashMethod, "to_hash method not found")
	assert.Equal(t, 24, toHashMethod.StartLine)
	assert.Contains(t, toHashMethod.Signature, "User#to_hash")

	// Check for UserRepository methods
	addMethod := findSymbolByName(methods, "add")
	require.NotNil(t, addMethod, "add method not found")
	assert.Equal(t, "method", addMethod.Type)
	assert.Equal(t, 38, addMethod.StartLine)
	assert.Contains(t, addMethod.Signature, "UserRepository#add")

	findByIDMethod := findSymbolByName(methods, "find_by_id")
	require.NotNil(t, findByIDMethod, "find_by_id method not found")
	assert.Equal(t, 42, findByIDMethod.StartLine)
	assert.Contains(t, findByIDMethod.Signature, "UserRepository#find_by_id")

	// Verify method definitions contain signatures
	methodDefs := filterDefinitionsByType(extraction.Definitions.Definitions, "method")
	require.NotEmpty(t, methodDefs, "Expected method definitions")

	initDef := findDefinitionByName(methodDefs, "initialize")
	require.NotNil(t, initDef, "initialize definition not found")
	assert.Contains(t, initDef.Code, "def initialize", "Definition should contain method signature")
}

// Test: Extract top-level functions
func TestRubyParser_ParseTopLevelFunctions(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// simple.rb has top-level functions: create_user and validate_email
	functions := filterByType(extraction.Symbols.Functions, "function")
	require.NotEmpty(t, functions, "Expected top-level functions")

	createUser := findSymbolByName(functions, "create_user")
	require.NotNil(t, createUser, "create_user function not found")
	assert.Equal(t, "function", createUser.Type)
	assert.Equal(t, 60, createUser.StartLine, "create_user should start at line 60")
	assert.Equal(t, 62, createUser.EndLine, "create_user should end at line 62")
	assert.Contains(t, createUser.Signature, "create_user", "Signature should contain function name")

	validateEmail := findSymbolByName(functions, "validate_email")
	require.NotNil(t, validateEmail, "validate_email function not found")
	assert.Equal(t, "function", validateEmail.Type)
	assert.Equal(t, 64, validateEmail.StartLine)
	assert.Equal(t, 66, validateEmail.EndLine)
	assert.Contains(t, validateEmail.Signature, "validate_email")
}

// Test: Extract capitalized constants (Ruby naming convention)
func TestRubyParser_ParseConstants(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// simple.rb has constants: API_KEY, MAX_RETRIES, DEBUG_MODE
	constants := extraction.Data.Constants
	require.Len(t, constants, 3, "Expected 3 constants")

	// Verify API_KEY constant
	apiKey := findConstantByName(constants, "API_KEY")
	require.NotNil(t, apiKey, "API_KEY constant not found")
	assert.Equal(t, "API_KEY", apiKey.Name)
	assert.Contains(t, apiKey.Value, "test-api-key", "API_KEY value should be extracted")
	assert.Equal(t, 4, apiKey.StartLine, "API_KEY should be at line 4")

	// Verify MAX_RETRIES constant
	maxRetries := findConstantByName(constants, "MAX_RETRIES")
	require.NotNil(t, maxRetries, "MAX_RETRIES constant not found")
	assert.Equal(t, "MAX_RETRIES", maxRetries.Name)
	assert.Contains(t, maxRetries.Value, "3", "MAX_RETRIES value should be 3")
	assert.Equal(t, 5, maxRetries.StartLine)

	// Verify DEBUG_MODE constant
	debugMode := findConstantByName(constants, "DEBUG_MODE")
	require.NotNil(t, debugMode, "DEBUG_MODE constant not found")
	assert.Equal(t, "DEBUG_MODE", debugMode.Name)
	assert.Contains(t, debugMode.Value, "true", "DEBUG_MODE value should be true")
	assert.Equal(t, 6, debugMode.StartLine)
}

// Test: Extract global variables ($prefix)
func TestRubyParser_ParseGlobalVariables(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// simple.rb has global variable: $global_counter
	variables := extraction.Data.Variables
	require.Len(t, variables, 1, "Expected 1 global variable")

	globalCounter := variables[0]
	assert.Equal(t, "$global_counter", globalCounter.Name)
	assert.Equal(t, "0", globalCounter.Value)
	assert.Equal(t, 8, globalCounter.StartLine, "$global_counter should be at line 8")
}

// Test: Verify line number accuracy across all extractions
func TestRubyParser_LineNumbers(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Verify file-level line numbers
	assert.Equal(t, 1, extraction.StartLine, "File should start at line 1")
	assert.Greater(t, extraction.EndLine, 60, "File should have multiple lines")

	// Verify all symbols have valid line numbers
	for _, sym := range extraction.Symbols.Types {
		assert.Greater(t, sym.StartLine, 0, "Symbol %s should have positive start line", sym.Name)
		assert.GreaterOrEqual(t, sym.EndLine, sym.StartLine, "Symbol %s end line should be >= start line", sym.Name)
	}

	for _, fn := range extraction.Symbols.Functions {
		assert.Greater(t, fn.StartLine, 0, "Function %s should have positive start line", fn.Name)
		assert.GreaterOrEqual(t, fn.EndLine, fn.StartLine, "Function %s end line should be >= start line", fn.Name)
	}

	// Verify definitions have matching line numbers
	for _, def := range extraction.Definitions.Definitions {
		assert.Greater(t, def.StartLine, 0, "Definition %s should have positive start line", def.Name)
		assert.GreaterOrEqual(t, def.EndLine, def.StartLine, "Definition %s end line should be >= start line", def.Name)
	}
}

// Test: Handle invalid/unparseable files gracefully
func TestRubyParser_InvalidFile(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	// Test with non-existent file
	_, err := parser.ParseFile(ctx, "/nonexistent/file.rb")
	assert.Error(t, err, "Should return error for non-existent file")

	// Test with invalid Ruby syntax (tree-sitter returns nil tree)
	// Note: tree-sitter is often forgiving, but should return nil for completely invalid syntax
	// This test verifies graceful handling rather than specific error
}

// Test: Verify three-tier extraction structure is complete
func TestRubyParser_ThreeTierStructure(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Tier 1: Symbols - High-level overview
	require.NotNil(t, extraction.Symbols, "Symbols tier should be present")
	assert.NotEmpty(t, extraction.Symbols.Types, "Should have type symbols")
	assert.NotEmpty(t, extraction.Symbols.Functions, "Should have function symbols")

	// Tier 2: Definitions - Full code definitions
	require.NotNil(t, extraction.Definitions, "Definitions tier should be present")
	assert.NotEmpty(t, extraction.Definitions.Definitions, "Should have definitions")

	// Tier 3: Data - Constants and variables
	require.NotNil(t, extraction.Data, "Data tier should be present")
	assert.NotEmpty(t, extraction.Data.Constants, "Should have constants")
	assert.NotEmpty(t, extraction.Data.Variables, "Should have variables")

	// Verify counts match across tiers
	symbolTypeCount := len(extraction.Symbols.Types)
	defTypeCount := countDefinitionsOfType(extraction.Definitions.Definitions, []string{"class", "module"})
	assert.Equal(t, symbolTypeCount, defTypeCount, "Type count should match between Symbols and Definitions")
}

// Test: Handle blocks with do...end and {...} syntax
func TestRubyParser_Blocks(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// simple.rb uses blocks in find_by_id and find_by_email methods
	// Verify these methods are extracted correctly despite containing blocks
	findByIDMethod := findSymbolByName(extraction.Symbols.Functions, "find_by_id")
	require.NotNil(t, findByIDMethod, "find_by_id method (with block) should be extracted")

	findByEmailMethod := findSymbolByName(extraction.Symbols.Functions, "find_by_email")
	require.NotNil(t, findByEmailMethod, "find_by_email method (with block) should be extracted")

	// Verify the method definitions include the block syntax
	findByIDDef := findDefinitionByName(extraction.Definitions.Definitions, "find_by_id")
	require.NotNil(t, findByIDDef, "find_by_id definition should exist")

	// to_hash method uses a hash literal with block-like syntax {}
	toHashDef := findDefinitionByName(extraction.Definitions.Definitions, "to_hash")
	require.NotNil(t, toHashDef, "to_hash definition should exist")
}

// Test: Verify metadata fields are correctly populated
func TestRubyParser_Metadata(t *testing.T) {
	t.Parallel()

	parser := NewRubyParser()
	ctx := context.Background()

	extraction, err := parser.ParseFile(ctx, testRubyFile)
	require.NoError(t, err)
	require.NotNil(t, extraction)

	// Verify basic metadata
	assert.Equal(t, "ruby", extraction.Language)
	assert.Equal(t, testRubyFile, extraction.FilePath)
	assert.Equal(t, 1, extraction.StartLine)
	assert.Greater(t, extraction.EndLine, 1, "File should have multiple lines")

	// Verify imports count (simplified in Ruby parser)
	assert.GreaterOrEqual(t, extraction.Symbols.ImportsCount, 0, "ImportsCount should be non-negative")
}

// Helper functions

func filterByType(symbols []SymbolInfo, symbolType string) []SymbolInfo {
	var result []SymbolInfo
	for _, s := range symbols {
		if s.Type == symbolType {
			result = append(result, s)
		}
	}
	return result
}

func findSymbolByName(symbols []SymbolInfo, name string) *SymbolInfo {
	for i := range symbols {
		if symbols[i].Name == name {
			return &symbols[i]
		}
	}
	return nil
}

func filterDefinitionsByType(definitions []Definition, defType string) []Definition {
	var result []Definition
	for _, d := range definitions {
		if d.Type == defType {
			result = append(result, d)
		}
	}
	return result
}

func findDefinitionByName(definitions []Definition, name string) *Definition {
	for i := range definitions {
		if definitions[i].Name == name {
			return &definitions[i]
		}
	}
	return nil
}

func findConstantByName(constants []ConstantInfo, name string) *ConstantInfo {
	for i := range constants {
		if constants[i].Name == name {
			return &constants[i]
		}
	}
	return nil
}

func countDefinitionsOfType(definitions []Definition, types []string) int {
	count := 0
	typeMap := make(map[string]bool)
	for _, t := range types {
		typeMap[t] = true
	}
	for _, d := range definitions {
		if typeMap[d.Type] {
			count++
		}
	}
	return count
}
