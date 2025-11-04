# Testing Workflow

This document describes the improved testing workflow for Project Cortex, designed to reduce approval friction when running tests with CGO dependencies.

## Problem

Project Cortex uses go-sqlite3 which requires CGO. Running tests requires verbose environment configuration:

```bash
CGO_ENABLED=1 CGO_CFLAGS="-I/Users/josephward/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.32" go test -tags fts5 ./internal/mcp -run TestChunkManager_Load -v 2>&1
```

This creates approval fatigue during development when running individual tests frequently.

## Solution

A three-layer approach for seamless test execution:

### 1. Smart Test Runner Script

**Location**: `scripts/test.sh`

**Features**:
- Auto-detects CGO configuration from `go env GOPATH`
- Supports flexible arguments for all common test scenarios
- Colored output for better readability
- Built-in help with examples

**Usage**:
```bash
# Run specific test (positional syntax)
./scripts/test.sh ./internal/mcp TestChunkManager_Load

# Run specific test (flag syntax)
./scripts/test.sh -run TestChunkManager_Load ./internal/mcp

# Run with verbose output
./scripts/test.sh -v ./internal/mcp TestLoader

# Run with race detector
./scripts/test.sh -r -v ./internal/mcp

# Run with coverage
./scripts/test.sh -c ./internal/graph

# Run all tests in package
./scripts/test.sh ./internal/indexer

# See all options
./scripts/test.sh --help
```

**Flags**:
- `-v, --verbose` - Enable verbose output
- `-r, --race` - Enable race detector
- `-c, --coverage` - Generate coverage report
- `-s, --short` - Run tests in short mode
- `-t, --tags TAGS` - Build tags (default: fts5)
- `-run PATTERN` - Test name pattern (alternative to positional arg)
- `-f, --flags FLAGS` - Additional go test flags

### 2. Enhanced Taskfile Tasks

Three new test tasks added to `Taskfile.yml`:

#### test:one - Run a specific test

```bash
# Basic usage
task test:one PKG=./internal/mcp TEST=TestChunkManager_Load

# With additional flags
task test:one PKG=./internal/mcp TEST=TestLoader FLAGS="-v"
task test:one PKG=./internal/indexer TEST=TestParser FLAGS="-v -race"
```

**Parameters**:
- `PKG` (required) - Package path
- `TEST` (required) - Test name pattern
- `FLAGS` (optional) - Additional flags

#### test:pkg - Run all tests in a package

```bash
# Run all tests in package
task test:pkg PKG=./internal/mcp

# With flags
task test:pkg PKG=./internal/indexer FLAGS="-v"
task test:pkg PKG=./internal/graph FLAGS="-race -v"
```

**Parameters**:
- `PKG` (required) - Package path
- `FLAGS` (optional) - Additional flags

#### test:quick - Quick runner with flexible arguments

```bash
# Most flexible - pass any arguments
task test:quick ARGS="./internal/mcp"
task test:quick ARGS="./internal/mcp TestLoader"
task test:quick ARGS="-v ./internal/mcp TestChunkManager"
task test:quick ARGS="-race -v ./..."
```

**Parameters**:
- `ARGS` - Any combination of flags, package path, and test pattern

### 3. Claude Code Skill

**Location**: `.claude/skills/test/`

When working with the go-engineer or other agents, they can now use the test skill which provides:
- Knowledge of the smart test runner
- Patterns for test discovery
- Best practices for debugging test failures
- Pre-configured workflows

**Usage**: Agents automatically access this when performing test-related tasks.

## Approval Strategy

Add the following to your Claude Code allowlist to avoid repeated approvals:

```json
{
  "Bash(./scripts/test.sh:*)": "auto-approve",
  "Bash(task test:one:*)": "auto-approve",
  "Bash(task test:pkg:*)": "auto-approve",
  "Bash(task test:quick:*)": "auto-approve"
}
```

This allows agents to run any test without asking for approval each time.

## Common Workflows

### During Development

```bash
# Quick test of function you just wrote
task test:one PKG=./internal/mcp TEST=TestNewFunction

# Run all tests in package after refactor
task test:pkg PKG=./internal/indexer FLAGS="-v"

# Check for race conditions
task test:pkg PKG=./internal/graph FLAGS="-race"
```

### Debugging Test Failures

```bash
# Run with verbose output
task test:one PKG=./internal/mcp TEST=TestFailingTest FLAGS="-v"

# Run multiple times to catch flaky tests
task test:one PKG=./internal/mcp TEST=TestFlaky FLAGS="-count=10"

# Run with race detector to find concurrency issues
task test:one PKG=./internal/mcp TEST=TestConcurrent FLAGS="-race -v"
```

### CI/CD Integration

The existing comprehensive test tasks remain unchanged:

```bash
# Run all tests
task test

# Run with coverage
task test:coverage

# Run with race detector
task test:race
```

## Integration with go-engineer Agent

When the go-engineer agent needs to run tests, it will:

1. Use `task test:one` or `task test:pkg` instead of raw `go test` commands
2. Leverage the test skill for best practices
3. Automatically configure CGO without manual intervention
4. Require minimal approvals if commands are allowlisted

## Examples

### Example 1: Test-Driven Development

```bash
# Write test first
vim internal/mcp/new_feature_test.go

# Run test (should fail)
task test:one PKG=./internal/mcp TEST=TestNewFeature FLAGS="-v"

# Implement feature
vim internal/mcp/new_feature.go

# Run test again (should pass)
task test:one PKG=./internal/mcp TEST=TestNewFeature FLAGS="-v"

# Run all package tests to ensure no regressions
task test:pkg PKG=./internal/mcp
```

### Example 2: Debugging Race Conditions

```bash
# Test fails occasionally
task test:one PKG=./internal/graph TEST=TestSearcher FLAGS="-count=20"

# Run with race detector for detailed output
task test:one PKG=./internal/graph TEST=TestSearcher FLAGS="-race -v"

# Fix the issue
vim internal/graph/searcher.go

# Verify fix with many iterations
task test:one PKG=./internal/graph TEST=TestSearcher FLAGS="-race -count=100"
```

### Example 3: Coverage-Driven Development

```bash
# Generate coverage for new code
task test:pkg PKG=./internal/indexer FLAGS="-coverprofile=coverage.out"

# View coverage report
go tool cover -html=coverage.out -o coverage.html
open coverage.html

# Identify untested code paths
# Write additional tests
# Re-run coverage
```

## Environment Variables

The script automatically detects these, but you can override:

- `GOPATH` - Override Go path (default: auto-detected via `go env GOPATH`)
- `SQLITE_VERSION` - Override sqlite3 version (default: v1.14.32)

Example:
```bash
SQLITE_VERSION=v1.14.33 ./scripts/test.sh ./internal/mcp TestLoader
```

## Troubleshooting

### CGO Configuration Issues

If you get CGO-related errors, verify your Go module cache:

```bash
# Check if go-sqlite3 is downloaded
ls $(go env GOPATH)/pkg/mod/github.com/mattn/go-sqlite3@v1.14.32/

# If missing, download it
go mod download github.com/mattn/go-sqlite3
```

### Build Tag Issues

Tests require the `fts5` build tag. The script includes this by default, but if you use `go test` directly:

```bash
# Wrong (missing fts5 tag)
go test ./internal/mcp

# Correct
go test -tags fts5 ./internal/mcp

# Or use the script (tag included automatically)
./scripts/test.sh ./internal/mcp
```

### Permission Issues

If the script isn't executable:

```bash
chmod +x scripts/test.sh
```

## Benefits

1. **No Approval Fatigue**: Add script/tasks to allowlist once, run unlimited tests
2. **Auto-Configuration**: CGO settings detected automatically
3. **Flexible**: Supports all go test flags and patterns
4. **Discoverable**: Clear task names in `task --list`
5. **Documented**: Built-in help and examples
6. **Agent-Friendly**: go-engineer can use these without user intervention
7. **Maintains Existing**: Comprehensive test tasks (`task test`, `task test:coverage`) unchanged

## Migration Guide

### For Users

**Before** (manual approval needed each time):
```bash
CGO_ENABLED=1 CGO_CFLAGS="-I/Users/josephward/go/pkg/mod/github.com/mattn/go-sqlite3@v1.14.32" go test -tags fts5 ./internal/mcp -run TestLoader -v
```

**After** (one-time approval):
```bash
task test:one PKG=./internal/mcp TEST=TestLoader FLAGS="-v"
```

### For Agents

Agents should now use:
- `task test:one` instead of raw `go test` commands
- Test skill workflows for discovery and debugging
- Script flags instead of raw go test flags

## Future Enhancements

Potential improvements:
- [ ] Test result caching (avoid re-running passing tests)
- [ ] Parallel package testing
- [ ] Watch mode integration
- [ ] Test selection by file path (not just package)
- [ ] Integration with test coverage tracking tools