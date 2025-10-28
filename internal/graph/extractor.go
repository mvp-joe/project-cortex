package graph

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

// Extractor extracts graph data from Go source files.
type Extractor interface {
	// ExtractFile extracts graph data from a single Go file.
	ExtractFile(filePath string) (*FileGraphData, error)
}

// goExtractor implements Extractor for Go files using go/ast.
type goExtractor struct {
	rootDir string // Project root directory for relative paths
}

// NewExtractor creates a new graph extractor for Go files.
func NewExtractor(rootDir string) Extractor {
	return &goExtractor{rootDir: rootDir}
}

// ExtractFile extracts nodes and edges from a Go source file.
func (e *goExtractor) ExtractFile(filePath string) (*FileGraphData, error) {
	// Get relative path for consistency
	relPath, err := filepath.Rel(e.rootDir, filePath)
	if err != nil {
		relPath = filePath
	}

	// Parse file
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, filePath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Go file: %w", err)
	}

	// Read source for context
	source, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read source file: %w", err)
	}

	result := &FileGraphData{
		FilePath: relPath,
		Nodes:    []Node{},
		Edges:    []Edge{},
	}

	// Extract package node
	pkgName := node.Name.Name
	pkgPath := extractPackagePath(relPath)
	result.Nodes = append(result.Nodes, Node{
		ID:        pkgPath,
		Kind:      NodePackage,
		File:      relPath,
		StartLine: 1,
		EndLine:   countLines(source),
	})

	// Extract import edges
	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)
		result.Edges = append(result.Edges, Edge{
			From: pkgPath,
			To:   importPath,
			Type: EdgeImports,
			Location: &Location{
				File: relPath,
				Line: fset.Position(imp.Pos()).Line,
			},
		})
	}

	// Walk AST to extract functions and calls
	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.FuncDecl:
			e.extractFunction(decl, fset, relPath, pkgName, result)
		}
		return true
	})

	return result, nil
}

// extractFunction extracts a function/method node and its call edges.
func (e *goExtractor) extractFunction(decl *ast.FuncDecl, fset *token.FileSet, relPath, pkgName string, result *FileGraphData) {
	funcName := decl.Name.Name
	startLine := fset.Position(decl.Pos()).Line
	endLine := fset.Position(decl.End()).Line

	// Build fully qualified function ID
	var funcID string
	var kind NodeKind

	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		// Method: extract receiver type
		recvType := extractReceiverType(decl.Recv.List[0].Type)
		funcID = recvType + "." + funcName
		kind = NodeMethod
	} else {
		// Function: package.function
		funcID = pkgName + "." + funcName
		kind = NodeFunction
	}

	// Add function node
	result.Nodes = append(result.Nodes, Node{
		ID:        funcID,
		Kind:      kind,
		File:      relPath,
		StartLine: startLine,
		EndLine:   endLine,
	})

	// Extract call edges from function body
	if decl.Body != nil {
		e.extractCalls(decl.Body, funcID, fset, relPath, result)
	}
}

// extractCalls extracts function call edges from a function body.
func (e *goExtractor) extractCalls(body *ast.BlockStmt, fromFunc string, fset *token.FileSet, relPath string, result *FileGraphData) {
	ast.Inspect(body, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Extract callee identifier
		callee := extractCalleeID(callExpr.Fun)
		if callee == "" {
			return true
		}

		// Add call edge
		result.Edges = append(result.Edges, Edge{
			From: fromFunc,
			To:   callee,
			Type: EdgeCalls,
			Location: &Location{
				File:   relPath,
				Line:   fset.Position(callExpr.Pos()).Line,
				Column: fset.Position(callExpr.Pos()).Column,
			},
		})

		return true
	})
}

// extractCalleeID extracts the fully qualified callee identifier from a call expression.
// Returns empty string if the callee cannot be determined.
//
// Limitations (without type checking):
// - Cannot resolve interface method calls (requires type info)
// - Cannot resolve function variables/closures (requires type info)
// - Cannot resolve generic function instantiations (requires type info)
// - Assumes package-level selectors are function calls (may be false positives)
// - Cannot distinguish between methods and package functions in selectors
// - Cannot resolve imported package aliases to full import paths
//
// For 100% accuracy, integrate with go/types package (Phase 2).
func extractCalleeID(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		// Direct call: foo()
		return f.Name

	case *ast.SelectorExpr:
		// Method or package call: obj.Method() or pkg.Function()
		if ident, ok := f.X.(*ast.Ident); ok {
			return ident.Name + "." + f.Sel.Name
		}

		// Nested selector: obj.field.Method()
		// For MVP, we extract the full chain if possible
		if chain := extractSelectorChain(f); chain != "" {
			return chain
		}

	case *ast.FuncLit:
		// Anonymous function, skip
		return ""
	}

	return ""
}

// extractSelectorChain extracts a chain of selectors like "a.b.c".
func extractSelectorChain(expr *ast.SelectorExpr) string {
	var parts []string

	// Walk backwards through selector chain
	for {
		parts = append([]string{expr.Sel.Name}, parts...)

		switch x := expr.X.(type) {
		case *ast.Ident:
			parts = append([]string{x.Name}, parts...)
			return strings.Join(parts, ".")
		case *ast.SelectorExpr:
			expr = x
		default:
			// Complex expression, give up
			return ""
		}
	}
}

// extractReceiverType extracts the type name from a receiver expression.
func extractReceiverType(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.Ident:
		// (T) receiver
		return t.Name
	case *ast.StarExpr:
		// (*T) receiver
		if ident, ok := t.X.(*ast.Ident); ok {
			return ident.Name
		}
	}
	return "unknown"
}

// extractPackagePath derives the package path from the relative file path.
// For example: "internal/graph/extractor.go" -> "internal/graph"
func extractPackagePath(relPath string) string {
	dir := filepath.Dir(relPath)
	if dir == "." {
		return "main"
	}
	return filepath.ToSlash(dir)
}

// countLines counts the number of lines in source code.
func countLines(source []byte) int {
	count := 1
	for _, b := range source {
		if b == '\n' {
			count++
		}
	}
	return count
}
