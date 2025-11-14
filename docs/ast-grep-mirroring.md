# ast-grep Binary Mirroring to Tigris

This document explains how to mirror ast-grep binaries from GitHub releases to Tigris object storage.

## Why Mirror to Tigris?

Project Cortex mirrors ast-grep binaries to Tigris object storage for several reasons:

1. **Rate Limit Protection** - GitHub API has rate limits that can affect downloads during development/testing
2. **Reliability** - We control the availability and don't depend on GitHub's uptime
3. **Speed** - Tigris CDN provides fast global distribution
4. **Consistency** - Matches the embedding server distribution pattern

## Prerequisites

### Required Tools

- **AWS CLI** - Install via: `brew install awscli`
- **Task** - Already installed if you can run `task` commands
- **Tigris Credentials** - Set as environment variables:
  ```bash
  export TIGRIS_ACCESS_KEY_ID="your_access_key"
  export TIGRIS_SECRET_ACCESS_KEY="your_secret_key"
  ```

### Storage Location

- **Bucket**: `project-cortex-files`
- **Base URL**: `https://project-cortex-files.t3.storage.dev/`
- **Endpoint**: `https://fly.storage.tigris.dev`

## Workflow: Updating ast-grep Version

When a new version of ast-grep is released, follow these steps to mirror it:

### 1. Download Binaries from GitHub

```bash
task ast-grep:download AST_GREP_VERSION=0.30.0
```

This downloads ast-grep binaries for all supported platforms:
- darwin-arm64 (macOS Apple Silicon)
- darwin-amd64 (macOS Intel)
- linux-arm64 (Linux ARM64)
- linux-amd64 (Linux x64)
- windows-amd64 (Windows x64)

**Output**: `bin/ast-grep/v0.30.0/app-*.zip`

### 2. Repackage with Cortex Naming

```bash
task ast-grep:repackage AST_GREP_VERSION=0.30.0
```

This:
- Extracts the `sg`/`sg.exe` binary from GitHub's zip archives
- Renames to `ast-grep`/`ast-grep.exe`
- Repackages as `ast-grep-v{version}-{platform}.zip`

**Output**: `bin/ast-grep/archives/ast-grep-v0.30.0-*.zip`

### 3. Upload to Tigris

```bash
# Ensure credentials are set
export TIGRIS_ACCESS_KEY_ID="your_access_key"
export TIGRIS_SECRET_ACCESS_KEY="your_secret_key"

task ast-grep:upload AST_GREP_VERSION=0.30.0
```

This:
- Uploads all archives to Tigris
- Sets public-read ACL so they're downloadable without auth
- Displays URLs for verification

**Output URLs**:
```
https://project-cortex-files.t3.storage.dev/ast-grep-v0.30.0-darwin-arm64.zip
https://project-cortex-files.t3.storage.dev/ast-grep-v0.30.0-darwin-amd64.zip
https://project-cortex-files.t3.storage.dev/ast-grep-v0.30.0-linux-arm64.zip
https://project-cortex-files.t3.storage.dev/ast-grep-v0.30.0-linux-amd64.zip
https://project-cortex-files.t3.storage.dev/ast-grep-v0.30.0-windows-amd64.zip
```

### 4. Update Code

```bash
task ast-grep:update-version AST_GREP_VERSION=0.30.0
```

This updates the `AstGrepVersion` constant in `internal/pattern/binary.go`.

### 5. Test

```bash
# Run unit tests
task test

# Optional: Test with real binary download
rm -rf ~/.cortex/bin/ast-grep*
go run cmd/cortex/main.go mcp  # Should download new version
```

### 6. Commit

```bash
git add -A
git commit -m "chore: update ast-grep to v0.30.0"
git push
```

## All-in-One Command

For convenience, all steps except the code update can be run together:

```bash
# Set credentials
export TIGRIS_ACCESS_KEY_ID="your_access_key"
export TIGRIS_SECRET_ACCESS_KEY="your_secret_key"

# Mirror binaries
task ast-grep:mirror AST_GREP_VERSION=0.30.0

# Update code
task ast-grep:update-version AST_GREP_VERSION=0.30.0

# Test and commit
task test
git commit -am "chore: update ast-grep to v0.30.0"
```

## File Naming Convention

### GitHub Release Format
```
https://github.com/ast-grep/ast-grep/releases/download/0.29.0/app-aarch64-apple-darwin.zip
                                                                ^^^ platform name
```

### Cortex/Tigris Format
```
https://project-cortex-files.t3.storage.dev/ast-grep-v0.29.0-darwin-arm64.zip
                                                    ^^^^^^^^ version with v prefix
                                                             ^^^^^^^^^^^ cortex platform name
```

### Platform Mapping

| Cortex Platform | ast-grep Platform (GitHub) |
|----------------|---------------------------|
| darwin-arm64   | aarch64-apple-darwin      |
| darwin-amd64   | x86_64-apple-darwin       |
| linux-arm64    | aarch64-unknown-linux-gnu |
| linux-amd64    | x86_64-unknown-linux-gnu  |
| windows-amd64  | x86_64-pc-windows-msvc    |

## Archive Structure

### GitHub Archives
```
app-aarch64-apple-darwin.zip
└── sg                    # binary named "sg" (or sg.exe on Windows)
```

### Tigris Archives (Cortex)
```
ast-grep-v0.29.0-darwin-arm64.zip
└── ast-grep              # binary renamed to "ast-grep" (or ast-grep.exe on Windows)
```

## Troubleshooting

### Upload Fails: Credentials Not Set

**Error**: `TIGRIS_ACCESS_KEY_ID environment variable is required`

**Solution**: Export credentials:
```bash
export TIGRIS_ACCESS_KEY_ID="your_access_key"
export TIGRIS_SECRET_ACCESS_KEY="your_secret_key"
```

### Upload Fails: AWS CLI Not Installed

**Error**: `AWS CLI is required. Install with: brew install awscli`

**Solution**: Install AWS CLI:
```bash
brew install awscli
```

### Download Fails: GitHub Rate Limit

**Error**: `download failed with status 403`

**Solution**: Wait for rate limit to reset (typically 1 hour) or use a GitHub Personal Access Token:
```bash
# Add auth header to downloads (modify Taskfile if needed)
curl -H "Authorization: token YOUR_GITHUB_TOKEN" -L "..."
```

### Version Already Exists on Tigris

If you try to upload a version that already exists, AWS CLI will overwrite it. This is safe but check that you intended to replace the files.

### Verify Upload Success

After uploading, test with curl:
```bash
# Should return HTTP 200
curl -I https://project-cortex-files.t3.storage.dev/ast-grep-v0.29.0-darwin-arm64.zip

# Should download the file
curl -L https://project-cortex-files.t3.storage.dev/ast-grep-v0.29.0-darwin-arm64.zip -o test.zip
unzip -l test.zip  # Should show ast-grep binary
```

## Maintenance

### Checking Current Version

```bash
# Check code version
grep "AstGrepVersion" internal/pattern/binary.go

# Check what's on Tigris
aws s3 ls s3://project-cortex-files/ast-grep-v \
  --endpoint-url https://fly.storage.tigris.dev
```

### Cleaning Up Old Versions

Old versions remain on Tigris until manually deleted:

```bash
# List all versions
aws s3 ls s3://project-cortex-files/ \
  --endpoint-url https://fly.storage.tigris.dev | grep ast-grep

# Delete specific version
aws s3 rm s3://project-cortex-files/ast-grep-v0.28.0-darwin-arm64.zip \
  --endpoint-url https://fly.storage.tigris.dev
```

**Recommendation**: Keep at least 2 recent versions for rollback capability.

## Security Considerations

1. **Public Read Access** - All uploaded binaries are public (no authentication required for downloads)
2. **Credentials** - Never commit Tigris credentials to git
3. **Binary Integrity** - Consider adding SHA256 checksum verification in future
4. **Source Trust** - Only mirror from official ast-grep GitHub releases

## Related Documentation

- [ast-grep GitHub Releases](https://github.com/ast-grep/ast-grep/releases)
- [Tigris Documentation](https://www.tigrisdata.com/docs/)
- [AWS CLI S3 Commands](https://docs.aws.amazon.com/cli/latest/reference/s3/)
- [Embedding Server](embedding-server.md) (similar distribution pattern)
