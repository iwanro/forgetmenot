# Launch drafts for forgetmenot v0.2.0

Post these within the same week as the release. Lead with the problem, not the
feature list. One post per platform; adapt length/tone per platform rules.

The strongest hooks for v0.2.0: **cross-session topic correlation (timeline)**
and the **embedded Web UI dashboard**. Both are immediately demoable.

---

## Hacker News (Show HN)

**Title:** Show HN: Forgetmenot – memory for AI agents that correlates topics across sessions

**Body:**

AI agents forget everything between sessions. You re-explain the same context
every time, and past decisions get lost. I built a memory layer that runs on
its own and, more importantly, lets you answer "how did this subject evolve
across sessions?"

forgetmenot is an MCP server written in Go. One static binary, zero runtime
dependencies, SQLite-backed, local-first.

What it does:

- **Cross-session correlation.** Sessions group memories, topics label them.
  `forgetmenot timeline -topic auth` shows every decision about auth across
  all your sessions, oldest first: "we chose JWT" last week, "we switched
  refresh tokens to 60m" yesterday. The agent gets this via `memory.timeline`.
- **Runs automatically.** SessionStart/Stop hooks inject project context and
  capture session summaries. No manual commands.
- **Web UI in the binary.** `forgetmenot web` serves a local dashboard:
  memories, timeline, conflicts (resolve inline), sessions. Nothing to install.
- **Memory as a security surface.** Every memory has a trust level, content is
  sanitized on write, untrusted items are visibly flagged when injected.

Built with the official MCP Go SDK and pure-Go SQLite, so it cross-compiles to
a static binary for linux/darwin/windows, no cgo.

Looking for feedback on the topic model and the trust levels.
https://github.com/iwanro/forgetmenot (MIT)

---

## Reddit – r/golang

**Title:** [Show] forgetmenot: memory for AI agents with cross-session topic timelines, single static Go binary

**Body:**

I wrote an MCP server in Go that gives AI coding agents persistent memory:
facts, decisions, preferences, project context. v0.2.0 adds the parts I think
matter most for real use:

- **Sessions + topics → timeline.** Memories attach to the session they were
  captured in and carry topic labels. `forgetmenot timeline -topic auth` gives
  you the evolution of that topic across sessions, with session context. The
  MCP tool `memory.timeline` exposes the same to agents.
- **Embedded Web UI.** `forgetmenot web` serves a dashboard from the same
  binary via go:embed: browse memories, timeline, resolve conflicts inline,
  list sessions. No separate frontend, no Node.
- **Runs automatically.** Claude Code hooks start/end sessions and inject
  context at SessionStart; the agent skill teaches recall/remember.

Why Go: official MCP SDK (`modelcontextprotocol/go-sdk`), `modernc.org/sqlite`
→ truly static binary, no cgo. `CGO_ENABLED=0 go build` → 13MB single file for
linux/darwin/windows, arm64 + amd64.

Trust levels + sanitization handle prompt-injection via memory. Eval harness
reports recall@k (100% on the fixed 20-query set in CI).

https://github.com/iwanro/forgetmenot

---

## Reddit – r/LocalLLaMA

**Title:** [P] Memory that correlates topics across sessions: sessions, topic labels, timeline, trust levels

**Body:**

Most agent memory tools store vectors and hope. I built forgetmenot around two
things people actually need: cross-session correlation and treating memory as
a security surface.

Correlation:

- Sessions group memories (hooks start/end them automatically).
- Topics label them (`-topics auth,security`).
- `memory.timeline` shows a subject's evolution across sessions: decisions,
  changes, context, oldest first. The demo: "we chose JWT" in session 1,
  "we switched refresh tokens to 60m" in session 2, correlated on topic.

Security:

- Every memory has a trust level. Low-trust content is flagged `[UNTRUSTED]`
  when injected into context, so the agent treats it as data, not instructions.
- Content is sanitized on every write path (control chars stripped, length
  capped).
- Conflicts are detected at write time and resolved explicitly; losers are
  marked superseded and hidden from recall.

It runs fully local: Ollama embeddings, SQLite, no cloud, no account. One
static Go binary, with an embedded Web UI (`forgetmenot web`) to browse
memories, timelines and conflicts.

https://github.com/iwanro/forgetmenot

---

## Reddit – r/ClaudeAI

**Title:** I built a memory layer for Claude Code with cross-session timelines + a web dashboard

**Body:**

Claude Code forgets everything between sessions. I built forgetmenot: an MCP
server that gives Claude persistent, structured memory, and it works
automatically:

1. **SessionStart hook** starts a session and injects your project context.
2. **Stop hook** captures a session summary and ends the session.
3. A **skill file** teaches Claude to recall before asking and remember
   decisions as they happen.

New in v0.2.0:

- **Timeline per topic**: `forgetmenot timeline -topic auth` shows how auth
  evolved across all your sessions (decisions + context, oldest first). Claude
  gets the same via `memory.timeline`.
- **Web dashboard**: `forgetmenot web` → http://127.0.0.1:8090. Browse
  memories, see the timeline, resolve memory conflicts inline, list sessions.
- **Markdown export**: `forgetmenot export-md` writes a readable `.md` per
  project you can open anywhere.

Local-first, one static Go binary, SQLite, Ollama or any OpenAI-compatible
embeddings. MIT. Setup is one command: `forgetmenot setup`.

https://github.com/iwanro/forgetmenot

---

## Optional: X/Twitter thread

1/ Agents forget everything between sessions. I built forgetmenot: memory for AI agents. One static Go binary, local-first, MIT.

2/ New: cross-session timelines. Sessions group memories, topics label them. `forgetmenot timeline -topic auth` = "we chose JWT" last week → "switched refresh tokens to 60m" yesterday. Correlation that actually works.

3/ Also new: a Web UI inside the binary. `forgetmenot web` → dashboard for memories, timeline, conflicts. Nothing to install.

4/ It runs itself: hooks inject context at session start, capture summaries at stop. Trust levels keep memory from becoming a prompt-injection vector.

https://github.com/iwanro/forgetmenot
