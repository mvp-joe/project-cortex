package storage

import (
	"database/sql"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/mvp-joe/project-cortex/internal/graph"
)

// GraphReader reads graph data from SQLite storage.
// Provides both bulk read (complete GraphData) and granular queries (types by file, etc.).
type GraphReader struct {
	db *sql.DB
}

// NewGraphReader creates a new GraphReader for the specified database.
// Opens database in read-only mode for safety.
func NewGraphReader(dbPath string) (*GraphReader, error) {
	// Open in read-only mode
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", dbPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	return &GraphReader{db: db}, nil
}

// Close closes the database connection.
func (r *GraphReader) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}

// ReadGraphData reconstructs complete graph.GraphData from SQL tables.
// Converts SQL rows back to graph.Node and graph.Edge format for compatibility
// with existing graph tools and MCP server.
//
// Steps:
//  1. Read all types → convert to Nodes
//  2. Read all functions → convert to Nodes
//  3. Read all relationships → convert to Edges
//  4. Read all calls → convert to Edges
//  5. Build GraphData with metadata
func (r *GraphReader) ReadGraphData() (*graph.GraphData, error) {
	var nodes []graph.Node
	var edges []graph.Edge

	// Read types and convert to nodes
	types, err := r.ReadTypes()
	if err != nil {
		return nil, fmt.Errorf("failed to read types: %w", err)
	}
	for _, t := range types {
		nodes = append(nodes, convertTypeToNode(t))
	}

	// Read functions and convert to nodes
	functions, err := r.ReadFunctions()
	if err != nil {
		return nil, fmt.Errorf("failed to read functions: %w", err)
	}
	for _, fn := range functions {
		nodes = append(nodes, convertFunctionToNode(fn))
	}

	// Read relationships and convert to edges
	relationships, err := r.ReadTypeRelationships()
	if err != nil {
		return nil, fmt.Errorf("failed to read relationships: %w", err)
	}
	for _, rel := range relationships {
		edges = append(edges, convertRelationshipToEdge(rel))
	}

	// Read calls and convert to edges
	calls, err := r.ReadCallGraph()
	if err != nil {
		return nil, fmt.Errorf("failed to read call graph: %w", err)
	}
	for _, call := range calls {
		edges = append(edges, convertCallToEdge(call))
	}

	// Build metadata
	metadata := graph.GraphMetadata{
		Version:     "2.0-sql",
		GeneratedAt: time.Now(), // Note: Should ideally come from cache_metadata
		NodeCount:   len(nodes),
		EdgeCount:   len(edges),
	}

	return &graph.GraphData{
		Metadata: metadata,
		Nodes:    nodes,
		Edges:    edges,
	}, nil
}

// ReadTypes loads all type records from the database.
func (r *GraphReader) ReadTypes() ([]*Type, error) {
	rows, err := sq.Select(
		"type_id", "file_path", "module_path", "name", "kind",
		"start_line", "end_line", "is_exported", "field_count", "method_count",
	).
		From("types").
		OrderBy("file_path", "start_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query types: %w", err)
	}
	defer rows.Close()

	var types []*Type
	for rows.Next() {
		t := &Type{}
		var isExported int
		err := rows.Scan(
			&t.ID, &t.FilePath, &t.ModulePath, &t.Name, &t.Kind,
			&t.StartLine, &t.EndLine, &isExported,
			&t.FieldCount, &t.MethodCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan type: %w", err)
		}
		t.IsExported = isExported == 1
		types = append(types, t)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating types: %w", err)
	}

	return types, nil
}

// ReadTypesByFile loads types for a specific file.
func (r *GraphReader) ReadTypesByFile(filePath string) ([]*Type, error) {
	rows, err := sq.Select(
		"type_id", "file_path", "module_path", "name", "kind",
		"start_line", "end_line", "is_exported", "field_count", "method_count",
	).
		From("types").
		Where(sq.Eq{"file_path": filePath}).
		OrderBy("start_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query types by file: %w", err)
	}
	defer rows.Close()

	var types []*Type
	for rows.Next() {
		t := &Type{}
		var isExported int
		err := rows.Scan(
			&t.ID, &t.FilePath, &t.ModulePath, &t.Name, &t.Kind,
			&t.StartLine, &t.EndLine, &isExported,
			&t.FieldCount, &t.MethodCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan type: %w", err)
		}
		t.IsExported = isExported == 1
		types = append(types, t)
	}

	return types, rows.Err()
}

// ReadFunctions loads all function records from the database.
func (r *GraphReader) ReadFunctions() ([]*Function, error) {
	rows, err := sq.Select(
		"function_id", "file_path", "module_path", "name",
		"start_line", "end_line", "is_exported", "is_method",
		"receiver_type_id", "receiver_type_name",
		"param_count", "return_count",
	).
		From("functions").
		OrderBy("file_path", "start_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query functions: %w", err)
	}
	defer rows.Close()

	var functions []*Function
	for rows.Next() {
		fn := &Function{}
		var isExported, isMethod int
		err := rows.Scan(
			&fn.ID, &fn.FilePath, &fn.ModulePath, &fn.Name,
			&fn.StartLine, &fn.EndLine, &isExported, &isMethod,
			&fn.ReceiverTypeID, &fn.ReceiverTypeName,
			&fn.ParamCount, &fn.ReturnCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan function: %w", err)
		}
		fn.IsExported = isExported == 1
		fn.IsMethod = isMethod == 1
		functions = append(functions, fn)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating functions: %w", err)
	}

	return functions, nil
}

// ReadFunctionsByFile loads functions for a specific file.
func (r *GraphReader) ReadFunctionsByFile(filePath string) ([]*Function, error) {
	rows, err := sq.Select(
		"function_id", "file_path", "module_path", "name",
		"start_line", "end_line", "is_exported", "is_method",
		"receiver_type_id", "receiver_type_name",
		"param_count", "return_count",
	).
		From("functions").
		Where(sq.Eq{"file_path": filePath}).
		OrderBy("start_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query functions by file: %w", err)
	}
	defer rows.Close()

	var functions []*Function
	for rows.Next() {
		fn := &Function{}
		var isExported, isMethod int
		err := rows.Scan(
			&fn.ID, &fn.FilePath, &fn.ModulePath, &fn.Name,
			&fn.StartLine, &fn.EndLine, &isExported, &isMethod,
			&fn.ReceiverTypeID, &fn.ReceiverTypeName,
			&fn.ParamCount, &fn.ReturnCount,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan function: %w", err)
		}
		fn.IsExported = isExported == 1
		fn.IsMethod = isMethod == 1
		functions = append(functions, fn)
	}

	return functions, rows.Err()
}

// ReadCallGraph loads all function call edges from the database.
func (r *GraphReader) ReadCallGraph() ([]*FunctionCall, error) {
	rows, err := sq.Select(
		"call_id", "caller_function_id", "callee_function_id", "callee_name",
		"source_file_path", "call_line", "call_column",
	).
		From("function_calls").
		OrderBy("source_file_path", "call_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query function calls: %w", err)
	}
	defer rows.Close()

	var calls []*FunctionCall
	for rows.Next() {
		call := &FunctionCall{}
		err := rows.Scan(
			&call.ID, &call.CallerFunctionID, &call.CalleeFunctionID, &call.CalleeName,
			&call.SourceFilePath, &call.CallLine, &call.CallColumn,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan function call: %w", err)
		}
		calls = append(calls, call)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating function calls: %w", err)
	}

	return calls, nil
}

// ReadTypeRelationships loads all type relationship edges from the database.
func (r *GraphReader) ReadTypeRelationships() ([]*TypeRelationship, error) {
	rows, err := sq.Select(
		"relationship_id", "from_type_id", "to_type_id",
		"relationship_type", "source_file_path", "source_line",
	).
		From("type_relationships").
		OrderBy("source_file_path", "source_line").
		RunWith(r.db).
		Query()
	if err != nil {
		return nil, fmt.Errorf("failed to query type relationships: %w", err)
	}
	defer rows.Close()

	var relationships []*TypeRelationship
	for rows.Next() {
		rel := &TypeRelationship{}
		err := rows.Scan(
			&rel.ID, &rel.FromTypeID, &rel.ToTypeID,
			&rel.RelationshipType, &rel.SourceFilePath, &rel.SourceLine,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan type relationship: %w", err)
		}
		relationships = append(relationships, rel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating type relationships: %w", err)
	}

	return relationships, nil
}

// Conversion functions: SQL structs → graph.Node/graph.Edge

func convertTypeToNode(t *Type) graph.Node {
	kind := graph.NodeInterface
	switch t.Kind {
	case "struct":
		kind = graph.NodeStruct
	case "interface":
		kind = graph.NodeInterface
	}

	return graph.Node{
		ID:        t.ID,
		Kind:      kind,
		File:      t.FilePath,
		StartLine: t.StartLine,
		EndLine:   t.EndLine,
		Methods:   []graph.MethodSignature{}, // Would need to query type_fields
	}
}

func convertFunctionToNode(fn *Function) graph.Node {
	kind := graph.NodeFunction
	if fn.IsMethod {
		kind = graph.NodeMethod
	}

	return graph.Node{
		ID:        fn.ID,
		Kind:      kind,
		File:      fn.FilePath,
		StartLine: fn.StartLine,
		EndLine:   fn.EndLine,
		Methods:   []graph.MethodSignature{}, // Would need to query function_parameters
	}
}

func convertRelationshipToEdge(rel *TypeRelationship) graph.Edge {
	edgeType := graph.EdgeImplements
	if rel.RelationshipType == "embeds" {
		edgeType = graph.EdgeEmbeds
	}

	return graph.Edge{
		From: rel.FromTypeID,
		To:   rel.ToTypeID,
		Type: edgeType,
		Location: &graph.Location{
			File: rel.SourceFilePath,
			Line: rel.SourceLine,
		},
	}
}

func convertCallToEdge(call *FunctionCall) graph.Edge {
	to := call.CalleeName
	if call.CalleeFunctionID != nil {
		to = *call.CalleeFunctionID
	}

	edge := graph.Edge{
		From: call.CallerFunctionID,
		To:   to,
		Type: graph.EdgeCalls,
		Location: &graph.Location{
			File: call.SourceFilePath,
			Line: call.CallLine,
		},
	}

	if call.CallColumn != nil {
		edge.Location.Column = *call.CallColumn
	}

	return edge
}

// CallGraphNode represents a node in the in-memory call graph structure.
type CallGraphNode struct {
	ID       string
	Name     string
	FilePath string
	Callers  []*CallGraphEdge
	Callees  []*CallGraphEdge
}

// CallGraphEdge represents an edge in the in-memory call graph structure.
type CallGraphEdge struct {
	From string
	To   string
	Line int
}

// BuildCallGraph builds an in-memory graph structure optimized for traversal algorithms.
// Use this for BFS/DFS, finding all callers, finding all callees, cycle detection, etc.
//
// Example usage:
//
//	callGraph, _ := reader.BuildCallGraph()
//	callers := findAllCallers(callGraph, "embed.Provider.Embed", 10)
func (r *GraphReader) BuildCallGraph() (map[string]*CallGraphNode, error) {
	// Load all functions
	functions, err := r.ReadFunctions()
	if err != nil {
		return nil, fmt.Errorf("failed to read functions: %w", err)
	}

	// Initialize nodes map
	nodes := make(map[string]*CallGraphNode)
	for _, fn := range functions {
		nodes[fn.ID] = &CallGraphNode{
			ID:       fn.ID,
			Name:     fn.Name,
			FilePath: fn.FilePath,
			Callers:  []*CallGraphEdge{},
			Callees:  []*CallGraphEdge{},
		}
	}

	// Load all call edges
	calls, err := r.ReadCallGraph()
	if err != nil {
		return nil, fmt.Errorf("failed to read call graph: %w", err)
	}

	// Build bidirectional edges
	for _, call := range calls {
		// Skip external calls (callee_function_id is NULL)
		if call.CalleeFunctionID == nil {
			continue
		}

		edge := &CallGraphEdge{
			From: call.CallerFunctionID,
			To:   *call.CalleeFunctionID,
			Line: call.CallLine,
		}

		// Add to caller's callees
		if caller, ok := nodes[call.CallerFunctionID]; ok {
			caller.Callees = append(caller.Callees, edge)
		}

		// Add to callee's callers
		if callee, ok := nodes[*call.CalleeFunctionID]; ok {
			callee.Callers = append(callee.Callers, edge)
		}
	}

	return nodes, nil
}

// FindAllCallers performs BFS traversal to find all functions that call the target,
// up to maxDepth levels up the call chain.
func FindAllCallers(callGraph map[string]*CallGraphNode, targetID string, maxDepth int) []*CallGraphNode {
	visited := make(map[string]bool)
	var results []*CallGraphNode

	var traverse func(id string, depth int)
	traverse = func(id string, depth int) {
		if depth > maxDepth || visited[id] {
			return
		}
		visited[id] = true

		node := callGraph[id]
		if node == nil {
			return
		}

		// Add node to results (exclude the target itself)
		if id != targetID {
			results = append(results, node)
		}

		// Recurse on callers
		for _, edge := range node.Callers {
			traverse(edge.From, depth+1)
		}
	}

	traverse(targetID, 0)
	return results
}

// FindAllCallees performs BFS traversal to find all functions called by the target,
// up to maxDepth levels down the call chain.
func FindAllCallees(callGraph map[string]*CallGraphNode, targetID string, maxDepth int) []*CallGraphNode {
	visited := make(map[string]bool)
	var results []*CallGraphNode

	var traverse func(id string, depth int)
	traverse = func(id string, depth int) {
		if depth > maxDepth || visited[id] {
			return
		}
		visited[id] = true

		node := callGraph[id]
		if node == nil {
			return
		}

		// Add node to results (exclude the target itself)
		if id != targetID {
			results = append(results, node)
		}

		// Recurse on callees
		for _, edge := range node.Callees {
			traverse(edge.To, depth+1)
		}
	}

	traverse(targetID, 0)
	return results
}
