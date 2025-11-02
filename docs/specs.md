# Specification Format and Lifecycle

This document defines the standardized format, lifecycle, and conventions for technical specifications in Project Cortex.

## Purpose

Specifications serve as the design blueprint for features and components before implementation begins. They capture:
- **Design thinking**: The "why" behind architectural decisions
- **Technical contracts**: APIs, interfaces, and data models
- **Implementation guidance**: What needs to be built and how
- **Historical record**: Original design intent, even as code evolves

Specs are **living documents during active work** but become **historical artifacts after archival**. Over time, code evolves beyond the original spec—and that's expected. The archival process ensures we maintain both historical context and up-to-date documentation.

## File Naming Convention

All specs use reverse date naming for chronological sorting:

```
YYYY-MM-DD_descriptive-title.md
```

**Examples:**
- `2025-10-26_indexer.md`
- `2025-10-29_cortex-search.md`
- `2025-11-15_authentication-service.md`

**Benefits:**
- Chronological sorting in file explorers
- Clear creation date without opening the file
- Easy to find recent vs older specs

**Date selection**: Use the date when design work begins (not implementation start).

## Front-Matter Format

All specs include YAML front-matter between `---` delimiters:

```yaml
---
status: draft
started_at: 2025-10-29T14:30:00Z
completed_at: null
dependencies: []
---
```

### Required Fields

#### `status`
Current lifecycle state (see Lifecycle section below).

**Valid values:**
- `draft` - Design in progress, not ready for implementation
- `ready-for-implementation` - Design complete, ready to build
- `in-progress` - Active implementation underway
- `implemented` - Implementation complete, feature shipped
- `archived` - Historical record, see current docs instead

#### `started_at`
ISO 8601 timestamp when design work began.

**Format:** `YYYY-MM-DDTHH:MM:SSZ`
**Value:** `null` if not yet started

#### `completed_at`
ISO 8601 timestamp when implementation finished.

**Format:** `YYYY-MM-DDTHH:MM:SSZ`
**Value:** `null` until implementation complete

#### `dependencies`
Array of other spec filenames this spec depends on.

**Format:** `[spec-filename-without-date, ...]`
**Examples:**
```yaml
dependencies: []                          # No dependencies
dependencies: [cortex-embed]              # Single dependency
dependencies: [indexer, mcp-server]       # Multiple dependencies
```

**Note:** Use the spec title/slug only (e.g., `indexer`), not the full dated filename.

### Example Front-Matter

```yaml
---
status: in-progress
started_at: 2025-10-26T09:00:00Z
completed_at: null
dependencies: [cortex-embed, chunk-manager]
---
```

## Document Structure

All specs follow this standardized structure:

### Required Sections

#### 1. Title (H1)
```markdown
# Feature Name Specification
```

#### 2. Purpose
One paragraph explaining **why** this feature exists and what problem it solves.

```markdown
## Purpose

The indexer parses source code using tree-sitter, extracts structural knowledge at multiple granularity levels, and generates semantically rich chunks optimized for vector embeddings.
```

#### 3. Core Concept
Input/Process/Output pattern for quick understanding.

```markdown
## Core Concept

**Input**: Source code files (Go, TypeScript, Python, etc.)
**Process**: Parse → Extract (symbols, definitions, data) → Chunk → Embed
**Output**: JSON chunk files in `.cortex/chunks/`
```

#### 4. Technology Stack
Languages, libraries, frameworks, and tools used.

```markdown
## Technology Stack

- **Language**: Go 1.25+
- **Parser**: tree-sitter (go-tree-sitter bindings)
- **Embeddings**: cortex-embed (Python FastAPI service)
- **Storage**: JSON files (`.cortex/chunks/`)
```

#### 5. Architecture
System design with diagrams (ASCII art encouraged).

```markdown
## Architecture

┌─────────────┐
│ Source Code │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│  Parser     │
└──────┬──────┘
       │
       ▼
```

#### 6. Data Model / Implementation
Detailed technical specifications: data structures, APIs, algorithms.

```markdown
## Data Model

### Chunk Structure

```go
type Chunk struct {
    ID        string    `json:"id"`
    Type      string    `json:"chunk_type"`
    Text      string    `json:"text"`
    Embedding []float32 `json:"embedding"`
}
```
\```
```

#### 7. Non-Goals
Explicit list of what this spec does NOT cover.

```markdown
## Non-Goals

- Real-time code analysis (indexing is batch-oriented)
- IDE integration (MCP server handles that separately)
- Syntax highlighting or formatting
```

### Optional Sections

Include as needed:
- **Configuration**: Environment variables, config files
- **Performance Characteristics**: Benchmarks, scalability limits
- **Error Handling**: Failure modes and recovery strategies
- **Testing Strategy**: How to validate correctness
- **Migration Path**: For breaking changes or refactors
- **Implementation Checklist**: Multi-phase task breakdown (see below)

## Implementation Checklists

For complex features requiring multi-phase implementation, specs may include an optional **Implementation Checklist** section. This checklist helps track progress and enables the `/implement-spec` command to orchestrate parallel agent execution.

**When to include a checklist:**
- Feature requires 3+ distinct implementation phases
- Multiple components need coordination
- Tasks can benefit from parallel execution
- You want structured progress tracking

**When to skip:**
- Simple, single-phase implementations
- Proof-of-concept or exploratory work
- Quick bug fixes or refactors

### Checklist Format

Implementation checklists use markdown checkboxes with emoji status indicators:

```markdown
## Implementation Checklist

### Phase 1: Foundation
- [ ] Create shared pagination package (with tests)
- [ ] Create shared db helpers package (with tests)

### Phase 2: Database Schema
- [ ] Design user and application tables
- [ ] Create migrations (up and down)
- [ ] Add indexes for common queries

### Phase 3: Repository Layer
- [ ] Implement accounts repository (with tests)
- [ ] Implement apps repository (with tests)

### Phase 4: Service Layer
- [ ] Implement accounts service (with tests)
- [ ] Implement apps service (with tests)
```

### Checkbox States

The `/implement-spec` command updates checkboxes as work progresses:

- `[ ]` - Not started (pending)
- `⏳` - In progress (agent actively working)
- `✅` - Completed (implementation and tests done)

**Example during implementation:**
```markdown
### Phase 1: Foundation
- ✅ Create shared pagination package (with tests)
- ⏳ Create shared db helpers package (with tests)

### Phase 2: Database Schema
- [ ] Design user and application tables
- [ ] Create migrations (up and down)
```

### Task Naming Conventions

Tasks should be:
- **Action-oriented**: Start with verbs (Create, Implement, Design, Add)
- **Specific**: Include what's being built, not just category names
- **Self-contained**: Each task can be assigned to a single agent
- **Test-inclusive**: Note "(with tests)" for code tasks (test-as-you-go, see `docs/testing-strategy.md`)

**Good examples:**
- ✅ "Implement user authentication service (with tests)"
- ✅ "Design database schema for multi-tenancy"
- ✅ "Create protobuf definitions for user API"

**Avoid:**
- ❌ "Build backend" (too vague)
- ❌ "Tests" (separate from implementation)
- ❌ "Fix stuff" (not specific)

### Using `/implement-spec` Command

Once a spec reaches `status: ready-for-implementation` with a checklist, use the `/implement-spec` command to orchestrate implementation:

```bash
/implement-spec specs/2025-11-01_feature-name.md
```

The command will:
1. Parse the checklist and identify phases
2. Analyze dependencies between tasks
3. Present an execution plan with parallel groups
4. Launch appropriate agents (go-engineer, api-designer, database-agent, etc.)
5. Update checkboxes as tasks complete
6. Run code reviews between phases

See `.claude/commands/implement-spec.md` for full workflow documentation.

## Lifecycle States

Specs move through five lifecycle states:

```
draft → ready-for-implementation → in-progress → implemented → archived
```

### 1. `draft`
**When**: Design work in progress, exploring options.

**Characteristics:**
- Actively being written and refined
- May have open questions or alternatives listed
- Not yet ready for implementation to begin
- `started_at`: Set when design begins
- `completed_at`: `null`

**Transitions to:** `ready-for-implementation` when design is finalized

### 2. `ready-for-implementation`
**When**: Design complete, implementation can begin.

**Characteristics:**
- All major design decisions made
- Technical approach validated
- Dependencies identified and available
- Clear acceptance criteria
- Optional implementation checklist defined
- `started_at`: Unchanged
- `completed_at`: `null`

**Transitions to:** `in-progress` when coding starts

**Note:** Use `/implement-spec` command to orchestrate complex multi-phase implementations with agent coordination.

### 3. `in-progress`
**When**: Active implementation underway.

**Characteristics:**
- Code being written based on spec
- Spec may receive minor clarifications
- Tests being developed alongside code (test-as-you-go)
- Optional checklist being updated with progress
- `started_at`: Unchanged
- `completed_at`: `null`

**Transitions to:** `implemented` when feature ships

### 4. `implemented`
**When**: Feature complete and shipped.

**Characteristics:**
- Code merged and deployed
- Tests passing
- Spec represents original design intent
- Code may have diverged slightly (expected)
- `started_at`: Unchanged
- `completed_at`: Set to implementation completion timestamp

**Transitions to:** `archived` when spec becomes historical record

**Note:** After implementation stabilizes and code evolves beyond the original design, use `/archive-spec` to preserve historical context and create up-to-date documentation.

### 5. `archived`
**When**: Spec moved to `specs/archived/` as historical record.

**Characteristics:**
- Original design thinking preserved
- Current documentation lives in `docs/`
- Spec file moved to `specs/archived/`
- Status updated to `archived`
- `started_at`: Unchanged
- `completed_at`: Unchanged

**Note:** Archived specs are read-only historical artifacts.

## Archival Process

Over time, code evolves beyond the original spec. The archival process ensures we maintain both historical context (the spec) and current reality (the docs).

### When to Archive

Archive a spec when:
- Code has diverged significantly from original design
- Refactors have changed the architecture
- You need up-to-date documentation for current implementation
- The spec has served its purpose as a design document

**Rule of thumb:** If you'd rather read current docs than the spec to understand how something works, it's time to archive.

### Three-Step Process

#### 1. Run the slash command

```bash
/archive-spec <spec-filename>
```

**Example:**
```bash
/archive-spec 2025-10-26_indexer.md
```

The `doc-writer` agent will:
- Read the original spec thoroughly
- Read all current documentation in `docs/`
- Read relevant code to understand changes since spec was written
- Identify what has changed vs original design

#### 2. Agent creates/updates documentation

The `doc-writer` agent will:
- Create or update relevant `docs/*.md` files with current implementation
- Include a reference section pointing to the archived spec
- Ensure documentation reflects actual code behavior
- Preserve links to related documentation

**Example reference section:**
```markdown
## Historical Context

This feature was originally designed in the [Indexer Specification](../specs/archived/2025-10-26_indexer.md).
The current implementation has evolved to include hot-reload capabilities and incremental indexing
not present in the original design.
```

#### 3. Spec moved to archive

The agent will:
- Move spec file to `specs/archived/` directory
- Update front-matter `status` to `archived`
- Preserve all timestamps and metadata
- Update any cross-references in other specs

**Result:** Historical design preserved, current docs accurate.

## Writing Conventions

### Style Guidelines

**Natural language first**: Begin each section with 1-2 sentences before diving into technical details.

**Example:**
```markdown
## Data Model

The indexer produces three types of chunks, each optimized for different use cases.

### Chunk Types
...
```

**Code blocks**: Always use language-tagged fenced code blocks.

```markdown
```go
type Provider interface {
    Embed(ctx context.Context, texts []string) ([][]float32, error)
}
\```
```

**ASCII diagrams**: Use box-drawing characters for architecture diagrams.

```
┌─────────────┐
│   Parser    │
└──────┬──────┘
       │
       ▼
┌─────────────┐
│   Chunker   │
└─────────────┘
```

**Lists**: Use bold keywords for scannability.

```markdown
- **Semantic search**: Natural language queries across code and docs
- **Exact search**: Keyword matching with boolean operators
- **Graph queries**: Structural code relationships
```

**Tables**: For comparisons, performance metrics, and structured data.

```markdown
| Status | Description | Transitions To |
|--------|-------------|----------------|
| draft  | Design work | ready-for-implementation |
```

### Tone and Voice

- **Technical but accessible**: Explain complex concepts clearly
- **Decisive**: "The indexer uses tree-sitter" not "We could use tree-sitter"
- **Present tense**: Describe the design as if it exists
- **Active voice**: "The parser extracts symbols" not "Symbols are extracted"

### Common Patterns

**Technology choices**: Explain **why** you chose a library/approach.

```markdown
## Why tree-sitter?

Tree-sitter provides incremental parsing and robust error recovery, essential for
handling incomplete or malformed code during development.
```

**Trade-offs**: Be explicit about what you're optimizing for.

```markdown
## Design Trade-offs

**Optimized for**: Search quality, historical record preservation
**Trade-off**: Specs may diverge from code over time (mitigated by archival process)
```

**Future work**: Use "Non-Goals" section, not "Future Work" (reduces ambiguity).

## Dependency Management

Specs can depend on other specs. Use the `dependencies` front-matter field.

### Dependency Rules

1. **List dependencies explicitly**: Include all specs whose designs this one builds upon
2. **Use spec slugs, not full filenames**: `dependencies: [indexer]` not `dependencies: [2025-10-26_indexer.md]`
3. **Keep dependencies minimal**: Only list direct dependencies, not transitive ones
4. **Check dependencies exist**: Reference actual spec filenames

### Dependency Graph Example

```yaml
# 2025-10-26_cortex-embed.md
---
dependencies: []
---

# 2025-10-26_indexer.md
---
dependencies: [cortex-embed]
---

# 2025-10-28_mcp-server.md
---
dependencies: [indexer, cortex-embed]
---
```

**Graph:**
```
cortex-embed
    │
    ├──▶ indexer
    │       │
    │       └──▶ mcp-server
    │
    └──────────▶ mcp-server
```

## Spec Creation Checklist

Before marking a spec as `ready-for-implementation`:

- [ ] Front-matter complete with all required fields
- [ ] Filename follows `YYYY-MM-DD_title.md` convention
- [ ] Purpose clearly states the "why"
- [ ] Core Concept provides Input/Process/Output summary
- [ ] Technology Stack lists all major dependencies
- [ ] Architecture section includes diagrams
- [ ] Data Model/Implementation is detailed enough to build from
- [ ] Non-Goals explicitly lists out-of-scope items
- [ ] Dependencies declared in front-matter
- [ ] Writing follows style conventions (natural language, code blocks, etc.)
- [ ] Technical decisions explained (not just described)
- [ ] Trade-offs acknowledged

## Examples

See existing specs for reference:
- `specs/2025-10-26_indexer.md` - Complex multi-phase pipeline
- `specs/2025-10-26_mcp-server.md` - Server architecture with hot-reload
- `specs/2025-10-28_cortex-exact.md` - Search interface with multiple query modes
- `specs/2025-10-29_cortex-pattern.md` - Simple MCP tool specification

## FAQ

### Should I update a spec during implementation?

**Minor clarifications:** Yes, if you discover edge cases or need to clarify ambiguous language.

**Major changes:** Consider whether this is a new spec. If the design fundamentally changes, the original spec should remain as-is (shows design evolution) and a new spec created.

**After implementation:** Generally no. Once `status: implemented`, the spec is a historical record. Updates go in `docs/` instead.

### When should I create a spec vs just writing code?

**Create a spec when:**
- Designing a new major feature or component
- Making architectural decisions that affect multiple parts of the system
- You need to communicate design intent to future developers
- The implementation will take more than a few days

**Skip the spec when:**
- Making small bug fixes
- Adding trivial features
- Refactoring implementation details (not architecture)
- The "design" is obvious and self-documenting

### How do I reference an archived spec?

In documentation, link to the archived spec with context:

```markdown
## Historical Context

This feature was originally designed in [Indexer Specification](../specs/archived/2025-10-26_indexer.md).
The current implementation has evolved to include hot-reload capabilities not present in the original design.
```

In code comments (rarely needed):

```go
// Implementation follows the design in specs/archived/2025-10-26_indexer.md
// with additional incremental indexing support added later.
```

### What if a spec becomes obsolete before implementation?

Update status to `archived` and add a note at the top explaining why:

```markdown
---
status: archived
started_at: 2025-10-29T10:00:00Z
completed_at: null
dependencies: []
---

# Feature X Specification

> **Note**: This spec was superseded by [Feature Y Specification](2025-11-15_feature-y.md)
> before implementation began. The design approach described here was replaced with a
> simpler architecture. Archived for historical context.
```

### Can I have multiple specs for the same feature?

Yes, if there are distinct phases or major versions:

- `2025-10-26_indexer.md` - Original design
- `2025-11-20_indexer-v2.md` - Major rewrite with different architecture

Link them in the front-matter or introduction:

```markdown
## Related Specs

This spec supersedes [Indexer Specification](2025-10-26_indexer.md) with a
streaming-based architecture that replaces the batch processing approach.
```
