#!/usr/bin/env bash
# Smart Test Runner for Project Cortex
# Automatically configures CGO environment and runs go test with proper flags

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Auto-detect CGO configuration
GOPATH="${GOPATH:-$(go env GOPATH)}"
SQLITE_VERSION="v1.14.32"
CGO_CFLAGS="-I${GOPATH}/pkg/mod/github.com/mattn/go-sqlite3@${SQLITE_VERSION}"

# Default values
VERBOSE=false
RACE=false
COVERAGE=false
SHORT=false
TAGS="fts5"
RUN_PATTERN=""
EXTRA_FLAGS=""

# Usage information
usage() {
    cat << EOF
Usage: $0 [OPTIONS] [PACKAGE] [TEST_PATTERN]

Smart test runner that automatically configures CGO environment for Project Cortex.

OPTIONS:
    -v, --verbose       Enable verbose output
    -r, --race          Enable race detector
    -c, --coverage      Generate coverage report
    -s, --short         Run tests in short mode
    -t, --tags TAGS     Build tags (default: fts5)
    -run PATTERN        Test name pattern to run (alternative to positional arg)
    -f, --flags FLAGS   Additional go test flags
    -h, --help          Show this help message

EXAMPLES:
    # Run all tests in a package
    $0 ./internal/mcp

    # Run specific test (positional syntax)
    $0 ./internal/mcp TestChunkManager_Load

    # Run specific test (flag syntax)
    $0 -run TestChunkManager_Load ./internal/mcp

    # Run with race detector and verbose output
    $0 -r -v ./internal/mcp TestChunkManager_Load

    # Run all tests
    $0 ./...

    # Run with coverage
    $0 -c ./internal/indexer

    # Run with additional flags
    $0 -f "-count=1" ./internal/mcp TestLoader

ENVIRONMENT:
    GOPATH              Override Go path (auto-detected)
    SQLITE_VERSION      Override sqlite3 version (default: v1.14.32)

NOTES:
    - CGO is automatically enabled
    - CGO_CFLAGS automatically set for go-sqlite3
    - Build tags include 'fts5' by default
EOF
    exit 0
}

# Parse arguments
POSITIONAL_ARGS=()
while [[ $# -gt 0 ]]; do
    case $1 in
        -v|--verbose)
            VERBOSE=true
            shift
            ;;
        -r|--race)
            RACE=true
            shift
            ;;
        -c|--coverage)
            COVERAGE=true
            shift
            ;;
        -s|--short)
            SHORT=true
            shift
            ;;
        -t|--tags)
            TAGS="$2"
            shift 2
            ;;
        -run)
            RUN_PATTERN="$2"
            shift 2
            ;;
        -f|--flags)
            EXTRA_FLAGS="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            POSITIONAL_ARGS+=("$1")
            shift
            ;;
    esac
done

# Restore positional parameters
set -- "${POSITIONAL_ARGS[@]}"

# Determine package and test pattern
PACKAGE="${1:-./...}"
# Use -run flag if provided, otherwise use positional arg
if [ -z "$RUN_PATTERN" ]; then
    RUN_PATTERN="${2:-}"
fi

# Build test command
TEST_CMD="CGO_ENABLED=1 CGO_CFLAGS=\"${CGO_CFLAGS}\" go test"

# Add tags
TEST_CMD="${TEST_CMD} -tags ${TAGS}"

# Add flags
if [ "$VERBOSE" = true ]; then
    TEST_CMD="${TEST_CMD} -v"
fi

if [ "$RACE" = true ]; then
    TEST_CMD="${TEST_CMD} -race"
fi

if [ "$COVERAGE" = true ]; then
    COVERAGE_FILE="coverage-$(date +%s).out"
    TEST_CMD="${TEST_CMD} -coverprofile=${COVERAGE_FILE}"
fi

if [ "$SHORT" = true ]; then
    TEST_CMD="${TEST_CMD} -short"
fi

# Add extra flags
if [ -n "$EXTRA_FLAGS" ]; then
    TEST_CMD="${TEST_CMD} ${EXTRA_FLAGS}"
fi

# Add package
TEST_CMD="${TEST_CMD} ${PACKAGE}"

# Add test pattern if specified
if [ -n "$RUN_PATTERN" ]; then
    TEST_CMD="${TEST_CMD} -run ${RUN_PATTERN}"
fi

# Print configuration
echo -e "${YELLOW}=== Project Cortex Test Runner ===${NC}"
echo -e "${GREEN}Package:${NC}     ${PACKAGE}"
if [ -n "$RUN_PATTERN" ]; then
    echo -e "${GREEN}Pattern:${NC}     ${RUN_PATTERN}"
fi
echo -e "${GREEN}CGO_CFLAGS:${NC}  ${CGO_CFLAGS}"
echo -e "${GREEN}Tags:${NC}        ${TAGS}"
if [ "$RACE" = true ]; then
    echo -e "${GREEN}Race:${NC}        enabled"
fi
if [ "$COVERAGE" = true ]; then
    echo -e "${GREEN}Coverage:${NC}    ${COVERAGE_FILE}"
fi
echo ""

# Run tests
eval "${TEST_CMD} 2>&1"
EXIT_CODE=$?

# Handle coverage report
if [ "$COVERAGE" = true ] && [ $EXIT_CODE -eq 0 ]; then
    echo ""
    echo -e "${GREEN}Coverage report:${NC} ${COVERAGE_FILE}"
    echo "Generate HTML: go tool cover -html=${COVERAGE_FILE} -o coverage.html"
fi

exit $EXIT_CODE
