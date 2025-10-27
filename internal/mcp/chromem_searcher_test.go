package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuildWhereFilter_EmptyOptions(t *testing.T) {
	t.Parallel()

	s := &chromemSearcher{}
	options := &SearchOptions{}

	whereFilter := s.buildWhereFilter(options)

	assert.Empty(t, whereFilter, "WHERE filter should be empty when no options specified")
}

func TestBuildWhereFilter_SingleTag(t *testing.T) {
	t.Parallel()

	s := &chromemSearcher{}
	options := &SearchOptions{
		Tags: []string{"go"},
	}

	whereFilter := s.buildWhereFilter(options)

	assert.Equal(t, map[string]string{
		"tag_0": "go",
	}, whereFilter, "WHERE filter should include first tag")
}

func TestBuildWhereFilter_MultipleTags(t *testing.T) {
	t.Parallel()

	s := &chromemSearcher{}
	options := &SearchOptions{
		Tags: []string{"go", "code", "symbols"},
	}

	whereFilter := s.buildWhereFilter(options)

	assert.Equal(t, map[string]string{
		"tag_0": "go",
	}, whereFilter, "WHERE filter should only include first tag (others post-filtered)")
}

func TestBuildWhereFilter_SingleChunkType(t *testing.T) {
	t.Parallel()

	s := &chromemSearcher{}
	options := &SearchOptions{
		ChunkTypes: []string{"documentation"},
	}

	whereFilter := s.buildWhereFilter(options)

	assert.Equal(t, map[string]string{
		"chunk_type": "documentation",
	}, whereFilter, "WHERE filter should include chunk type")
}

func TestBuildWhereFilter_MultipleChunkTypes(t *testing.T) {
	t.Parallel()

	s := &chromemSearcher{}
	options := &SearchOptions{
		ChunkTypes: []string{"symbols", "definitions"},
	}

	whereFilter := s.buildWhereFilter(options)

	assert.Equal(t, map[string]string{
		"chunk_type": "symbols",
	}, whereFilter, "WHERE filter should only include first chunk type (others post-filtered)")
}

func TestBuildWhereFilter_Combined(t *testing.T) {
	t.Parallel()

	s := &chromemSearcher{}
	options := &SearchOptions{
		ChunkTypes: []string{"documentation"},
		Tags:       []string{"architecture", "design"},
	}

	whereFilter := s.buildWhereFilter(options)

	assert.Equal(t, map[string]string{
		"chunk_type": "documentation",
		"tag_0":      "architecture",
	}, whereFilter, "WHERE filter should include first chunk type and first tag")
}

func TestExtractTagsFromMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		metadata map[string]string
		expected []string
	}{
		{
			name:     "no tags",
			metadata: map[string]string{},
			expected: []string{},
		},
		{
			name: "single tag",
			metadata: map[string]string{
				"tag_0": "go",
			},
			expected: []string{"go"},
		},
		{
			name: "multiple tags",
			metadata: map[string]string{
				"tag_0": "go",
				"tag_1": "code",
				"tag_2": "symbols",
			},
			expected: []string{"go", "code", "symbols"},
		},
		{
			name: "tags with other metadata",
			metadata: map[string]string{
				"tag_0":     "documentation",
				"tag_1":     "markdown",
				"file_path": "docs/README.md",
				"source":    "markdown",
			},
			expected: []string{"documentation", "markdown"},
		},
		{
			name: "missing middle tag (stops at first gap)",
			metadata: map[string]string{
				"tag_0": "go",
				"tag_2": "symbols",
			},
			expected: []string{"go"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTagsFromMetadata(tt.metadata)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertMetadata_ExcludesTagKeys(t *testing.T) {
	t.Parallel()

	metadata := map[string]string{
		"tag_0":     "go",
		"tag_1":     "code",
		"tag_2":     "symbols",
		"chunk_type": "symbols",
		"file_path": "internal/indexer/types.go",
		"source":    "code",
		"language":  "go",
	}

	result := convertMetadata(metadata)

	assert.NotContains(t, result, "tag_0")
	assert.NotContains(t, result, "tag_1")
	assert.NotContains(t, result, "tag_2")
	assert.NotContains(t, result, "chunk_type")
	assert.Contains(t, result, "file_path")
	assert.Contains(t, result, "source")
	assert.Contains(t, result, "language")
}
