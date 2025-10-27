package indexer

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test Plan for Chunker:
// - Splits markdown by ## headers
// - Small sections become single chunks
// - Large sections split by paragraphs
// - Preserves code blocks (never split)
// - Tracks start_line and end_line correctly
// - Handles empty content
// - Handles documents without sections
// - Large paragraphs split by sentences
// - Token estimation is reasonable

func TestChunker_SmallDocument(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(800, 100)

	// Test: Small document with multiple sections, each section is one chunk
	content := `# Introduction

This is a small document.

## Section One

First section content.

## Section Two

Second section content.
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)
	require.Len(t, chunks, 3) // Intro + 2 sections

	// Check first chunk (before first ##)
	assert.Equal(t, 0, chunks[0].SectionIndex)
	assert.Equal(t, 0, chunks[0].ChunkIndex)
	assert.Contains(t, chunks[0].Text, "Introduction")
	assert.Greater(t, chunks[0].StartLine, 0)
	assert.Greater(t, chunks[0].EndLine, chunks[0].StartLine)

	// Check second chunk (Section One)
	assert.Equal(t, 1, chunks[1].SectionIndex)
	assert.Contains(t, chunks[1].Text, "Section One")

	// Check third chunk (Section Two)
	assert.Equal(t, 2, chunks[2].SectionIndex)
	assert.Contains(t, chunks[2].Text, "Section Two")
}

func TestChunker_PreservesCodeBlocks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(800, 100)

	// Test: Code blocks are preserved as single units
	content := `## Code Example

Here is some code:

` + "```go" + `
func main() {
    fmt.Println("Hello")
}
` + "```" + `

After the code.
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)
	require.Len(t, chunks, 1)

	// Code block should be in the chunk
	assert.Contains(t, chunks[0].Text, "```go")
	assert.Contains(t, chunks[0].Text, "func main()")
	assert.Contains(t, chunks[0].Text, "```")
	assert.Contains(t, chunks[0].Text, "After the code")
}

func TestChunker_LargeSectionSplitsByParagraphs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(200, 50) // Small target size to force splitting

	// Test: Large section splits into multiple chunks by paragraphs
	// Each paragraph is ~50 tokens, so we need 4 paragraphs to exceed 200 tokens
	content := `## Large Section

Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat.

Duis aute irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla pariatur. Excepteur sint occaecat cupidatat non proident, sunt in culpa qui officia deserunt mollit anim id est laborum.

Sed ut perspiciatis unde omnis iste natus error sit voluptatem accusantium doloremque laudantium, totam rem aperiam, eaque ipsa quae ab illo inventore veritatis et quasi architecto beatae vitae dicta sunt explicabo.

Nemo enim ipsam voluptatem quia voluptas sit aspernatur aut odit aut fugit, sed quia consequuntur magni dolores eos qui ratione voluptatem sequi nesciunt.
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)
	// Should be split into multiple chunks
	assert.GreaterOrEqual(t, len(chunks), 2)

	// All chunks should have same section index
	for _, chunk := range chunks {
		assert.Equal(t, 0, chunk.SectionIndex)
	}

	// Chunk indices should be sequential
	for i, chunk := range chunks {
		assert.Equal(t, i, chunk.ChunkIndex)
	}
}

func TestChunker_EmptyContent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(800, 100)

	// Test: Empty content returns empty chunks
	chunks, err := chunker.ChunkDocument(ctx, "test.md", "   \n\n   ")

	require.NoError(t, err)
	assert.Empty(t, chunks)
}

func TestChunker_NoHeaders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(800, 100)

	// Test: Document without ## headers (just paragraphs)
	content := `This is a document without headers.

Just some paragraphs.

And more text.
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)
	require.Len(t, chunks, 1)

	assert.Equal(t, 0, chunks[0].SectionIndex)
	assert.Contains(t, chunks[0].Text, "without headers")
}

func TestChunker_LineNumbers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(800, 100)

	// Test: Line numbers are tracked correctly
	content := `Line 1

## Section One
Line 4
Line 5

## Section Two
Line 8
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// All chunks should have valid line numbers
	for _, chunk := range chunks {
		assert.Greater(t, chunk.StartLine, 0)
		assert.GreaterOrEqual(t, chunk.EndLine, chunk.StartLine)
	}
}

func TestChunker_MultipleSections(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(800, 100)

	// Test: Multiple sections with different sizes
	content := `## Small Section One

Short content.

## Small Section Two

Also short.

## Small Section Three

Still short.
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)
	assert.Len(t, chunks, 3)

	// Each section should be its own chunk
	assert.Equal(t, 0, chunks[0].SectionIndex)
	assert.Equal(t, 1, chunks[1].SectionIndex)
	assert.Equal(t, 2, chunks[2].SectionIndex)
}

func TestChunker_EstimateTokens(t *testing.T) {
	t.Parallel()

	chunker := NewChunker(800, 100).(*chunker)

	// Test: Token estimation is reasonable (1 token ≈ 4 chars)
	tests := []struct {
		text           string
		expectedTokens int
	}{
		{"", 0},
		{"test", 1},      // 4 chars = 1 token
		{"test test", 2}, // 9 chars = 2 tokens
		{"This is a longer sentence with more words.", 10}, // 43 chars = 10 tokens (corrected)
	}

	for _, tt := range tests {
		tokens := chunker.estimateTokens(tt.text)
		assert.Equal(t, tt.expectedTokens, tokens, "text: %s", tt.text)
	}
}

func TestChunker_CodeBlockNotSplit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(100, 20) // Very small target size

	// Test: Code block should never be split even if it exceeds target size
	content := `## Code Section

` + "```python" + `
def very_long_function_name_that_exceeds_target_size():
    print("This is a long code block")
    print("With multiple lines")
    print("That should stay together")
    return True
` + "```" + `
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)
	require.GreaterOrEqual(t, len(chunks), 1)

	// Find the chunk with the code block
	var codeChunk *DocumentationChunk
	for i := range chunks {
		if strings.Contains(chunks[i].Text, "```python") {
			codeChunk = &chunks[i]
			break
		}
	}

	require.NotNil(t, codeChunk)

	// Code block should be complete
	assert.Contains(t, codeChunk.Text, "```python")
	assert.Contains(t, codeChunk.Text, "def very_long_function")
	assert.Contains(t, codeChunk.Text, "```")
}

func TestChunker_ReadFromFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(800, 100)

	// Test: Read content from file when content parameter is empty
	chunks, err := chunker.ChunkDocument(ctx, "../../testdata/docs/simple.md", "")

	require.NoError(t, err)
	assert.NotEmpty(t, chunks)

	// Should have multiple sections
	assert.GreaterOrEqual(t, len(chunks), 2)
}

func TestChunker_LargeParagraphMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	chunker := NewChunker(50, 10) // Very small to force paragraph splitting

	// Test: Large paragraph sets IsLargeParagraph and IsSplitParagraph flags
	content := `## Section

This is a very long paragraph that contains many sentences. It should exceed the target size and be split into multiple chunks by sentences. Each chunk should have the IsLargeParagraph and IsSplitParagraph flags set to true.
`

	chunks, err := chunker.ChunkDocument(ctx, "test.md", content)

	require.NoError(t, err)

	// Check if any chunk has the large paragraph flags
	hasLargeParagraph := false
	for _, chunk := range chunks {
		if chunk.IsLargeParagraph {
			hasLargeParagraph = true
			assert.True(t, chunk.IsSplitParagraph)
		}
	}

	// With a target size of 50 tokens, the paragraph should be split
	assert.True(t, hasLargeParagraph, "Expected at least one chunk with IsLargeParagraph=true")
}
