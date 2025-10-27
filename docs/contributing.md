# Contributing to Project Cortex

Thank you for your interest in contributing to Project Cortex! This guide will help you get started with development, testing, and contributing code.

## Table of Contents

- [Development Setup](#development-setup)
- [Project Structure](#project-structure)
- [Building and Testing](#building-and-testing)
- [Adding Language Support](#adding-language-support)
- [Code Style](#code-style)
- [Submitting Changes](#submitting-changes)
- [Areas for Contribution](#areas-for-contribution)

## Development Setup

### Prerequisites

- **Go 1.25+**: [Install Go](https://golang.org/doc/install)
- **Git**: For version control
- **Task**: For build automation ([Install Task](https://taskfile.dev/installation/))
- **tree-sitter CLI** (optional): For testing parsers

### Clone the Repository

```bash
git clone https://github.com/mvp-joe/project-cortex.git
cd project-cortex
```

### Install Dependencies

```bash
go mod download
```

### Build the Project

```bash
task build
```

Binary will be in `bin/cortex`.

### Run Tests

```bash
task test
```

### Install Locally

```bash
task install
```

This installs to `$GOPATH/bin/cortex`.

## Project Structure

```
project-cortex/
├── cmd/
│   └── cortex/           # Main entry point
│       └── main.go
├── internal/
│   ├── cli/              # CLI commands (Cobra)
│   │   ├── root.go       # Root command
│   │   ├── index.go      # Index command
│   │   ├── mcp.go        # MCP server command
│   │   └── ...
│   ├── parser/           # Tree-sitter parsers
│   │   ├── parser.go     # Parser interface
│   │   ├── go.go         # Go parser
│   │   ├── typescript.go # TypeScript parser
│   │   └── ...
│   ├── extractor/        # Code extractors
│   │   ├── extractor.go  # Extractor interface
│   │   ├── symbols.go    # Symbol extraction
│   │   ├── definitions.go # Definition extraction
│   │   └── data.go       # Data extraction
│   ├── chunker/          # Chunking logic
│   │   ├── chunker.go
│   │   └── markdown.go
│   ├── embedder/         # Embedding generation
│   │   ├── embedder.go   # Embedder interface
│   │   ├── local.go      # Local embedder
│   │   ├── openai.go     # OpenAI embedder
│   │   └── ...
│   ├── indexer/          # Main indexing logic
│   │   ├── indexer.go
│   │   ├── incremental.go
│   │   └── watch.go
│   ├── mcp/              # MCP server
│   │   ├── server.go
│   │   ├── tools.go
│   │   └── protocol.go
│   ├── storage/          # Chunk storage
│   │   ├── storage.go
│   │   └── json.go
│   └── config/           # Configuration
│       └── config.go
├── pkg/                  # Public packages (if any)
├── docs/                 # Documentation
├── Taskfile.yml          # Build automation
├── go.mod
└── README.md
```

## Building and Testing

### Build Commands

```bash
# Build binary
task build

# Clean build artifacts
task clean

# Format code
task fmt

# Run linter
task lint

# Run vet
task vet

# All checks
task test lint vet
```

### Running Tests

```bash
# All tests
task test

# With coverage
task coverage

# Specific package
go test ./internal/parser/...

# Verbose output
go test -v ./...

# Run specific test
go test -run TestGoParser ./internal/parser/
```

### Manual Testing

Test the indexer on a sample project:

```bash
# Create a test project
mkdir -p /tmp/test-project
cd /tmp/test-project
echo 'package main\n\nfunc main() {}' > main.go

# Run indexer
cortex index

# Check output
ls -lah .cortex/chunks/

# Test MCP server
cortex mcp --log-level debug
```

## Adding Language Support

Adding support for a new language involves three steps:

### 1. Add Tree-sitter Grammar

Install the tree-sitter grammar as a Go dependency:

```bash
go get github.com/tree-sitter/tree-sitter-<language>
```

### 2. Create Parser Implementation

Create `internal/parser/<language>.go`:

```go
package parser

import (
    "github.com/smacker/go-tree-sitter"
    tree_sitter_<language> "github.com/tree-sitter/tree-sitter-<language>"
)

type LanguageParser struct {
    language *tree_sitter.Language
}

func NewLanguageParser() *LanguageParser {
    return &LanguageParser{
        language: tree_sitter.<language>.GetLanguage(),
    }
}

func (p *LanguageParser) Parse(content []byte) (*tree_sitter.Tree, error) {
    parser := tree_sitter.NewParser()
    parser.SetLanguage(p.language)
    return parser.Parse(content), nil
}

func (p *LanguageParser) ExtractSymbols(tree *tree_sitter.Tree, content []byte) (map[string]interface{}, error) {
    // Implement symbol extraction using tree-sitter queries
    // See internal/parser/go.go for reference
}

func (p *LanguageParser) ExtractDefinitions(tree *tree_sitter.Tree, content []byte) ([]Definition, error) {
    // Implement definition extraction
}

func (p *LanguageParser) ExtractData(tree *tree_sitter.Tree, content []byte) ([]DataItem, error) {
    // Implement data extraction
}
```

### 3. Write Tree-sitter Queries

Use tree-sitter queries to extract patterns:

```go
// Example: Extract function definitions
query := `
    (function_declaration
        name: (identifier) @name
        parameters: (parameter_list) @params
        return_type: (_)? @return
    ) @function
`

// Execute query
q, err := tree_sitter.NewQuery([]byte(query), p.language)
if err != nil {
    return nil, err
}

cursor := tree_sitter.NewQueryCursor()
cursor.Exec(q, tree.RootNode())

// Process matches
for {
    match, ok := cursor.NextMatch()
    if !ok {
        break
    }
    // Extract function info from match
}
```

### 4. Register the Parser

Add to `internal/parser/registry.go`:

```go
func init() {
    Register("language", NewLanguageParser())
}
```

### 5. Add Tests

Create `internal/parser/<language>_test.go`:

```go
package parser

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestLanguageParser_ExtractSymbols(t *testing.T) {
    parser := NewLanguageParser()
    content := []byte(`
        // Sample language code
    `)

    tree, err := parser.Parse(content)
    assert.NoError(t, err)

    symbols, err := parser.ExtractSymbols(tree, content)
    assert.NoError(t, err)
    assert.NotNil(t, symbols)

    // Assert expected symbols
    assert.Equal(t, "expected_package", symbols["package"])
}
```

### 6. Document the Language

Add a section to `docs/languages.md` documenting:
- File extensions
- What gets extracted at each tier
- Example output
- Language-specific configuration

### Example: Adding Kotlin Support

1. **Install grammar**:
   ```bash
   go get github.com/tree-sitter/tree-sitter-kotlin
   ```

2. **Create parser**:
   ```go
   // internal/parser/kotlin.go
   package parser

   import (
       sitter "github.com/smacker/go-tree-sitter"
       "github.com/tree-sitter/tree-sitter-kotlin"
   )

   type KotlinParser struct {
       language *sitter.Language
   }

   func NewKotlinParser() *KotlinParser {
       return &KotlinParser{
           language: kotlin.GetLanguage(),
       }
   }

   func (p *KotlinParser) ExtractSymbols(tree *sitter.Tree, content []byte) (map[string]interface{}, error) {
       query := `
           (package_header
               (identifier) @package
           )
           (class_declaration
               (type_identifier) @class
           )
           (function_declaration
               (simple_identifier) @function
           )
       `
       // ... execute query and return results
   }
   ```

3. **Register**:
   ```go
   // internal/parser/registry.go
   func init() {
       Register("kotlin", NewKotlinParser())
   }
   ```

4. **Test**:
   ```bash
   go test ./internal/parser/ -run TestKotlin
   ```

## Code Style

### Go Style

Follow standard Go conventions:

- Use `gofmt` for formatting
- Follow [Effective Go](https://golang.org/doc/effective_go)
- Use meaningful variable names
- Write godoc comments for exported functions

```go
// Good
func ExtractSymbols(tree *sitter.Tree, content []byte) (map[string]interface{}, error) {
    // Implementation
}

// Bad
func es(t *sitter.Tree, c []byte) (map[string]interface{}, error) {
    // Implementation
}
```

### Error Handling

Always handle errors explicitly:

```go
// Good
tree, err := parser.Parse(content)
if err != nil {
    return nil, fmt.Errorf("failed to parse: %w", err)
}

// Bad
tree, _ := parser.Parse(content)
```

### Logging

Use structured logging:

```go
import "log/slog"

slog.Info("indexing file", "path", filePath, "language", lang)
slog.Error("failed to parse", "error", err, "file", filePath)
```

### Testing

Write table-driven tests:

```go
func TestExtractSymbols(t *testing.T) {
    tests := []struct {
        name     string
        input    string
        expected map[string]interface{}
    }{
        {
            name:  "simple function",
            input: "func main() {}",
            expected: map[string]interface{}{
                "functions": []string{"main"},
            },
        },
        // More test cases...
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := ExtractSymbols([]byte(tt.input))
            assert.NoError(t, err)
            assert.Equal(t, tt.expected, result)
        })
    }
}
```

## Submitting Changes

### 1. Fork the Repository

Click "Fork" on GitHub and clone your fork:

```bash
git clone https://github.com/yourusername/project-cortex.git
cd project-cortex
git remote add upstream https://github.com/mvp-joe/project-cortex.git
```

### 2. Create a Branch

```bash
git checkout -b feature/add-kotlin-support
```

Use descriptive branch names:
- `feature/add-<language>-support`
- `fix/parser-crash-on-<issue>`
- `docs/improve-<section>`

### 3. Make Changes

- Write code
- Add tests
- Update documentation
- Run `task test lint`

### 4. Commit Changes

Write clear commit messages:

```bash
git add .
git commit -m "feat: add Kotlin language support

- Add tree-sitter-kotlin parser
- Implement symbol/definition/data extraction
- Add tests for Kotlin parser
- Update docs/languages.md
"
```

Commit message format:
```
<type>: <short summary>

<optional body>

<optional footer>
```

Types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`

### 5. Push and Create PR

```bash
git push origin feature/add-kotlin-support
```

Create a pull request on GitHub with:
- Clear title
- Description of changes
- Related issues (if any)
- Screenshots (if UI changes)

### 6. Code Review

- Address review feedback
- Keep commits focused
- Squash if requested

## Areas for Contribution

### High Priority

1. **Language Support**
   - Kotlin, Swift, Scala, Elixir
   - Better JSX/TSX component extraction
   - Improved Python type hint parsing

2. **Performance**
   - Parallel processing optimizations
   - Memory usage improvements
   - Faster incremental indexing

3. **Features**
   - Cross-file symbol resolution
   - Call graph extraction
   - Dependency graph visualization
   - Comment extraction

### Medium Priority

4. **MCP Server**
   - Additional search tools
   - Filtering capabilities
   - Caching improvements
   - Multi-project support

5. **Embeddings**
   - More embedding providers
   - Hybrid search (vector + keyword)
   - Custom embedding models

6. **Documentation**
   - Video tutorials
   - More examples
   - Blog posts

### Lower Priority

7. **Tooling**
   - VS Code extension
   - CLI auto-completion improvements
   - Web UI for browsing chunks

8. **Testing**
   - Integration tests
   - Benchmark suite
   - Fuzzing

## Development Workflow

### Daily Development

```bash
# Pull latest changes
git pull upstream main

# Create feature branch
git checkout -b feature/my-feature

# Make changes, test frequently
task test

# Commit when ready
git commit -m "feat: description"

# Push and create PR
git push origin feature/my-feature
```

### Running in Development

```bash
# Build and run without installing
go run cmd/cortex/main.go index

# Use local build
./bin/cortex index

# Enable debug logging
./bin/cortex --log-level debug index
```

### Debugging

Use `delve` for debugging:

```bash
# Install delve
go install github.com/go-delve/delve/cmd/dlv@latest

# Debug
dlv debug cmd/cortex/main.go -- index
```

## Getting Help

- **Questions**: Open a GitHub Discussion
- **Bugs**: Open a GitHub Issue
- **Security**: Email security@projectcortex.dev
- **Chat**: Join our Discord (link in README)

## Code of Conduct

Be respectful, inclusive, and professional. We follow the [Contributor Covenant](https://www.contributor-covenant.org/).

## License

By contributing to Project Cortex, you agree that your contributions will be licensed under the Apache License 2.0, the same license as the project.

All contributions are covered by the LICENSE file at the root of the repository. No license headers are required in individual source files.

---

Thank you for contributing to ProjectCortex!
