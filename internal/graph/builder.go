package graph

import (
	"context"
	"log"
	"path/filepath"
	"strings"
)

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
}

// NewBuilder creates a new graph builder.
func NewBuilder(rootDir string) Builder {
	return &builder{
		extractor: NewExtractor(rootDir),
		rootDir:   rootDir,
	}
}

// BuildFull builds the complete graph from all Go files.
func (b *builder) BuildFull(ctx context.Context, files []string) (*GraphData, error) {
	var allNodes []Node
	var allEdges []Edge

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
			continue
		}

		allNodes = append(allNodes, fileData.Nodes...)
		allEdges = append(allEdges, fileData.Edges...)
	}

	// Deduplicate nodes by ID (prefer non-test files)
	nodeMap := make(map[string]Node)
	for _, node := range allNodes {
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

	// Phase 2: Interface matching
	log.Printf("Resolving interface embeddings and inferring implementations...")
	matcher := NewInterfaceMatcher(uniqueNodes)

	// Step 1: Resolve embedded interfaces (flatten method sets)
	matcher.ResolveEmbeddings()

	// Update nodes with resolved methods
	for i := range uniqueNodes {
		if resolved := matcher.nodes[uniqueNodes[i].ID]; resolved != nil {
			uniqueNodes[i].ResolvedMethods = resolved.ResolvedMethods
		}
	}

	// Step 2: Infer interface implementations
	implEdges := matcher.InferImplementations()
	allEdges = append(allEdges, implEdges...)

	log.Printf("Found %d interface implementations", len(implEdges))

	return &GraphData{
		Nodes: uniqueNodes,
		Edges: allEdges,
	}, nil
}

// BuildIncremental updates the graph for changed files only.
func (b *builder) BuildIncremental(ctx context.Context, previousGraph *GraphData, changedFiles, deletedFiles []string, allFiles []string) (*GraphData, error) {
	if previousGraph == nil {
		// No previous graph, do full build
		return b.BuildFull(ctx, allFiles)
	}

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

	// Extract graph data from changed files
	var newNodes []Node
	var newEdges []Edge

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
			continue
		}

		newNodes = append(newNodes, fileData.Nodes...)
		newEdges = append(newEdges, fileData.Edges...)
	}

	// Merge preserved and new data
	allNodes := append(preservedNodes, newNodes...)
	allEdges := append(preservedEdges, newEdges...)

	// Deduplicate nodes (prefer non-test files)
	nodeMap := make(map[string]Node)
	for _, node := range allNodes {
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

	uniqueNodes := make([]Node, 0, len(nodeMap))
	for _, node := range nodeMap {
		uniqueNodes = append(uniqueNodes, node)
	}

	// Phase 2: Interface matching (incremental)
	log.Printf("Resolving interface embeddings and inferring implementations (incremental)...")
	matcher := NewInterfaceMatcher(uniqueNodes)

	// Step 1: Resolve embedded interfaces
	matcher.ResolveEmbeddings()

	// Update nodes with resolved methods
	for i := range uniqueNodes {
		if resolved := matcher.nodes[uniqueNodes[i].ID]; resolved != nil {
			uniqueNodes[i].ResolvedMethods = resolved.ResolvedMethods
		}
	}

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

	// Remove old implementation edges for changed entities
	filteredEdges := []Edge{}
	for _, edge := range allEdges {
		if edge.Type == EdgeImplements {
			// Remove if involves changed struct or interface
			isChangedStruct := false
			for _, id := range changedStructIDs {
				if edge.From == id {
					isChangedStruct = true
					break
				}
			}
			isChangedInterface := false
			for _, id := range changedInterfaceIDs {
				if edge.To == id {
					isChangedInterface = true
					break
				}
			}

			if isChangedStruct || isChangedInterface {
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

	return &GraphData{
		Nodes: uniqueNodes,
		Edges: validEdges,
	}, nil
}

// BuildGraphFromFiles is a helper function that filters Go files and builds the graph.
func BuildGraphFromFiles(ctx context.Context, rootDir string, files []string) (*GraphData, error) {
	builder := NewBuilder(rootDir)
	return builder.BuildFull(ctx, files)
}
