package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseStringArg(t *testing.T) {
	t.Parallel()

	t.Run("required string present", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"name": "test-value",
		}
		result, err := parseStringArg(argsMap, "name", true)
		require.NoError(t, err)
		assert.Equal(t, "test-value", result)
	})

	t.Run("required string missing", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result, err := parseStringArg(argsMap, "name", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name parameter is required")
		assert.Empty(t, result)
	})

	t.Run("required string empty", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"name": "",
		}
		result, err := parseStringArg(argsMap, "name", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name cannot be empty")
		assert.Empty(t, result)
	})

	t.Run("optional string missing", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result, err := parseStringArg(argsMap, "name", false)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("optional string empty", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"name": "",
		}
		result, err := parseStringArg(argsMap, "name", false)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("wrong type", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"name": 42,
		}
		result, err := parseStringArg(argsMap, "name", true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name must be a string")
		assert.Empty(t, result)
	})
}

func TestParseIntArg(t *testing.T) {
	t.Parallel()

	t.Run("int present", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"limit": float64(42), // MCP sends numbers as float64
		}
		result := parseIntArg(argsMap, "limit", 10)
		assert.Equal(t, 42, result)
	})

	t.Run("int missing", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result := parseIntArg(argsMap, "limit", 10)
		assert.Equal(t, 10, result)
	})

	t.Run("wrong type", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"limit": "not-a-number",
		}
		result := parseIntArg(argsMap, "limit", 10)
		assert.Equal(t, 10, result) // Returns default on invalid type
	})

	t.Run("zero value", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"limit": float64(0),
		}
		result := parseIntArg(argsMap, "limit", 10)
		assert.Equal(t, 0, result) // 0 is valid
	})
}

func TestParseIntArgPtr(t *testing.T) {
	t.Parallel()

	t.Run("int present", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"lines": float64(5),
		}
		result := parseIntArgPtr(argsMap, "lines")
		require.NotNil(t, result)
		assert.Equal(t, 5, *result)
	})

	t.Run("int missing", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result := parseIntArgPtr(argsMap, "lines")
		assert.Nil(t, result)
	})

	t.Run("zero value", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"lines": float64(0),
		}
		result := parseIntArgPtr(argsMap, "lines")
		require.NotNil(t, result)
		assert.Equal(t, 0, *result) // 0 is valid and different from nil
	})

	t.Run("wrong type", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"lines": "not-a-number",
		}
		result := parseIntArgPtr(argsMap, "lines")
		assert.Nil(t, result) // Returns nil on invalid type
	})
}

func TestParseBoolArg(t *testing.T) {
	t.Parallel()

	t.Run("bool true", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"flag": true,
		}
		result := parseBoolArg(argsMap, "flag", false)
		assert.True(t, result)
	})

	t.Run("bool false", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"flag": false,
		}
		result := parseBoolArg(argsMap, "flag", true)
		assert.False(t, result)
	})

	t.Run("bool missing", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result := parseBoolArg(argsMap, "flag", true)
		assert.True(t, result) // Returns default
	})

	t.Run("wrong type", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"flag": "not-a-bool",
		}
		result := parseBoolArg(argsMap, "flag", true)
		assert.True(t, result) // Returns default on invalid type
	})
}

func TestParseArrayArg(t *testing.T) {
	t.Parallel()

	t.Run("array present", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"tags": []interface{}{"go", "code", "test"},
		}
		result := parseArrayArg(argsMap, "tags")
		require.NotNil(t, result)
		assert.Equal(t, []string{"go", "code", "test"}, result)
	})

	t.Run("array missing", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result := parseArrayArg(argsMap, "tags")
		assert.Nil(t, result)
	})

	t.Run("empty array", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"tags": []interface{}{},
		}
		result := parseArrayArg(argsMap, "tags")
		require.NotNil(t, result)
		assert.Empty(t, result)
	})

	t.Run("mixed types", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"tags": []interface{}{"go", 42, "code", true, "test"},
		}
		result := parseArrayArg(argsMap, "tags")
		require.NotNil(t, result)
		// Only string elements should be included
		assert.Equal(t, []string{"go", "code", "test"}, result)
	})

	t.Run("wrong type", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"tags": "not-an-array",
		}
		result := parseArrayArg(argsMap, "tags")
		assert.Nil(t, result)
	})
}

func TestParseClampedInt(t *testing.T) {
	t.Parallel()

	t.Run("within bounds", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"lines": float64(5),
		}
		result := parseClampedInt(argsMap, "lines", 3, 0, 10)
		assert.Equal(t, 5, result)
	})

	t.Run("below minimum", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"lines": float64(-5),
		}
		result := parseClampedInt(argsMap, "lines", 3, 0, 10)
		assert.Equal(t, 0, result) // Clamped to min
	})

	t.Run("above maximum", func(t *testing.T) {
		argsMap := map[string]interface{}{
			"lines": float64(100),
		}
		result := parseClampedInt(argsMap, "lines", 3, 0, 10)
		assert.Equal(t, 10, result) // Clamped to max
	})

	t.Run("missing uses default", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result := parseClampedInt(argsMap, "lines", 3, 0, 10)
		assert.Equal(t, 3, result)
	})

	t.Run("default below minimum", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result := parseClampedInt(argsMap, "lines", -5, 0, 10)
		assert.Equal(t, 0, result) // Default is clamped too
	})

	t.Run("default above maximum", func(t *testing.T) {
		argsMap := map[string]interface{}{}
		result := parseClampedInt(argsMap, "lines", 100, 0, 10)
		assert.Equal(t, 10, result) // Default is clamped too
	})
}
