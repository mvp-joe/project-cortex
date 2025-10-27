package indexer

import "time"

// ChunkType represents the type of content in a chunk.
type ChunkType string

const (
	ChunkTypeSymbols        ChunkType = "symbols"
	ChunkTypeDefinitions    ChunkType = "definitions"
	ChunkTypeData           ChunkType = "data"
	ChunkTypeDocumentation  ChunkType = "documentation"
)

// Chunk represents a piece of indexed content with its embedding.
type Chunk struct {
	ID        string                 `json:"id"`
	ChunkType ChunkType              `json:"chunk_type"`
	Title     string                 `json:"title"`
	Text      string                 `json:"text"`
	Embedding []float32              `json:"embedding"`
	Tags      []string               `json:"tags"`
	Metadata  map[string]interface{} `json:"metadata"`
	CreatedAt time.Time              `json:"created_at"`
	UpdatedAt time.Time              `json:"updated_at"`
}

// ChunkFile represents the JSON structure for storing chunks.
type ChunkFile struct {
	Metadata ChunkFileMetadata `json:"_metadata"`
	Chunks   []Chunk           `json:"chunks"`
}

// ChunkFileMetadata contains metadata about the chunk file.
type ChunkFileMetadata struct {
	Model      string    `json:"model"`
	Dimensions int       `json:"dimensions"`
	ChunkType  ChunkType `json:"chunk_type"`
	Generated  time.Time `json:"generated"`
	Version    string    `json:"version"`
}

// GeneratorMetadata tracks file checksums and processing stats for incremental indexing.
type GeneratorMetadata struct {
	Version        string            `json:"version"`
	GeneratedAt    time.Time         `json:"generated_at"`
	FileChecksums  map[string]string `json:"file_checksums"`
	Stats          ProcessingStats   `json:"stats"`
}

// ProcessingStats tracks statistics about the indexing process.
type ProcessingStats struct {
	DocsProcessed        int     `json:"docs_processed"`
	CodeFilesProcessed   int     `json:"code_files_processed"`
	TotalDocChunks       int     `json:"total_doc_chunks"`
	TotalCodeChunks      int     `json:"total_code_chunks"`
	ProcessingTimeSeconds float64 `json:"processing_time_seconds"`
}

// CodeExtraction represents the three-tier extraction from a source code file.
type CodeExtraction struct {
	// Symbols contains high-level overview (package, imports count, type/function names)
	Symbols *SymbolsData

	// Definitions contains full type definitions and function signatures
	Definitions *DefinitionsData

	// Data contains constants, global variables, and configuration
	Data *DataData

	// Metadata about the extraction
	Language  string
	FilePath  string
	StartLine int
	EndLine   int
}

// SymbolsData represents the high-level symbols in a file.
type SymbolsData struct {
	PackageName  string
	ImportsCount int
	Types        []SymbolInfo
	Functions    []SymbolInfo
}

// SymbolInfo represents a symbol with its location.
type SymbolInfo struct {
	Name      string
	Type      string // "struct", "interface", "function", "method", etc.
	StartLine int
	EndLine   int
	Signature string // For functions/methods
}

// DefinitionsData represents type definitions and function signatures.
type DefinitionsData struct {
	Definitions []Definition
}

// Definition represents a single type or function definition.
type Definition struct {
	Name      string
	Type      string // "type", "interface", "function", etc.
	Code      string // The actual code
	StartLine int
	EndLine   int
}

// DataData represents constants and configuration values.
type DataData struct {
	Constants []ConstantInfo
	Variables []VariableInfo
}

// ConstantInfo represents a constant declaration.
type ConstantInfo struct {
	Name      string
	Value     string
	Type      string
	StartLine int
	EndLine   int
}

// VariableInfo represents a global variable.
type VariableInfo struct {
	Name      string
	Value     string
	Type      string
	StartLine int
	EndLine   int
}

// DocumentationChunk represents a chunk of documentation content.
type DocumentationChunk struct {
	FilePath           string
	SectionIndex       int
	ChunkIndex         int
	Text               string
	StartLine          int
	EndLine            int
	IsLargeParagraph   bool
	IsSplitParagraph   bool
}
