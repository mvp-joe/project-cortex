---
status: draft
started_at: 2025-10-27T00:00:00Z
dependencies: [chunk-manager, mcp-server]
---

# Cortex Exact Specification

## Purpose

The exact search tool provides keyword-based full-text search to complement semantic vector search and structural graph queries. This enables precise identifier lookup, boolean search, and exact phrase matching—use cases where semantic similarity is less important than literal text matching.

## Core Concept

**Input**: User keyword query (identifiers, boolean expressions, phrases)

**Process**: Parse query → Search bleve index → Filter results → Return with highlights

**Output**: Matching chunks with highlighted search terms

## Technology Stack

- **Language**: Go 1.25+
- **Search Library**: `github.com/blevesearch/bleve/v2` (full-text search, pure Go)
- **Index Type**: In-memory Scorch index (segmented architecture)
- **Shared Data**: Chunks loaded once via ChunkManager, shared with vector search
- **Incremental Updates**: Both chromem-go and bleve support add/delete operations

## Why Three Search Tools?

**cortex_search** (semantic, vector-based):
- **Strengths**: Understands concepts ("authentication flow"), finds similar patterns, cross-references code + docs
- **Limitations**: Can't reliably find exact identifiers, no boolean logic

**cortex_exact** (keyword, full-text):
- **Strengths**: Finds exact matches (`sync.RWMutex`), boolean queries (`Provider AND interface`), fast
- **Limitations**: No semantic understanding, requires knowing exact terms

**cortex_graph** (structural, graph-based):
- **Strengths**: Relationships ("who calls X"), dependencies, impact analysis
- **Limitations**: Requires exact identifiers, no text search

**Together**: LLM selects appropriate tool based on query intent.

## Architecture

```
┌─────────────────┐
│  Chunk Files    │ (.cortex/chunks/*.json)
└────────┬────────┘
         │
         ▼
┌─────────────────┐
│ ChunkManager    │ ← Load once, share across searchers
│ (shared)        │   Detect changes, build indexes
└────────┬────────┘
         │
         ├──────────────────┬──────────────────┐
         ▼                  ▼
┌────────────────┐ ┌────────────────┐
│ chromem-go     │ │ bleve index    │
│ (vector)       │ │ (full-text)    │
└────────┬───────┘ └────────┬───────┘
         │                  │
         ▼                  ▼
┌────────────────┐ ┌────────────────┐
│ cortex_search  │ │ cortex_exact   │
│ (MCP tool)     │ │ (MCP tool)     │
└────────────────┘ └────────────────┘
```

**Note**: `cortex_graph` uses separate data (`.cortex/graph/code-graph.json`), not chunks.

## Shared Chunk Manager

**See**: `specs/chunk-manager.md` for complete specification.

### Summary

**Problem**: Loading and deserializing chunks is expensive (~30ms for 10K chunks). Adding cortex_exact would require loading chunks twice (once for chromem, once for bleve).

**Solution**: Extract chunk loading into shared ChunkManager:
- Load chunks once → ~35ms (all files)
- Detect changes → ~5ms (timestamp comparison)
- Share read-only ChunkSet across all searchers
- **Savings**: ~30ms per reload + enables incremental updates

### Integration

```go
// ChunkManager defined in specs/chunk-manager.md
type ChunkManager struct {
    chunksDir      string
    current        *ChunkSet    // Read-only after creation
    lastReloadTime time.Time
    mu             sync.RWMutex // Protects swap only
}

type ChunkSet struct {
    chunks  []*ContextChunk              // All chunks
    byID    map[string]*ContextChunk     // Fast lookup by ID
    byFile  map[string][]*ContextChunk   // For incremental updates
}
```

**Thread safety**:
- `ChunkSet` is **immutable after creation** → safe for concurrent reads
- `mu.RWMutex` protects only the swap of `current` reference
- No locks needed when accessing chunks from a `ChunkSet`

### Load Process

```go
func (cm *ChunkManager) Load(ctx context.Context) (*ChunkSet, error) {
    // Load from disk (expensive)
    chunks, err := LoadChunks(cm.chunksDir)
    if err != nil {
        return nil, err
    }

    // Build indexes
    set := &ChunkSet{
        chunks: chunks,
        byID:   make(map[string]*ContextChunk),
        byFile: make(map[string][]*ContextChunk),
    }

    for _, chunk := range chunks {
        set.byID[chunk.ID] = chunk

        if filePath, ok := chunk.Metadata["file_path"].(string); ok {
            set.byFile[filePath] = append(set.byFile[filePath], chunk)
        }
    }

    return set, nil
}
```

### Change Detection

```go
func (cm *ChunkManager) DetectChanges(newSet *ChunkSet) (
    added, updated []*ContextChunk,
    deleted []string,
) {
    old := cm.GetCurrent()

    // Find deleted chunks
    for id := range old.byID {
        if _, exists := newSet.byID[id]; !exists {
            deleted = append(deleted, id)
        }
    }

    // Find added/updated chunks
    for id, newChunk := range newSet.byID {
        oldChunk, exists := old.byID[id]
        if !exists {
            added = append(added, newChunk)
        } else if chunkChanged(oldChunk, newChunk) {
            updated = append(updated, newChunk)
        }
    }

    return added, updated, deleted
}

func chunkChanged(old, new *ContextChunk) bool {
    // Compare text and embedding (metadata changes don't require reindex)
    return old.Text != new.Text || !embeddingsEqual(old.Embedding, new.Embedding)
}
```

## Incremental Update Strategy

### Why Incremental Updates Matter

**Full rebuild** (old approach):
```
File changed → Load all chunks → Rebuild entire index
- 10K chunks: ~360ms
- 100K chunks: ~3-5s
- Blocks queries during rebuild
```

**Incremental updates** (new approach):
```
File changed → Load chunks → Detect changes (50 chunks) → Update only those 50
- 10K chunks, 50 changed: ~45ms
- 100K chunks, 500 changed: ~150ms
- 8x faster, scales better
```

### Both Libraries Support Incremental Updates

**chromem-go v0.7.0**:
- `collection.AddDocument(doc)` - Add new document
- `collection.Delete(id)` - Remove document
- Uses flat HNSW index, operations are O(1) amortized

**bleve v2 (Scorch)**:
- `index.Index(id, doc)` - Add or update document
- `batch.Delete(id)` - Remove from batch
- `index.Batch(batch)` - Execute batch operation
- Segmented architecture, no full rebuild needed

### Coordinated Reload

```go
// internal/mcp/searcher_coordinator.go (NEW)
type SearcherCoordinator struct {
    chunkManager   *ChunkManager
    vectorSearcher *chromemSearcher   // Semantic search
    textSearcher   *exactSearcher     // Keyword search

    mu sync.Mutex  // Protects reload operation (not queries)
}

func (sc *SearcherCoordinator) Reload(ctx context.Context) error {
    // 1. Load chunks ONCE (shared across both searchers)
    newSet, err := sc.chunkManager.Load(ctx)
    if err != nil {
        return err
    }

    // 2. Detect what changed
    added, updated, deleted := sc.chunkManager.DetectChanges(newSet)

    // 3. Update both indexes in PARALLEL (no rebuild!)
    var wg sync.WaitGroup
    var vectorErr, textErr error

    wg.Add(2)

    // Update vector DB incrementally
    go func() {
        defer wg.Done()
        vectorErr = sc.vectorSearcher.UpdateIncremental(ctx, added, updated, deleted)
    }()

    // Update text index incrementally
    go func() {
        defer wg.Done()
        textErr = sc.textSearcher.UpdateIncremental(ctx, added, updated, deleted)
    }()

    wg.Wait()

    if vectorErr != nil {
        return vectorErr
    }
    if textErr != nil {
        return textErr
    }

    // 4. Swap chunk manager reference (atomic)
    sc.chunkManager.Update(newSet, calculateHash(newSet))

    return nil
}
```

**Performance breakdown**:
```
Old (full rebuild both):
- Load chunks for chromem:     ~30ms
- Rebuild chromem:             ~200ms
- Load chunks for bleve:       ~30ms
- Rebuild bleve:               ~100ms
Total: ~360ms

New (incremental both):
- Load chunks once:            ~30ms
- Detect changes:              ~5ms
- Update chromem (parallel):   ~10ms  (50 docs)
- Update bleve (parallel):     ~10ms  (50 docs batch)
Total: ~45ms  (8x faster!)
```

## Bleve Index Architecture

### In-Memory Scorch Index

**Scorch** is bleve's default index implementation using a segmented architecture:

```
┌──────────────────────────────────────┐
│          Bleve Index                 │
├──────────────────────────────────────┤
│  Segment 1  │  Segment 2  │  Seg 3  │  ← Immutable segments
├──────────────────────────────────────┤
│     Background Merger                 │  ← Combines small segments
└──────────────────────────────────────┘
```

**How updates work**:
1. New documents → New segment created
2. Deleted documents → Marked via bitmask (segment unchanged)
3. Updated documents → Delete (mark) + Add (new segment)
4. Background merger → Combines small segments into larger ones

**Benefits**:
- No full rebuild needed
- Queries work concurrently with updates
- Optimal batch size: 100-1000 documents

### Index Mapping

```go
func buildBleveMapping() *mapping.IndexMappingImpl {
    indexMapping := bleve.NewIndexMapping()

    // Standard analyzer: tokenize, lowercase, stop words
    standardAnalyzer := "standard"

    // Text field (primary search target)
    textMapping := bleve.NewTextFieldMapping()
    textMapping.Analyzer = standardAnalyzer
    textMapping.Store = true           // Store for highlighting
    textMapping.Index = true           // Searchable
    textMapping.IncludeTermVectors = true  // Enable phrase search

    // Chunk type field (filterable)
    chunkTypeMapping := bleve.NewTextFieldMapping()
    chunkTypeMapping.Analyzer = "keyword"  // Exact matching
    chunkTypeMapping.Store = true
    chunkTypeMapping.Index = true

    // Tags field (filterable, array)
    tagsMapping := bleve.NewTextFieldMapping()
    tagsMapping.Analyzer = "keyword"
    tagsMapping.Store = true
    tagsMapping.Index = true

    // File path field (filterable)
    filePathMapping := bleve.NewTextFieldMapping()
    filePathMapping.Analyzer = standardAnalyzer  // Allow partial matching
    filePathMapping.Store = true
    filePathMapping.Index = true

    // Title field (searchable, but lower priority than text)
    titleMapping := bleve.NewTextFieldMapping()
    titleMapping.Analyzer = standardAnalyzer
    titleMapping.Store = true
    titleMapping.Index = true

    // Document mapping
    docMapping := bleve.NewDocumentMapping()
    docMapping.AddFieldMappingsAt("text", textMapping)
    docMapping.AddFieldMappingsAt("chunk_type", chunkTypeMapping)
    docMapping.AddFieldMappingsAt("tags", tagsMapping)
    docMapping.AddFieldMappingsAt("file_path", filePathMapping)
    docMapping.AddFieldMappingsAt("title", titleMapping)

    indexMapping.DefaultMapping = docMapping
    return indexMapping
}
```

**All fields indexed and searchable**:
- `text` - Chunk content (code or docs) - PRIMARY search target
- `chunk_type` - "symbols", "definitions", "data", "documentation" - keyword analyzer
- `tags` - ["code", "go", "definitions"] - keyword analyzer for exact matching
- `file_path` - "internal/mcp/searcher.go" - standard analyzer for partial matching
- `title` - Chunk title - standard analyzer

**Why index everything?**
- Field scoping prevents noise: `text:handler` searches ONLY text field
- Native bleve filtering: `chunk_type:definitions` is faster than Go post-filtering
- Power users can search metadata: `file_path:auth` finds all auth-related files
- LLMs can construct complex queries: `text:handler AND tags:go AND -file_path:test`

### Index Creation

```go
func NewExactSearcher(chunks []*ContextChunk) (*exactSearcher, error) {
    // Create in-memory index
    mapping := buildBleveMapping()
    index, err := bleve.NewMemOnly(mapping)
    if err != nil {
        return nil, err
    }

    // Batch index all chunks (optimal for initial load)
    batch := index.NewBatch()

    for _, chunk := range chunks {
        // Extract file_path from metadata
        filePath, _ := chunk.Metadata["file_path"].(string)

        doc := map[string]interface{}{
            "text":       chunk.Text,
            "chunk_type": chunk.ChunkType,
            "tags":       chunk.Tags,
            "file_path":  filePath,
            "title":      chunk.Title,
        }
        batch.Index(chunk.ID, doc)

        // Execute batch every 1000 docs (optimal size)
        if batch.Size() >= 1000 {
            if err := index.Batch(batch); err != nil {
                return nil, err
            }
            batch = index.NewBatch()
        }
    }

    // Execute remaining
    if batch.Size() > 0 {
        if err := index.Batch(batch); err != nil {
            return nil, err
        }
    }

    return &exactSearcher{
        index: index,
    }, nil
}
```

## MCP Tool Interface

### Tool Registration

```go
func AddCortexExactTool(s *server.MCPServer, searcher ExactSearcher) {
    tool := mcp.NewTool(
        "cortex_exact",
        mcp.WithDescription(`Full-text keyword search using bleve query syntax.

Supports:
- Field scoping: text:provider, tags:go, chunk_type:definitions, file_path:auth
- Boolean operators: AND, OR, NOT, +required, -excluded
- Phrase search: "error handling"
- Wildcards: Prov* (prefix matching)
- Fuzzy: Provdier~1 (edit distance)
- Combinations: text:handler AND tags:go AND -file_path:test

Default: Searches 'text' field only to avoid path/metadata noise.

Examples:
- text:Provider - Find "Provider" in code/docs
- text:handler AND tags:go - Go handlers only
- text:"error handling" - Exact phrase
- (text:handler OR text:controller) AND -tags:test - Exclude tests`),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
    )
    s.AddTool(tool, createExactSearchHandler(searcher))
}
```

### Request Schema

```go
type CortexExactRequest struct {
    Query string `json:"query" jsonschema:"required"`
    Limit int    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=15"`
}
```

**Simplified API**: Only `query` and `limit` parameters. All filtering done via bleve query syntax in the `query` string.

JSON representation:
```json
{
  "query": "string (required, bleve query syntax)",
  "limit": "number (optional, 1-100, default: 15)"
}
```

**Query String Syntax**: Uses bleve QueryStringQuery syntax (similar to Elasticsearch/Lucene):

| Feature | Syntax | Example |
|---------|--------|---------|
| Field scoping | `field:term` | `text:handler` |
| Boolean AND | `term1 AND term2` | `text:handler AND tags:go` |
| Boolean OR | `term1 OR term2` | `text:handler OR text:controller` |
| Boolean NOT | `-term` | `-tags:test` |
| Required | `+term` | `+text:handler` |
| Phrase | `"phrase"` | `text:"error handling"` |
| Prefix wildcard | `term*` | `Prov*` |
| Fuzzy | `term~N` | `Provdier~1` |
| Grouping | `(...)` | `(text:A OR text:B) AND tags:go` |

### Response Schema

```go
type CortexExactResponse struct {
    Query         string           `json:"query"`
    MatchType     string           `json:"match_type"`
    Results       []*SearchResult  `json:"results"`
    TotalFound    int              `json:"total_found"`
    TotalReturned int              `json:"total_returned"`
    Metadata      ResponseMetadata `json:"metadata"`
}

type SearchResult struct {
    Chunk      *ContextChunk `json:"chunk"`
    Score      float64       `json:"score"`       // Match quality (0-1)
    Highlights []string      `json:"highlights"`  // Matching snippets with <em> tags
}

type ResponseMetadata struct {
    TookMs int    `json:"took_ms"`
    Source string `json:"source"`  // "exact"
}
```

## Query Examples

### 1. Basic Text Search (Default - No Noise)

**Query**: `"text:Provider"`

**Behavior**: Searches ONLY in text field (code/doc content), avoiding file path false positives

**Request**:
```json
{
  "query": "text:Provider",
  "limit": 10
}
```

**Response**:
```json
{
  "query": "text:Provider",
  "results": [
    {
      "chunk": {
        "id": "code-definitions-internal/embed/provider.go",
        "text": "type Provider interface {\n    Embed(ctx context.Context, texts []string) ([][]float32, error)\n}",
        "chunk_type": "definitions",
        "tags": ["code", "go", "definitions"],
        ...
      },
      "score": 0.98,
      "highlights": ["type <em>Provider</em> interface"]
    }
  ],
  "total_found": 15,
  "total_returned": 10
}
```

### 2. Field-Filtered Search

**Query**: `"text:handler AND tags:go AND chunk_type:definitions"`

**Behavior**: Boolean combination with native bleve filtering (no post-processing)

**Request**:
```json
{
  "query": "text:handler AND tags:go AND chunk_type:definitions",
  "limit": 15
}
```

**Result**: Only Go type/function definitions containing "handler"

### 3. Exclude Files (Negative Filter)

**Query**: `"text:authentication AND -file_path:test"`

**Behavior**: Find auth code, exclude test files

**Request**:
```json
{
  "query": "text:authentication AND -file_path:test",
  "limit": 20
}
```

**Result**: Auth code from non-test files only

### 4. Phrase Search

**Query**: `"text:\"error handling\""`

**Behavior**: Exact phrase match (terms must be adjacent)

**Request**:
```json
{
  "query": "text:\"error handling\"",
  "limit": 10
}
```

**Result**: Documents with exact phrase "error handling"

### 5. Multiple OR Conditions

**Query**: `"(text:handler OR text:controller) AND tags:typescript AND -tags:test"`

**Behavior**: Find TypeScript handlers OR controllers, exclude tests

**Request**:
```json
{
  "query": "(text:handler OR text:controller) AND tags:typescript AND -tags:test"
}
```

**Result**: Production TypeScript handler/controller code

### 6. Prefix Wildcard

**Query**: `"text:Prov*"`

**Behavior**: Match any term starting with "Prov" (Provider, ProviderConfig, etc.)

**Request**:
```json
{
  "query": "text:Prov*"
}
```

**Result**: All code containing Provider, ProviderFactory, etc.

### 7. Fuzzy Match (Typo Tolerance)

**Query**: `"text:Provdier~1"`

**Behavior**: Match "Provider" with edit distance ≤1 (handles typos)

**Request**:
```json
{
  "query": "text:Provdier~1"
}
```

**Result**: Finds "Provider" even with typo in query

### Phrase Search

**Query**: `"\"error handling\""`

**Behavior**: Exact phrase match (words in sequence)

**Example**:
```json
{
  "query": "\"error handling\""
}
```

### Prefix Wildcard

**Query**: `"Prov*"`

**Match type**: `"prefix"`

**Behavior**: Matches "Provider", "Provision", "Provide", etc.

**Example**:
```json
{
  "query": "Prov*",
  "match_type": "prefix"
}
```

### Fuzzy Matching

**Query**: `"Provdier"`

**Match type**: `"fuzzy"`

**Behavior**: Typo-tolerant, matches "Provider" (Levenshtein distance)

**Example**:
```json
{
  "query": "Provdier",
  "match_type": "fuzzy"
}
```

**Response**:
```json
{
  "results": [
    {
      "chunk": {...},
      "score": 0.85,
      "highlights": ["type <em>Provider</em> interface"]
    }
  ]
}
```

## Query Execution

### Search Flow (Simplified with Field Scoping)

```go
func (s *exactSearcher) Search(ctx context.Context, queryStr string, limit int) ([]*SearchResult, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    // 1. Parse query string - bleve handles all syntax (field scoping, boolean, etc.)
    q := bleve.NewQueryStringQuery(queryStr)

    // 2. Execute search with highlighting
    searchRequest := bleve.NewSearchRequestOptions(q, limit, 0, false)
    searchRequest.Highlight = bleve.NewHighlight()
    searchRequest.Highlight.Style = bleve.HtmlFragmenterName
    searchRequest.Highlight.Fields = []string{"text"}  // Only highlight text field

    searchResult, err := s.index.Search(searchRequest)
    if err != nil {
        return nil, fmt.Errorf("bleve search failed: %w", err)
    }

    // 3. Convert results (NO POST-FILTERING - bleve did it natively)
    results := make([]*SearchResult, 0, len(searchResult.Hits))
    for _, hit := range searchResult.Hits {
        // Retrieve stored fields from bleve (avoids ChunkManager lookup)
        text, _ := hit.Fields["text"].(string)
        chunkType, _ := hit.Fields["chunk_type"].(string)
        filePath, _ := hit.Fields["file_path"].(string)
        title, _ := hit.Fields["title"].(string)

        // Reconstruct minimal chunk for response
        chunk := &ContextChunk{
            ID:        hit.ID,
            Text:      text,
            ChunkType: chunkType,
            Title:     title,
            Metadata: map[string]interface{}{
                "file_path": filePath,
            },
        }

        // Tags may be []interface{} from bleve
        if tagsRaw, ok := hit.Fields["tags"].([]interface{}); ok {
            tags := make([]string, len(tagsRaw))
            for i, t := range tagsRaw {
                tags[i], _ = t.(string)
            }
            chunk.Tags = tags
        }

        // Extract highlights
        highlights := extractHighlights(hit.Fragments)

        results = append(results, &SearchResult{
            Chunk:      chunk,
            Score:      hit.Score,
            Highlights: highlights,
        })
    }

    return results, nil
}
```

**Key changes from original spec**:
1. **No match_type parameter**: QueryStringQuery handles all syntax (phrase, prefix, fuzzy, boolean)
2. **No post-filtering**: Bleve native filtering via query syntax (`chunk_type:definitions`)
3. **No ChunkManager lookup**: Retrieve stored fields directly from bleve (faster)
4. **Simpler**: ~30 lines vs ~80 lines with post-filtering logic

**Why this is better**:
- ✅ Respects limit correctly (no filtering after search reduces result count)
- ✅ Faster (native index filtering vs Go iteration)
- ✅ More powerful (complex boolean expressions supported)
- ✅ Cleaner code (single code path, no special cases)

### Highlighting

```go
func extractHighlights(fragments map[string][]string) []string {
    var highlights []string

    // Bleve returns fragments as map[field][]snippets
    for _, snippets := range fragments {
        highlights = append(highlights, snippets...)
    }

    // Limit to 3 highlights per result (avoid overwhelming LLM)
    if len(highlights) > 3 {
        highlights = highlights[:3]
    }

    return highlights
}
```

**Format**: `<em>` tags around matches (standard HTML, LLM-friendly)

**Example highlight**: `"type <em>Provider</em> <em>interface</em> {"`

## Incremental Update Implementation

### chromem-go Incremental

```go
// internal/mcp/chromem_searcher.go (MODIFIED)
func (s *chromemSearcher) UpdateIncremental(ctx context.Context,
    added, updated []*ContextChunk,
    deleted []string,
) error {
    // NO LOCK - chromem operations are thread-safe

    // 1. Delete removed chunks
    for _, id := range deleted {
        if err := s.collection.Delete(ctx, nil, nil, id); err != nil {
            // Log error but continue (chunk might not exist)
            log.Printf("Failed to delete chunk %s: %v", id, err)
        }
    }

    // 2. Update changed chunks (delete + add)
    for _, chunk := range updated {
        // Delete old version
        s.collection.Delete(ctx, nil, nil, chunk.ID)

        // Add updated version
        doc := chromem.Document{
            ID:        chunk.ID,
            Content:   chunk.Text,
            Embedding: chunk.Embedding,
            Metadata:  toMetadata(chunk),
        }
        if err := s.collection.AddDocument(ctx, doc); err != nil {
            return err
        }
    }

    // 3. Add new chunks
    for _, chunk := range added {
        doc := chromem.Document{
            ID:        chunk.ID,
            Content:   chunk.Text,
            Embedding: chunk.Embedding,
            Metadata:  toMetadata(chunk),
        }
        if err := s.collection.AddDocument(ctx, doc); err != nil {
            return err
        }
    }

    return nil
}
```

### bleve Incremental

```go
// internal/mcp/exact_searcher.go (NEW)
func (s *exactSearcher) UpdateIncremental(ctx context.Context,
    added, updated []*ContextChunk,
    deleted []string,
) error {
    // Build batch (optimal: 100-1000 ops)
    batch := s.index.NewBatch()

    // 1. Delete removed chunks
    for _, id := range deleted {
        batch.Delete(id)
    }

    // 2. Add new + updated chunks (Index() handles both)
    for _, chunk := range append(added, updated...) {
        doc := map[string]interface{}{
            "text": chunk.Text,
        }
        batch.Index(chunk.ID, doc)
    }

    // 3. Execute batch (no lock - bleve handles concurrency)
    return s.index.Batch(batch)
}
```

**Performance**: Batch of 50 docs = ~10ms

## LLM Tool Routing Logic

### Decision Tree

```
User query analysis:

Is natural language question ("how", "why", "explain")?
  → cortex_search (semantic understanding)

Contains exact identifier (Provider, sync.RWMutex)?
  → Check query intent:

    Contains boolean operators (AND, OR, NOT)?
      → cortex_exact (keyword search)

    Asks about relationships ("who calls", "what implements")?
      → cortex_graph (structural query)

    Wants to find definition/usage?
      → cortex_exact (faster for exact matches)

    Wants to understand behavior?
      → cortex_search (semantic context)

Contains wildcard (*) or typo?
  → cortex_exact (prefix/fuzzy matching)

Asks about architecture or dependencies?
  → cortex_graph (structural relationships)

Default:
  → cortex_search (safest for ambiguous queries)
```

### Example Routing

**Query**: "Find all uses of sync.RWMutex"
- Contains exact identifier: ✓
- Wants find usage: ✓
- **Route to**: cortex_exact
- **Reason**: Fast exact match

**Query**: "How does the mutex protect the collection?"
- Natural language question: ✓
- **Route to**: cortex_search
- **Reason**: Needs semantic understanding

**Query**: "What calls provider.Embed?"
- Asks about relationships: ✓
- **Route to**: cortex_graph
- **Reason**: Structural call graph query

**Query**: "Provider AND interface"
- Boolean operators: ✓
- **Route to**: cortex_exact
- **Reason**: Boolean keyword search

**Query**: "Prov* interface"
- Wildcard: ✓
- **Route to**: cortex_exact (match_type: prefix)
- **Reason**: Prefix matching

## Hybrid Query Patterns

### Example 1: Find + Understand

**User**: "Find sync.RWMutex and explain how it's used"

**LLM strategy**:
```javascript
// Step 1: Find exact matches (cortex_exact)
const matches = await cortex_exact({
  query: "sync.RWMutex",
  chunk_types: ["definitions"]
})

// Step 2: Understand usage (cortex_search)
for (const match of matches.results) {
  const context = await cortex_search({
    query: "mutex synchronization patterns",
    tags: [match.chunk.metadata.file_path]
  })
}

// LLM synthesizes: "sync.RWMutex appears in 8 files. It's used to protect
// concurrent access to the collection..."
```

### Example 2: Exact Match + Graph

**User**: "Find Provider interface and show all implementations"

**LLM strategy**:
```javascript
// Step 1: Find interface definition (cortex_exact)
const definition = await cortex_exact({
  query: "Provider interface",
  chunk_types: ["definitions"]
})

// Step 2: Find implementations (cortex_graph)
const impls = await cortex_graph({
  operation: "implementations",
  target: "embed.Provider"
})

// LLM synthesizes: "Provider interface (line 20-39) has 2 implementations:
// localProvider and MockProvider..."
```

### Example 3: Boolean + Semantic

**User**: "Find error handling in Provider implementations"

**LLM strategy**:
```javascript
// Step 1: Find Provider code (cortex_exact)
const providers = await cortex_exact({
  query: "Provider",
  chunk_types: ["definitions"]
})

// Step 2: Semantic search for error handling (cortex_search)
const errorHandling = await cortex_search({
  query: "error handling patterns",
  tags: providers.results.map(r => r.chunk.metadata.file_path)
})

// LLM synthesizes: "Provider implementations handle errors by..."
```

## Performance Characteristics

### Query Performance

| Operation | Typical Time | Notes |
|-----------|--------------|-------|
| Exact match | <5ms | Single term |
| Boolean AND/OR | <10ms | 2-3 terms |
| Complex boolean | <20ms | 5+ terms with nesting |
| Phrase search | <10ms | Exact sequence |
| Prefix wildcard | <15ms | Depends on prefix length |
| Fuzzy match | <20ms | Levenshtein distance |
| With highlighting | +2-5ms | Per result |
| Post-filtering | <1ms | Typical result count |

**Total typical query**: 5-25ms

### Reload Performance

**Old (full rebuild)**:
```
chromem load + rebuild:  ~230ms
bleve load + rebuild:    ~130ms
Total: ~360ms
```

**New (incremental)**:
```
Load chunks once:        ~30ms
Detect changes:          ~5ms
chromem incremental:     ~10ms  (parallel)
bleve incremental:       ~10ms  (parallel)
Total: ~45ms  (8x faster)
```

**Scalability**:

| Chunks | Changed | Old Reload | New Reload | Speedup |
|--------|---------|------------|------------|---------|
| 1K | 10 | ~50ms | ~10ms | 5x |
| 10K | 100 | ~360ms | ~45ms | 8x |
| 100K | 1000 | ~3-5s | ~200ms | 15-25x |

### Memory Footprint

| Component | Size |
|-----------|------|
| Shared ChunkSet | ~10MB (10K chunks) |
| chromem-go index | ~50MB (vectors + content) |
| bleve index | ~20MB (inverted index) |
| **Total** | **~80MB** |

**Comparison to old approach**:
- Old: ~60MB (chromem only, chunks loaded twice in memory briefly)
- New: ~80MB (+20MB for bleve, but chunks shared)
- **Trade-off**: +20MB for keyword search capability

## Integration with Existing System

### Modified Files

**internal/mcp/chunk_manager.go** (NEW):
- ChunkManager: Load chunks, detect changes
- ChunkSet: Immutable chunk data structure
- Change detection algorithm

**internal/mcp/searcher_coordinator.go** (NEW):
- SearcherCoordinator: Orchestrates reload
- Parallel update of chromem + bleve
- Error handling and rollback

**internal/mcp/exact_searcher.go** (NEW):
- ExactSearcher interface
- Bleve integration
- Query execution with filtering
- Incremental update implementation

**internal/mcp/tool_exact.go** (NEW):
- MCP tool registration
- Request/response handling
- Query parsing and validation

**internal/mcp/chromem_searcher.go** (MODIFIED):
- Add UpdateIncremental() method
- Remove loadChunks() (moved to ChunkManager)
- Use shared ChunkSet

**internal/mcp/server.go** (MODIFIED):
- Create SearcherCoordinator
- Register cortex_exact tool
- Wire up file watcher to coordinator

### File Watcher Integration

```go
// internal/mcp/watcher.go (MODIFIED)
type FileWatcher struct {
    coordinator  *SearcherCoordinator  // Changed from searcher
    watcher      *fsnotify.Watcher
    debounceTime time.Duration
}

func (fw *FileWatcher) watch(ctx context.Context) {
    // Same fsnotify logic
    for {
        select {
        case event := <-fw.watcher.Events:
            if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
                fw.lastEvent = time.Now()
                fw.pendingReload = true
            }
        case <-debounceTimer.C:
            if fw.pendingReload {
                // Reload both searchers via coordinator
                if err := fw.coordinator.Reload(ctx); err != nil {
                    log.Printf("Reload failed: %v", err)
                }
                fw.pendingReload = false
            }
        }
    }
}
```

**No changes needed**:
- Same 500ms debounce
- Same fsnotify events
- Just calls coordinator instead of single searcher

## Implementation Phases

### Phase 1: Shared Chunk Manager (MVP)

**Goal**: Single chunk load, shared across searchers

**Scope**:
- Implement ChunkManager with ChunkSet
- Modify chromem_searcher to use shared chunks
- Implement change detection
- Add incremental update to chromem_searcher

**Complexity**: Medium
**Timeline**: 2-3 days
**Value**: Foundation for all incremental updates

### Phase 2: Bleve Integration (Core Feature)

**Goal**: Add cortex_exact tool with basic search

**Scope**:
- ExactSearcher implementation
- Bleve index creation and management
- Basic exact match search
- MCP tool registration

**Complexity**: Medium
**Timeline**: 3-4 days
**Value**: Enables keyword search

### Phase 3: Advanced Query Syntax

**Goal**: Boolean operators, phrase search

**Scope**:
- Query string parsing for boolean logic
- Phrase search support
- Highlighting with `<em>` tags
- Post-query filtering (chunk_types, tags)

**Complexity**: Low-Medium
**Timeline**: 2-3 days
**Value**: Makes keyword search powerful

### Phase 4: Fuzzy and Prefix Matching

**Goal**: Typo tolerance and wildcards

**Scope**:
- Prefix matching (`Prov*`)
- Fuzzy matching (Levenshtein distance)
- Match type parameter handling

**Complexity**: Low
**Timeline**: 1-2 days
**Value**: Better UX for imprecise queries

### Phase 5: Coordinator Integration

**Goal**: Parallel reload of both indexes

**Scope**:
- SearcherCoordinator implementation
- Parallel goroutines for chromem + bleve updates
- Error handling and rollback
- Metrics tracking

**Complexity**: Medium
**Timeline**: 2-3 days
**Value**: Optimal reload performance

## Non-Goals

1. **Field-specific queries**: No `file:internal/mcp Provider` syntax for MVP (requires indexing metadata)
2. **Disk persistence**: In-memory only (fast startup priority)
3. **Query caching**: Not needed (queries fast enough without it)
4. **Custom analyzers**: Standard analyzer sufficient for code search
5. **Proximity operators**: No `Provider NEAR/5 interface` (complexity not justified)
6. **Regular expressions**: No regex search (use prefix/fuzzy instead)
7. **Faceted search**: No result grouping by chunk_type (LLM handles synthesis)

## Design Decisions

### 1. Case Sensitivity

**Decision**: Case-insensitive by default

**Rationale**:
- Users rarely care about `Provider` vs `provider`
- Code identifiers are PascalCase/camelCase but users type lowercase
- Standard analyzer lowercases tokens automatically

**Trade-off**: Can't distinguish between different-cased identifiers (rare edge case)

### 2. Highlighting Format

**Decision**: Use `<em>` tags (standard HTML)

**Rationale**:
- Standard format across search engines
- LLMs well-trained on XML/HTML syntax
- Easy to parse if needed
- Bleve default highlighter uses this

**Alternative considered**: Plain text markers `**match**` (rejected: ambiguous with markdown)

### 3. Boolean Operator Precedence

**Decision**: Query string parser handles boolean logic, overrides match_type

**Rationale**:
- `"Provider AND interface"` clearly wants boolean search
- Don't make user specify `match_type: "boolean"`
- Bleve QueryStringQuery handles precedence correctly

**Example**: `query: "Provider AND interface", match_type: "fuzzy"` → boolean search wins

### 4. Index Scope (MVP)

**Decision**: Index only `text` field, exclude metadata

**Rationale**:
- Simpler implementation
- Metadata queries can be post-filtered (fast enough)
- Avoids noise from metadata in search results
- Can add metadata fields later without breaking changes

**Future**: Add optional `file:`, `lang:` field queries

### 5. Memory vs Disk

**Decision**: In-memory index only

**Rationale**:
- Fast startup (no disk I/O)
- Simple deployment (no index files to manage)
- Memory is cheap (~20MB for 10K chunks)
- Consistent with chromem-go (also in-memory)

**Trade-off**: Must rebuild on process restart (acceptable, takes ~100ms)

## Testing Strategy

### Unit Tests

```go
// internal/mcp/chunk_manager_test.go
func TestChunkManager_DetectChanges(t *testing.T) {
    // Test add/update/delete detection
}

// internal/mcp/exact_searcher_test.go
func TestExactSearcher_ExactMatch(t *testing.T) {
    // Test basic exact search
}

func TestExactSearcher_BooleanQuery(t *testing.T) {
    // Test AND/OR/NOT operators
}

func TestExactSearcher_FuzzyMatch(t *testing.T) {
    // Test typo tolerance
}

// internal/mcp/searcher_coordinator_test.go
func TestCoordinator_IncrementalReload(t *testing.T) {
    // Test parallel update of both indexes
}
```

### Integration Tests

```go
// internal/mcp/integration_test.go
// +build integration

func TestEndToEnd_ExactSearch(t *testing.T) {
    // 1. Load test chunks
    // 2. Create coordinator with both searchers
    // 3. Query via cortex_exact
    // 4. Verify results
}

func TestEndToEnd_IncrementalUpdate(t *testing.T) {
    // 1. Initial load
    // 2. Modify chunks
    // 3. Reload
    // 4. Verify only changed chunks updated
}
```

### Performance Benchmarks

```go
// internal/mcp/bench_test.go
func BenchmarkExactSearch_Simple(b *testing.B) {
    // Measure query time for exact match
}

func BenchmarkReload_Incremental(b *testing.B) {
    // Measure incremental update vs full rebuild
}

func BenchmarkReload_LargeProject(b *testing.B) {
    // 100K chunks, 1K changed
}
```

## Summary

**cortex_exact** provides fast keyword search to complement semantic and structural queries:

- **Library**: bleve v2 with Scorch index (segmented, incremental)
- **Shared Chunks**: Single load via ChunkManager, shared across searchers
- **Incremental Updates**: Both chromem-go and bleve support add/delete
- **Performance**: ~5-25ms queries, ~45ms reload (8x faster than full rebuild)
- **Memory**: ~80MB total (chromem + bleve + shared chunks)
- **Query Syntax**: Boolean (AND/OR/NOT), phrase, prefix, fuzzy
- **Integration**: Parallel reload, minimal locking, same file watcher

**Value proposition**: Completes the three-tool search strategy (semantic + exact + structural), enabling LLMs to precisely find code identifiers, boolean search across codebase, and handle typos—all while maintaining fast reload times through incremental updates and shared chunk management.
