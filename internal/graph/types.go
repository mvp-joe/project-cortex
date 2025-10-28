package graph

import "time"

// NodeKind represents the type of a code entity.
type NodeKind string

const (
	NodeFunction NodeKind = "function"
	NodeMethod   NodeKind = "method"
	NodePackage  NodeKind = "package"
)

// Node represents a code entity with its source location.
type Node struct {
	ID        string   `json:"id"`         // Fully qualified identifier (e.g., "embed.Provider", "localProvider.Embed")
	Kind      NodeKind `json:"kind"`       // Type of node
	File      string   `json:"file"`       // Relative file path
	StartLine int      `json:"start_line"` // Start line number (1-indexed)
	EndLine   int      `json:"end_line"`   // End line number (1-indexed)
}

// EdgeType represents the type of relationship between nodes.
type EdgeType string

const (
	EdgeCalls   EdgeType = "calls"   // Function calls function
	EdgeImports EdgeType = "imports" // Package imports package
)

// Edge represents a relationship between two code entities.
type Edge struct {
	From     string    `json:"from"`     // Source node ID
	To       string    `json:"to"`       // Target node ID
	Type     EdgeType  `json:"type"`     // Relationship type
	Location *Location `json:"location"` // Where relationship occurs
}

// Location represents the source location of a relationship.
type Location struct {
	File   string `json:"file"`   // File where relationship originates
	Line   int    `json:"line"`   // Line number
	Column int    `json:"column"` // Column number (optional, 0 if unknown)
}

// GraphData represents the complete code graph structure stored in JSON.
type GraphData struct {
	Metadata GraphMetadata `json:"_metadata"`
	Nodes    []Node        `json:"nodes"`
	Edges    []Edge        `json:"edges"`
}

// GraphMetadata contains metadata about the graph.
type GraphMetadata struct {
	Version     string    `json:"version"`
	GeneratedAt time.Time `json:"generated_at"`
	NodeCount   int       `json:"node_count"`
	EdgeCount   int       `json:"edge_count"`
}

// FileGraphData represents the graph data extracted from a single file.
// Used during incremental updates to track which nodes/edges belong to which files.
type FileGraphData struct {
	FilePath string
	Nodes    []Node
	Edges    []Edge
}
