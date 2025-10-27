# Release Process

This document describes how to create a new release of Project Cortex.

## Overview

Project Cortex has **two independent release workflows**:

1. **cortex CLI** (`v*` tags) - Frequent releases for features, indexer improvements, MCP tools
2. **cortex-embed** (`v*-embed` suffix) - Rare releases only when embedding model or Python dependencies change

Each workflow builds its own binaries independently via [GoReleaser](https://goreleaser.com/) and GitHub Actions.

## Prerequisites

- Push access to the repository
- Ability to create tags
- [GoReleaser](https://goreleaser.com/) installed locally (for testing): `brew install goreleaser`
- [Task](https://taskfile.dev) installed: `brew install go-task`

## Releasing Cortex CLI (Most Common)

Use this workflow for most releases: new features, bug fixes, indexer improvements, MCP tool updates, etc.

### 1. Prepare the Release

1. **Test locally**
   ```bash
   # Run all tests
   task test

   # Run all quality checks
   task check

   # Build cortex to ensure it works
   task build
   ```

2. **Test the release process (optional)**
   ```bash
   # Test snapshot release locally
   goreleaser release --snapshot --clean --config .goreleaser.yml
   ls -lh dist/
   ```

### 2. Create and Push the Tag

1. **Determine the version**
   Follow [Semantic Versioning](https://semver.org/):
   - **MAJOR**: Breaking changes
   - **MINOR**: New features (backwards compatible)
   - **PATCH**: Bug fixes (backwards compatible)

2. **Create the tag with `v` prefix**
   ```bash
   # Example for version 1.5.0
   git tag -a v1.5.0 -m "Release v1.5.0

   - Feature: Add new MCP tool for X
   - Fix: Resolve chunking issue with Y
   - Improvement: Optimize indexer performance
   "
   ```

3. **Push the tag**
   ```bash
   git push origin v1.5.0
   ```

### 3. Monitor the Release

1. **Watch the GitHub Actions workflow**
   - Go to: https://github.com/mvp-joe/project-cortex/actions
   - Find the "Release Cortex CLI" workflow run
   - Build time: ~5 minutes

2. **Verify the release**
   - Go to: https://github.com/mvp-joe/project-cortex/releases
   - Check that the release was created with tag `v1.5.0`
   - Verify cortex artifacts are present:
     - `cortex_*_darwin_arm64.tar.gz`
     - `cortex_*_darwin_x86_64.tar.gz`
     - `cortex_*_linux_x86_64.tar.gz`
     - `cortex_*_linux_arm64.tar.gz`
     - `cortex_*_windows_x86_64.zip`
     - `checksums.txt`

## Releasing Cortex-Embed (Rare)

Use this workflow **only when**:
- Updating the embedding model
- Changing Python dependencies in `requirements.txt`
- Modifying the embedding server implementation

### 1. Prepare the Release

1. **Test locally**
   ```bash
   # Generate Python deps for your platform (if needed)
   task python:deps:darwin-arm64  # or your platform

   # Build cortex-embed
   task build:embed

   # Test it works
   ./bin/cortex-embed
   ```

2. **Test the release process (optional)**
   ```bash
   # Generate all platform deps (slow, 10-20 min)
   task python:deps:all

   # Test snapshot release locally
   goreleaser release --snapshot --clean --config .goreleaser.embed.yml
   ls -lh dist/
   ```

### 2. Create and Push the Tag

1. **Create the tag with `-embed` suffix**
   ```bash
   # Example for embed version 1.1.0
   git tag -a v1.1.0-embed -m "Release cortex-embed v1.1.0

   - Update to sentence-transformers 3.0
   - Add support for new embedding model
   "
   ```

2. **Push the tag**
   ```bash
   git push origin v1.1.0-embed
   ```

### 3. Monitor the Release

1. **Watch the GitHub Actions workflow**
   - Go to: https://github.com/mvp-joe/project-cortex/actions
   - Find the "Release Cortex-Embed" workflow run
   - Build time: ~6-10 minutes (parallel Python dep generation with caching)

2. **Verify the release**
   - Go to: https://github.com/mvp-joe/project-cortex/releases
   - Check that the release was created with tag `v1.1.0-embed`
   - Verify cortex-embed artifacts are present:
     - `cortex-embed_*_darwin_arm64.tar.gz` (~150MB)
     - `cortex-embed_*_darwin_x86_64.tar.gz` (~150MB)
     - `cortex-embed_*_linux_x86_64.tar.gz` (~150MB)
     - `cortex-embed_*_linux_arm64.tar.gz` (~150MB)
     - `cortex-embed_*_windows_x86_64.zip` (~150MB)
     - `checksums.txt`

## Post-Release Testing

### Testing Cortex CLI Release

```bash
# Test go install
go install github.com/mvp-joe/project-cortex/cmd/cortex@v1.5.0

# Test the binary
cortex --version

# Or download pre-built binary from GitHub releases and test
```

### Testing Cortex-Embed Release

```bash
# Download platform-specific binary from GitHub releases
# Extract and test
./cortex-embed --version
./cortex-embed  # Should start embedding server
```

## Release Artifacts

### Cortex CLI Releases (`v*` tags)
- `cortex_VERSION_darwin_arm64.tar.gz` (~7MB extracted)
- `cortex_VERSION_darwin_x86_64.tar.gz` (~7MB extracted)
- `cortex_VERSION_linux_x86_64.tar.gz` (~7MB extracted)
- `cortex_VERSION_linux_arm64.tar.gz` (~7MB extracted)
- `cortex_VERSION_windows_x86_64.zip` (~7MB extracted)
- `checksums.txt`

### Cortex-Embed Releases (`v*-embed` suffix)
- `cortex-embed_VERSION_darwin_arm64.tar.gz` (~150MB compressed, ~300MB extracted)
- `cortex-embed_VERSION_darwin_x86_64.tar.gz` (~150MB compressed, ~300MB extracted)
- `cortex-embed_VERSION_linux_x86_64.tar.gz` (~150MB compressed, ~300MB extracted)
- `cortex-embed_VERSION_linux_arm64.tar.gz` (~150MB compressed, ~300MB extracted)
- `cortex-embed_VERSION_windows_x86_64.zip` (~150MB compressed, ~300MB extracted)
- `checksums.txt`

## Troubleshooting

### Wrong workflow triggered

**Problem**: Tagged `v1.5.0` but cortex-embed workflow ran (or vice versa)

**Solution**:
- Cortex CLI releases must use `v*` tags (e.g., `v1.5.0`)
- Cortex-embed releases must use `v*-embed` suffix (e.g., `v1.1.0-embed`)
- Delete the incorrect tag and re-tag with the correct pattern

### GitHub Actions fails to generate Python dependencies (cortex-embed)

**Problem**: Python dependency generation times out or fails

**Solution**:
1. Check the GitHub Actions logs for errors
2. Test locally: `task python:deps:all`
3. Ensure `requirements.txt` is valid
4. Check for network issues with PyPI
5. Cache may be corrupted - clear it in GitHub Actions settings

### GoReleaser build fails

**Problem**: GoReleaser can't build one of the binaries

**Solution**:
1. Check the error message in GitHub Actions logs
2. Test locally:
   - Cortex: `goreleaser release --snapshot --clean --config .goreleaser.yml`
   - Cortex-embed: `goreleaser release --snapshot --clean --config .goreleaser.embed.yml`
3. Verify configuration: `goreleaser check --config .goreleaser.yml`
4. For cortex-embed: Ensure Python dependencies exist for all platforms

### Release missing artifacts

**Problem**: Some binaries or archives are missing from the release

**Solution**:
1. Check if the build succeeded for all platforms in GitHub Actions logs
2. Review the GoReleaser output logs
3. For cortex-embed: Verify all 5 platform Python dependencies were downloaded

### go install fails after cortex release

**Problem**: `go install github.com/mvp-joe/project-cortex/cmd/cortex@vX.Y.Z` fails

**Solution**:
1. Verify the tag was pushed correctly: `git ls-remote --tags origin`
2. Wait a few minutes for Go module proxy to update
3. Try clearing the module cache: `go clean -modcache`
4. Verify `go.mod` has the correct module path

## Manual Release (Emergency)

### Manual Cortex CLI Release

```bash
export GITHUB_TOKEN=your_github_token
goreleaser release --clean --config .goreleaser.yml
```

### Manual Cortex-Embed Release

```bash
# 1. Generate Python dependencies
task python:deps:all

# 2. Create release
export GITHUB_TOKEN=your_github_token
goreleaser release --clean --config .goreleaser.embed.yml
```

## Rolling Back a Release

If you need to roll back a release:

1. **Delete the tag locally and remotely**
   ```bash
   # For cortex release
   git tag -d v1.5.0
   git push origin :refs/tags/v1.5.0

   # For cortex-embed release
   git tag -d v1.1.0-embed
   git push origin :refs/tags/v1.1.0-embed
   ```

2. **Delete the GitHub release**
   - Go to the release page
   - Click "Delete this release"

3. **Fix the issue and create a new release**

## Important Notes

### Release Independence
- Cortex and cortex-embed have **independent version numbers**
- Cortex `v1.5.0` may work with cortex-embed `v1.0.0`
- Update cortex-embed only when embedding infrastructure changes
- Most releases will be cortex-only (`v*` tags)

### Performance
- **Cortex releases**: ~5 minutes (fast, no Python deps)
- **Cortex-embed releases**: ~6-10 minutes total
  - First run or `requirements.txt` change: ~6 min (parallel generation)
  - Subsequent runs: ~1 min (cache hits) + ~5 min (build)

### Tag Discipline
- **ALWAYS** use correct tag pattern:
  - Cortex: `v1.5.0`, `v2.0.0-beta`, etc.
  - Cortex-embed: `v1.0.0-embed`, `v1.1.0-embed`, etc.
- Wrong pattern triggers wrong workflow
- Embed releases always include `-embed` suffix
- Pre-releases for cortex-embed: `v1.0.0-beta-embed`, `v1.0.0-rc1-embed`

### Caching (cortex-embed only)
- Python dependencies cached by `requirements.txt` hash
- Cache hit: ~1 minute (5 parallel cache restores)
- Cache miss: ~6 minutes (5 parallel builds)
- Cache expires after 7 days of inactivity
