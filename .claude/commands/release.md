# Release Command

You are helping create a new release for Project Cortex.

## Pre-flight Checks

Before starting the release process, you MUST verify:

1. Working directory is clean (no uncommitted changes)
2. Current branch is `main`
3. Local branch is up to date with remote

If any check fails, STOP and inform the user what needs to be fixed.

## Release Process

### Step 1: Gather Release Information

Get the latest git tag and commits since that tag:

```bash
# Get latest tag
git describe --tags --abbrev=0

# Get commits since last tag (with full messages for analysis)
git log <last-tag>..HEAD --format="%H|%s|%b"
```

### Step 2: Analyze Commits and Suggest Version

Analyze the commits to determine the appropriate semver bump:

- **Major (x.0.0)**: Breaking changes, API changes (look for "BREAKING CHANGE:" in commit body or "!")
- **Minor (0.x.0)**: New features, enhancements (commits starting with "feat:", "add:", or "Add")
- **Patch (0.0.x)**: Bug fixes, documentation, refactoring (commits starting with "fix:", "docs:", "refactor:", "perf:")

Use AskUserQuestion to prompt for the version number. Present options:
- Patch bump (e.g., v1.2.0 → v1.2.1)
- Minor bump (e.g., v1.2.0 → v1.3.0)
- Major bump (e.g., v1.2.0 → v2.0.0)
- Custom (allow user to specify exact version)

In the question description, include your analysis of the commits and recommendation.

### Step 3: Generate Release Notes

Parse the commits and generate release notes in this format:

```
v<version>

<summary line if you can infer one>

Features:
- <feature commits formatted nicely>

Bug Fixes:
- <fix commits formatted nicely>

Performance:
- <perf commits formatted nicely>

Other Changes:
- <other commits formatted nicely>

Full Changelog: https://github.com/mvp-joe/project-cortex/compare/<last-tag>...v<new-version>
```

**Formatting rules**:
- Remove conventional commit prefixes (feat:, fix:, etc.) from the lines
- Capitalize first letter
- Keep descriptions concise but clear
- Group related changes together
- Omit sections that have no commits

### Step 4: Create and Push Tag

Create an annotated git tag with the release notes:

```bash
git tag -a v<version> -m "<release notes>"
```

Use a HEREDOC for the tag message to handle multi-line release notes properly.

Then push the tag:

```bash
git push origin v<version>
```

### Step 5: Update README.md

Update the install instructions in README.md (around line 45) to replace `@latest` with the specific version:

```bash
# Current format (with @latest):
go install github.com/mvp-joe/project-cortex/cmd/cortex@latest

# Should become:
go install github.com/mvp-joe/project-cortex/cmd/cortex@v<new-version>
```

Use the Edit tool to replace `@latest` with `@v<new-version>`.

**Why**: This ensures the README always shows the current stable version. Users who want the absolute latest can still use `@latest`, but the documented default is the last known-good release.

### Step 6: Commit and Push README Change

```bash
git add README.md
git commit -m "Update README with v<version>"
git push
```

### Step 7: Cache-bust Go Module Proxy

Force the Go module proxy to fetch the new version:

```bash
curl "https://proxy.golang.org/github.com/mvp-joe/project-cortex/@v/v<version>.info"
```

This ensures `go install @latest` immediately sees the new version.

### Step 8: Success Message

Print a success message with:
- The new version number
- Link to create GitHub release: https://github.com/mvp-joe/project-cortex/releases/new?tag=v<version>
- Install command: `go install github.com/mvp-joe/project-cortex/cmd/cortex@v<version>`
- Note that GoReleaser will build binaries automatically via GitHub Actions

## Important Notes

- Always use `v` prefix for version tags (e.g., `v1.3.0`, not `1.3.0`)
- Release notes should be clear and user-focused
- The tag message should contain the full release notes (used by GitHub)
- GoReleaser automatically builds binaries when a tag is pushed
- Users can install immediately with `go install @latest` after cache-bust

## Error Handling

If any step fails:
1. Inform the user clearly what went wrong
2. Provide the exact error message
3. Suggest how to fix it
4. If a tag was created but push failed, remind them to delete the local tag: `git tag -d v<version>`

## Example Output

```
🚀 Creating release for Project Cortex

✓ Latest tag: v1.2.0
✓ Found 8 commits since v1.2.0
✓ Analysis: Recommend minor bump (new features detected)

[User selects v1.3.0]

✓ Generated release notes
✓ Created tag v1.3.0
✓ Pushed tag to origin
✓ Updated README.md with v1.3.0
✓ Committed and pushed README change
✓ Cache-busted Go module proxy

🎉 Release v1.3.0 created successfully!

Next steps:
1. Create GitHub release: https://github.com/mvp-joe/project-cortex/releases/new?tag=v1.3.0
2. GoReleaser will automatically build binaries via GitHub Actions
3. Users can install: go install github.com/mvp-joe/project-cortex/cmd/cortex@v1.3.0
```
