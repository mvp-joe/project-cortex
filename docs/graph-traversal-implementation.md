# Graph Traversal Implementation

## Algorithm Choice: BFS with Cycle Detection

The `queryCallers()` and `queryCallees()` functions use **iterative BFS (Breadth-First Search)** with explicit cycle detection.

## Why BFS?

1. **Semantic fit**: "Find all callers at depth N" naturally maps to level-order traversal
2. **Memory safety**: Iterative approach avoids stack overflow on deep graphs
3. **Predictable behavior**: Results grouped by depth, deterministic ordering
4. **Efficient for common case**: Most queries are shallow (depth 1-3)

## Why Not dominikbraun/graph Built-in Traversal?

The project uses dominikbraun/graph for the graph data structure, but implements custom traversal because:

1. **Performance**: Reverse indexes (`s.callers`, `s.callees`) provide O(1) lookups
   - Library's `DFS()`/`BFS()` traverse edges: O(E) where E = edge count
   - Reverse index lookup: O(1) per node

2. **Different abstraction**: Library traverses graph edges, we need reverse relationships
   - For "who calls X?": Need reverse lookup (callee → callers)
   - Graph edges go forward (caller → callee)
   - Building reverse graph on every query would be wasteful

3. **Depth tracking**: Library provides `BFSWithDepth()` but at wrong granularity
   - We track depth per result, not per graph level
   - Need fine control over depth limits and result collection

## Cycle Detection

**Implementation**: Boolean visited map
```go
visited := make(map[string]bool)
if !visited[node] {
    visited[node] = true
    // Process node
}
```

**Why this works**:
- Each node visited exactly once
- Cycles detected when attempting to revisit
- Simple, correct, efficient (O(1) lookup)

**Alternative considered**: `graph.CreatesCycle()`
- Not applicable: We're not modifying the graph
- We're querying existing relationships
- Cycle detection happens during traversal, not construction

## Performance Characteristics

- **Time complexity**: O(V + E) where V = vertices, E = edges (BFS standard)
- **Space complexity**: O(V) for visited map and queue
- **Actual performance**:
  - Depth 1: ~180ns (1 allocation)
  - Depth 5: ~750ns (13 allocations)
  - Depth 10: ~1800ns (29 allocations)

## Implementation Details

### Queue Management
```go
type queueItem struct {
    id    string
    depth int
}
queue := []queueItem{}

// Enqueue
queue = append(queue, queueItem{id: caller, depth: nextDepth})

// Dequeue
current := queue[0]
queue = queue[1:]
```

**Optimization opportunity**: Use ring buffer for frequent enqueue/dequeue
- Current slice-based approach is simple and performs well for typical graph sizes
- Consider ring buffer if profiling shows dequeue as bottleneck

### Depth Limiting
```go
if current.depth >= depth {
    continue
}
```

**Key insight**: Check depth before expanding, not during
- Allows precise control over result depth
- Avoids unnecessary queue entries
- Cleaner than recursive depth checking

## Testing Strategy

1. **Cycle tests**: Graphs with A→B→C→A loops
2. **Depth tests**: Verify results at each depth level
3. **Edge cases**: Empty graphs, self-loops, disconnected components
4. **Benchmarks**: Performance across depths 1, 5, 10

## Future Considerations

If graph size grows significantly (>100k nodes):

1. **Parallel BFS**: Process levels concurrently
2. **Bidirectional search**: Meet in middle for path queries
3. **Graph compression**: If many nodes have same relationships
4. **Incremental updates**: Update reverse indexes without full rebuild

Current implementation is optimized for typical use case: graphs with 1k-10k nodes, shallow queries (depth 1-3).
