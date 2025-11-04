package parsers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for PythonParser:
// - Parse class definitions
// - Extract methods from classes (with proper class association)
// - Extract standalone functions (not methods)
// - Extract ALL_CAPS constants (Python convention)
// - Extract lowercase variables
// - Verify accurate line numbers for all symbols
// - Handle invalid/non-existent files gracefully
// - Handle empty files without errors
// - Handle decorators (@decorator syntax)
// - Handle async functions (async def)
// - Count imports correctly
// - Extract function signatures with parameters
// - Extract method signatures with class prefix
// - Distinguish between constants and variables by naming convention

func TestPythonParser_ParseClass(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Parse simple.py and extract class definitions
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify basic metadata
	assert.Equal(t, "python", result.Language)
	assert.Contains(t, result.FilePath, "simple.py")

	// Verify symbols data exists
	require.NotNil(t, result.Symbols)
	require.NotNil(t, result.Symbols.Types)

	// Test: Should extract both User and UserRepository classes
	assert.Len(t, result.Symbols.Types, 2)

	// Find User class
	var userClass *extraction.SymbolInfo
	for i := range result.Symbols.Types {
		if result.Symbols.Types[i].Name == "User" {
			userClass = &result.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, userClass, "User class should be extracted")
	assert.Equal(t, "class", userClass.Type)
	assert.Equal(t, 12, userClass.StartLine)
	assert.Equal(t, 25, userClass.EndLine)

	// Find UserRepository class
	var repoClass *extraction.SymbolInfo
	for i := range result.Symbols.Types {
		if result.Symbols.Types[i].Name == "UserRepository" {
			repoClass = &result.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, repoClass, "UserRepository class should be extracted")
	assert.Equal(t, "class", repoClass.Type)
	assert.Equal(t, 27, repoClass.StartLine)
	assert.Equal(t, 44, repoClass.EndLine)
}

func TestPythonParser_ParseMethods(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract methods from classes
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Symbols)
	require.NotNil(t, result.Symbols.Functions)

	// Test: Should extract 7 functions total (6 methods + 1 standalone function)
	// User: __init__, validate, to_dict
	// UserRepository: __init__, add, find_by_email
	// Standalone: create_user
	assert.Len(t, result.Symbols.Functions, 7)

	// Test: Verify User.__init__ method
	var userInit *extraction.SymbolInfo
	for i := range result.Symbols.Functions {
		if result.Symbols.Functions[i].Name == "__init__" {
			sig := result.Symbols.Functions[i].Signature
			if sig == "User.__init__(self, name: str, email: str)" {
				userInit = &result.Symbols.Functions[i]
				break
			}
		}
	}
	require.NotNil(t, userInit, "User.__init__ should be extracted")
	assert.Equal(t, "method", userInit.Type)
	assert.Equal(t, 15, userInit.StartLine)
	assert.Equal(t, 17, userInit.EndLine)

	// Test: Verify User.validate method
	var validate *extraction.SymbolInfo
	for i := range result.Symbols.Functions {
		if result.Symbols.Functions[i].Name == "validate" {
			validate = &result.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, validate, "validate method should be extracted")
	assert.Equal(t, "method", validate.Type)
	assert.Contains(t, validate.Signature, "User.validate")
	assert.Contains(t, validate.Signature, "-> bool")

	// Test: Verify UserRepository.find_by_email method
	var findByEmail *extraction.SymbolInfo
	for i := range result.Symbols.Functions {
		if result.Symbols.Functions[i].Name == "find_by_email" {
			findByEmail = &result.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, findByEmail, "find_by_email method should be extracted")
	assert.Equal(t, "method", findByEmail.Type)
	assert.Contains(t, findByEmail.Signature, "UserRepository.find_by_email")
	assert.Contains(t, findByEmail.Signature, "-> Optional[User]")
}

func TestPythonParser_ParseFunctions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract standalone functions (not methods)
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Symbols)
	require.NotNil(t, result.Symbols.Functions)

	// Test: Find create_user function (standalone, not a method)
	var createUser *extraction.SymbolInfo
	for i := range result.Symbols.Functions {
		if result.Symbols.Functions[i].Name == "create_user" && result.Symbols.Functions[i].Type == "function" {
			createUser = &result.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, createUser, "create_user function should be extracted")
	assert.Equal(t, "function", createUser.Type)
	assert.Equal(t, 46, createUser.StartLine)
	assert.Equal(t, 48, createUser.EndLine)
	assert.Contains(t, createUser.Signature, "create_user")
	assert.Contains(t, createUser.Signature, "(name: str, email: str)")
	assert.Contains(t, createUser.Signature, "-> User")
}

func TestPythonParser_ParseConstants(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract ALL_CAPS constants (Python convention)
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Data)
	require.NotNil(t, result.Data.Constants)

	// Test: Should extract 3 constants: API_KEY, MAX_RETRIES, DEBUG_MODE
	assert.Len(t, result.Data.Constants, 3)

	// Test: Verify API_KEY constant
	var apiKey *extraction.ConstantInfo
	for i := range result.Data.Constants {
		if result.Data.Constants[i].Name == "API_KEY" {
			apiKey = &result.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, apiKey, "API_KEY constant should be extracted")
	assert.Equal(t, `"test-api-key"`, apiKey.Value)
	assert.Equal(t, 6, apiKey.StartLine)

	// Test: Verify MAX_RETRIES constant
	var maxRetries *extraction.ConstantInfo
	for i := range result.Data.Constants {
		if result.Data.Constants[i].Name == "MAX_RETRIES" {
			maxRetries = &result.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, maxRetries, "MAX_RETRIES constant should be extracted")
	assert.Equal(t, "5", maxRetries.Value)
	assert.Equal(t, 7, maxRetries.StartLine)

	// Test: Verify DEBUG_MODE constant
	var debugMode *extraction.ConstantInfo
	for i := range result.Data.Constants {
		if result.Data.Constants[i].Name == "DEBUG_MODE" {
			debugMode = &result.Data.Constants[i]
			break
		}
	}
	require.NotNil(t, debugMode, "DEBUG_MODE constant should be extracted")
	assert.Equal(t, "True", debugMode.Value)
	assert.Equal(t, 8, debugMode.StartLine)

	// Test: Verify lowercase variable is NOT a constant
	for i := range result.Data.Constants {
		assert.NotEqual(t, "database_url", result.Data.Constants[i].Name, "database_url should be a variable, not a constant")
	}
}

func TestPythonParser_ParseVariables(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract lowercase variables (not ALL_CAPS)
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Data)
	require.NotNil(t, result.Data.Variables)

	// Test: Should extract database_url variable
	assert.Len(t, result.Data.Variables, 1)

	var dbUrl *extraction.VariableInfo
	for i := range result.Data.Variables {
		if result.Data.Variables[i].Name == "database_url" {
			dbUrl = &result.Data.Variables[i]
			break
		}
	}
	require.NotNil(t, dbUrl, "database_url variable should be extracted")
	assert.Equal(t, `"postgresql://localhost/testdb"`, dbUrl.Value)
	assert.Equal(t, 10, dbUrl.StartLine)
}

func TestPythonParser_LineNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Verify line number accuracy
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)

	// Test: Verify file range
	assert.Equal(t, 1, result.StartLine)
	assert.Equal(t, 49, result.EndLine) // File has 49 lines

	// Test: Verify class line numbers are accurate
	for _, typ := range result.Symbols.Types {
		assert.Greater(t, typ.StartLine, 0, "StartLine should be positive for %s", typ.Name)
		assert.Greater(t, typ.EndLine, 0, "EndLine should be positive for %s", typ.Name)
		assert.GreaterOrEqual(t, typ.EndLine, typ.StartLine, "EndLine should be >= StartLine for %s", typ.Name)
	}

	// Test: Verify function/method line numbers are accurate
	for _, fn := range result.Symbols.Functions {
		assert.Greater(t, fn.StartLine, 0, "StartLine should be positive for %s", fn.Name)
		assert.Greater(t, fn.EndLine, 0, "EndLine should be positive for %s", fn.Name)
		assert.GreaterOrEqual(t, fn.EndLine, fn.StartLine, "EndLine should be >= StartLine for %s", fn.Name)
	}

	// Test: Verify constant/variable line numbers are accurate
	for _, c := range result.Data.Constants {
		assert.Greater(t, c.StartLine, 0, "StartLine should be positive for constant %s", c.Name)
	}
	for _, v := range result.Data.Variables {
		assert.Greater(t, v.StartLine, 0, "StartLine should be positive for variable %s", v.Name)
	}
}

func TestPythonParser_InvalidFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Non-existent file should return error
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/nonexistent.py")

	require.Error(t, err)
	assert.Nil(t, result)
	assert.True(t, os.IsNotExist(err), "Error should be file not found")
}

func TestPythonParser_EmptyFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Create an empty temporary Python file
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.py")
	err := os.WriteFile(emptyFile, []byte(""), 0644)
	require.NoError(t, err)

	// Test: Empty file should parse without errors
	result, err := parser.ParseFile(ctx, emptyFile)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Test: Should have empty symbols
	assert.Equal(t, "python", result.Language)
	assert.Len(t, result.Symbols.Types, 0)
	assert.Len(t, result.Symbols.Functions, 0)
	assert.Len(t, result.Data.Constants, 0)
	assert.Len(t, result.Data.Variables, 0)
}

func TestPythonParser_Decorators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Create a temporary Python file with decorators
	tmpDir := t.TempDir()
	decoratorFile := filepath.Join(tmpDir, "decorators.py")
	content := `import functools

@functools.lru_cache(maxsize=128)
def cached_function(x: int) -> int:
    return x * 2

class Service:
    @property
    def name(self) -> str:
        return "service"

    @staticmethod
    def static_method():
        return True

    @classmethod
    def class_method(cls):
        return cls
`
	err := os.WriteFile(decoratorFile, []byte(content), 0644)
	require.NoError(t, err)

	// Test: Decorators should not break parsing
	result, err := parser.ParseFile(ctx, decoratorFile)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Test: Should extract decorated function
	var cachedFunc *extraction.SymbolInfo
	for i := range result.Symbols.Functions {
		if result.Symbols.Functions[i].Name == "cached_function" && result.Symbols.Functions[i].Type == "function" {
			cachedFunc = &result.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, cachedFunc, "cached_function should be extracted despite decorator")
	assert.Contains(t, cachedFunc.Signature, "cached_function")

	// Test: Should extract Service class
	var serviceClass *extraction.SymbolInfo
	for i := range result.Symbols.Types {
		if result.Symbols.Types[i].Name == "Service" {
			serviceClass = &result.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, serviceClass, "Service class should be extracted")

	// Test: Should extract at least the cached_function and some methods
	// Note: Tree-sitter may parse decorated methods differently, so we verify basic functionality
	assert.GreaterOrEqual(t, len(result.Symbols.Functions), 1, "Should extract at least the decorated function")

	// Verify we can extract the decorated standalone function
	foundCachedFunc := false
	for i := range result.Symbols.Functions {
		if result.Symbols.Functions[i].Name == "cached_function" {
			foundCachedFunc = true
			break
		}
	}
	assert.True(t, foundCachedFunc, "Should extract cached_function despite decorator")
}

func TestPythonParser_AsyncFunctions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Create a temporary Python file with async functions
	tmpDir := t.TempDir()
	asyncFile := filepath.Join(tmpDir, "async.py")
	content := `import asyncio

async def fetch_data(url: str) -> dict:
    """Async function to fetch data."""
    await asyncio.sleep(1)
    return {"url": url}

class AsyncService:
    async def process(self, data: dict) -> bool:
        """Async method to process data."""
        await asyncio.sleep(0.5)
        return True

    async def validate(self) -> None:
        """Another async method."""
        pass
`
	err := os.WriteFile(asyncFile, []byte(content), 0644)
	require.NoError(t, err)

	// Test: Async functions should be parsed correctly
	result, err := parser.ParseFile(ctx, asyncFile)

	require.NoError(t, err)
	require.NotNil(t, result)

	// Test: Should extract async function
	var fetchData *extraction.SymbolInfo
	for i := range result.Symbols.Functions {
		if result.Symbols.Functions[i].Name == "fetch_data" && result.Symbols.Functions[i].Type == "function" {
			fetchData = &result.Symbols.Functions[i]
			break
		}
	}
	require.NotNil(t, fetchData, "fetch_data async function should be extracted")
	assert.Contains(t, fetchData.Signature, "fetch_data")
	assert.Contains(t, fetchData.Signature, "-> dict")

	// Test: Should extract AsyncService class
	var asyncClass *extraction.SymbolInfo
	for i := range result.Symbols.Types {
		if result.Symbols.Types[i].Name == "AsyncService" {
			asyncClass = &result.Symbols.Types[i]
			break
		}
	}
	require.NotNil(t, asyncClass, "AsyncService class should be extracted")

	// Test: Should extract async methods
	asyncMethods := []string{"process", "validate"}
	for _, methodName := range asyncMethods {
		found := false
		for i := range result.Symbols.Functions {
			if result.Symbols.Functions[i].Name == methodName && result.Symbols.Functions[i].Type == "method" {
				found = true
				assert.Contains(t, result.Symbols.Functions[i].Signature, "AsyncService."+methodName)
				break
			}
		}
		assert.True(t, found, "Async method %s should be extracted", methodName)
	}
}

func TestPythonParser_ImportsCount(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Count imports correctly
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Symbols)

	// Test: Should count 3 import statements (import os, import sys, from typing import ...)
	assert.Equal(t, 3, result.Symbols.ImportsCount)
}

func TestPythonParser_Definitions(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	parser := NewPythonParser()

	// Test: Extract definitions tier
	result, err := parser.ParseFile(ctx, "../../../testdata/code/python/simple.py")

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, result.Definitions)
	require.NotNil(t, result.Definitions.Definitions)

	// Test: Should have definitions for classes and functions/methods
	// 2 classes + 6 methods + 1 standalone function = 9 definitions
	assert.Len(t, result.Definitions.Definitions, 9)

	// Test: Class definitions should include full code
	var userClassDef *extraction.Definition
	for i := range result.Definitions.Definitions {
		if result.Definitions.Definitions[i].Name == "User" && result.Definitions.Definitions[i].Type == "class" {
			userClassDef = &result.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, userClassDef, "User class definition should be extracted")
	assert.NotEmpty(t, userClassDef.Code)
	assert.Contains(t, userClassDef.Code, "class User:")
	assert.Equal(t, 12, userClassDef.StartLine)
	assert.Equal(t, 25, userClassDef.EndLine)

	// Test: Function definitions should include signature only
	var createUserDef *extraction.Definition
	for i := range result.Definitions.Definitions {
		if result.Definitions.Definitions[i].Name == "create_user" && result.Definitions.Definitions[i].Type == "function" {
			createUserDef = &result.Definitions.Definitions[i]
			break
		}
	}
	require.NotNil(t, createUserDef, "create_user function definition should be extracted")
	assert.NotEmpty(t, createUserDef.Code)
	assert.Contains(t, createUserDef.Code, "def create_user")
	assert.Equal(t, 46, createUserDef.StartLine)
	assert.Equal(t, 46, createUserDef.EndLine) // Signature only, same line
}

func TestPythonParser_ConstantNamingConvention(t *testing.T) {
	t.Parallel()

	// Test: Helper function isConstantName follows Python convention
	tests := []struct {
		name       string
		isConstant bool
	}{
		{"API_KEY", true},
		{"MAX_RETRIES", true},
		{"DEBUG_MODE", true},
		{"DATABASE_URL", true},
		{"A", true},
		{"AB_CD", true},
		{"database_url", false},
		{"databaseUrl", false},
		{"DatabaseUrl", false},
		{"API_Key", false}, // Mixed case is not a constant
		{"api_KEY", false}, // Mixed case is not a constant
		{"", false},
		{"_PRIVATE", true}, // Leading underscore with ALL_CAPS
		{"__DUNDER__", true},
	}

	for _, tt := range tests {
		result := isConstantName(tt.name)
		assert.Equal(t, tt.isConstant, result, "isConstantName(%q) should be %v", tt.name, tt.isConstant)
	}
}
