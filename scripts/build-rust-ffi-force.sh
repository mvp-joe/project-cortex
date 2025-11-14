#!/bin/bash
set -e

PROJECT_ROOT="/Users/josephward/code/project-cortex"
RUST_DIR="$PROJECT_ROOT/internal/embeddings-ffi"
BINARY="$PROJECT_ROOT/bin/cortex-rust"

echo "==> Force cleaning..."
rm -f "$BINARY"
go clean -cache

echo "==> Building Rust library (with verification)..."
cd "$RUST_DIR"
cargo build --release
RUST_LIB="$RUST_DIR/target/release/libembeddings_ffi.a"

if ! strings "$RUST_LIB" | grep -q "\[RAYON\]"; then
    echo "ERROR: Rust library doesn't contain [RAYON] strings!"
    echo "Library may not have rebuilt properly"
    exit 1
fi
echo "✓ Rust library verified (contains [RAYON] code)"

echo "==> Building Go binary..."
cd "$PROJECT_ROOT"
env CGO_ENABLED=1 \
  CGO_CFLAGS="-I$(go env GOPATH)/pkg/mod/github.com/mattn/go-sqlite3@v1.14.32" \
  CGO_LDFLAGS="-L$RUST_DIR/target/release" \
  go build -x -a -tags "fts5 rust_ffi" -o "$BINARY" ./cmd/cortex 2>&1 | grep -E "(embeddings_ffi|CGO_LDFLAGS)" || true

if [ ! -f "$BINARY" ]; then
    echo "ERROR: Binary wasn't created!"
    exit 1
fi

echo "==> Verifying binary..."
if ! strings "$BINARY" | grep -q "\[RAYON\]"; then
    echo "ERROR: Binary doesn't contain [RAYON] strings!"
    echo "Go may not have linked the new library"
    exit 1
fi
echo "✓ Binary verified (contains [RAYON] code)"

echo "==> Done!"
ls -lh "$BINARY"
