# Testing Strategy

This document outlines the testing philosophy and conventions for Project Cortex. Our goal is to ensure the codebase remains fast to iterate on, deeply reliable, and easy for contributors to test at any layer.

We use a layered approach to testing that emphasizes **clear boundaries**, **fast feedback**, and **real-world confidence**.

---

## ✅ Testing Layers

### 1. Unit Tests (Go)

**Scope:**
Test individual Go components in isolation.

**Guidelines:**
- Use [`testify`](https://github.com/stretchr/testify) for assertions and mocking.
- Prefer internal interfaces + explicit injection over reflection or monkeypatching.
- Follow TDD when designing reusable components.
- Favor clarity and coverage over DRY in test cases.

**Examples:**
- `parser_test.go` tests tree-sitter parsing for each supported language
- `chunker_test.go` validates markdown semantic chunking with line tracking
- `provider_test.go` tests embedding provider interface and factory

---

### 2. Integration Tests (Go)

**Scope:**
Test how internal components fit together within the CLI tool:
- Tree-sitter parsing and extraction
- Embedding provider integration (embedding server)
- Vector database operations (chromem-go)
- File watching and hot reload
- MCP protocol integration

**Guidelines:**
- Use real embedding server for embedding tests
- Use chromem-go in-memory for vector search tests
- Test file system operations with `t.TempDir()`
- Prefer `black-box` validation: treat components as units and assert observable behavior

**Examples:**
- Indexer processing a code file end-to-end (parse → chunk → embed → write)
- MCP server loading chunks and executing searches
- File watcher detecting changes and triggering reload
- Incremental indexing with checksum comparison

**File Naming:**
- `*_integration_test.go` in component directories (e.g., `internal/indexer/`, `internal/mcp/`)

---

### 3. CLI E2E Tests (Go)

**Scope:**
Test complete CLI workflows from user perspective:
- Full indexing pipeline (discover → parse → chunk → embed → write)
- MCP server lifecycle (startup → load → search → hot reload → shutdown)
- Watch mode with file changes
- Configuration loading and validation

**Guidelines:**
- Use `t.TempDir()` for isolated test environments
- Create realistic test projects with code and docs
- Test CLI commands via `exec.Command` or direct function calls
- Validate output files and MCP responses

**Examples:**
- `cortex index` on test project → validates chunk files created
- `cortex mcp` starts → client queries → validates search results
- `cortex index --watch` → modify file → validates incremental update
- Invalid config → validates error messages

**File Location:**
```
tests/e2e/
├── indexer_test.go
├── mcp_test.go
└── watch_test.go
```

---

### 4. MCP Protocol Tests (Go)

**Scope:**
Validate MCP protocol compliance and tool interface:
- Tool registration and schema generation
- Request/response serialization
- Error handling per MCP spec
- stdio transport

**Guidelines:**
- Use `github.com/mark3labs/mcp-go` test utilities
- Test against MCP protocol spec
- Validate JSON-RPC 2.0 compliance
- Test tool schema matches implementation

**Examples:**
- `cortex_search` tool registration and schema
- Valid search request → correct response format
- Invalid request → proper MCP error codes
- Large result sets → response size limits

**File Location:**
```
internal/mcp/
├── server_test.go
├── tool_test.go
└── protocol_integration_test.go
```

---

## 🧪 Testing Goals

| Goal | Approach |
|------|----------|
| 🟢 Fast feedback on core components | Use focused unit tests with real dependencies where possible |
| 🔒 Type-safe interfaces | Test against embed.Provider and MCP interfaces |
| ⚙️ Confidence in parsing | Test tree-sitter extraction for all supported languages |
| 🔄 Integration reliability | Test end-to-end indexing and search flows |
| 🌐 MCP protocol compliance | Validate against MCP spec and mcp-go test utilities |
| 📊 Performance awareness | Test indexing speed and search latency with realistic codebases |
| 🧰 Developer confidence | Build test fixtures that mirror real project structures |

---

## File Structure Convention

```bash
cmd/
  cortex/
    main.go
    main_test.go                   # CLI integration tests

internal/
  indexer/
    parser.go
    parser_test.go                 # Unit tests
    parser_integration_test.go     # Integration tests with tree-sitter
    chunker.go
    chunker_test.go
  mcp/
    server.go
    server_test.go
    server_integration_test.go     # MCP protocol integration
  embed/
    provider.go
    provider_test.go
    daemon/
      daemon.go
      daemon_test.go
      daemon_integration_test.go   # Tests with real embedding server
    onnx/
      onnx.go
      onnx_test.go

tests/
  e2e/
    indexer_test.go                # Full indexing pipeline tests
    mcp_test.go                    # MCP server lifecycle tests
    watch_test.go                  # Watch mode tests
  fixtures/
    sample-project/                # Test codebase
      README.md
      main.go
      internal/
        handler.go

testdata/
  code/                            # Sample code for parser tests
    go/
      simple.go
      complex.go
    typescript/
      react.tsx
      types.ts
  docs/
    simple.md
    complex-with-code.md
```

## Test Tools

### Go Testing
- **testify**: Assertions and mocking (`github.com/stretchr/testify`)
- **Standard library**: `testing` package with `t.Parallel()` for concurrent tests
- **Task**: Test runner with better output formatting (via Taskfile)

### Language-Specific Testing
- **tree-sitter**: Official Go bindings (`github.com/tree-sitter/go-tree-sitter`)
- **mcp-go**: MCP protocol testing utilities (`github.com/mark3labs/mcp-go`)
- **chromem-go**: In-memory vector database (`github.com/philippgille/chromem-go`)

### Running Tests

**All Tests:**
```bash
task test              # All tests (unit + integration + e2e)
task test:unit         # Unit tests only
task test:integration  # Integration tests only
task test:e2e          # E2E tests only
task test:coverage     # Generate coverage report
```

**Specific Components:**
```bash
go test ./internal/indexer/...        # Indexer tests
go test ./internal/mcp/...            # MCP server tests
go test ./internal/embed/...          # Embedding provider tests
```

**With Race Detector:**
```bash
task test:race         # Run with -race flag
```

**Verbose Output:**
```bash
go test -v ./internal/indexer/parser_test.go
```
