package graph

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"time"
)

// GraphProgressReporter reports progress during graph building.
type GraphProgressReporter interface {
	OnGraphBuildingStart(totalFiles int)
	OnGraphFileProcessed(processedFiles, totalFiles int, fileName string)
	OnGraphBuildingComplete(nodeCount, edgeCount int, duration time.Duration)
}

// Builder builds graph data from source files.
type Builder interface {
	// BuildFull builds the complete graph from all Go files.
	BuildFull(ctx context.Context, files []string) (*GraphData, error)

	// BuildIncremental updates the graph for changed files only.
	BuildIncremental(ctx context.Context, previousGraph *GraphData, changedFiles, deletedFiles []string, allFiles []string) (*GraphData, error)
}

// builder implements Builder.
type builder struct {
	extractor Extractor
	rootDir   string
	progress  GraphProgressReporter
}

// BuilderOption configures a Builder.
type BuilderOption func(*builder)

// WithProgress configures progress reporting.
func WithProgress(progress GraphProgressReporter) BuilderOption {
	return func(b *builder) {
		b.progress = progress
	}
}

// NewBuilder creates a new graph builder.
func NewBuilder(rootDir string, opts ...BuilderOption) Builder {
	b := &builder{
		extractor: NewExtractor(rootDir),
		rootDir:   rootDir,
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// BuildFull builds the complete graph from all Go files.
func (b *builder) BuildFull(ctx context.Context, files []string) (*GraphData, error) {
	startTime := time.Now()

	var allNodes []Node
	var allEdges []Edge

	// Count Go files for progress tracking
	goFiles := 0
	for _, file := range files {
		if filepath.Ext(file) == ".go" {
			goFiles++
		}
	}

	// Report start
	if b.progress != nil {
		b.progress.OnGraphBuildingStart(goFiles)
	}

	processed := 0
	// Extract graph data from each file
	for _, file := range files {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Only process Go files in Phase 1
		if filepath.Ext(file) != ".go" {
			continue
		}

		fileData, err := b.extractor.ExtractFile(file)
		if err != nil {
			log.Printf("Warning: failed to extract graph from %s: %v\n", file, err)
			// Report progress even on error
			processed++
			if b.progress != nil {
				b.progress.OnGraphFileProcessed(processed, goFiles, filepath.Base(file))
			}
			continue
		}

		allNodes = append(allNodes, fileData.Nodes...)
		allEdges = append(allEdges, fileData.Edges...)

		// Report progress
		processed++
		if b.progress != nil {
			b.progress.OnGraphFileProcessed(processed, goFiles, filepath.Base(file))
		}
	}

	// Deduplicate nodes by ID (prefer non-test files)
	uniqueNodes := b.deduplicateNodes(allNodes)

	// Phase 2: Interface matching
	implEdges := b.resolveInterfaceEmbeddings(uniqueNodes)
	allEdges = append(allEdges, implEdges...)

	log.Printf("Found %d interface implementations", len(implEdges))

	graphData := &GraphData{
		Nodes: uniqueNodes,
		Edges: allEdges,
	}

	// Report completion
	if b.progress != nil {
		duration := time.Since(startTime)
		b.progress.OnGraphBuildingComplete(len(uniqueNodes), len(allEdges), duration)
	}

	return graphData, nil
}

// BuildIncremental updates the graph for changed files only.
func (b *builder) BuildIncremental(ctx context.Context, previousGraph *GraphData, changedFiles, deletedFiles []string, allFiles []string) (*GraphData, error) {
	if previousGraph == nil {
		// No previous graph, do full build
		return b.BuildFull(ctx, allFiles)
	}

	startTime := time.Now()

	// Build set of changed/deleted file paths (relative)
	changedSet := make(map[string]bool)
	for _, file := range changedFiles {
		relPath, _ := filepath.Rel(b.rootDir, file)
		changedSet[relPath] = true
	}
	for _, file := range deletedFiles {
		relPath, _ := filepath.Rel(b.rootDir, file)
		changedSet[relPath] = true
	}

	// Filter out nodes and edges from changed/deleted files
	preservedNodes := []Node{}
	for _, node := range previousGraph.Nodes {
		if !changedSet[node.File] {
			preservedNodes = append(preservedNodes, node)
		}
	}

	preservedEdges := []Edge{}
	for _, edge := range previousGraph.Edges {
		// Only preserve edge if its source file is unchanged
		if !changedSet[edge.Location.File] {
			preservedEdges = append(preservedEdges, edge)
		}
	}

	// Count Go files for progress tracking
	goFiles := 0
	for _, file := range changedFiles {
		if filepath.Ext(file) == ".go" {
			goFiles++
		}
	}

	// Report start
	if b.progress != nil {
		b.progress.OnGraphBuildingStart(goFiles)
	}

	// Extract graph data from changed files
	var newNodes []Node
	var newEdges []Edge

	processed := 0
	for _, file := range changedFiles {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Only process Go files
		if filepath.Ext(file) != ".go" {
			continue
		}

		fileData, err := b.extractor.ExtractFile(file)
		if err != nil {
			log.Printf("Warning: failed to extract graph from %s: %v\n", file, err)
			// Report progress even on error
			processed++
			if b.progress != nil {
				b.progress.OnGraphFileProcessed(processed, goFiles, filepath.Base(file))
			}
			continue
		}

		newNodes = append(newNodes, fileData.Nodes...)
		newEdges = append(newEdges, fileData.Edges...)

		// Report progress
		processed++
		if b.progress != nil {
			b.progress.OnGraphFileProcessed(processed, goFiles, filepath.Base(file))
		}
	}

	// Merge preserved and new data
	allNodes := append(preservedNodes, newNodes...)
	allEdges := append(preservedEdges, newEdges...)

	// Deduplicate nodes (prefer non-test files)
	uniqueNodes := b.deduplicateNodes(allNodes)

	// Phase 2: Interface matching (incremental)
	matcher := b.resolveInterfaceEmbeddingsForIncremental(uniqueNodes)

	// Step 2: Determine which interfaces/structs changed
	changedInterfaceIDs := []string{}
	changedStructIDs := []string{}

	for _, node := range newNodes {
		if node.Kind == NodeInterface {
			changedInterfaceIDs = append(changedInterfaceIDs, node.ID)
		} else if node.Kind == NodeStruct {
			changedStructIDs = append(changedStructIDs, node.ID)
		}
	}

	// Build sets for O(1) lookup instead of O(n) search
	// Performance: This changes O(n²) to O(n) complexity
	changedStructSet := make(map[string]bool, len(changedStructIDs))
	for _, id := range changedStructIDs {
		changedStructSet[id] = true
	}

	changedInterfaceSet := make(map[string]bool, len(changedInterfaceIDs))
	for _, id := range changedInterfaceIDs {
		changedInterfaceSet[id] = true
	}

	// Remove old implementation edges for changed entities
	// Performance: O(n) instead of O(n²) by using map lookups
	filteredEdges := []Edge{}
	for _, edge := range allEdges {
		if edge.Type == EdgeImplements {
			// O(1) lookup instead of O(n) linear search
			if changedStructSet[edge.From] || changedInterfaceSet[edge.To] {
				continue // Skip old implementation edge
			}
		}
		filteredEdges = append(filteredEdges, edge)
	}

	// Re-infer implementations for changed entities
	implEdges := matcher.InferImplementationsIncremental(changedInterfaceIDs, changedStructIDs)
	filteredEdges = append(filteredEdges, implEdges...)

	log.Printf("Found %d interface implementations (incremental)", len(implEdges))

	// Build set of remaining node IDs for dangling edge detection
	remainingNodeIDs := make(map[string]bool)
	for _, node := range uniqueNodes {
		remainingNodeIDs[node.ID] = true
	}

	// Filter edges to only those where both From and To nodes exist
	validEdges := []Edge{}
	for _, edge := range filteredEdges {
		// For implements edges, allow missing interface nodes (could be in stdlib)
		if edge.Type == EdgeImplements {
			if remainingNodeIDs[edge.From] {
				validEdges = append(validEdges, edge)
			}
		} else {
			if remainingNodeIDs[edge.From] && remainingNodeIDs[edge.To] {
				validEdges = append(validEdges, edge)
			}
		}
	}

	log.Printf("Graph update: kept %d nodes, removed %d nodes, added %d new nodes",
		len(preservedNodes), len(previousGraph.Nodes)-len(preservedNodes), len(newNodes))

	graphData := &GraphData{
		Nodes: uniqueNodes,
		Edges: validEdges,
	}

	// Report completion
	if b.progress != nil {
		duration := time.Since(startTime)
		b.progress.OnGraphBuildingComplete(len(uniqueNodes), len(validEdges), duration)
	}

	return graphData, nil
}

// BuildGraphFromFiles is a helper function that filters Go files and builds the graph.
func BuildGraphFromFiles(ctx context.Context, rootDir string, files []string) (*GraphData, error) {
	builder := NewBuilder(rootDir)
	return builder.BuildFull(ctx, files)
}

// deduplicateNodes removes duplicate nodes by ID, preferring non-test files.
func (b *builder) deduplicateNodes(nodes []Node) []Node {
	nodeMap := make(map[string]Node)
	for _, node := range nodes {
		if existing, exists := nodeMap[node.ID]; exists {
			// Warn about duplicate node IDs
			log.Printf("[WARN] duplicate node ID '%s' found in %s and %s",
				node.ID, existing.File, node.File)

			// Keep node from non-test file, or first if both test/non-test
			if !strings.HasSuffix(existing.File, "_test.go") {
				continue // Keep existing non-test file
			}
			// Replace with non-test file or keep first test file
		}
		nodeMap[node.ID] = node
	}

	// Convert map back to slice
	uniqueNodes := make([]Node, 0, len(nodeMap))
	for _, node := range nodeMap {
		uniqueNodes = append(uniqueNodes, node)
	}
	return uniqueNodes
}

// resolveInterfaceEmbeddings resolves interface embeddings and infers implementations.
// This is used in the full build path.
func (b *builder) resolveInterfaceEmbeddings(nodes []Node) []Edge {
	log.Printf("Resolving interface embeddings and inferring implementations...")
	matcher := NewInterfaceMatcher(nodes)

	// Step 1: Resolve embedded interfaces (flatten method sets)
	matcher.ResolveEmbeddings()

	// Update nodes with resolved methods
	for i := range nodes {
		if resolved := matcher.nodes[nodes[i].ID]; resolved != nil {
			nodes[i].ResolvedMethods = resolved.ResolvedMethods
		}
	}

	// Step 2: Infer interface implementations
	implEdges := matcher.InferImplementations()
	log.Printf("Found %d interface implementations", len(implEdges))
	return implEdges
}

// resolveInterfaceEmbeddingsForIncremental resolves interface embeddings without inferring implementations.
// This is used in the incremental build path where implementations are handled separately.
// Returns the matcher for use in incremental inference.
func (b *builder) resolveInterfaceEmbeddingsForIncremental(nodes []Node) *InterfaceMatcher {
	log.Printf("Resolving interface embeddings and inferring implementations (incremental)...")
	matcher := NewInterfaceMatcher(nodes)

	// Step 1: Resolve embedded interfaces
	matcher.ResolveEmbeddings()

	// Update nodes with resolved methods
	for i := range nodes {
		if resolved := matcher.nodes[nodes[i].ID]; resolved != nil {
			nodes[i].ResolvedMethods = resolved.ResolvedMethods
		}
	}

	return matcher
}
