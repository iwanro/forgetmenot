# Launch drafts for forgetmenot v0.3.1

Post these within the same week as the release. Lead with the problem, not the
feature list. One post per platform; adapt length/tone per platform rules.

The strongest hooks: **cross-session topic correlation (timeline)**, the
**embedded Web UI**, and **LLM-powered memory hygiene** (auto-topics +
summarize). All three are immediately demoable.

Current release: v0.3.1 (MIT, single static Go binary, local-first).

---

## Hacker News (Show HN)

**Title:** Show HN: Forgetmenot – memory for AI agents that correlates topics across sessions and compresses itself

**Body:**

AI agents forget everything between sessions. You re-explain the same context
every time, and past decisions get lost. I built a memory layer that runs on
its own, correlates topics across sessions, and compresses itself with an LLM.

forgetmenot is an MCP server written in Go. One static binary, zero runtime
dependencies, SQLite-backed, local-first.

What it does:

- **Cross-session correlation.** Sessions group memories, topics label them.
  `forgetmenot timeline -topic auth` shows how auth evolved across all your
  sessions: "we chose JWT" last week → "we switched refresh tokens to 60m"
  yesterday. The agent gets this via `memory.timeline`.
- **Self-compressing memory.** `forgetmenot summarize` sends stale session
  notes to a local LLM (Ollama) and replaces them with one concise summary.
  `remember -auto-topics` tags new memories automatically.
- **Runs automatically.** SessionStart/Stop hooks inject context and capture
  summaries. No manual commands.
- **Web UI in the binary.** `forgetmenot web` serves a local dashboard:
  memories, timeline, conflicts (resolve inline), sessions. Nothing to install.
- **Memory as a security surface.** Every memory has a trust level, content is
  sanitized on write, untrusted items are visibly flagged when injected.
- **`forgetmenot doctor`** diagnoses DB, embeddings endpoint and hooks.

Built with the official MCP Go SDK and pure-Go SQLite, cross-compiled to a
static binary for linux/darwin/windows, no cgo.

Looking for feedback on the topic model, the trust levels, and the
LLM-compression approach.
https://github.com/iwanro/forgetmenot (MIT, v0.3.1)

---

## Reddit – r/golang

**Title:** [Show] forgetmenot: memory for AI agents with topic timelines, LLM compression and an embedded web UI – single static Go binary

**Body:**

I wrote an MCP server in Go that gives AI coding agents persistent memory:
facts, decisions, preferences, project context. v0.3 adds the parts that make
it self-maintaining:

- **Sessions + topics → timeline.** Memories attach to the session they were
  captured in and carry topic labels. `forgetmenot timeline -topic auth`
  shows the evolution of that topic across sessions. MCP tool
  `memory.timeline` exposes the same to agents.
- **LLM compression.** `forgetmenot summarize` sends stale episode notes to
  Ollama/OpenAI, stores a single context summary, and marks the old episodes
  superseded so they stop dominating recall. `remember -auto-topics` tags new
  memories automatically.
- **Embedded Web UI.** `forgetmenot web` serves a dashboard from the same
  binary via go:embed: memories, timeline, conflicts, sessions. No Node.
- **`forgetmenot doctor`** checks DB, embeddings endpoint, hooks, active
  session. Releases are built automatically by goreleaser on tag.

Why Go: official MCP SDK (`modelcontextprotocol/go-sdk`), `modernc.org/sqlite`
→ truly static binary, no cgo. `CGO_ENABLED=0 go build` → ~13MB single file
for linux/darwin/windows, arm64 + amd64.

Trust levels + sanitization handle prompt-injection via memory. Eval harness
reports recall@k (100% on the fixed 20-query set in CI).

https://github.com/iwanro/forgetmenot

---

## Reddit – r/LocalLLaMA

**Title:** [P] Memory that correlates topics across sessions and compresses itself with an LLM

**Body:**

Most agent memory tools store vectors and hope. I built forgetmenot around
three things people actually need: cross-session correlation, LLM-powered
hygiene, and treating memory as a security surface.

Correlation:

- Sessions group memories (hooks start/end them automatically).
- Topics label them (`-topics auth,security` or `-auto-topics` with the LLM).
- `memory.timeline` shows a subject's evolution across sessions: decisions,
  changes, context, oldest first.

Hygiene (this is the interesting part):

- `forgetmenot summarize` sends stale session notes to a local model
  (Ollama), stores one concise context summary, and marks the old episodes
  superseded so recall/project_context stop surfacing them. The memory
  literally compresses itself.
- `maintain` decays stale episode importance on a schedule.

Security:

- Every memory has a trust level; low-trust content is flagged
  `[UNTRUSTED]` when injected, so the agent treats it as data, not
  instructions.
- Content is sanitized on every write path; conflicts are detected at write
  time and resolved explicitly.

It runs fully local: Ollama embeddings + chat, SQLite, no cloud, no account.
One static Go binary with an embedded Web UI (`forgetmenot web`).

https://github.com/iwanro/forgetmenot

---

## Reddit – r/ClaudeAI

**Title:** I built a memory layer for Claude Code with cross-session timelines, LLM compression and a web dashboard

**Body:**

Claude Code forgets everything between sessions. I built forgetmenot: an MCP
server that gives Claude persistent, structured memory, and it works
automatically:

1. **SessionStart hook** starts a session and injects your project context.
2. **Stop hook** captures a session summary and ends the session.
3. A **skill file** teaches Claude to recall before asking and remember
   decisions as they happen.

What's new in v0.3:

- **Timeline per topic**: `forgetmenot timeline -topic auth` shows how auth
  evolved across all your sessions. Claude gets the same via
  `memory.timeline`.
- **LLM compression**: `forgetmenot summarize` condenses old sessions into
  one summary, so memory stays clean without manual cleanup.
- **Web dashboard**: `forgetmenot web` → http://127.0.0.1:8090. Browse
  memories, timeline, conflicts; resolve conflicts inline.
- **`forgetmenot doctor`** tells you if your setup is healthy.

Local-first, one static Go binary, SQLite, Ollama or any OpenAI-compatible
endpoint. MIT. Setup is one command: `forgetmenot setup`.

https://github.com/iwanro/forgetmenot

---

## Optional: X/Twitter thread

1/ Agents forget everything between sessions. I built forgetmenot: memory for AI agents. One static Go binary, local-first, MIT.

2/ Cross-session timelines: sessions group memories, topics label them. `forgetmenot timeline -topic auth` = "we chose JWT" last week → "switched refresh tokens to 60m" yesterday.

3/ It compresses itself: `summarize` sends stale session notes to a local LLM and replaces them with one summary. `-auto-topics` tags new memories.

4/ Web UI inside the binary (`forgetmenot web`), trust levels against prompt injection, doctor command. Setup: `forgetmenot setup`.

https://github.com/iwanro/forgetmenot
