---
status: implemented
started_at: 2025-10-29T00:00:00Z
completed_at: 2025-11-02T00:00:00Z
dependencies: []
---

# cortex_pattern Specification

## Purpose

The `cortex_pattern` tool enables structural code pattern matching using Abstract Syntax Tree (AST) analysis via ast-grep. Unlike text-based search (`cortex_exact`) or semantic search (`cortex_search`), `cortex_pattern` understands code structure, allowing LLMs to find complex patterns, anti-patterns, and language-specific idioms that are difficult or impossible to express with text queries.

## Core Concept

**Input**: AST pattern (code-like syntax with metavariables), target language, optional file filters

**Process**: Download/cache ast-grep binary → Construct safe command → Execute pattern match → Parse JSON results

**Output**: Structured matches with file paths, line ranges, matched code, surrounding context, and extracted metavariables

## Technology Stack

- **Language**: Go 1.25+
- **External Binary**: ast-grep (`sg`) v0.29.0+
  - Binary size: ~44MB
  - Cache location: `~/.cortex/bin/ast-grep[.exe]`
  - Download from: `https://github.com/ast-grep/ast-grep/releases`
- **Supported Languages**: Same as tree-sitter (go, typescript, javascript, tsx, jsx, python, rust, c, cpp, java, php, ruby)

## Use Cases

### What cortex_pattern Enables

1. **Security Vulnerability Detection**
   - Find SQL concatenation: `"SELECT * FROM " + $TABLE`
   - Locate unsafe deserialization patterns
   - Identify hardcoded secrets in code

2. **Anti-Pattern Detection**
   - React hooks in conditionals: `if ($COND) { useState($INIT) }`
   - Deeply nested conditionals (>3 levels)
   - God objects (classes with >20 methods)

3. **Code Refactoring**
   - Find all usages of deprecated APIs
   - Locate specific error handling patterns
   - Identify code duplication via structural similarity

4. **Language-Specific Idioms**
   - Go: Find defer statements with immediate execution: `defer $FUNC()`
   - TypeScript: Locate all React component definitions
   - Python: Find all `with` statements without error handling

### What cortex_pattern Does NOT Replace

- **cortex_exact**: For fast keyword/boolean text search
- **cortex_search**: For semantic/concept-based search
- **cortex_graph**: For relationship traversal (callers, dependencies)

**Relationship**: `cortex_pattern` finds structural patterns, other tools provide context. Example workflow:
1. Use `cortex_pattern` to find all defer statements with immediate evaluation
2. Use `cortex_search` to understand why the code was written that way
3. Use `cortex_graph` to see who calls these functions

## Binary Management

### Download Strategy

Follows the same pattern as `cortex-embed`:

1. **Versioning**: Pinned version (e.g., `v0.29.0`) via constant
2. **Platform Detection**: Auto-detect OS/arch (`darwin-arm64`, `linux-amd64`, etc.)
3. **Download URL Pattern**:
   ```
   https://github.com/ast-grep/ast-grep/releases/download/{version}/ast-grep-{platform}.{ext}
   ```
4. **Caching**: Store in `~/.cortex/bin/ast-grep[.exe]`
5. **Verification**: Run `ast-grep --version` on first use to verify binary

### Lazy Initialization

```go
type AstGrepProvider struct {
    binaryPath string
    version    string
    initialized bool
}

func (p *AstGrepProvider) ensureBinaryInstalled(ctx context.Context) error {
    if p.initialized {
        return nil
    }

    // Check if binary exists
    if fileExists(p.binaryPath) {
        // Verify it works
        if err := p.verifyBinary(ctx); err == nil {
            p.initialized = true
            return nil
        }
    }

    // Download binary
    if err := p.downloadBinary(ctx); err != nil {
        return fmt.Errorf("failed to download ast-grep: %w", err)
    }

    // Verify downloaded binary
    if err := p.verifyBinary(ctx); err != nil {
        return fmt.Errorf("downloaded binary verification failed: %w", err)
    }

    p.initialized = true
    return nil
}

func (p *AstGrepProvider) verifyBinary(ctx context.Context) error {
    cmd := exec.CommandContext(ctx, p.binaryPath, "--version")
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("binary check failed: %w", err)
    }

    // Parse version from output
    // Expected format: "ast-grep 0.29.0"
    if !strings.Contains(string(output), "ast-grep") {
        return fmt.Errorf("invalid binary output: %s", output)
    }

    return nil
}
```

### Platform-Specific Binary Names

```go
func detectPlatform() string {
    os := runtime.GOOS
    arch := runtime.GOARCH

    // Map Go arch names to ast-grep release names
    switch os {
    case "darwin":
        if arch == "arm64" {
            return "aarch64-apple-darwin"
        }
        return "x86_64-apple-darwin"
    case "linux":
        if arch == "arm64" {
            return "aarch64-unknown-linux-gnu"
        }
        return "x86_64-unknown-linux-gnu"
    case "windows":
        return "x86_64-pc-windows-msvc.exe"
    }

    return ""
}
```

## MCP Tool Interface

### Request Schema

```json
{
  "pattern": "defer $FUNC()",
  "language": "go",
  "file_paths": ["internal/**/*.go"],
  "context_lines": 3,
  "strictness": "smart",
  "limit": 50
}
```

**Parameters:**

| Parameter | Type | Required | Default | Validation | Description |
|-----------|------|----------|---------|------------|-------------|
| `pattern` | string | ✅ Yes | - | Non-empty | AST pattern with metavariables |
| `language` | string | ✅ Yes | - | Enum (see below) | Target language |
| `file_paths` | string[] | ❌ No | `[]` | Within project root | File/glob filters |
| `context_lines` | number | ❌ No | `3` | 0-10 | Lines before/after match |
| `strictness` | string | ❌ No | `"smart"` | Enum (see below) | Matching algorithm |
| `limit` | number | ❌ No | `50` | 1-100 | Max results to return |

**Language Enum:**
```go
var supportedLanguages = map[string]bool{
    "go":         true,
    "typescript": true,
    "javascript": true,
    "tsx":        true,
    "jsx":        true,
    "python":     true,
    "rust":       true,
    "c":          true,
    "cpp":        true,
    "java":       true,
    "php":        true,
    "ruby":       true,
}
```

**Strictness Enum:**

| Value | Behavior | Use Case |
|-------|----------|----------|
| `cst` | Match all nodes exactly | Precise structural matching |
| `smart` | Skip unnamed nodes (default) | Balanced precision/flexibility |
| `ast` | Only named AST nodes | Ignore syntax trivia |
| `relaxed` | Ignore comments & unnamed nodes | Semantic matching |
| `signature` | Match only node kinds | Loose structural matching |

### Response Schema

```json
{
  "matches": [
    {
      "file_path": "internal/mcp/server.go",
      "start_line": 42,
      "end_line": 42,
      "match_text": "defer conn.Close()",
      "context": "func (s *Server) handle() {\n\tdefer conn.Close()\n\treturn nil\n}",
      "metavars": {
        "FUNC": "conn.Close"
      }
    }
  ],
  "total": 15,
  "metadata": {
    "took_ms": 450,
    "pattern": "defer $FUNC()",
    "language": "go",
    "strictness": "smart"
  }
}
```

**Response Fields:**

```go
type PatternMatch struct {
    FilePath  string            `json:"file_path"`   // Relative to project root
    StartLine int               `json:"start_line"`  // 1-indexed
    EndLine   int               `json:"end_line"`    // 1-indexed
    MatchText string            `json:"match_text"`  // The matched code
    Context   string            `json:"context"`     // Surrounding lines (from -C flag)
    Metavars  map[string]string `json:"metavars"`    // Extracted metavariables
}

type PatternResponse struct {
    Matches  []PatternMatch `json:"matches"`
    Total    int            `json:"total"`      // Total found (may be > len(Matches) if limited)
    Metadata struct {
        TookMs     int    `json:"took_ms"`
        Pattern    string `json:"pattern"`
        Language   string `json:"language"`
        Strictness string `json:"strictness"`
    } `json:"metadata"`
}
```

## Pattern Syntax

### Metavariables

ast-grep uses code-like patterns with metavariables:

**Single node** (`$VAR`):
```javascript
console.log($ARG)  // Matches: console.log("hello"), console.log(x + y)
```

**Multiple nodes** (`$$$VAR`):
```javascript
function $NAME($$$PARAMS) { $$$BODY }  // Matches any function
```

**Examples by Language:**

**Go:**
```go
// Find all defer statements
defer $FUNC()

// Find error checks
if err != nil { $$$BODY }

// Find functions returning errors
func $NAME($$$PARAMS) error { $$$BODY }

// Find mutex locks
$MU.Lock()

// Find goroutine spawns
go $FUNC($$$ARGS)
```

**TypeScript:**
```typescript
// Find useState calls
useState($INIT)

// Find React components
function $NAME($PROPS): JSX.Element { $$$BODY }

// Find promise chains
$PROMISE.then($$$ARGS)

// Find class inheritance
class $NAME extends $PARENT { $$$BODY }
```

**Python:**
```python
// Find with statements
with $CTX as $VAR: $$$BODY

// Find exception handling
try: $$$BODY
except $EXC: $$$HANDLER

// Find list comprehensions
[$EXPR for $VAR in $ITER]
```

### Context and Selector

For ambiguous patterns, use context + selector:

```yaml
# Instead of just: a = 123
pattern:
  context: "class A { a = 123 }"
  selector: field_definition
```

This helps the parser understand whether `a = 123` is an assignment or a class field.

## Command Construction

### Safe Argument Building

**Never use shell execution**. Build argv directly:

```go
func buildAstGrepCommand(req *PatternRequest, projectRoot string) *exec.Cmd {
    args := []string{
        "--pattern", req.Pattern,
        "--lang", req.Language,
        "--json=compact",  // Always use compact JSON output
    }

    // Context lines (default: 3)
    contextLines := 3
    if req.ContextLines != nil {
        contextLines = *req.ContextLines
    }
    if contextLines > 0 {
        args = append(args, "-C", strconv.Itoa(contextLines))
    }

    // Strictness (default: smart)
    strictness := "smart"
    if req.Strictness != "" {
        strictness = req.Strictness
    }
    args = append(args, "--strictness", strictness)

    // File path filters
    if len(req.FilePaths) > 0 {
        // Validate paths are within project root
        for _, path := range req.FilePaths {
            absPath := filepath.Join(projectRoot, path)
            if !strings.HasPrefix(absPath, projectRoot) {
                return nil, fmt.Errorf("path outside project root: %s", path)
            }
        }
        args = append(args, "--globs", strings.Join(req.FilePaths, ","))
    }

    // Search current directory
    args = append(args, ".")

    cmd := exec.Command("ast-grep", args...)
    cmd.Dir = projectRoot  // Run from project root
    return cmd
}
```

### Flags We Always Use

- `--json=compact` - Structured output for parsing
- `-C <NUM>` - Context lines (default: 3)
- `--strictness <LEVEL>` - Matching algorithm (default: smart)
- `--lang <LANG>` - Language specification (required)

### Flags We Never Use

These are mutation operations, never exposed to MCP:

- `--rewrite <FIX>` - Rewrite matched code
- `--inline` - Apply rewrite inline
- `--update-all` - Apply all rewrites without confirmation
- `-U` - Short for `--update-all`
- `-i` - Interactive mode

**Security model**: We control the command construction entirely. LLM provides high-level parameters, we build the safe command.

## Performance Characteristics

### Execution Time

- **Small repos (<1000 files)**: 100-500ms
- **Medium repos (1000-10K files)**: 500ms-2s
- **Large repos (>10K files)**: 2s-5s

**Timeout**: 30 seconds (configurable)

```go
ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()

cmd := exec.CommandContext(ctx, "ast-grep", args...)
```

If timeout expires, return error to LLM:
```go
if ctx.Err() == context.DeadlineExceeded {
    return mcp.NewToolResultError("Pattern search timed out (30s)")
}
```

### Result Limiting

ast-grep doesn't have a built-in `--limit` flag. We post-process:

```go
func limitResults(matches []PatternMatch, limit int) []PatternMatch {
    if len(matches) <= limit {
        return matches
    }
    return matches[:limit]
}
```

**Default limit**: 50 matches
**Max limit**: 100 matches

Rationale: LLMs struggle with >100 results. Better to show top matches and let LLM refine the query.

### Memory Usage

ast-grep streams results, so memory usage is constant (~10-20MB) regardless of result count.

## Error Handling

### User Errors (MCP tool errors)

Return via `mcp.NewToolResultError()`:

```go
// Invalid pattern syntax
if err := validatePattern(req.Pattern); err != nil {
    return mcp.NewToolResultError(fmt.Sprintf("Invalid pattern: %v", err))
}

// Unsupported language
if !supportedLanguages[req.Language] {
    return mcp.NewToolResultError(fmt.Sprintf("Unsupported language: %s", req.Language))
}

// Path outside project root
if !isWithinProjectRoot(path) {
    return mcp.NewToolResultError(fmt.Sprintf("Path outside project: %s", path))
}

// Timeout
if ctx.Err() == context.DeadlineExceeded {
    return mcp.NewToolResultError("Pattern search timed out (30s)")
}
```

### System Errors (internal failures)

Return via `error`:

```go
// Binary download failed
if err := provider.ensureBinaryInstalled(ctx); err != nil {
    return nil, fmt.Errorf("ast-grep binary unavailable: %w", err)
}

// Command execution failed (not timeout)
if err != nil && ctx.Err() == nil {
    return nil, fmt.Errorf("ast-grep execution failed: %w", err)
}

// JSON parsing failed
if err := json.Unmarshal(output, &result); err != nil {
    return nil, fmt.Errorf("failed to parse ast-grep output: %w", err)
}
```

### ast-grep Error Output

ast-grep returns errors on stderr. Capture and wrap:

```go
cmd := exec.CommandContext(ctx, "ast-grep", args...)
var stdout, stderr bytes.Buffer
cmd.Stdout = &stdout
cmd.Stderr = &stderr

if err := cmd.Run(); err != nil {
    if stderr.Len() > 0 {
        return mcp.NewToolResultError(fmt.Sprintf("Pattern error: %s", stderr.String()))
    }
    return nil, err
}
```

## Integration with Existing Tools

### When to Use cortex_pattern

**Use cortex_pattern when:**
- Finding structural code patterns (not just text)
- Detecting anti-patterns or code smells
- Locating language-specific idioms
- Searching for security vulnerabilities
- Pattern involves AST structure (nesting, relationships)

**Example queries that need cortex_pattern:**
- "Find all defer statements with immediate function calls"
- "Locate React hooks used inside conditionals"
- "Show me functions with >5 parameters"
- "Find SQL queries built with string concatenation"

### When to Use cortex_exact

**Use cortex_exact when:**
- Finding exact identifiers or keywords
- Boolean text queries (AND, OR, NOT)
- Phrase matching ("error handling")
- Fast keyword search

**Example queries for cortex_exact:**
- "Find all occurrences of `sync.RWMutex`"
- "Search for `authentication AND -test`"

### When to Use cortex_search

**Use cortex_search when:**
- Understanding concepts ("how does authentication work")
- Finding code by functionality, not structure
- Semantic similarity ("show me error handling patterns")

**Example queries for cortex_search:**
- "How is the database connection managed?"
- "Find authentication-related code"

### When to Use cortex_graph

**Use cortex_graph when:**
- Finding relationships (who calls this function?)
- Dependency analysis (what does this package import?)
- Traversal queries (callers at depth 3)

**Example queries for cortex_graph:**
- "Who calls the `Embed` function?"
- "What packages depend on `internal/mcp`?"

### Hybrid Workflows

**Pattern 1: Find structural pattern → Understand semantically**
```
1. cortex_pattern: Find all defer statements with immediate execution
2. cortex_search: "Why are these defer statements dangerous?"
```

**Pattern 2: Find pattern → Explore relationships**
```
1. cortex_pattern: Find all functions with >10 parameters
2. cortex_graph: "Who calls these complex functions?"
```

**Pattern 3: Boolean filter → Structural analysis**
```
1. cortex_exact: Find all files containing "authentication"
2. cortex_pattern: Find authentication patterns in those files
```

### Integration with Unified Cache

**cortex_pattern operates on source files directly** (not cached data).

Unlike `cortex_search`, `cortex_exact`, `cortex_files`, and `cortex_graph` which query the unified SQLite cache, `cortex_pattern` runs ast-grep directly on source files in the project directory.

**Why this is appropriate:**

1. **Pattern matching requires current file state**: AST patterns must match against the actual source code, not cached representations
2. **ast-grep is fast**: Sub-second searches on typical codebases (<10K files)
3. **No cache dependency**: Works even if cache is stale or missing
4. **Simpler architecture**: No need to cache AST representations

**Relationship to unified cache:**

```
cortex_search    ────┐
cortex_exact     ────┤
cortex_files     ────┼──> Query unified SQLite cache
cortex_graph     ────┘     (~/.cortex/cache/{key}/branches/{branch}.db)

cortex_pattern   ────────> Read source files directly
                            (project directory)
```

**Branch awareness:**

Since `cortex_pattern` operates on source files, it automatically reflects the current git branch (whatever is checked out in the working directory). No special branch switching logic is needed.

**Integration point with daemon:**

In daemon mode, `cortex_pattern` still operates on source files but can optionally use file metadata from the cache to filter which files to search:

```go
func (d *Daemon) handleCortexPattern(query *PatternQuery) (*PatternResult, error) {
    // Optional: Filter files using cache metadata
    var filesToSearch []string
    if query.FileFilter != "" {
        // Query cache for matching files
        rows, _ := d.cacheDB.Query(`
            SELECT file_path FROM files
            WHERE language = ? OR module_path LIKE ?
        `, query.Language, query.ModuleFilter)
        // ... build filesToSearch from rows
    } else {
        // Search all source files
        filesToSearch = getAllSourceFiles(d.projectPath)
    }

    // Run ast-grep on filtered files
    return d.patternSearcher.Search(query.Pattern, filesToSearch)
}
```

**Benefits:**
- Leverages cache for smart file filtering
- Still runs patterns on actual source files
- Best of both worlds (cache optimization + live AST matching)

## MCP Tool Registration

```go
func AddCortexPatternTool(s *server.MCPServer, provider PatternSearcher) {
    tool := mcp.NewTool(
        "cortex_pattern",
        mcp.WithDescription("Search code using structural AST patterns. Use for finding anti-patterns, code smells, language-specific idioms, and complex structural patterns that text search cannot handle."),
        mcp.WithString("pattern",
            mcp.Required(),
            mcp.Description("AST pattern with metavariables (e.g., 'defer $FUNC()' or 'useState($INIT)')")),
        mcp.WithString("language",
            mcp.Required(),
            mcp.Description("Target language: go, typescript, javascript, tsx, jsx, python, rust, c, cpp, java, php, ruby")),
        mcp.WithArray("file_paths",
            mcp.Description("Optional file/glob filters (e.g., ['internal/**/*.go'])")),
        mcp.WithNumber("context_lines",
            mcp.Description("Lines of context before/after match (0-10, default: 3)")),
        mcp.WithString("strictness",
            mcp.Description("Matching algorithm: cst, smart (default), ast, relaxed, signature")),
        mcp.WithNumber("limit",
            mcp.Description("Maximum results to return (1-100, default: 50)")),
        mcp.WithReadOnlyHintAnnotation(true),
        mcp.WithDestructiveHintAnnotation(false),
    )

    handler := createCortexPatternHandler(provider)
    s.AddTool(tool, handler)
}

func createCortexPatternHandler(provider PatternSearcher) mcp.ToolHandler {
    return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
        var req PatternRequest
        if err := json.Unmarshal([]byte(request.Params.Arguments), &req); err != nil {
            return mcp.NewToolResultError(fmt.Sprintf("Invalid request: %v", err)), nil
        }

        // Validate request
        if err := validatePatternRequest(&req); err != nil {
            return mcp.NewToolResultError(err.Error()), nil
        }

        // Execute pattern search
        result, err := provider.Search(ctx, &req)
        if err != nil {
            return nil, err
        }

        // Return as JSON
        jsonBytes, err := json.Marshal(result)
        if err != nil {
            return nil, fmt.Errorf("failed to marshal response: %w", err)
        }

        return mcp.NewToolResultText(string(jsonBytes)), nil
    }
}
```

## Testing Strategy

### Unit Tests

**Binary management:**
```go
func TestDownloadBinary(t *testing.T)
func TestVerifyBinary(t *testing.T)
func TestPlatformDetection(t *testing.T)
```

**Command construction:**
```go
func TestBuildCommand(t *testing.T)
func TestBuildCommand_WithFilePaths(t *testing.T)
func TestBuildCommand_WithContextLines(t *testing.T)
func TestBuildCommand_WithStrictness(t *testing.T)
func TestBuildCommand_PathTraversalPrevention(t *testing.T)
```

**Validation:**
```go
func TestValidateLanguage(t *testing.T)
func TestValidateStrictness(t *testing.T)
func TestValidateFilePaths(t *testing.T)
```

### Integration Tests

**With real ast-grep binary:**

```go
//go:build integration

func TestPatternSearch_Defer(t *testing.T) {
    // Create test Go file with defer statements
    testFile := createTempGoFile(`
        func example() {
            defer conn.Close()
            defer cleanup()
        }
    `)

    provider := NewAstGrepProvider()
    result, err := provider.Search(ctx, &PatternRequest{
        Pattern: "defer $FUNC()",
        Language: "go",
    })

    require.NoError(t, err)
    assert.Equal(t, 2, len(result.Matches))
    assert.Contains(t, result.Matches[0].MatchText, "defer conn.Close()")
}

func TestPatternSearch_ReactHooks(t *testing.T) {
    testFile := createTempTSXFile(`
        function Component() {
            if (condition) {
                useState(0);  // Anti-pattern!
            }
            return <div />;
        }
    `)

    result, err := provider.Search(ctx, &PatternRequest{
        Pattern: "if ($COND) { useState($INIT) }",
        Language: "tsx",
        Strictness: "relaxed",
    })

    require.NoError(t, err)
    assert.Equal(t, 1, len(result.Matches))
}
```

### MCP Protocol Tests

```go
func TestMCPTool_PatternSearch(t *testing.T) {
    // Test MCP request/response serialization
    request := mcp.CallToolRequest{
        Params: mcp.CallToolParams{
            Arguments: `{
                "pattern": "defer $FUNC()",
                "language": "go"
            }`,
        },
    }

    result, err := handler(ctx, request)
    require.NoError(t, err)

    var response PatternResponse
    json.Unmarshal([]byte(result.Content[0].Text), &response)
    assert.NotEmpty(t, response.Matches)
}

func TestMCPTool_InvalidLanguage(t *testing.T) {
    request := mcp.CallToolRequest{
        Params: mcp.CallToolParams{
            Arguments: `{
                "pattern": "test",
                "language": "invalid"
            }`,
        },
    }

    result, err := handler(ctx, request)
    require.NoError(t, err)
    assert.True(t, result.IsError)
    assert.Contains(t, result.Content[0].Text, "Unsupported language")
}
```

## Implementation Checklist

### Phase 1: Binary Management
- ✅ Implement platform detection logic for ast-grep binary names (with tests)
- ✅ Implement binary download from GitHub releases (with tests)
- ✅ Implement binary caching in `~/.cortex/bin/` (with tests)
- ✅ Implement binary verification via `--version` check (with tests)
- ✅ Implement lazy initialization pattern for AstGrepProvider (with tests)

### Phase 2: Command Construction
- ✅ Implement safe argv building without shell execution (with tests)
- ✅ Implement language validation against supported languages (with tests)
- ✅ Implement strictness validation (with tests)
- ✅ Implement file path validation to prevent directory traversal (with tests)
- ✅ Implement context lines and limit parameters (with tests)

### Phase 3: Execution & Parsing
- ✅ Implement ast-grep execution with 30s timeout (with tests)
- ✅ Implement JSON output parsing for compact format (with tests)
- ✅ Implement transformation to PatternResponse format (with tests)
- ✅ Implement metavariable extraction (with tests)
- ✅ Implement result limiting and context handling (with integration tests)

### Phase 4: MCP Tool Integration
- ✅ Define PatternSearcher interface in internal/pattern/
- ✅ Implement AstGrepProvider with all phases 1-3 (with tests)
- ✅ Implement MCP tool registration in internal/mcp/tools_pattern.go
- ✅ Implement request/response validation and error handling (with tests)
- ✅ Add cortex_pattern tool to MCP server initialization

### Phase 5: Documentation & CLI Integration
- ✅ Update CLAUDE.md with cortex_pattern usage guidance and examples
- ✅ Add pattern syntax examples for each supported language to docs
- ✅ Document hybrid workflows (pattern + search/exact/graph)
- ✅ Verify MCP tool works end-to-end via build and test verification

## Future Enhancements

### Potential Additions

1. **Pattern library**: Pre-defined patterns for common anti-patterns
2. **Severity levels**: Categorize patterns by severity (error, warning, info)
3. **Custom patterns**: User-defined pattern files (YAML config)
4. **Batch queries**: Execute multiple patterns in one call
5. **Caching**: Cache results for repeated patterns
6. **Rewrite suggestions**: Safe refactoring suggestions (read-only, no execution)

### Integration Opportunities

1. **cortex_files integration**: Use stats to prioritize which files to search
2. **cortex_graph integration**: Find patterns, then explore call graphs
3. **Incremental search**: Only search changed files (via file watcher)
