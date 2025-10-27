---
status: proposed
created_at: 2025-10-26T00:00:00Z
depends_on: [indexer, mcp-server]
---

# Hybrid Filtering Strategy for Chromem-go

## Problem Statement

The current MCP server implementation performs all filtering in application code after chromem-go returns vector search results. This approach has limitations:

1. **Inefficient native filtering utilization**: chromem-go supports WHERE clause filtering on metadata, but we pass `nil` for both WHERE parameters
2. **Potential underdelivery**: When filters are restrictive, the 2x multiplier may not fetch enough matching results
3. **Wasted computation**: Fetching and scoring results that will be filtered out post-hoc

**Current Implementation** (internal/mcp/chromem_searcher.go:146):
```go
// Fetch 2x results with NO native filtering
docs, err := collection.QueryEmbedding(ctx, queryEmbedding, nResults, nil, nil)

// Manual post-filtering for ALL criteria
for _, doc := range docs {
    // Filter chunk_type
    // Filter tags (AND logic)
    // Filter min_score
}
```

## Proposed Solution: Hybrid Filtering

Combine chromem-go's native WHERE filtering with post-filtering for complex criteria:

1. **Native filtering**: Use chromem WHERE clause for first tag and chunk_type
2. **Post-filtering**: Handle additional tags (AND logic), multiple chunk_types, min_score
3. **Early exit**: Stop post-filtering once we reach the requested limit
4. **Structured metadata**: Store tags as `tag_0`, `tag_1`, `tag_2` for direct WHERE access

### Benefits

- **Better targeting**: chromem returns results that already match primary filters
- **Reduced waste**: Fewer irrelevant results in the candidate set
- **Predictable behavior**: Native filter + post-filter split is explicit and documented
- **Improved quality**: More relevant results within the same fetch budget

## Technical Design

### 1. Metadata Storage Format

**Current** (CSV string):
```go
metadata := map[string]string{
    "chunk_type": "symbols",
    "tags": "go,code,symbols",  // CSV string
}
```

**New** (indexed tags):
```go
metadata := map[string]string{
    "chunk_type": "symbols",
    "tag_0": "go",      // First tag - used in WHERE
    "tag_1": "code",    // Additional tags - post-filtered
    "tag_2": "symbols",
}
```

### 2. WHERE Filter Construction

```go
const (
    // DefaultResultMultiplier controls over-fetching for post-filtering headroom
    DefaultResultMultiplier = 2
)

func (s *chromemSearcher) buildWhereFilter(options *SearchOptions) map[string]string {
    whereFilter := make(map[string]string)

    // Add chunk type filtering if specified
    if len(options.ChunkTypes) > 0 {
        // Use FIRST chunk type for native WHERE filtering
        // Post-filter handles multiple chunk types
        whereFilter["chunk_type"] = options.ChunkTypes[0]
    }

    // Add tag filtering if specified
    if len(options.Tags) > 0 {
        // Use FIRST tag for native WHERE filtering
        // Post-filter handles additional tags (AND logic)
        whereFilter["tag_0"] = options.Tags[0]
    }

    return whereFilter
}
```

### 3. Hybrid Query Execution

```go
func (s *chromemSearcher) Query(ctx context.Context, query string, options *SearchOptions) ([]*SearchResult, error) {
    // Generate query embedding
    queryEmbedding, err := s.embedProvider.Embed(ctx, []string{query}, embed.EmbedModeQuery)

    // Build native WHERE filter (first tag, first chunk_type)
    whereFilter := s.buildWhereFilter(options)

    // Fetch with multiplier for post-filtering headroom
    nResults := options.Limit * DefaultResultMultiplier

    // Native chromem filtering via WHERE clause
    docs, err := s.collection.QueryEmbedding(
        ctx,
        queryEmbedding[0],
        nResults,
        whereFilter,           // Native filter (first values)
        nil,                   // WhereDocument unused
    )

    // Post-filter for additional criteria
    return s.convertAndFilterResults(docs, options)
}
```

### 4. Post-Filtering with Early Exit

```go
func (s *chromemSearcher) convertAndFilterResults(chromemResults []chromem.Result, options *SearchOptions) ([]*SearchResult, error) {
    results := make([]*SearchResult, 0, options.Limit)

    for _, doc := range chromemResults {
        chunk := s.chunkMap[doc.ID]
        if chunk == nil {
            continue
        }

        // Post-filter: Multiple chunk types
        if len(options.ChunkTypes) > 1 {
            hasMatchingType := false
            for _, requiredType := range options.ChunkTypes {
                if chunk.ChunkType == requiredType {
                    hasMatchingType = true
                    break
                }
            }
            if !hasMatchingType {
                continue
            }
        }

        // Post-filter: Additional tags (skip tag_0, already filtered by WHERE)
        if len(options.Tags) > 1 {
            hasAllTags := true
            for _, requiredTag := range options.Tags[1:] {  // Skip first tag
                if !slices.Contains(chunk.Tags, requiredTag) {
                    hasAllTags = false
                    break
                }
            }
            if !hasAllTags {
                continue
            }
        }

        // Post-filter: Minimum score
        if options.MinScore > 0 && doc.Similarity < float32(options.MinScore) {
            continue
        }

        results = append(results, &SearchResult{
            Chunk:         chunk,
            CombinedScore: float64(doc.Similarity),
        })

        // Early exit: Stop once we have enough results
        if len(results) >= options.Limit {
            break
        }
    }

    return results, nil
}
```

## Implementation Changes

### Files to Modify

1. **internal/indexer/chunker.go** (or metadata builder)
   - Change tag storage from CSV string to indexed format (`tag_0`, `tag_1`, etc.)
   - Update metadata building for code and doc chunks

2. **internal/mcp/chromem_searcher.go**
   - Add `buildWhereFilter()` method
   - Update `Query()` to use WHERE filters
   - Modify `convertAndFilterResults()` for early exit and skip first tag
   - Add `DefaultResultMultiplier` constant

3. **internal/mcp/types.go** (if applicable)
   - Add `DefaultResultMultiplier = 2` constant with documentation

### Backward Compatibility

**Not required** - Project not yet released. Existing users will re-index from scratch.

## Testing Strategy

### Unit Tests

1. **buildWhereFilter()**: Verify correct WHERE map construction
   - Single tag → `{"tag_0": "go"}`
   - Multiple tags → `{"tag_0": "go"}` (others post-filtered)
   - Single chunk_type → `{"chunk_type": "symbols"}`
   - Combined → `{"chunk_type": "symbols", "tag_0": "go"}`

2. **convertAndFilterResults()**: Verify post-filtering logic
   - Multiple chunk_types correctly filtered
   - Additional tags (tag_1, tag_2) correctly filtered with AND logic
   - Early exit when limit reached
   - MinScore filtering still works

### Integration Tests

1. **Query with single tag**: Verify native WHERE used, no post-filtering
2. **Query with multiple tags**: Verify first tag in WHERE, rest post-filtered
3. **Query with restrictive filters**: Verify result count reaches limit (no underdelivery)
4. **Query with no filters**: Verify pure vector similarity search

### Performance Tests

Compare query latency before/after:
- Single tag filter (should be faster - native WHERE)
- Multiple tag filter (similar - hybrid approach)
- No filters (identical - pure vector search)

## Example Scenarios

### Scenario 1: Architecture Documentation Search

**Query**: "authentication design decisions"
**Filters**: `chunk_types=["documentation"], tags=["architecture"]`

**Execution**:
```go
whereFilter = {
    "chunk_type": "documentation",  // Native WHERE
    "tag_0": "architecture",        // Native WHERE
}
// chromem returns only docs tagged "architecture"
// No post-filtering needed (only one value per filter)
```

### Scenario 2: Multi-Tag Code Search

**Query**: "error handling patterns"
**Filters**: `tags=["go", "code", "error-handling"]`

**Execution**:
```go
whereFilter = {
    "tag_0": "go",  // Native WHERE
}
// chromem returns Go-tagged results
// Post-filter checks for "code" AND "error-handling" tags
```

### Scenario 3: Multiple Chunk Types

**Query**: "authentication implementation"
**Filters**: `chunk_types=["symbols", "definitions"]`

**Execution**:
```go
whereFilter = {
    "chunk_type": "symbols",  // Native WHERE
}
// chromem returns symbol chunks
// Post-filter includes definition chunks too
```

## Non-Goals

- **Dynamic multiplier adjustment**: Keep fixed 2x multiplier for simplicity
- **Iterative fetching**: Don't fetch more if post-filter underdelivers (acceptable trade-off)
- **OR logic for tags**: Continue requiring ALL tags (AND logic)
- **WhereDocument filtering**: Not used (text content matching not needed)

## Rollout Plan

1. Update indexer to use `tag_0`, `tag_1`, `tag_2` metadata format
2. Update MCP server to use hybrid filtering
3. Add unit tests for WHERE filter construction and post-filtering
4. Add integration tests for query scenarios
5. Document in MCP server spec (specs/mcp-server.md) under "Search Algorithm" section

## Success Metrics

- **Result quality**: Users get full `limit` results when filters are reasonable
- **Performance**: Query latency unchanged or improved for filtered searches
- **Code clarity**: Filtering logic is explicit (native vs. post-filter split documented)
- **Maintainability**: Easy to understand which filters use WHERE vs. post-filtering
