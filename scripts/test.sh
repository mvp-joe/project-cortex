#!/usr/bin/env bash
# Smart Test Runner for Project Cortex
# Uses gotestsum for clean, readable test output

set -uo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Auto-detect CGO configuration
GOPATH="${GOPATH:-$(go env GOPATH)}"
SQLITE_VERSION="v1.14.32"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

CGO_CFLAGS="-I${GOPATH}/pkg/mod/github.com/mattn/go-sqlite3@${SQLITE_VERSION}"
CGO_LDFLAGS="-L${PROJECT_ROOT}/internal/embeddings-ffi/target/release -lembeddings_ffi -framework Security -framework CoreFoundation"

# Default values
VERBOSE=false
RACE=false
COVERAGE=false
SHORT=false
STOP_ON_FAIL=false
TAGS="fts5,rust_ffi"
RUN_PATTERN=""
EXTRA_FLAGS=""

# Usage information
usage() {
    cat << EOF
Usage: $0 [OPTIONS] [PACKAGE] [TEST_PATTERN]

Smart test runner with gotestsum for readable output.

OPTIONS:
    -v, --verbose       Enable verbose output
    -r, --race          Enable race detector
    -c, --coverage      Generate coverage report
    -s, --short         Run tests in short mode
    --stop-on-fail      Stop on first package failure
    -t, --tags TAGS     Build tags (default: fts5,rust_ffi)
    -run PATTERN        Test name pattern
    -f, --flags FLAGS   Additional go test flags
    -h, --help          Show this help message

EXAMPLES:
    $0 ./...                    # Run all tests
    $0 --stop-on-fail ./...     # Stop on first failure
    $0 -v ./internal/cli        # Verbose output for one package
    $0 -run TestFoo ./...       # Run specific test pattern
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
        --stop-on-fail)
            STOP_ON_FAIL=true
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

# Determine package
PACKAGE="${1:-./...}"
if [ -z "$RUN_PATTERN" ]; then
    RUN_PATTERN="${2:-}"
fi

# Print configuration
echo -e "${YELLOW}=== Project Cortex Test Runner ===${NC}"
echo -e "${GREEN}Package:${NC}     ${PACKAGE}"
if [ -n "$RUN_PATTERN" ]; then
    echo -e "${GREEN}Pattern:${NC}     ${RUN_PATTERN}"
fi
echo ""

# Check if gotestsum is available
if ! command -v gotestsum >/dev/null 2>&1; then
    echo -e "${RED}ERROR: gotestsum not found${NC}"
    echo "Install with: go install gotest.tools/gotestsum@latest"
    exit 1
fi

# Build gotestsum command
GOTESTSUM_CMD="CGO_ENABLED=1 CGO_CFLAGS=\"${CGO_CFLAGS}\" CGO_LDFLAGS=\"${CGO_LDFLAGS}\" gotestsum"

# Format: show package names and failed test details
GOTESTSUM_CMD="${GOTESTSUM_CMD} --format pkgname-and-test-fails"

# Use high visibility icons
GOTESTSUM_CMD="${GOTESTSUM_CMD} --format-icons hivis"

# Stop on first failure if requested
if [ "$STOP_ON_FAIL" = true ]; then
    GOTESTSUM_CMD="${GOTESTSUM_CMD} --max-fails 1"
fi

# Separator before go test flags
GOTESTSUM_CMD="${GOTESTSUM_CMD} --"

# Add go test flags
GOTESTSUM_CMD="${GOTESTSUM_CMD} -tags ${TAGS}"

if [ "$VERBOSE" = true ]; then
    GOTESTSUM_CMD="${GOTESTSUM_CMD} -v"
fi

if [ "$RACE" = true ]; then
    GOTESTSUM_CMD="${GOTESTSUM_CMD} -race"
fi

if [ "$SHORT" = true ]; then
    GOTESTSUM_CMD="${GOTESTSUM_CMD} -short"
fi

if [ "$COVERAGE" = true ]; then
    COVERAGE_FILE="coverage-$(date +%s).out"
    GOTESTSUM_CMD="${GOTESTSUM_CMD} -coverprofile=${COVERAGE_FILE}"
fi

if [ -n "$EXTRA_FLAGS" ]; then
    GOTESTSUM_CMD="${GOTESTSUM_CMD} ${EXTRA_FLAGS}"
fi

# Add package
GOTESTSUM_CMD="${GOTESTSUM_CMD} ${PACKAGE}"

# Add test pattern if specified
if [ -n "$RUN_PATTERN" ]; then
    GOTESTSUM_CMD="${GOTESTSUM_CMD} -run ${RUN_PATTERN}"
fi

# Run tests
eval "${GOTESTSUM_CMD}"
EXIT_CODE=$?

# Handle coverage report
if [ "$COVERAGE" = true ] && [ $EXIT_CODE -eq 0 ]; then
    echo ""
    echo -e "${GREEN}Coverage report:${NC} ${COVERAGE_FILE}"
    echo "Generate HTML: go tool cover -html=${COVERAGE_FILE} -o coverage.html"
fi

exit $EXIT_CODE
