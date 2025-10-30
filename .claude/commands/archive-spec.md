---
description: Archive a spec and create/update current documentation
---

You are managing the spec archival workflow. This is a three-step process:

## Context: Spec Archival Process

Specifications serve as design blueprints during development but become historical artifacts over time as code evolves. The archival process ensures we maintain both:
- **Historical context**: Original design thinking (archived spec)
- **Current reality**: Up-to-date documentation (docs/)

## Your Workflow

### Step 1: Invoke the doc-writer agent

Use the Task tool to invoke the `doc-writer` agent with this high-level task:

> Read the spec at `specs/<filename>` and analyze how the current implementation has evolved since it was originally designed. Create or update documentation in `docs/` that accurately reflects the current implementation. Include a "Historical Context" section with a reference to the archived spec location (`specs/archived/<filename>`).

Let the doc-writer agent:
- Find and read relevant documentation and code
- Determine what needs to be documented
- Write documentation following its own guidelines
- Make its own decisions about structure and content

**Do not** micromanage how it writes documentation or what code it reads. The doc-writer has its own instructions.

### Step 2: After agent completes

Review what the doc-writer created/updated. Ensure it added a reference to the archived spec.

### Step 3: Move spec to archive

- Move `specs/<filename>` to `specs/archived/<filename>` (use Bash `mv` command)
- Update the spec's front-matter `status` field from current value to `archived`
- Keep all other metadata unchanged (started_at, completed_at, dependencies)

### Step 4: Report completion

Provide a brief summary:
- Which spec was archived
- What documentation was created/updated
- Where the spec now lives

## Getting Started

If the user hasn't specified which spec to archive, ask them for the filename (e.g., `2025-10-26_indexer.md`).

Then proceed with the three-step workflow above.