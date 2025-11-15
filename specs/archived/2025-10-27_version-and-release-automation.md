---
status: archived
created_at: 2025-10-27T00:00:00Z
depends_on: []
---

# Version Detection and Release Automation Spec

## Problem Statement

1. **Version Display Issue**: When users install cortex via `go install github.com/mvp-joe/project-cortex/cmd/cortex@v1.2.0`, running `cortex version` shows:
   ```
   Cortex dev
   Git commit: none
   Build date: unknown
   ```

2. **Manual Release Process**: Currently creating releases requires manual steps:
   - Analyzing commits since last tag
   - Writing release notes
   - Creating and pushing git tag
   - Updating README with new version
   - Cache-busting Go module proxy

## Root Cause

- Local builds via `task build` use ldflags to inject version info (works)
- `go install` doesn't use Taskfile, so no ldflags applied
- Binary defaults to "dev" / "none" / "unknown"
- `go install` downloads source as zip (no git history), but Go toolchain DOES embed version info via `runtime/debug.BuildInfo`

## Solution

### Part 1: Fix Version Detection

Modify `internal/cli/version.go` to use `runtime/debug.ReadBuildInfo()` as fallback when ldflags version is "dev".

**Implementation**:
```go
import "runtime/debug"

func getVersion() string {
    if Version != "dev" {
        return Version  // Use ldflags version if set (task build)
    }

    // Fallback: get version from module info (go install)
    if info, ok := debug.ReadBuildInfo(); ok {
        if info.Main.Version != "" && info.Main.Version != "(devel)" {
            return info.Main.Version
        }
    }

    return "dev"
}

func getGitCommit() string {
    if GitCommit != "none" {
        return GitCommit
    }

    // Fallback: get commit from build settings
    if info, ok := debug.ReadBuildInfo(); ok {
        for _, setting := range info.Settings {
            if setting.Key == "vcs.revision" {
                return setting.Value[:7]  // Short hash
            }
        }
    }

    return "none"
}

func getBuildDate() string {
    if BuildDate != "unknown" {
        return BuildDate
    }

    // Fallback: get build time from build settings
    if info, ok := debug.ReadBuildInfo(); ok {
        for _, setting := range info.Settings {
            if setting.Key == "vcs.time" {
                return setting.Value
            }
        }
    }

    return "unknown"
}
```

Update version command to use these helpers:
```go
Run: func(cmd *cobra.Command, args []string) {
    fmt.Printf("Cortex %s\n", getVersion())
    fmt.Printf("Git commit: %s\n", getGitCommit())
    fmt.Printf("Build date: %s\n", getBuildDate())
},
```

**Testing**:
1. Build with `task build` - should show ldflags version
2. Install with `go install @v1.2.0` - should show v1.2.0 from BuildInfo
3. Build with `go build` (no ldflags) - should show version from BuildInfo or "dev"

### Part 2: Release Automation

Create `.claude/commands/release.md` slash command that automates the release process.

**Workflow**:
1. Analyze commits since last tag (using `git log`)
2. Prompt user for new version number (suggest based on semver)
3. Generate release notes from commit messages
4. Create annotated git tag with release notes
5. Push tag to origin
6. Update README.md install instructions with new version
7. Cache-bust Go module proxy: `curl "https://proxy.golang.org/github.com/mvp-joe/project-cortex/@v/<version>.info"`

**Command structure**:
```markdown
# Release Command

You are helping create a new release for Project Cortex.

## Steps

1. Get the latest tag: `git describe --tags --abbrev=0`
2. Get commits since last tag: `git log <last-tag>..HEAD --oneline`
3. Prompt user for new version (suggest next patch/minor/major)
4. Generate release notes from commits
5. Create annotated tag: `git tag -a <version> -m "<release notes>"`
6. Push tag: `git push origin <version>`
7. Update README.md line 45 with new version
8. Commit README change
9. Cache-bust: `curl "https://proxy.golang.org/github.com/mvp-joe/project-cortex/@v/<version>.info"`
10. Print success message with install command

## Example Release Notes Format

```
v1.3.0

Features:
- Add cortex clean command
- Improve version detection for go install

Fixes:
- Fix incremental indexing metadata
- Fix watch mode startup behavior
```
```

## Success Criteria

1. `cortex version` shows correct version after `go install @v1.2.0`
2. `/release` command creates tags, updates README, and cache-busts proxy automatically
3. Users can upgrade with `go install @latest` within minutes of release

## Files to Modify

- `internal/cli/version.go` - Add BuildInfo fallback
- `.claude/commands/release.md` - New release automation command
- `README.md` - Update install instructions to show specific version example

## Current State

- v1.2.0 tag exists and is pushed
- `cortex clean` command implemented and committed
- Version detection partially implemented (needs completion and testing)
