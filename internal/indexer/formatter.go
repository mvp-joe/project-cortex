package indexer

import (
	"fmt"
	"strings"

	"github.com/mvp-joe/project-cortex/internal/indexer/extraction"
)

// formatter implements the Formatter interface.
type formatter struct{}

// NewFormatter creates a new formatter instance.
func NewFormatter() Formatter {
	return &formatter{}
}

// FormatSymbols converts SymbolsData into natural language text.
func (f *formatter) FormatSymbols(data *extraction.SymbolsData, language string) string {
	var sb strings.Builder

	// Package/module name
	if data.PackageName != "" {
		sb.WriteString(fmt.Sprintf("Package: %s\n\n", data.PackageName))
	}

	// Import count
	if data.ImportsCount > 0 {
		sb.WriteString(fmt.Sprintf("Imports: %d packages\n\n", data.ImportsCount))
	}

	// Types
	if len(data.Types) > 0 {
		sb.WriteString("Types:\n")
		for _, typ := range data.Types {
			lineRange := formatLineRange(typ.StartLine, typ.EndLine)
			sb.WriteString(fmt.Sprintf("  - %s (%s) %s\n", typ.Name, typ.Type, lineRange))
		}
		sb.WriteString("\n")
	}

	// Functions
	if len(data.Functions) > 0 {
		sb.WriteString("Functions:\n")
		for _, fn := range data.Functions {
			lineRange := formatLineRange(fn.StartLine, fn.EndLine)
			if fn.Signature != "" {
				sb.WriteString(fmt.Sprintf("  - %s %s\n", fn.Signature, lineRange))
			} else {
				sb.WriteString(fmt.Sprintf("  - %s() %s\n", fn.Name, lineRange))
			}
		}
	}

	return strings.TrimSpace(sb.String())
}

// FormatDefinitions converts DefinitionsData into formatted code with line comments.
func (f *formatter) FormatDefinitions(data *extraction.DefinitionsData, language string) string {
	var sb strings.Builder

	for i, def := range data.Definitions {
		if i > 0 {
			sb.WriteString("\n\n")
		}

		// Add line comment
		lineRange := formatLineRange(def.StartLine, def.EndLine)
		sb.WriteString(fmt.Sprintf("// %s\n", lineRange))

		// Add code
		sb.WriteString(def.Code)
	}

	return sb.String()
}

// FormatData converts DataData into formatted code with line comments.
func (f *formatter) FormatData(data *extraction.DataData, language string) string {
	var sb strings.Builder

	// Constants
	if len(data.Constants) > 0 {
		for i, constant := range data.Constants {
			if i > 0 {
				sb.WriteString("\n\n")
			}

			lineRange := formatLineRange(constant.StartLine, constant.EndLine)
			sb.WriteString(fmt.Sprintf("// %s\n", lineRange))

			// Format based on language
			sb.WriteString(formatConstant(constant, language))
		}
	}

	// Variables
	if len(data.Variables) > 0 {
		if len(data.Constants) > 0 {
			sb.WriteString("\n\n")
		}

		for i, variable := range data.Variables {
			if i > 0 {
				sb.WriteString("\n\n")
			}

			lineRange := formatLineRange(variable.StartLine, variable.EndLine)
			sb.WriteString(fmt.Sprintf("// %s\n", lineRange))

			// Format based on language
			sb.WriteString(formatVariable(variable, language))
		}
	}

	return strings.TrimSpace(sb.String())
}

// FormatDocumentation formats a documentation chunk.
func (f *formatter) FormatDocumentation(chunk *DocumentationChunk) string {
	// Documentation chunks are already in natural language (markdown)
	// We just return the text as-is
	return strings.TrimSpace(chunk.Text)
}

// formatLineRange formats line numbers into a human-readable range.
func formatLineRange(start, end int) string {
	if start == end {
		return fmt.Sprintf("(line %d)", start)
	}
	return fmt.Sprintf("(lines %d-%d)", start, end)
}

// formatConstant formats a constant based on the language.
func formatConstant(c extraction.ConstantInfo, language string) string {
	switch language {
	case "go":
		if c.Type != "" {
			return fmt.Sprintf("const %s %s = %s", c.Name, c.Type, c.Value)
		}
		return fmt.Sprintf("const %s = %s", c.Name, c.Value)
	case "python":
		return fmt.Sprintf("%s = %s", c.Name, c.Value)
	case "typescript", "javascript":
		return fmt.Sprintf("const %s = %s", c.Name, c.Value)
	default:
		return fmt.Sprintf("%s = %s", c.Name, c.Value)
	}
}

// formatVariable formats a variable based on the language.
func formatVariable(v extraction.VariableInfo, language string) string {
	switch language {
	case "go":
		if v.Type != "" {
			return fmt.Sprintf("var %s %s = %s", v.Name, v.Type, v.Value)
		}
		return fmt.Sprintf("var %s = %s", v.Name, v.Value)
	case "python":
		return fmt.Sprintf("%s = %s", v.Name, v.Value)
	case "typescript":
		return fmt.Sprintf("let %s = %s", v.Name, v.Value)
	case "javascript":
		return fmt.Sprintf("let %s = %s", v.Name, v.Value)
	default:
		return fmt.Sprintf("%s = %s", v.Name, v.Value)
	}
}
