package graph

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// sqlSearcher implements Searcher interface using direct SQL queries.
// Unlike the in-memory searcher, it has no state beyond the DB connection
// and context extractor, making it lightweight and always up-to-date.
type sqlSearcher struct {
	db      *sql.DB
	context *ContextExtractor // Shared context extraction (no cache)
}

// NewSQLSearcher creates a new SQL-based graph searcher.
// The rootDir parameter is accepted for interface compatibility but not used
// since context extraction queries SQLite directly.
func NewSQLSearcher(db *sql.DB, rootDir string) (Searcher, error) {
	if db == nil {
		return nil, fmt.Errorf("db cannot be nil")
	}

	return &sqlSearcher{
		db:      db,
		context: NewContextExtractor(db),
	}, nil
}

// Query executes a graph query and returns results.
// All queries are wrapped in read-only transactions for consistent snapshots.
func (s *sqlSearcher) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error) {
	// Start read-only transaction for consistent snapshot
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() // Safe to call even after commit

	// Sanitize and enforce depth limits
	const DefaultDepth = 3
	const MaxDepth = 6
	if req.Depth <= 0 {
		req.Depth = DefaultDepth
	}
	if req.Depth > MaxDepth {
		return nil, fmt.Errorf("depth %d exceeds maximum %d", req.Depth, MaxDepth)
	}

	start := time.Now()

	var resp *QueryResponse

	switch req.Operation {
	case OperationCallers:
		resp, err = s.queryCallers(ctx, tx, req)
	case OperationCallees:
		resp, err = s.queryCallees(ctx, tx, req)
	case OperationDependencies:
		resp, err = s.queryDependencies(ctx, tx, req)
	case OperationDependents:
		resp, err = s.queryDependents(ctx, tx, req)
	case OperationTypeUsages:
		resp, err = s.queryTypeUsages(ctx, tx, req)
	case OperationImplementations:
		resp, err = s.queryImplementations(ctx, tx, req)
	case OperationPath:
		resp, err = s.queryPath(ctx, tx, req)
	case OperationImpact:
		resp, err = s.queryImpact(ctx, tx, req)
	default:
		return nil, fmt.Errorf("unsupported operation: %s", req.Operation)
	}

	if err != nil {
		return nil, err
	}

	resp.Metadata.TookMs = int(time.Since(start).Milliseconds())
	resp.Metadata.Source = "graph"
	return resp, nil
}

// Reload reloads the graph from storage.
// No-op for SQL searcher since database is always current.
func (s *sqlSearcher) Reload(ctx context.Context) error {
	// No-op: SQL is always current
	return nil
}

// Close releases resources.
// No-op for SQL searcher since it doesn't own the database connection.
func (s *sqlSearcher) Close() error {
	// No resources to clean up (db owned by caller)
	return nil
}

// queryCallers finds all functions that call the target.
func (s *sqlSearcher) queryCallers(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	sql, args := s.buildCallersSQL(req.Target, req.Depth, req.MaxResults, req)
	return s.executeFunctionQuery(ctx, tx, sql, args, req)
}

// queryCallees finds all functions called by the target.
func (s *sqlSearcher) queryCallees(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	sql, args := s.buildCalleesSQL(req.Target, req.Depth, req.MaxResults, req)
	return s.executeFunctionQuery(ctx, tx, sql, args, req)
}

// queryDependencies finds all packages imported by the target package.
func (s *sqlSearcher) queryDependencies(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	sql, args := s.buildDependenciesSQL(req.Target, req.MaxResults)
	return s.executeDependencyQuery(ctx, tx, sql, args, req)
}

// queryDependents finds all packages that import the target package.
func (s *sqlSearcher) queryDependents(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	sql, args := s.buildDependentsSQL(req.Target, req.MaxResults)
	return s.executeDependencyQuery(ctx, tx, sql, args, req)
}

// queryTypeUsages finds all locations where the target type is used.
func (s *sqlSearcher) queryTypeUsages(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	sql, args := s.buildTypeUsagesSQL(req.Target, req.MaxResults, req)
	return s.executeFunctionQuery(ctx, tx, sql, args, req)
}

// queryImplementations finds all types that implement the target interface.
func (s *sqlSearcher) queryImplementations(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	sql, args := s.buildImplementationsSQL(req.Target, req.MaxResults, req)
	return s.executeTypeQuery(ctx, tx, sql, args, req)
}

// PathEdge represents a directed edge in the call graph for pathfinding.
// This is a lightweight structure used only for BFS traversal.
type PathEdge struct {
	From string
	To   string
}

// loadReachableEdges loads all edges reachable from startID within maxDepth
func (s *sqlSearcher) loadReachableEdges(ctx context.Context, tx *sql.Tx, startID string, maxDepth int) ([]PathEdge, error) {
	query := `
		WITH RECURSIVE reachable(function_id, depth) AS (
			SELECT ?, 0
			UNION ALL
			SELECT fc.callee_function_id, r.depth + 1
			FROM reachable r
			JOIN function_calls fc ON r.function_id = fc.caller_function_id
			WHERE r.depth < ? AND fc.callee_function_id IS NOT NULL
		)
		SELECT DISTINCT fc.caller_function_id, fc.callee_function_id
		FROM reachable r
		JOIN function_calls fc ON r.function_id = fc.caller_function_id
		WHERE fc.callee_function_id IS NOT NULL
	`

	rows, err := tx.QueryContext(ctx, query, startID, maxDepth)
	if err != nil {
		return nil, fmt.Errorf("load edges: %w", err)
	}
	defer rows.Close()

	var edges []PathEdge
	for rows.Next() {
		var edge PathEdge
		if err := rows.Scan(&edge.From, &edge.To); err != nil {
			return nil, fmt.Errorf("scan edge: %w", err)
		}
		edges = append(edges, edge)
	}

	return edges, nil
}

// PathNode represents a node in BFS traversal
type PathNode struct {
	ID   string
	Path []string
}

// bfsPath finds shortest path using BFS on in-memory graph
func (s *sqlSearcher) bfsPath(start, end string, maxDepth int, graph map[string][]string) []string {
	visited := make(map[string]bool)
	queue := []PathNode{{ID: start, Path: []string{start}}}
	visited[start] = true

	for len(queue) > 0 && len(queue[0].Path) <= maxDepth {
		current := queue[0]
		queue = queue[1:]

		if current.ID == end {
			return current.Path
		}

		for _, next := range graph[current.ID] {
			if !visited[next] {
				visited[next] = true
				newPath := append([]string{}, current.Path...)
				newPath = append(newPath, next)
				queue = append(queue, PathNode{ID: next, Path: newPath})
			}
		}
	}

	return nil // No path found
}

// buildPathResponse fetches function details for each node in the path
func (s *sqlSearcher) buildPathResponse(ctx context.Context, tx *sql.Tx, path []string, req *QueryRequest) (*QueryResponse, error) {
	results := []QueryResult{}

	for i, functionID := range path {
		// Fetch function details
		var node Node
		var isMethod bool
		var name, modulePath string
		var receiverName sql.NullString

		err := tx.QueryRowContext(ctx, `
			SELECT function_id, file_path, start_line, end_line,
				   start_pos, end_pos, name, module_path, is_method, receiver_type_name
			FROM functions WHERE function_id = ?
		`, functionID).Scan(
			&node.ID, &node.File, &node.StartLine, &node.EndLine,
			&node.StartPos, &node.EndPos, &name, &modulePath,
			&isMethod, &receiverName,
		)
		if err != nil {
			continue // Skip if function not found
		}

		if isMethod {
			node.Kind = NodeMethod
			if receiverName.Valid {
				node.ID = receiverName.String + "." + name
			}
		} else {
			node.Kind = NodeFunction
		}

		result := QueryResult{Node: &node, Depth: i}

		// Add context if requested
		if req.IncludeContext {
			contextStr, _ := s.context.ExtractContext(
				node.File,
				LineRange{Start: node.StartLine, End: node.EndLine},
				ByteRange{Start: node.StartPos, End: node.EndPos},
				req.ContextLines,
			)
			result.Context = contextStr
		}

		results = append(results, result)
	}

	return &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(results),
		TotalReturned: len(results),
	}, nil
}

// queryPath finds the shortest call path from target to destination.
func (s *sqlSearcher) queryPath(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	if req.To == "" {
		return nil, fmt.Errorf("path query requires 'to' parameter")
	}

	// Load entire reachable subgraph in ONE query
	edges, err := s.loadReachableEdges(ctx, tx, req.Target, req.Depth)
	if err != nil {
		return nil, err
	}

	// Build adjacency map
	graph := make(map[string][]string)
	for _, edge := range edges {
		graph[edge.From] = append(graph[edge.From], edge.To)
	}

	// BFS to find shortest path
	path := s.bfsPath(req.Target, req.To, req.Depth, graph)
	if path == nil {
		return &QueryResponse{
			Operation:  string(req.Operation),
			Target:     req.Target,
			Results:    []QueryResult{},
			Suggestion: fmt.Sprintf("No path from %s to %s within depth %d", req.Target, req.To, req.Depth),
		}, nil
	}

	// Build response with full node details
	return s.buildPathResponse(ctx, tx, path, req)
}

// queryImpact analyzes the blast radius of changing a function/interface.
// Implements three-phase analysis: implementations + direct callers + transitive callers.
func (s *sqlSearcher) queryImpact(ctx context.Context, tx *sql.Tx, req *QueryRequest) (*QueryResponse, error) {
	allResults := []QueryResult{}
	summary := &ImpactSummary{}

	// Phase 1: Find implementations (for interfaces)
	implReq := &QueryRequest{
		Operation:      OperationImplementations,
		Target:         req.Target,
		MaxResults:     req.MaxResults,
		IncludeContext: req.IncludeContext,
		ContextLines:   req.ContextLines,
	}
	implResp, _ := s.queryImplementations(ctx, tx, implReq)
	summary.Implementations = len(implResp.Results)
	for _, r := range implResp.Results {
		r.ImpactType = "implementation"
		r.Severity = "must_update"
		allResults = append(allResults, r)
	}

	// Phase 2: Find direct callers (depth 1)
	callersReq := &QueryRequest{
		Operation:      OperationCallers,
		Target:         req.Target,
		Depth:          1,
		MaxResults:     req.MaxResults,
		IncludeContext: req.IncludeContext,
		ContextLines:   req.ContextLines,
	}
	callersResp, _ := s.queryCallers(ctx, tx, callersReq)
	summary.DirectCallers = len(callersResp.Results)
	for _, r := range callersResp.Results {
		r.ImpactType = "direct_caller"
		r.Severity = "must_update"
		allResults = append(allResults, r)
	}

	// Phase 3: Find transitive callers (depth 2+)
	if req.Depth > 1 {
		transitiveReq := &QueryRequest{
			Operation:      OperationCallers,
			Target:         req.Target,
			Depth:          req.Depth,
			MaxResults:     req.MaxResults,
			IncludeContext: req.IncludeContext,
			ContextLines:   req.ContextLines,
		}
		transitiveResp, _ := s.queryCallers(ctx, tx, transitiveReq)
		for _, r := range transitiveResp.Results {
			if r.Depth > 1 {
				r.ImpactType = "transitive"
				r.Severity = "review_needed"
				allResults = append(allResults, r)
				summary.TransitiveCallers++
			}
		}
	}

	return &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       allResults,
		Summary:       summary,
		TotalFound:    len(allResults),
		TotalReturned: len(allResults),
		Truncated:     len(allResults) >= req.MaxResults,
	}, nil
}

// buildCallersSQL builds the SQL query for finding all functions that call the target.
// For depth 1, uses simple JOIN. For depth > 1, uses recursive CTE.
func (s *sqlSearcher) buildCallersSQL(target string, depth int, limit int, req *QueryRequest) (string, []interface{}) {
	if depth == 1 {
		// Depth 1: Simple JOIN for direct callers
		sql := `
			SELECT DISTINCT
				f.function_id, f.file_path, f.start_line, f.end_line,
				f.start_pos, f.end_pos,
				f.name, f.module_path, f.is_method, f.receiver_type_name,
				1 as depth
			FROM function_calls fc
			JOIN functions f ON fc.caller_function_id = f.function_id
			WHERE (fc.callee_function_id = ? OR fc.callee_name = ?)
		`
		args := []interface{}{target, target}
		sql = s.applyFilters(sql, req, &args)
		sql += " LIMIT ?"
		args = append(args, limit)
		return sql, args
	}

	// Depth N: WITH RECURSIVE CTE for transitive callers
	sql := `
		WITH RECURSIVE caller_chain(function_id, depth) AS (
			-- Base case: direct callers
			SELECT DISTINCT
				fc.caller_function_id,
				1
			FROM function_calls fc
			WHERE fc.callee_function_id = ? OR fc.callee_name = ?

			UNION ALL

			-- Recursive case: callers of callers
			SELECT DISTINCT
				fc.caller_function_id,
				cc.depth + 1
			FROM caller_chain cc
			JOIN function_calls fc ON cc.function_id = fc.callee_function_id
			WHERE cc.depth < ?
		)
		SELECT DISTINCT
			f.function_id, f.file_path, f.start_line, f.end_line,
			f.start_pos, f.end_pos,
			f.name, f.module_path, f.is_method, f.receiver_type_name,
			cc.depth
		FROM caller_chain cc
		JOIN functions f ON cc.function_id = f.function_id
	`
	args := []interface{}{target, target, depth}
	sql = s.applyFilters(sql, req, &args)
	sql += " ORDER BY cc.depth, f.function_id LIMIT ?"
	args = append(args, limit)
	return sql, args
}

// buildCalleesSQL builds the SQL query for finding all functions called by the target.
// For depth 1, uses simple JOIN. For depth > 1, uses recursive CTE.
func (s *sqlSearcher) buildCalleesSQL(target string, depth int, limit int, req *QueryRequest) (string, []interface{}) {
	if depth == 1 {
		// Depth 1: Simple JOIN for direct callees
		sql := `
			SELECT DISTINCT
				f.function_id, f.file_path, f.start_line, f.end_line,
				f.start_pos, f.end_pos,
				f.name, f.module_path, f.is_method, f.receiver_type_name,
				1 as depth
			FROM function_calls fc
			JOIN functions f ON fc.callee_function_id = f.function_id
			WHERE fc.caller_function_id = ?
		`
		args := []interface{}{target}
		sql = s.applyFilters(sql, req, &args)
		sql += " LIMIT ?"
		args = append(args, limit)
		return sql, args
	}

	// Depth N: WITH RECURSIVE CTE for transitive callees
	sql := `
		WITH RECURSIVE callee_chain(function_id, depth) AS (
			-- Base case: direct callees
			SELECT DISTINCT
				fc.callee_function_id,
				1
			FROM function_calls fc
			WHERE fc.caller_function_id = ?

			UNION ALL

			-- Recursive case: callees of callees
			SELECT DISTINCT
				fc.callee_function_id,
				cc.depth + 1
			FROM callee_chain cc
			JOIN function_calls fc ON cc.function_id = fc.caller_function_id
			WHERE cc.depth < ?
		)
		SELECT DISTINCT
			f.function_id, f.file_path, f.start_line, f.end_line,
			f.start_pos, f.end_pos,
			f.name, f.module_path, f.is_method, f.receiver_type_name,
			cc.depth
		FROM callee_chain cc
		JOIN functions f ON cc.function_id = f.function_id
	`
	args := []interface{}{target, depth}
	sql = s.applyFilters(sql, req, &args)
	sql += " ORDER BY cc.depth, f.function_id LIMIT ?"
	args = append(args, limit)
	return sql, args
}

// buildDependenciesSQL constructs the SQL query for finding package dependencies.
// Direct lookup (depth 1 only) - returns all imports from the target package.
func (s *sqlSearcher) buildDependenciesSQL(target string, limit int) (string, []interface{}) {
	query := `
		SELECT DISTINCT i.import_path, i.file_path, i.import_line
		FROM imports i
		JOIN files f ON i.file_path = f.file_path
		WHERE f.module_path = ? OR i.file_path = ?
		ORDER BY i.import_path
		LIMIT ?
	`
	return query, []interface{}{target, target, limit}
}

// buildDependentsSQL constructs the SQL query for finding package dependents.
// Reverse lookup (depth 1 only) - returns all packages that import the target.
func (s *sqlSearcher) buildDependentsSQL(target string, limit int) (string, []interface{}) {
	query := `
		SELECT DISTINCT f.module_path, i.file_path, i.import_line
		FROM imports i
		JOIN files f ON i.file_path = f.file_path
		WHERE i.import_path = ?
		ORDER BY f.module_path
		LIMIT ?
	`
	return query, []interface{}{target, limit}
}

// buildImplementationsSQL constructs the SQL query for finding type implementations.
// Uses pre-inferred relationships from type_relationships table.
// Includes byte positions (start_pos, end_pos) for context extraction.
func (s *sqlSearcher) buildImplementationsSQL(target string, limit int, req *QueryRequest) (string, []interface{}) {
	sql := `
		SELECT DISTINCT
			t.type_id, t.file_path, t.start_line, t.end_line,
			t.start_pos, t.end_pos,
			t.name, t.module_path, t.kind
		FROM type_relationships tr
		JOIN types t ON tr.from_type_id = t.type_id
		WHERE tr.to_type_id = ?
		  AND tr.relationship_type = 'implements'
	`
	args := []interface{}{target}
	sql = s.applyFilters(sql, req, &args)
	sql += " ORDER BY t.type_id LIMIT ?"
	args = append(args, limit)
	return sql, args
}

// buildTypeUsagesSQL constructs the SQL query for finding type usages.
// Text-based parameter/field matching with pattern support via LIKE.
// Includes byte positions (start_pos, end_pos) for context extraction.
//
// Pattern matching behavior:
//   - Exact: target = "User" → finds exact User type only
//   - Pattern: target = "%User%" → finds *User, []User, map[string]User, etc.
//   - Generics: target = "%[User]%" → finds PaginatedResult[User]
func (s *sqlSearcher) buildTypeUsagesSQL(typePattern string, limit int, req *QueryRequest) (string, []interface{}) {
	sql := `
		SELECT DISTINCT
			f.function_id, f.file_path, f.start_line, f.end_line,
			f.start_pos, f.end_pos,
			f.name, f.module_path, f.is_method, f.receiver_type_name,
			1 as depth
		FROM functions f
		JOIN function_parameters fp ON f.function_id = fp.function_id
		WHERE fp.param_type LIKE ?
	`
	args := []interface{}{typePattern}
	sql = s.applyFilters(sql, req, &args)
	sql += " ORDER BY f.function_id LIMIT ?"
	args = append(args, limit)
	return sql, args
}

// scanTypeRow scans a type row from the database into a Node struct.
func (s *sqlSearcher) scanTypeRow(rows *sql.Rows) (*Node, error) {
	var node Node
	var kind string
	var name, modulePath string

	err := rows.Scan(
		&node.ID, &node.File, &node.StartLine, &node.EndLine,
		&node.StartPos, &node.EndPos,
		&name, &modulePath, &kind,
	)
	if err != nil {
		return nil, fmt.Errorf("scan row: %w", err)
	}

	node.Kind = NodeKind(kind)
	// ID is already set from database (type_id column)
	// For display purposes, the ID should be the fully qualified name
	// which is what's in the database
	return &node, nil
}

// executeTypeQuery executes a type-based query and returns results with optional context.
func (s *sqlSearcher) executeTypeQuery(ctx context.Context, tx *sql.Tx, sql string, args []interface{}, req *QueryRequest) (*QueryResponse, error) {
	rows, err := tx.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	results := []QueryResult{}
	for rows.Next() {
		node, err := s.scanTypeRow(rows)
		if err != nil {
			return nil, err
		}

		result := QueryResult{Node: node, Depth: 1}

		// Add context if requested
		if req.IncludeContext {
			contextStr, err := s.context.ExtractContext(
				node.File,
				LineRange{Start: node.StartLine, End: node.EndLine},
				ByteRange{Start: node.StartPos, End: node.EndPos},
				req.ContextLines,
			)
			if err == nil {
				result.Context = contextStr
			}
			// Gracefully skip context on error (log warning in production)
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(results),
		TotalReturned: len(results),
		Truncated:     len(results) >= req.MaxResults,
	}, nil
}

// scanFunctionRow scans a function row from query results, reading byte positions.
func (s *sqlSearcher) scanFunctionRow(rows *sql.Rows) (*Node, int, error) {
	var node Node
	var depth int
	var isMethod bool
	var name, modulePath string
	var receiverName sql.NullString

	err := rows.Scan(
		&node.ID, &node.File, &node.StartLine, &node.EndLine,
		&node.StartPos, &node.EndPos, // Read byte positions
		&name, &modulePath, &isMethod, &receiverName, &depth,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("scan row: %w", err)
	}

	if isMethod {
		node.Kind = NodeMethod
		if receiverName.Valid {
			node.ID = receiverName.String + "." + name
		}
	} else {
		node.Kind = NodeFunction
	}

	return &node, depth, nil
}

// executeFunctionQuery executes a function query and returns results with optional context.
func (s *sqlSearcher) executeFunctionQuery(ctx context.Context, tx *sql.Tx, sql string, args []interface{}, req *QueryRequest) (*QueryResponse, error) {
	rows, err := tx.QueryContext(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	results := []QueryResult{}
	for rows.Next() {
		node, depth, err := s.scanFunctionRow(rows)
		if err != nil {
			return nil, err
		}

		result := QueryResult{Node: node, Depth: depth}

		// Add context if requested
		if req.IncludeContext {
			contextStr, err := s.context.ExtractContext(
				node.File,
				LineRange{Start: node.StartLine, End: node.EndLine},
				ByteRange{Start: node.StartPos, End: node.EndPos},
				req.ContextLines,
			)
			if err == nil {
				result.Context = contextStr
			}
			// Gracefully skip context on error (log warning in production)
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(results),
		TotalReturned: len(results),
		Truncated:     len(results) >= req.MaxResults,
	}, nil
}

// applyFilters adds file path filtering to SQL queries.
// Appends WHERE clauses for Scope and ExcludePatterns to the given SQL string.
// Modifies the args slice in place to include filter parameters.
func (s *sqlSearcher) applyFilters(sql string, req *QueryRequest, args *[]interface{}) string {
	if req.Scope != "" {
		sql += " AND file_path LIKE ?"
		*args = append(*args, req.Scope)
	}

	for _, pattern := range req.ExcludePatterns {
		sql += " AND file_path NOT LIKE ?"
		*args = append(*args, pattern)
	}

	return sql
}

// executeDependencyQuery executes SQL for dependency/dependent queries.
// Returns package nodes with import information.
func (s *sqlSearcher) executeDependencyQuery(ctx context.Context, tx *sql.Tx, query string, args []interface{}, req *QueryRequest) (*QueryResponse, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer rows.Close()

	results := []QueryResult{}
	for rows.Next() {
		var importPath, filePath string
		var importLine int

		err := rows.Scan(&importPath, &filePath, &importLine)
		if err != nil {
			return nil, fmt.Errorf("scan row: %w", err)
		}

		// Create node for dependency
		node := &Node{
			ID:        importPath,
			File:      filePath,
			StartLine: importLine,
			EndLine:   importLine,
			Kind:      NodePackage,
		}

		result := QueryResult{Node: node, Depth: 1}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}

	return &QueryResponse{
		Operation:     string(req.Operation),
		Target:        req.Target,
		Results:       results,
		TotalFound:    len(results),
		TotalReturned: len(results),
		Truncated:     len(results) >= req.MaxResults,
	}, nil
}
