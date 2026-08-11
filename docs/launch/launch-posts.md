# Launch drafts for forgetmenot v0.1.0

Post these within the same week as the release. Lead with the problem, not the
feature list. One post per platform; adapt length/tone per platform rules.

---

## Hacker News (Show HN)

**Title:** Show HN: Forgetmenot – persistent memory for AI agents, one static Go binary

**Body:**

AI agents forget everything between sessions. You re-explain the same context
every time, architectural decisions get lost, and your preferences are
ignored. So I built a memory layer that runs on its own.

forgetmenot is an MCP server written in Go. One static binary, zero runtime
dependencies, SQLite-backed, data stays on your machine.

The part I care most about is that it works automatically, no manual steps:

- SessionStart hook injects the project context before you ask
- Stop hook saves a session summary as a memory
- An agent skill teaches the agent to recall/remember on its own
- A `maintain` command decays stale memories on a cron

It also treats memory as a security surface: every memory has a trust level
(high/low), content is sanitized on write, and untrusted items are visibly
flagged when injected into context. That's the part nobody else seems to do.

Built with the official MCP Go SDK and pure-Go SQLite (modernc.org), so it
cross-compiles to a static binary for linux/darwin/windows with no cgo.

Looking for feedback on the trust model and whether the hook-based automation
actually fits real workflows.

Links: https://github.com/iwanro/forgetmenot (MIT, ~2k LOC)

---

## Reddit – r/golang

**Title:** [Show] forgetmenot: persistent memory for AI agents, single static Go binary, no cgo

**Body:**

I wrote an MCP server in Go that gives AI coding agents (Claude Code, Cursor,
Codex) persistent memory: facts, decisions, preferences, project context.

Why Go: the official MCP SDK (`modelcontextprotocol/go-sdk`) is maintained
with Google, and `modernc.org/sqlite` gives us a truly static binary with no
cgo. `CGO_ENABLED=0 go build` → 12MB single file for linux/darwin/windows,
arm64 + amd64.

What it does:
- `memory.remember/recall/update/link/conflicts` MCP tools
- Automatic operation via Claude Code hooks (SessionStart injects context,
  Stop captures summaries) — the user never runs commands manually
- CLAUDE.md bridge: sync project context to the agent's native memory file
- Trust levels + sanitization as prompt-injection defense
- CLI for scripting: remember, capture, project_context, maintain, bridge,
  export/import, eval

The eval harness (`forgetmenot eval`) runs a fixed 20-query dataset and
reports recall@k — currently 100% in CI.

Feedback welcome, especially on the trust model and hook design.
https://github.com/iwanro/forgetmenot

---

## Reddit – r/LocalLLaMA

**Title:** [P] Local-first persistent memory for agents: trust levels against prompt injection, works with Ollama embeddings

**Body:**

Most agent memory tools just store vectors and hope. I built forgetmenot with
memory hygiene and security as first-class concerns:

- Every memory has a trust level. Auto-captured or external content can be
  marked low-trust; recall returns source + trust, and context injection
  visibly flags `[UNTRUSTED]` so the agent treats it as data, not
  instructions.
- Content is sanitized on every write path (control chars stripped, length
  capped).
- Conflicts are detected at write time and resolved explicitly, with the
  loser marked superseded and hidden from recall.

It runs fully local: embeddings via Ollama (`nomic-embed-text`), SQLite for
storage, no cloud, no account. One static Go binary.

It also runs itself: hooks inject context at session start and capture
summaries at session end, so you never manually "save" anything.

20-query eval harness, recall@k reported in README.
https://github.com/iwanro/forgetmenot

---

## Reddit – r/ClaudeAI

**Title:** I built a local memory layer for Claude Code that runs itself (hooks, no manual commands)

**Body:**

Claude Code forgets everything between sessions. I got tired of re-explaining
my stack and past decisions, so I built forgetmenot: an MCP server that gives
Claude persistent, structured memory, and it works automatically:

1. On session start, a hook injects your project context (facts, decisions,
   preferences) so Claude already knows the project.
2. On stop, a hook saves a session summary as an episode.
3. A skill file teaches Claude to recall before asking and remember decisions
   as they happen — no manual "save this" needed.

It also syncs with CLAUDE.md via `forgetmenot bridge export/import`, so
memory works even before MCP tools load.

Local-first, one static Go binary, SQLite, works with Ollama or any
OpenAI-compatible embeddings. MIT.

Setup is one command: `forgetmenot setup`.
https://github.com/iwanro/forgetmenot

---

## Optional: X/Twitter thread

1/ Agents forget everything between sessions. So I built forgetmenot: persistent memory for AI agents. One static Go binary, local-first, MIT.

2/ It runs itself: hooks inject project context at session start, capture summaries at session end. No manual commands.

3/ The part I'm most proud of: trust levels. Every memory is high/low trust; untrusted content is flagged when injected, so memory can't become a prompt-injection vector.

4/ SQLite (pure Go, no cgo), Ollama embeddings, cross-compiled for 4 platforms. 20-query eval: 100% recall@k.

https://github.com/iwanro/forgetmenot
