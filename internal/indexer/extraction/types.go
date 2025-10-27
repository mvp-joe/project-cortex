package extraction

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
