# Interface Definitions

## CanopyProvider (Indexer Side)

Lives in `internal/indexer/canopy_provider.go`. Wraps `canopy.Engine` for the indexer pipeline.

```go
package indexer

import (
    "context"

    "github.com/jward/canopy"
    canopyscripts "github.com/jward/canopy/scripts"
)

// CanopyProvider wraps a canopy.Engine for code graph indexing.
// It manages engine lifecycle and delegates indexing to canopy's public API.
// Uses canopy's embedded scripts (canopyscripts.FS) automatically.
type CanopyProvider struct {
    engine     *canopy.Engine
    projectDir string
}

// NewCanopyProvider creates a CanopyProvider for the given project.
// dbPath is the path to canopy's SQLite database (e.g., "{projectDir}/.canopy/index.db").
// languages optionally restricts which languages to index (nil = all supported).
// Uses canopy's embedded Risor scripts via canopyscripts.FS.
func NewCanopyProvider(projectDir string, dbPath string, languages []string) (*CanopyProvider, error)

// Index runs canopy's full indexing pipeline on the project directory.
// Calls engine.IndexDirectory() then engine.Resolve() -- the same workflow
// as the `canopy index` CLI. Canopy handles its own file diffing via content
// hashing, so this is safe to call on every file change notification.
func (cp *CanopyProvider) Index(ctx context.Context) error

// ScriptsChanged reports whether canopy's extraction scripts have changed
// since the last indexing run. If true, caller should delete the database
// and reindex from scratch.
func (cp *CanopyProvider) ScriptsChanged() bool

// Query returns canopy's QueryBuilder for graph queries.
// The returned QueryBuilder is safe for concurrent use.
func (cp *CanopyProvider) Query() *canopy.QueryBuilder

// Engine returns the underlying canopy.Engine for direct access.
// This exists for test and debugging purposes only and is not used
// in normal operation.
func (cp *CanopyProvider) Engine() *canopy.Engine

// Close releases the canopy engine and database resources.
func (cp *CanopyProvider) Close() error
```

## CanopySearcher (MCP Side)

Lives in `internal/graph/canopy_searcher.go`. Implements the `mcp.GraphQuerier` interface using canopy's query API.

```go
package graph

import (
    "context"

    "github.com/jward/canopy"
)

// CanopySearcher implements mcp.GraphQuerier using canopy's query API.
// It translates cortex's QueryRequest into canopy API calls and maps results
// back to cortex's QueryResponse format. This is the sole graph provider —
// there is no fallback or legacy searcher.
type CanopySearcher struct {
    query      *canopy.QueryBuilder
    projectDir string
}

// NewCanopySearcher creates a new canopy-backed graph searcher.
// The QueryBuilder is obtained from canopy.Engine.Query() and is safe for
// concurrent use. projectDir is used for resolving relative file paths.
func NewCanopySearcher(query *canopy.QueryBuilder, projectDir string) (*CanopySearcher, error)

// Query executes a graph query and returns results.
// Dispatches to operation-specific methods based on req.Operation.
func (cs *CanopySearcher) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// Close releases resources. No-op since CanopySearcher does not own the engine.
func (cs *CanopySearcher) Close() error

// --- Internal methods ---

// resolveTarget converts a string target (e.g., "embed.Provider", "internal/mcp")
// to a canopy SymbolResult. The caller uses SymbolResult.ID (int64) to pass to
// canopy API methods like Callers(symbolID), Callees(symbolID), etc.
// Returns error if no matching symbol is found.
func (cs *CanopySearcher) resolveTarget(target string) (*canopy.SymbolResult, error)

// resolveFileTarget converts a package/path target to a canopy file ID.
// Used for dependencies/dependents operations.
func (cs *CanopySearcher) resolveFileTarget(target string) (int64, error)

// queryCallers finds functions that call the target, with depth traversal.
func (cs *CanopySearcher) queryCallers(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryCallees finds functions called by the target, with depth traversal.
func (cs *CanopySearcher) queryCallees(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryDependencies finds imports for the target file/package.
func (cs *CanopySearcher) queryDependencies(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryDependents finds files/packages that import the target.
func (cs *CanopySearcher) queryDependents(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryTypeUsages finds where a type is referenced. Uses ReferencesTo.
func (cs *CanopySearcher) queryTypeUsages(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryReferences finds all references to a symbol. New operation.
func (cs *CanopySearcher) queryReferences(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryImplementations finds types implementing an interface. New operation.
func (cs *CanopySearcher) queryImplementations(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryDefinition finds the definition of a symbol at a location. New operation.
func (cs *CanopySearcher) queryDefinition(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryImpact composes callers + implementations for blast radius analysis.
func (cs *CanopySearcher) queryImpact(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// queryPath finds shortest call path between two symbols via iterative BFS.
func (cs *CanopySearcher) queryPath(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// extractContext reads source file from disk and returns code snippet with
// context lines around the given location.
func (cs *CanopySearcher) extractContext(loc canopy.Location, contextLines int) (string, error)

// canopyLocationToNode converts a canopy Location to a cortex graph Node.
func (cs *CanopySearcher) canopyLocationToNode(loc canopy.Location, symbolName string, kind NodeKind) *Node

// callEdgeToNode converts a canopy CallEdge to a cortex graph Node,
// resolving the symbol ID to its location and name.
func (cs *CanopySearcher) callEdgeToNode(edge *canopy.CallEdge, depth int) (*QueryResult, error)
```

## New QueryOperations

Added to `internal/graph/searcher_types.go`:

```go
const (
    // Existing operations (unchanged)
    OperationCallers         QueryOperation = "callers"
    OperationCallees         QueryOperation = "callees"
    OperationDependencies    QueryOperation = "dependencies"
    OperationDependents      QueryOperation = "dependents"
    OperationTypeUsages      QueryOperation = "type_usages"
    OperationImplementations QueryOperation = "implementations"
    OperationPath            QueryOperation = "path"
    OperationImpact          QueryOperation = "impact"

    // New operations (canopy-powered)
    OperationReferences      QueryOperation = "references"
    OperationDefinition      QueryOperation = "definition"
    OperationSymbols         QueryOperation = "symbols"
    OperationSearch          QueryOperation = "search"
    OperationSummary         QueryOperation = "summary"
    OperationPackageSummary  QueryOperation = "package_summary"
)
```

## Updated MCP Tool Schema

Updated `internal/mcp/graph_tool.go` to register new operations:

```go
// CortexGraphRequest updated with new fields for path and discovery operations.
type CortexGraphRequest struct {
    Operation      string   `json:"operation"`
    Target         string   `json:"target"`
    To             string   `json:"to"`               // For "path" operation: destination target
    IncludeContext *bool    `json:"include_context"`
    ContextLines   int      `json:"context_lines"`
    Depth          int      `json:"depth"`
    MaxResults     int      `json:"max_results"`

    // New fields for discovery operations
    Pattern        string   `json:"pattern"`         // For "search" operation: glob pattern
    Kinds          []string `json:"kinds"`            // For "symbols"/"search": filter by symbol kinds
    Visibility     string   `json:"visibility"`       // For "symbols"/"search": filter by visibility
    PathPrefix     string   `json:"path_prefix"`      // For "symbols"/"search"/"package_summary": filter by path
    TopN           int      `json:"top_n"`            // For "summary": number of top symbols
}
```

The MCP tool enum is expanded:

```go
mcp.WithString("operation",
    mcp.Required(),
    mcp.Enum(
        // Existing
        "callers", "callees", "dependencies", "dependents",
        "type_usages", "implementations", "impact", "path",
        // New (Phase 3)
        "references", "definition",
        // New (Phase 4 - discovery)
        "symbols", "search", "summary", "package_summary",
    ),
    mcp.Description("..."),
),
```

New optional parameters:

```go
mcp.WithString("to",
    mcp.Description("Destination target for 'path' operation (e.g., 'Provider.Embed'). Required for 'path', ignored for other operations.")),
mcp.WithString("pattern",
    mcp.Description("Glob pattern for symbol search (e.g., 'Get*', '*Provider'). Only used with 'search' operation.")),
mcp.WithArray("kinds",
    mcp.Description("Filter by symbol kinds (e.g., ['function', 'interface', 'struct']). Used with 'symbols' and 'search' operations.")),
mcp.WithString("visibility",
    mcp.Description("Filter by visibility: 'public' or 'private'. Used with 'symbols' and 'search' operations.")),
mcp.WithString("path_prefix",
    mcp.Description("Filter by file path prefix (e.g., 'internal/mcp'). Used with 'symbols', 'search', and 'package_summary' operations.")),
mcp.WithNumber("top_n",
    mcp.Description("Number of top symbols to include in summary (default: 10). Only used with 'summary' operation.")),
```

## Discovery Response Types

New response types for discovery operations. These types live in the `graph` package (`internal/graph/`) and are marshaled to JSON by the MCP handler layer in `internal/mcp/graph_tool.go` before being returned to the MCP client.

```go
// DiscoveryResponse is used for symbols/search/summary/package_summary operations.
// These operations return richer data than the standard QueryResponse.
type DiscoveryResponse struct {
    Operation     string          `json:"operation"`
    Results       []SymbolInfo    `json:"results,omitempty"`
    Summary       *SummaryInfo    `json:"summary,omitempty"`
    TotalFound    int             `json:"total_found"`
    TotalReturned int             `json:"total_returned"`
    Metadata      ResponseMeta    `json:"metadata"`
}

// SymbolInfo represents a symbol in discovery results.
type SymbolInfo struct {
    Name             string `json:"name"`
    Kind             string `json:"kind"`
    Visibility       string `json:"visibility"`
    File             string `json:"file"`
    StartLine        int    `json:"start_line"`
    EndLine          int    `json:"end_line"`
    RefCount         int    `json:"ref_count"`
    ExternalRefCount int    `json:"external_ref_count"`
    Context          string `json:"context,omitempty"`
}

// SummaryInfo represents project or package summary data.
type SummaryInfo struct {
    // For project summary
    Languages    []LanguageInfo `json:"languages,omitempty"`
    PackageCount int            `json:"package_count,omitempty"`
    TopSymbols   []SymbolInfo   `json:"top_symbols,omitempty"`

    // For package summary
    PackageName     string       `json:"package_name,omitempty"`
    PackagePath     string       `json:"package_path,omitempty"`
    FileCount       int          `json:"file_count,omitempty"`
    ExportedSymbols []SymbolInfo `json:"exported_symbols,omitempty"`
    KindCounts      map[string]int `json:"kind_counts,omitempty"`
    Dependencies    []string     `json:"dependencies,omitempty"`
    Dependents      []string     `json:"dependents,omitempty"`
}

// LanguageInfo represents per-language statistics.
type LanguageInfo struct {
    Language    string         `json:"language"`
    FileCount   int            `json:"file_count"`
    LineCount   int            `json:"line_count"`
    SymbolCount int            `json:"symbol_count"`
    KindCounts  map[string]int `json:"kind_counts"`
}
```

## Configuration

Added to `.cortex/config.yml`:

```yaml
graph:
  canopy:
    db_path: ".canopy/index.db"  # relative to project root
    languages: []  # empty = all supported languages
```

Added to `internal/config/config.go`:

```go
type GraphConfig struct {
    Canopy CanopyConfig `yaml:"canopy" mapstructure:"canopy"`
}

type CanopyConfig struct {
    DBPath    string   `yaml:"db_path" mapstructure:"db_path"`
    Languages []string `yaml:"languages" mapstructure:"languages"`
}
```
