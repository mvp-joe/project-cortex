package indexer

import (
	"context"
	"os"
	"regexp"
	"strings"
)

// chunker implements the Chunker interface.
type chunker struct {
	targetSize int // target size in tokens (approximate)
	overlap    int // overlap in tokens
}

// NewChunker creates a new documentation chunker.
func NewChunker(targetSize, overlap int) Chunker {
	return &chunker{
		targetSize: targetSize,
		overlap:    overlap,
	}
}

// ChunkDocument splits a markdown file into semantic chunks.
// Algorithm:
// 1. Split by ## headers (level 2)
// 2. If section < target size, create single chunk
// 3. If section > target size, split by paragraphs (double newline)
// 4. Never split inside ```code blocks```
// 5. Track start_line and end_line for every chunk
func (c *chunker) ChunkDocument(ctx context.Context, filePath string, content string) ([]DocumentationChunk, error) {
	// Read file if content is empty (and file path is not just a test name)
	if content == "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return nil, err
		}
		content = string(data)
	}

	// Handle completely empty content
	if strings.TrimSpace(content) == "" {
		return []DocumentationChunk{}, nil
	}

	lines := strings.Split(content, "\n")
	chunks := []DocumentationChunk{}

	// Split by ## headers
	sections := c.splitByHeaders(lines)

	for sectionIdx, section := range sections {
		sectionChunks := c.processSection(filePath, sectionIdx, section)
		chunks = append(chunks, sectionChunks...)
	}

	return chunks, nil
}

// section represents a markdown section with its lines and start position.
type section struct {
	startLine int
	lines     []string
}

// splitByHeaders splits the document into sections by ## headers.
func (c *chunker) splitByHeaders(lines []string) []section {
	sections := []section{}
	currentSection := section{startLine: 1, lines: []string{}}
	headerPattern := regexp.MustCompile(`^##\s+`)

	for i, line := range lines {
		if headerPattern.MatchString(line) && i > 0 {
			// Start new section
			if len(currentSection.lines) > 0 {
				sections = append(sections, currentSection)
			}
			currentSection = section{startLine: i + 1, lines: []string{line}}
		} else {
			currentSection.lines = append(currentSection.lines, line)
		}
	}

	// Add final section
	if len(currentSection.lines) > 0 {
		sections = append(sections, currentSection)
	}

	return sections
}

// processSection processes a single section and returns one or more chunks.
func (c *chunker) processSection(filePath string, sectionIdx int, sec section) []DocumentationChunk {
	text := strings.Join(sec.lines, "\n")
	tokenCount := c.estimateTokens(text)

	// If section is small enough, return as single chunk
	if tokenCount <= c.targetSize {
		return []DocumentationChunk{{
			FilePath:     filePath,
			SectionIndex: sectionIdx,
			ChunkIndex:   0,
			Text:         strings.TrimSpace(text),
			StartLine:    sec.startLine,
			EndLine:      sec.startLine + len(sec.lines) - 1,
		}}
	}

	// Section is too large, split by paragraphs
	return c.splitByParagraphs(filePath, sectionIdx, sec)
}

// splitByParagraphs splits a large section by paragraphs (double newline).
func (c *chunker) splitByParagraphs(filePath string, sectionIdx int, sec section) []DocumentationChunk {
	chunks := []DocumentationChunk{}
	paragraphs := c.extractParagraphs(sec.lines, sec.startLine)

	currentChunk := []paragraph{}
	currentSize := 0
	chunkIndex := 0

	for _, para := range paragraphs {
		paraSize := c.estimateTokens(para.text)

		// If adding this paragraph exceeds target size, finalize current chunk
		if currentSize > 0 && currentSize+paraSize > c.targetSize {
			chunk := c.buildChunk(filePath, sectionIdx, chunkIndex, currentChunk)
			chunks = append(chunks, chunk)
			chunkIndex++
			currentChunk = []paragraph{}
			currentSize = 0
		}

		// If single paragraph exceeds target size, split by sentences
		if paraSize > c.targetSize {
			// Handle large paragraph - split by sentences
			sentenceChunks := c.splitLargeParagraph(filePath, sectionIdx, chunkIndex, para)
			chunks = append(chunks, sentenceChunks...)
			chunkIndex += len(sentenceChunks)
			continue
		}

		currentChunk = append(currentChunk, para)
		currentSize += paraSize
	}

	// Add final chunk
	if len(currentChunk) > 0 {
		chunk := c.buildChunk(filePath, sectionIdx, chunkIndex, currentChunk)
		chunks = append(chunks, chunk)
	}

	return chunks
}

// paragraph represents a paragraph with its text and line range.
type paragraph struct {
	text      string
	startLine int
	endLine   int
	isCode    bool
}

// extractParagraphs extracts paragraphs from lines.
// Preserves code blocks as single paragraphs.
func (c *chunker) extractParagraphs(lines []string, startLine int) []paragraph {
	paragraphs := []paragraph{}
	currentPara := []string{}
	currentStart := startLine
	inCodeBlock := false
	codeBlockPattern := regexp.MustCompile("^```")

	for i, line := range lines {
		lineNum := startLine + i

		// Check for code block boundaries
		if codeBlockPattern.MatchString(line) {
			if !inCodeBlock {
				// Start of code block
				if len(currentPara) > 0 {
					// Finalize previous paragraph
					text := strings.TrimSpace(strings.Join(currentPara, "\n"))
					if text != "" {
						paragraphs = append(paragraphs, paragraph{
							text:      text,
							startLine: currentStart,
							endLine:   lineNum - 1,
							isCode:    false,
						})
					}
					currentPara = []string{}
				}
				inCodeBlock = true
				currentStart = lineNum
				currentPara = append(currentPara, line)
			} else {
				// End of code block
				currentPara = append(currentPara, line)
				text := strings.TrimSpace(strings.Join(currentPara, "\n"))
				paragraphs = append(paragraphs, paragraph{
					text:      text,
					startLine: currentStart,
					endLine:   lineNum,
					isCode:    true,
				})
				currentPara = []string{}
				currentStart = lineNum + 1
				inCodeBlock = false
			}
			continue
		}

		if inCodeBlock {
			currentPara = append(currentPara, line)
			continue
		}

		// Normal paragraph handling (outside code blocks)
		if strings.TrimSpace(line) == "" {
			// Empty line - finalize paragraph
			if len(currentPara) > 0 {
				text := strings.TrimSpace(strings.Join(currentPara, "\n"))
				if text != "" {
					paragraphs = append(paragraphs, paragraph{
						text:      text,
						startLine: currentStart,
						endLine:   lineNum - 1,
						isCode:    false,
					})
				}
				currentPara = []string{}
				currentStart = lineNum + 1
			}
		} else {
			currentPara = append(currentPara, line)
		}
	}

	// Add final paragraph
	if len(currentPara) > 0 {
		text := strings.TrimSpace(strings.Join(currentPara, "\n"))
		if text != "" {
			endLine := startLine + len(lines) - 1
			paragraphs = append(paragraphs, paragraph{
				text:      text,
				startLine: currentStart,
				endLine:   endLine,
				isCode:    inCodeBlock,
			})
		}
	}

	return paragraphs
}

// buildChunk builds a DocumentationChunk from paragraphs.
func (c *chunker) buildChunk(filePath string, sectionIdx, chunkIdx int, paragraphs []paragraph) DocumentationChunk {
	texts := make([]string, len(paragraphs))
	for i, p := range paragraphs {
		texts[i] = p.text
	}

	return DocumentationChunk{
		FilePath:     filePath,
		SectionIndex: sectionIdx,
		ChunkIndex:   chunkIdx,
		Text:         strings.Join(texts, "\n\n"),
		StartLine:    paragraphs[0].startLine,
		EndLine:      paragraphs[len(paragraphs)-1].endLine,
	}
}

// splitLargeParagraph splits a large paragraph by sentences.
func (c *chunker) splitLargeParagraph(filePath string, sectionIdx, startChunkIdx int, para paragraph) []DocumentationChunk {
	// Simple sentence splitting (can be improved)
	sentencePattern := regexp.MustCompile(`[.!?]+\s+`)
	sentences := sentencePattern.Split(para.text, -1)

	chunks := []DocumentationChunk{}
	currentText := []string{}
	currentSize := 0
	chunkIdx := startChunkIdx

	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}

		sentenceSize := c.estimateTokens(sentence)

		if currentSize > 0 && currentSize+sentenceSize > c.targetSize {
			// Finalize current chunk
			chunks = append(chunks, DocumentationChunk{
				FilePath:         filePath,
				SectionIndex:     sectionIdx,
				ChunkIndex:       chunkIdx,
				Text:             strings.Join(currentText, " "),
				StartLine:        para.startLine,
				EndLine:          para.endLine,
				IsLargeParagraph: true,
				IsSplitParagraph: true,
			})
			chunkIdx++
			currentText = []string{}
			currentSize = 0
		}

		currentText = append(currentText, sentence)
		currentSize += sentenceSize
	}

	// Add final chunk
	if len(currentText) > 0 {
		chunks = append(chunks, DocumentationChunk{
			FilePath:         filePath,
			SectionIndex:     sectionIdx,
			ChunkIndex:       chunkIdx,
			Text:             strings.Join(currentText, " "),
			StartLine:        para.startLine,
			EndLine:          para.endLine,
			IsLargeParagraph: true,
			IsSplitParagraph: true,
		})
	}

	return chunks
}

// estimateTokens estimates token count (rough approximation: 1 token ≈ 4 chars).
func (c *chunker) estimateTokens(text string) int {
	return len(text) / 4
}
