# Release Process

This document describes how to create a new release of Project Cortex.

## Overview

Project Cortex uses [GoReleaser](https://goreleaser.com/) to create releases, which are automated via GitHub Actions when you push a git tag.

The release process builds:
1. **cortex** - Lightweight CLI (~7MB) for all platforms
2. **cortex-embed** - Full binary with embedded Python runtime (~300MB) for all platforms

## Prerequisites

- Push access to the repository
- Ability to create tags
- [GoReleaser](https://goreleaser.com/) installed locally (for testing): `brew install goreleaser`
- [Task](https://taskfile.dev) installed: `brew install go-task`

## Release Checklist

### 1. Prepare the Release

1. **Update version in code** (if applicable)
   - Check for any hardcoded versions
   - Update CHANGELOG or docs if needed

2. **Test locally**
   ```bash
   # Run all tests
   task test

   # Run all quality checks
   task check

   # Build both binaries to ensure they work
   task build:all
   ```

3. **Test the release process**
   ```bash
   # This creates a snapshot release in dist/ without publishing
   task release:snapshot

   # Check the release configuration
   task release:check
   ```

4. **Review the snapshot output**
   ```bash
   ls -lh dist/
   ```

   You should see:
   - `cortex_*` archives for all platforms
   - `cortex-embed_*` archives for all platforms
   - Checksums file

### 2. Create and Push the Tag

1. **Determine the version**
   Follow [Semantic Versioning](https://semver.org/):
   - **MAJOR**: Breaking changes
   - **MINOR**: New features (backwards compatible)
   - **PATCH**: Bug fixes (backwards compatible)

2. **Create the tag**
   ```bash
   # Example for version 1.0.0
   git tag -a v1.0.0 -m "Release v1.0.0"

   # Or with a longer message
   git tag -a v1.0.0 -m "Release v1.0.0

   - Feature: Add support for X
   - Fix: Resolve issue with Y
   - Improvement: Optimize Z
   "
   ```

3. **Push the tag**
   ```bash
   git push origin v1.0.0
   ```

### 3. Monitor the Release

1. **Watch the GitHub Actions workflow**
   - Go to: https://github.com/mvp-joe/project-cortex/actions
   - Find the "Release" workflow run
   - Monitor the progress

   The workflow will:
   1. Generate Python dependencies for all platforms (~20-30 minutes)
   2. Build all binaries with GoReleaser
   3. Create a GitHub release
   4. Upload all artifacts

2. **Verify the release**
   - Go to: https://github.com/mvp-joe/project-cortex/releases
   - Check that the release was created
   - Verify all artifacts are present:
     - cortex binaries (darwin/linux/windows, amd64/arm64)
     - cortex-embed binaries (darwin/linux/windows, amd64/arm64)
     - checksums.txt

### 4. Post-Release

1. **Test the published artifacts**
   ```bash
   # Test go install
   go install github.com/mvp-joe/project-cortex/cmd/cortex@v1.0.0

   # Test the binary
   cortex --version

   # Test downloading a pre-built binary
   # Download from GitHub releases and verify it works
   ```

2. **Announce the release**
   - Update project documentation if needed
   - Announce on relevant channels
   - Update any dependent projects

## Release Artifacts

Each release creates the following artifacts:

### cortex (lightweight CLI)
- `cortex_VERSION_darwin_arm64.tar.gz` (~5MB) - macOS Apple Silicon
- `cortex_VERSION_darwin_x86_64.tar.gz` (~5MB) - macOS Intel
- `cortex_VERSION_linux_x86_64.tar.gz` (~5MB) - Linux x64
- `cortex_VERSION_linux_arm64.tar.gz` (~5MB) - Linux ARM64
- `cortex_VERSION_windows_x86_64.zip` (~5MB) - Windows x64

### cortex-embed (with Python runtime)
- `cortex-embed_VERSION_darwin_arm64.tar.gz` (~150MB) - macOS Apple Silicon
- `cortex-embed_VERSION_darwin_x86_64.tar.gz` (~150MB) - macOS Intel
- `cortex-embed_VERSION_linux_x86_64.tar.gz` (~150MB) - Linux x64
- `cortex-embed_VERSION_linux_arm64.tar.gz` (~150MB) - Linux ARM64
- `cortex-embed_VERSION_windows_x86_64.zip` (~150MB) - Windows x64

> **Note**: Archives are compressed. When extracted:
> - `cortex` binaries: ~7MB each
> - `cortex-embed` binaries: ~300MB each (due to embedded Python runtime)

### Other
- `checksums.txt` - SHA256 checksums for all artifacts

## Troubleshooting

### GitHub Actions fails to generate Python dependencies

**Problem**: Python dependency generation times out or fails

**Solution**:
1. Check the GitHub Actions logs for errors
2. Test locally: `task python:deps:all`
3. Ensure requirements.txt is valid
4. Check for network issues with PyPI

### GoReleaser build fails

**Problem**: GoReleaser can't build one of the binaries

**Solution**:
1. Check the error message in GitHub Actions logs
2. Test locally: `task release:snapshot`
3. Verify .goreleaser.yml configuration: `task release:check`
4. Ensure all platforms have Python dependencies generated

### Release missing artifacts

**Problem**: Some binaries or archives are missing from the release

**Solution**:
1. Check if the build succeeded for all platforms
2. Review the GoReleaser output logs
3. Ensure Python dependencies exist for all platforms before goreleaser runs

### go install fails after release

**Problem**: `go install github.com/mvp-joe/project-cortex/cmd/cortex@vX.Y.Z` fails

**Solution**:
1. Verify the tag was pushed correctly: `git ls-remote --tags origin`
2. Wait a few minutes for Go module proxy to update
3. Try clearing the module cache: `go clean -modcache`
4. Verify go.mod has the correct module path

## Manual Release (Emergency)

If GitHub Actions is down or you need to release manually:

1. **Generate Python dependencies**
   ```bash
   task python:deps:all
   ```

2. **Create release with GoReleaser**
   ```bash
   export GITHUB_TOKEN=your_github_token
   goreleaser release --clean
   ```

3. **Verify the release on GitHub**

## Rolling Back a Release

If you need to roll back a release:

1. **Delete the tag locally and remotely**
   ```bash
   git tag -d v1.0.0
   git push origin :refs/tags/v1.0.0
   ```

2. **Delete the GitHub release**
   - Go to the release page
   - Click "Delete this release"

3. **Fix the issue and create a new release**

## Notes

- Releases are immutable - once published, they should not be changed
- Always test with `task release:snapshot` before creating a real release
- Python dependency generation takes 20-30 minutes in CI
- The first run will be slower as dependencies are downloaded
- Pre-releases are automatically detected (tags with -alpha, -beta, -rc suffixes)
