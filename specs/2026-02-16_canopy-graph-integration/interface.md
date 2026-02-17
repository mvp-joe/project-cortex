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

Lives in `internal/graph/canopy_searcher.go`. Used directly by the MCP handler — no intermediate interface.

```go
package graph

import (
    "context"

    "github.com/jward/canopy"
)

// CanopySearcher provides graph query capabilities using canopy's query API.
// It translates cortex's QueryRequest into canopy API calls and maps results
// back to cortex's response formats. Used directly by the MCP handler —
// there is no GraphQuerier interface, no fallback, no legacy searcher.
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
// Used for: callers, callees, dependencies, dependents, type_usages,
// references, implementations, definition, impact, path.
func (cs *CanopySearcher) Query(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

// AdvancedQuery executes an advanced operation and returns results.
// Dispatches to operation-specific methods based on req.Operation.
// Used for: symbols, search, summary, package_summary, detail,
// type_hierarchy, unused_symbols, hotspots, circular_dependencies,
// dependency_graph, scope.
func (cs *CanopySearcher) AdvancedQuery(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)

// Close releases resources. No-op since CanopySearcher does not own the engine.
func (cs *CanopySearcher) Close() error

// --- Internal methods ---

// resolveTarget converts a string target (e.g., "embed.Provider", "internal/mcp")
// to a canopy Symbol. The caller uses Symbol.ID (int64) to pass to canopy API
// methods like Callers(symbolID), Callees(symbolID), etc.
// Returns *canopy.Symbol (not SymbolResult) because SymbolAt() returns Symbol
// directly, while SearchSymbols() returns SymbolResult which embeds Symbol.
// Returns error if no matching symbol is found.
func (cs *CanopySearcher) resolveTarget(target string) (*canopy.Symbol, error)

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

// queryImplements finds interfaces that a type implements (inverse of implementations). New operation.
func (cs *CanopySearcher) queryImplements(ctx context.Context, req *QueryRequest) (*QueryResponse, error)

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

// callGraphNodeToResult converts a canopy CallGraphNode to a cortex QueryResult.
// CallGraphNode already contains full SymbolResult — no separate lookup needed.
func (cs *CanopySearcher) callGraphNodeToResult(node canopy.CallGraphNode) (*QueryResult, error)

// --- Advanced query internal methods ---

// queryDetail returns rich symbol metadata (params, members, type params, annotations).
func (cs *CanopySearcher) queryDetail(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)

// queryTypeHierarchy returns full type hierarchy (implements, implemented-by, composes, composed-by).
func (cs *CanopySearcher) queryTypeHierarchy(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)

// queryScope returns scope chain at a file:line:col position.
func (cs *CanopySearcher) queryScope(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)

// queryUnusedSymbols finds symbols with zero resolved references.
func (cs *CanopySearcher) queryUnusedSymbols(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)

// queryHotspots returns most-referenced symbols with fan-in/fan-out.
func (cs *CanopySearcher) queryHotspots(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)

// queryCircularDependencies detects package dependency cycles.
func (cs *CanopySearcher) queryCircularDependencies(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)

// queryDependencyGraph returns full package-to-package dependency graph.
func (cs *CanopySearcher) queryDependencyGraph(ctx context.Context, req *QueryRequest) (*AdvancedQueryResponse, error)
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

    // New standard operations (return QueryResponse)
    OperationReferences      QueryOperation = "references"
    OperationDefinition      QueryOperation = "definition"
    OperationImplements      QueryOperation = "implements"

    // Advanced operations (return AdvancedQueryResponse)
    OperationSymbols              QueryOperation = "symbols"
    OperationSearch               QueryOperation = "search"
    OperationSummary              QueryOperation = "summary"
    OperationPackageSummary       QueryOperation = "package_summary"
    OperationDetail               QueryOperation = "detail"
    OperationTypeHierarchy        QueryOperation = "type_hierarchy"
    OperationScope                QueryOperation = "scope"
    OperationUnusedSymbols        QueryOperation = "unused_symbols"
    OperationHotspots             QueryOperation = "hotspots"
    OperationCircularDependencies QueryOperation = "circular_dependencies"
    OperationDependencyGraph      QueryOperation = "dependency_graph"
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

    // Fields for advanced operations
    Pattern        string   `json:"pattern"`          // For "search": glob pattern (e.g., "Get*", "*Provider")
    Kinds          []string `json:"kinds"`             // For "symbols"/"search"/"unused_symbols": filter by symbol kinds
    Visibility     string   `json:"visibility"`        // For "symbols"/"search"/"unused_symbols": "public" or "private"
    PathPrefix     string   `json:"path_prefix"`       // For "symbols"/"search"/"package_summary"/"unused_symbols": filter by path
    TopN           int      `json:"top_n"`             // For "summary"/"hotspots": number of top results
    RefCountMin    *int     `json:"ref_count_min"`     // For "symbols"/"search": minimum reference count
    RefCountMax    *int     `json:"ref_count_max"`     // For "symbols"/"search": maximum reference count
}
```

The MCP tool enum is expanded:

```go
mcp.WithString("operation",
    mcp.Required(),
    mcp.Enum(
        // Standard operations (return QueryResponse)
        "callers", "callees", "dependencies", "dependents",
        "type_usages", "implementations", "implements", "impact", "path",
        "references", "definition",
        // Advanced operations (return AdvancedQueryResponse)
        "symbols", "search", "summary", "package_summary",
        "detail", "type_hierarchy", "scope",
        "unused_symbols", "hotspots", "circular_dependencies", "dependency_graph",
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
    mcp.Description("Number of top results (default: 10). Used with 'summary' and 'hotspots' operations.")),
mcp.WithNumber("ref_count_min",
    mcp.Description("Minimum reference count filter. Used with 'symbols' and 'search' operations.")),
mcp.WithNumber("ref_count_max",
    mcp.Description("Maximum reference count filter. Used with 'symbols' and 'search' operations.")),
```

## Advanced Query Response Types

Response types for advanced operations. These types live in the `graph` package (`internal/graph/`) and are marshaled to JSON by the MCP handler layer. Each operation populates exactly one field (plus Operation and Metadata).

```go
// AdvancedQueryResponse is returned by CanopySearcher.AdvancedQuery().
// Each operation populates one field; the rest are omitted from JSON.
type AdvancedQueryResponse struct {
    Operation       string               `json:"operation"`
    Metadata        ResponseMeta         `json:"metadata"`

    // Populated by: symbols, search, unused_symbols
    Symbols         *SymbolsResult       `json:"symbols,omitempty"`

    // Populated by: summary
    Summary         *SummaryInfo         `json:"summary,omitempty"`

    // Populated by: package_summary
    PackageSummary  *PackageSummaryInfo  `json:"package_summary,omitempty"`

    // Populated by: detail
    Detail          *DetailInfo          `json:"detail,omitempty"`

    // Populated by: type_hierarchy
    Hierarchy       *HierarchyInfo       `json:"hierarchy,omitempty"`

    // Populated by: hotspots
    Hotspots        []HotspotInfo        `json:"hotspots,omitempty"`

    // Populated by: circular_dependencies
    Cycles          [][]string           `json:"cycles,omitempty"`

    // Populated by: dependency_graph
    DependencyGraph *DependencyGraphInfo `json:"dependency_graph,omitempty"`

    // Populated by: scope
    Scopes          []ScopeInfo          `json:"scopes,omitempty"`
}

// SymbolsResult wraps paginated symbol results.
type SymbolsResult struct {
    Items         []SymbolInfo `json:"items"`
    TotalFound    int          `json:"total_found"`
    TotalReturned int          `json:"total_returned"`
}

// SymbolInfo represents a symbol in results.
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

// SummaryInfo represents project-level summary.
type SummaryInfo struct {
    Languages    []LanguageInfo `json:"languages"`
    PackageCount int            `json:"package_count"`
    TopSymbols   []SymbolInfo   `json:"top_symbols"`
}

// PackageSummaryInfo represents single-package summary.
type PackageSummaryInfo struct {
    PackageName     string         `json:"package_name"`
    PackagePath     string         `json:"package_path"`
    FileCount       int            `json:"file_count"`
    ExportedSymbols []SymbolInfo   `json:"exported_symbols"`
    KindCounts      map[string]int `json:"kind_counts"`
    Dependencies    []string       `json:"dependencies"`
    Dependents      []string       `json:"dependents"`
}

// DetailInfo represents rich symbol metadata.
type DetailInfo struct {
    Symbol      SymbolInfo      `json:"symbol"`
    Parameters  []ParamInfo     `json:"parameters,omitempty"`
    Members     []MemberInfo    `json:"members,omitempty"`
    TypeParams  []TypeParamInfo `json:"type_params,omitempty"`
    Annotations []AnnotationInfo `json:"annotations,omitempty"`
}

// HierarchyInfo represents full type hierarchy.
type HierarchyInfo struct {
    Symbol        SymbolInfo         `json:"symbol"`
    Implements    []TypeRelationInfo `json:"implements,omitempty"`
    ImplementedBy []TypeRelationInfo `json:"implemented_by,omitempty"`
    Composes      []TypeRelationInfo `json:"composes,omitempty"`
    ComposedBy    []TypeRelationInfo `json:"composed_by,omitempty"`
}

// TypeRelationInfo represents a type relationship.
type TypeRelationInfo struct {
    Symbol SymbolInfo `json:"symbol"`
    Kind   string     `json:"kind"` // "inheritance", "interface_impl", "composition", "embedding", "implicit"
}

// HotspotInfo represents a heavily-referenced symbol.
type HotspotInfo struct {
    Symbol      SymbolInfo `json:"symbol"`
    CallerCount int        `json:"caller_count"`
    CalleeCount int        `json:"callee_count"`
}

// DependencyGraphInfo represents package-to-package dependencies.
type DependencyGraphInfo struct {
    Packages []PackageNodeInfo    `json:"packages"`
    Edges    []DependencyEdgeInfo `json:"edges"`
}

type PackageNodeInfo struct {
    Name      string `json:"name"`
    FileCount int    `json:"file_count"`
    LineCount int    `json:"line_count"`
}

type DependencyEdgeInfo struct {
    From        string `json:"from"`
    To          string `json:"to"`
    ImportCount int    `json:"import_count"`
}

// ScopeInfo represents a lexical scope in the scope chain.
type ScopeInfo struct {
    Kind      string `json:"kind"` // "file", "function", "block", etc.
    File      string `json:"file"`
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
}

// ParamInfo represents a function parameter or return value.
type ParamInfo struct {
    Name       string `json:"name"`
    TypeExpr   string `json:"type_expr"`
    IsReceiver bool   `json:"is_receiver,omitempty"`
    IsReturn   bool   `json:"is_return,omitempty"`
}

// MemberInfo represents a struct field or type member.
type MemberInfo struct {
    Name       string `json:"name"`
    Kind       string `json:"kind"` // "field", "method", etc.
    TypeExpr   string `json:"type_expr"`
    Visibility string `json:"visibility"`
}

// TypeParamInfo represents a generic type parameter.
type TypeParamInfo struct {
    Name        string `json:"name"`
    Constraints string `json:"constraints,omitempty"`
}

// AnnotationInfo represents a decorator/annotation.
type AnnotationInfo struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments,omitempty"`
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
