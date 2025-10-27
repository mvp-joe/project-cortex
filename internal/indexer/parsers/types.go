package parsers

import "github.com/mvp-joe/project-cortex/internal/indexer/extraction"

// CodeExtraction represents the three-tier extraction from a source code file.
type CodeExtraction struct {
	// Symbols contains high-level overview (package, imports count, type/function names)
	Symbols *extraction.SymbolsData

	// Definitions contains full type definitions and function signatures
	Definitions *extraction.DefinitionsData

	// Data contains constants, global variables, and configuration
	Data *extraction.DataData

	// Metadata about the extraction
	Language  string
	FilePath  string
	StartLine int
	EndLine   int
}
