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

	// Build import map for type resolution
	imports := e.buildImportMap(node)

	// Walk AST to extract types, functions, and calls
	ast.Inspect(node, func(n ast.Node) bool {
		switch decl := n.(type) {
		case *ast.GenDecl:
			// Extract type declarations (interfaces, structs)
			if decl.Tok == token.TYPE {
				for _, spec := range decl.Specs {
					if typeSpec, ok := spec.(*ast.TypeSpec); ok {
						e.extractType(typeSpec, fset, relPath, pkgName, imports, result)
					}
				}
			}
		case *ast.FuncDecl:
			e.extractFunction(decl, fset, relPath, pkgName, imports, result)
		}
		return true
	})

	return result, nil
}

// extractType extracts interface or struct type declarations.
func (e *goExtractor) extractType(typeSpec *ast.TypeSpec, fset *token.FileSet, relPath, pkgName string, imports map[string]string, result *FileGraphData) {
	typeName := typeSpec.Name.Name
	startLine := fset.Position(typeSpec.Pos()).Line
	endLine := fset.Position(typeSpec.End()).Line

	switch typeExpr := typeSpec.Type.(type) {
	case *ast.InterfaceType:
		// Extract interface
		methods, embeddedTypes := e.extractInterfaceMembers(typeExpr, imports, pkgName)
		nodeID := pkgName + "." + typeName
		result.Nodes = append(result.Nodes, Node{
			ID:            nodeID,
			Kind:          NodeInterface,
			File:          relPath,
			StartLine:     startLine,
			EndLine:       endLine,
			Methods:       methods,
			EmbeddedTypes: embeddedTypes,
		})

		// Create embedding edges
		for _, embeddedType := range embeddedTypes {
			result.Edges = append(result.Edges, Edge{
				From: nodeID,
				To:   embeddedType,
				Type: EdgeEmbeds,
				Location: &Location{
					File: relPath,
					Line: startLine,
				},
			})
		}

		// Create type usage edges for interface method parameters and returns
		for _, method := range methods {
			// Parameters
			for _, param := range method.Parameters {
				if edge := e.createTypeUsageEdge(nodeID, param.Type, pkgName, relPath, startLine); edge != nil {
					result.Edges = append(result.Edges, *edge)
				}
			}
			// Returns
			for _, ret := range method.Returns {
				if edge := e.createTypeUsageEdge(nodeID, ret.Type, pkgName, relPath, startLine); edge != nil {
					result.Edges = append(result.Edges, *edge)
				}
			}
		}

	case *ast.StructType:
		// Extract struct
		_, embeddedTypes := e.extractStructMembers(typeExpr, imports, pkgName)
		nodeID := pkgName + "." + typeName
		result.Nodes = append(result.Nodes, Node{
			ID:            nodeID,
			Kind:          NodeStruct,
			File:          relPath,
			StartLine:     startLine,
			EndLine:       endLine,
			EmbeddedTypes: embeddedTypes,
			// Methods will be added separately when we encounter method declarations
			// Store placeholder here, methods added during function extraction phase
		})

		// Create embedding edges
		for _, embeddedType := range embeddedTypes {
			result.Edges = append(result.Edges, Edge{
				From: nodeID,
				To:   embeddedType,
				Type: EdgeEmbeds,
				Location: &Location{
					File: relPath,
					Line: startLine,
				},
			})
		}

		// Create type usage edges for all struct fields (embedded and named)
		if typeExpr.Fields != nil {
			for _, field := range typeExpr.Fields.List {
				typeRef := e.resolveTypeRef(field.Type, imports)
				if edge := e.createTypeUsageEdge(nodeID, typeRef, pkgName, relPath, startLine); edge != nil {
					result.Edges = append(result.Edges, *edge)
				}
			}
		}
	}
}

// extractFunction extracts a function/method node and its call edges.
func (e *goExtractor) extractFunction(decl *ast.FuncDecl, fset *token.FileSet, relPath, pkgName string, imports map[string]string, result *FileGraphData) {
	funcName := decl.Name.Name
	startLine := fset.Position(decl.Pos()).Line
	endLine := fset.Position(decl.End()).Line

	// Build fully qualified function ID
	var funcID string
	var kind NodeKind

	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		// Method: extract receiver type
		recvType := extractReceiverType(decl.Recv.List[0].Type)
		// Method ID includes package: pkg.Type.Method
		funcID = pkgName + "." + recvType + "." + funcName
		kind = NodeMethod

		// Extract method signature and add to struct's methods
		methodSig := MethodSignature{
			Name:       funcName,
			Parameters: e.extractParameters(decl.Type.Params, imports),
			Returns:    e.extractParameters(decl.Type.Results, imports),
		}

		// Find the struct node and add this method to it
		// Match against fully qualified struct ID: pkgName.recvType
		structID := pkgName + "." + recvType
		for i := range result.Nodes {
			if result.Nodes[i].ID == structID && result.Nodes[i].Kind == NodeStruct {
				result.Nodes[i].Methods = append(result.Nodes[i].Methods, methodSig)
				break
			}
		}
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

	// Create type usage edges for function parameters
	if decl.Type.Params != nil {
		params := e.extractParameters(decl.Type.Params, imports)
		for _, param := range params {
			if edge := e.createTypeUsageEdge(funcID, param.Type, pkgName, relPath, startLine); edge != nil {
				result.Edges = append(result.Edges, *edge)
			}
		}
	}

	// Create type usage edges for function returns
	if decl.Type.Results != nil {
		returns := e.extractParameters(decl.Type.Results, imports)
		for _, ret := range returns {
			if edge := e.createTypeUsageEdge(funcID, ret.Type, pkgName, relPath, startLine); edge != nil {
				result.Edges = append(result.Edges, *edge)
			}
		}
	}

	// Extract call edges from function body
	if decl.Body != nil {
		e.extractCalls(decl.Body, funcID, pkgName, fset, relPath, result)
	}
}

// extractCalls extracts function call edges from a function body.
func (e *goExtractor) extractCalls(body *ast.BlockStmt, fromFunc, pkgName string, fset *token.FileSet, relPath string, result *FileGraphData) {
	ast.Inspect(body, func(n ast.Node) bool {
		callExpr, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		// Extract callee identifier
		callee := extractCalleeID(callExpr.Fun, pkgName)
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
func extractCalleeID(fun ast.Expr, pkgName string) string {
	switch f := fun.(type) {
	case *ast.Ident:
		// Direct call: foo()
		// Qualify with package name for same-package calls
		return pkgName + "." + f.Name

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

// normalizePackage normalizes package names to canonical form.
// Converts "." (current package reference) to "" (empty string).
func normalizePackage(pkg string) string {
	if pkg == "." {
		return ""
	}
	return pkg
}

// buildImportMap builds a map of import aliases to full import paths.
func (e *goExtractor) buildImportMap(node *ast.File) map[string]string {
	imports := make(map[string]string)

	for _, imp := range node.Imports {
		importPath := strings.Trim(imp.Path.Value, `"`)

		// Determine the import alias
		var alias string
		if imp.Name != nil {
			// Explicit alias: import foo "path/to/package"
			alias = imp.Name.Name
		} else {
			// Default alias is last component of path
			parts := strings.Split(importPath, "/")
			alias = parts[len(parts)-1]
		}

		imports[alias] = importPath
	}

	return imports
}

// extractInterfaceMembers extracts methods and embedded types from an interface.
func (e *goExtractor) extractInterfaceMembers(iface *ast.InterfaceType, imports map[string]string, pkgName string) ([]MethodSignature, []string) {
	var methods []MethodSignature
	var embeddedTypes []string

	for _, field := range iface.Methods.List {
		if len(field.Names) > 0 {
			// Named method
			for _, name := range field.Names {
				if funcType, ok := field.Type.(*ast.FuncType); ok {
					methods = append(methods, MethodSignature{
						Name:       name.Name,
						Parameters: e.extractParameters(funcType.Params, imports),
						Returns:    e.extractParameters(funcType.Results, imports),
					})
				}
			}
		} else {
			// Embedded interface
			embeddedType := e.resolveTypeRef(field.Type, imports)
			pkg := normalizePackage(embeddedType.Package)
			if pkg != "" {
				embeddedTypes = append(embeddedTypes, pkg+"."+embeddedType.Name)
			} else {
				// Same package
				embeddedTypes = append(embeddedTypes, pkgName+"."+embeddedType.Name)
			}
		}
	}

	return methods, embeddedTypes
}

// extractStructMembers extracts embedded types from a struct.
// Struct methods are extracted separately via method declarations.
func (e *goExtractor) extractStructMembers(strct *ast.StructType, imports map[string]string, pkgName string) ([]MethodSignature, []string) {
	var embeddedTypes []string

	if strct.Fields == nil {
		return nil, nil
	}

	for _, field := range strct.Fields.List {
		// Embedded field has no name
		if len(field.Names) == 0 {
			embeddedType := e.resolveTypeRef(field.Type, imports)
			pkg := normalizePackage(embeddedType.Package)
			if pkg != "" {
				embeddedTypes = append(embeddedTypes, pkg+"."+embeddedType.Name)
			} else {
				// Same package
				embeddedTypes = append(embeddedTypes, pkgName+"."+embeddedType.Name)
			}
		}
	}

	return nil, embeddedTypes
}

// extractParameters extracts parameters from a field list (params or returns).
func (e *goExtractor) extractParameters(fieldList *ast.FieldList, imports map[string]string) []Parameter {
	if fieldList == nil {
		return nil
	}

	var params []Parameter

	for _, field := range fieldList.List {
		typeRef := e.resolveTypeRef(field.Type, imports)

		// Handle multiple names with same type (e.g., a, b int)
		if len(field.Names) == 0 {
			// Unnamed parameter (common in returns)
			params = append(params, Parameter{Type: typeRef})
		} else {
			for _, name := range field.Names {
				params = append(params, Parameter{
					Name: name.Name,
					Type: typeRef,
				})
			}
		}
	}

	return params
}

// resolveTypeRef resolves a type expression to a TypeRef.
func (e *goExtractor) resolveTypeRef(expr ast.Expr, imports map[string]string) TypeRef {
	switch t := expr.(type) {
	case *ast.Ident:
		// Simple type: int, string, MyType
		return TypeRef{Name: t.Name}

	case *ast.SelectorExpr:
		// Qualified type: context.Context, http.Handler
		if ident, ok := t.X.(*ast.Ident); ok {
			pkg := imports[ident.Name] // Resolve import alias
			if pkg == "" {
				pkg = ident.Name // Use alias if package path not found
			}
			return TypeRef{
				Name:    t.Sel.Name,
				Package: normalizePackage(pkg),
			}
		}

	case *ast.StarExpr:
		// Pointer: *Type
		ref := e.resolveTypeRef(t.X, imports)
		ref.IsPointer = true
		return ref

	case *ast.ArrayType:
		// Slice or array: []Type or [N]Type
		ref := e.resolveTypeRef(t.Elt, imports)
		ref.IsSlice = true
		return ref

	case *ast.MapType:
		// Map: map[K]V (simplified - just mark as map)
		// For more precision, we'd need to store key/value types
		return TypeRef{Name: "map", IsMap: true}

	case *ast.InterfaceType:
		// Inline interface (e.g., interface{})
		return TypeRef{Name: "interface"}

	case *ast.FuncType:
		// Function type (e.g., func(int) error)
		return TypeRef{Name: "func"}

	case *ast.Ellipsis:
		// Variadic: ...Type
		ref := e.resolveTypeRef(t.Elt, imports)
		ref.IsSlice = true // Treat variadic as slice
		return ref
	}

	return TypeRef{Name: "unknown"}
}

// createTypeUsageEdge creates an EdgeUsesType edge from a function/struct to a type.
// Returns nil if the type is a built-in or the edge cannot be created.
func (e *goExtractor) createTypeUsageEdge(fromID string, typeRef TypeRef, pkgName string, file string, line int) *Edge {
	// Skip built-in types - no need to track basic types
	if isBuiltin(typeRef.Name) {
		return nil
	}

	// Build the target node ID
	var toID string
	if typeRef.Package != "" {
		// Cross-package reference (e.g., context.Context)
		toID = typeRef.Package + "." + typeRef.Name
	} else if typeRef.Name == "interface" || typeRef.Name == "func" || typeRef.Name == "map" || typeRef.Name == "unknown" {
		// Skip inline/anonymous types
		return nil
	} else {
		// Same-package reference
		toID = pkgName + "." + typeRef.Name
	}

	return &Edge{
		From: fromID,
		To:   toID,
		Type: EdgeUsesType,
		Location: &Location{
			File: file,
			Line: line,
		},
	}
}

// isBuiltin checks if a type name is a Go built-in type.
func isBuiltin(typeName string) bool {
	builtins := map[string]bool{
		"bool":       true,
		"byte":       true,
		"rune":       true,
		"int":        true,
		"int8":       true,
		"int16":      true,
		"int32":      true,
		"int64":      true,
		"uint":       true,
		"uint8":      true,
		"uint16":     true,
		"uint32":     true,
		"uint64":     true,
		"uintptr":    true,
		"float32":    true,
		"float64":    true,
		"complex64":  true,
		"complex128": true,
		"string":     true,
		"error":      true,
	}
	return builtins[typeName]
}
