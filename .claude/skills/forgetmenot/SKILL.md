---
name: forgetmenot
description: Persistent memory for agents. Use it automatically: recall project context when starting any task, and remember decisions, facts and preferences as they come up. Never ask the user to do memory operations manually.
---

# forgetmenot memory

You have access to a persistent memory via the `memory.*` MCP tools. Use them
**automatically**, without being asked. Do not tell the user "I saved that" or
ask permission for routine recall.

## When to recall (automatic)

- At the **start of any task or project change**: call `memory.recall` with the
  task as the query, filtered to the current project, before asking the user
  for context they have already given before.
- When the user references something that might have been discussed before
  ("as we discussed", "like last time", a feature name, a past decision).
- When you are about to guess an architectural choice, a preference or a
  constraint: check memory first.

## When to remember (automatic)

- **Decisions**: call `memory.remember` (type `decision`) whenever a meaningful
  choice is made, including the rationale. Example: "we chose SQLite for local
  tests because Postgres is too heavy".
- **Facts**: type `fact` for anything the user states about the project that
  will matter later (stack, versions, services, endpoints, conventions).
- **Preferences**: type `preference` for style/workflow preferences (commit
  format, language, testing style, tooling).
- **Entities**: type `entity` for people, services or components with stable
  properties.
- **Context**: type `context` for the current state of work (in progress,
  blocked on, next step).
- **Episodes**: type `episode` for significant past events that explain the
  present.

Always pass `project` when the memory belongs to a specific project, and a
short `source` label. Keep content concise and self-contained (one fact per
memory). If you notice a contradiction with what you recall, call
`memory.conflicts` and resolve it via `memory.resolve_conflict` with the newer
or more accurate memory as the winner.

## Rules

- Never store secrets: API keys, tokens, passwords.
- Prefer a few precise memories over one long paragraph.
- Do not duplicate: recall first; if the same fact already exists, update it
  instead of adding a copy.
