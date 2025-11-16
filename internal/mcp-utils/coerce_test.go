package mcputils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockArgumentGetter implements ArgumentGetter for testing
type mockArgumentGetter struct {
	args map[string]interface{}
}

func (m *mockArgumentGetter) GetArguments() map[string]interface{} {
	return m.args
}

// TestRequest is a test struct that matches the typical MCP request structure
type TestRequest struct {
	Query      string   `json:"query"`
	Limit      int      `json:"limit,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	ChunkTypes []string `json:"chunk_types,omitempty"`
}

func TestCoerceBindArguments(t *testing.T) {
	t.Run("JSON string arrays", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":       "test query",
				"chunk_types": `["symbols", "definitions", "documentation"]`,
				"tags":        `["go", "test"]`,
				"limit":       "10",
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test query", result.Query)
		assert.Equal(t, 10, result.Limit)
		assert.Equal(t, []string{"symbols", "definitions", "documentation"}, result.ChunkTypes)
		assert.Equal(t, []string{"go", "test"}, result.Tags)
	})

	t.Run("Already proper types", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":       "test query",
				"chunk_types": []string{"symbols", "definitions"},
				"tags":        []string{"go", "test"},
				"limit":       10,
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test query", result.Query)
		assert.Equal(t, 10, result.Limit)
		assert.Equal(t, []string{"symbols", "definitions"}, result.ChunkTypes)
		assert.Equal(t, []string{"go", "test"}, result.Tags)
	})

	t.Run("Mixed string and proper types", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":       "test query",
				"chunk_types": `["symbols"]`,
				"tags":        []string{"go"},
				"limit":       "5",
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test query", result.Query)
		assert.Equal(t, 5, result.Limit)
		assert.Equal(t, []string{"symbols"}, result.ChunkTypes)
		assert.Equal(t, []string{"go"}, result.Tags)
	})

	t.Run("Empty JSON arrays", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":       "test query",
				"chunk_types": "[]",
				"tags":        "[]",
				"limit":       "0",
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test query", result.Query)
		assert.Equal(t, 0, result.Limit)
		assert.Empty(t, result.ChunkTypes)
		assert.Empty(t, result.Tags)
	})

	t.Run("Null and empty strings", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":       "test query",
				"chunk_types": "",
				"tags":        nil,
				"limit":       nil,
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test query", result.Query)
		assert.Equal(t, 0, result.Limit)
		assert.Empty(t, result.ChunkTypes)
		assert.Empty(t, result.Tags)
	})

	t.Run("JSON objects", func(t *testing.T) {
		type ComplexRequest struct {
			Query   string                 `json:"query"`
			Options map[string]interface{} `json:"options"`
		}

		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":   "test",
				"options": `{"debug": true, "verbose": false, "count": 42}`,
			},
		}

		var result ComplexRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test", result.Query)
		assert.NotNil(t, result.Options)
		assert.Equal(t, true, result.Options["debug"])
		assert.Equal(t, false, result.Options["verbose"])
		assert.Equal(t, float64(42), result.Options["count"]) // JSON numbers decode as float64
	})

	t.Run("JSON booleans", func(t *testing.T) {
		type BoolRequest struct {
			Enabled  bool `json:"enabled"`
			Disabled bool `json:"disabled"`
		}

		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"enabled":  "true",
				"disabled": "false",
			},
		}

		var result BoolRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.True(t, result.Enabled)
		assert.False(t, result.Disabled)
	})

	t.Run("JSON numbers", func(t *testing.T) {
		type NumberRequest struct {
			Count  int     `json:"count"`
			Price  float64 `json:"price"`
			Offset int64   `json:"offset"`
		}

		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"count":  "42",
				"price":  "19.99",
				"offset": "1000000",
			},
		}

		var result NumberRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, 42, result.Count)
		assert.Equal(t, 19.99, result.Price)
		assert.Equal(t, int64(1000000), result.Offset)
	})

	t.Run("Comma-separated fallback", func(t *testing.T) {
		// When JSON parsing fails, mapstructure's StringToSliceHookFunc should handle comma-separated
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query": "test",
				"tags":  "go,test,example", // Not JSON, but should still work
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test", result.Query)
		assert.Equal(t, []string{"go", "test", "example"}, result.Tags)
	})

	t.Run("Invalid JSON is passed through", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":       "test",
				"chunk_types": "[invalid json", // Invalid JSON
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		// Invalid JSON should be treated as a single string element
		assert.Equal(t, "test", result.Query)
		assert.Equal(t, []string{"[invalid json"}, result.ChunkTypes)
	})

	t.Run("Nested JSON arrays", func(t *testing.T) {
		type NestedRequest struct {
			Query  string     `json:"query"`
			Matrix [][]string `json:"matrix"`
		}

		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query":  "test",
				"matrix": `[["a", "b"], ["c", "d"]]`,
			},
		}

		var result NestedRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test", result.Query)
		assert.Equal(t, [][]string{{"a", "b"}, {"c", "d"}}, result.Matrix)
	})

	t.Run("Special characters in JSON strings", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query": "test",
				"tags":  `["with \"quotes\"", "with\nnewline", "with\ttab"]`,
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test", result.Query)
		assert.Equal(t, []string{`with "quotes"`, "with\nnewline", "with\ttab"}, result.Tags)
	})

	t.Run("Unicode in JSON strings", func(t *testing.T) {
		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"query": "test",
				"tags":  `["hello", "世界", "🌍"]`,
			},
		}

		var result TestRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, "test", result.Query)
		assert.Equal(t, []string{"hello", "世界", "🌍"}, result.Tags)
	})

	t.Run("WeaklyTypedInput conversions", func(t *testing.T) {
		// Test that mapstructure's WeaklyTypedInput still works
		type WeakRequest struct {
			Count   int    `json:"count"`
			Enabled bool   `json:"enabled"`
			Name    string `json:"name"`
		}

		request := &mockArgumentGetter{
			args: map[string]interface{}{
				"count":   "42", // string to int
				"enabled": 1,    // int to bool
				"name":    123,  // number to string
			},
		}

		var result WeakRequest
		err := CoerceBindArguments(request, &result)
		require.NoError(t, err)

		assert.Equal(t, 42, result.Count)
		assert.True(t, result.Enabled)
		assert.Equal(t, "123", result.Name)
	})
}
