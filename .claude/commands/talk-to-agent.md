---
description: Talk to a specific agent by name. The agent's persona will be used for the conversation. Usage: /talk-to-agent <agent-name>
---

You are now switching to agent persona mode. Follow these steps:

## Step 1: Parse the Agent Name

Extract the agent name from the user's command. The format is `/talk-to-agent <agent-name>`.

If no agent name was provided, list all available agents from `.claude/agents/` directory and ask the user which one they want to talk to.

## Step 2: Find the Agent File

Search for agent files in `.claude/agents/` directory. The agent files are named like `<agent-name>.md`.

Perform fuzzy matching to find the best match even if the user's input isn't exact:
- Case-insensitive matching
- Match partial names (e.g., "backend" should match "go-backend-dev")
- Match with or without hyphens (e.g., "code reviewer" should match "go-code-reviewer")
- If multiple matches are found, select the best one or ask the user to clarify

Examples of fuzzy matching:
- "backend" → `go-backend-dev.md`
- "spec" → `spec-writer.md`
- "code review" → `go-code-reviewer.md`
- "security" → `security-auditor.md`

## Step 3: Load the Agent Persona

Once you've found the matching agent file:
1. Read the complete agent markdown file from `.claude/agents/<agent-name>.md`
2. Extract the agent's full persona/prompt (everything after the frontmatter `---`)
3. Acknowledge the switch to the user with a message like:

   "Now talking as **[Agent Name]**. I'm [brief description from frontmatter]."

## Step 4: Adopt the Agent Persona

From this point forward in the conversation:
- Assume the full persona, expertise, and behavioral traits defined in the agent's markdown file
- Follow all instructions, guidelines, and constraints specified in the agent file
- Respond as that agent would respond
- Maintain the agent's tone, style, and approach
- Apply the agent's specialized knowledge and focus areas

## Step 5: Continue the Conversation

The user can now interact with you as if they're directly talking to that specialized agent. Continue the conversation in that persona until:
- The user explicitly asks to exit or switch agents
- The conversation naturally concludes
- The user starts a new command

---

**Important Notes:**
- The agent persona should be adopted completely - not just referenced
- Use the agent's specialized knowledge and approach defined in their file
- If the agent file specifies specific workflows, tools, or patterns, follow them
- Maintain consistency with the agent's defined expertise throughout the conversation