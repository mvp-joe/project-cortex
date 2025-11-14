#!/bin/bash
set -e

cd "$(dirname "$0")/.."
echo "Building from: $(pwd)"

echo "==> Building Rust FFI library..."
cd internal/embeddings-ffi
touch src/lib.rs  # Force rebuild
cargo build --release
cd ../..

echo "==> Building cortex binary with rust_ffi tag..."
env CGO_ENABLED=1 \
  CGO_CFLAGS="-I$(go env GOPATH)/pkg/mod/github.com/mattn/go-sqlite3@v1.14.32" \
  CGO_LDFLAGS="-L$(pwd)/internal/embeddings-ffi/target/release -lembeddings_ffi" \
  go build -tags "fts5 rust_ffi" -o bin/cortex-rust ./cmd/cortex

echo "==> Done!"
ls -lh bin/cortex-rust
