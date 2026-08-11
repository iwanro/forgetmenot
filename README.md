# forgetmenot 🧠

[![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/iwanro/forgetmenot/actions/workflows/ci.yml/badge.svg)](https://github.com/iwanro/forgetmenot/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/iwanro/forgetmenot)](https://goreportcard.com/report/github.com/iwanro/forgetmenot)

Persistent, structured, semantically searchable memory for AI agents, delivered as a local **MCP server** written in Go. One static binary, zero runtime dependencies, data stays on your machine.

Works with any MCP-capable agent: Claude Code, Cursor, Codex, OpenClaw and others.

![forgetmenot demo](docs/demo.gif)

## Why

Agents forget everything between sessions. You re-explain the same context, architectural decisions get lost, your preferences are ignored. `forgetmenot` gives your agents long-term memory: facts, decisions, preferences, entities, project context and episodes, stored locally and found semantically.

Positioning: **hygiene + trust** (dedupe, provenance, conflicts, intelligent forgetting) as first-class features, not add-ons. Details in [PRD.md](./PRD.md).

## Features (M4) ✨

- `memory.remember` - store a memory; automatic dedupe + conflict detection + topic labels
- `memory.recall` - semantic search with similarity score, project/type filters, hides superseded memories, returns source + trust
- `memory.timeline` - trace a topic's evolution across sessions (correlation!)
- `memory.forget` - delete a memory
- `memory.update` - change content/type/project/importance/trust/session/metadata
- `memory.link` - relations between memories: `related`, `supersedes`, `part_of`
- `memory.conflicts` - list open conflicts
- `memory.resolve_conflict` - pick the winner; the loser becomes superseded
- `memory.stats` - memory and project counts
- Memory types: `fact`, `preference`, `decision`, `entity`, `context`, `episode`
- **Sessions**: memories grouped per agent session; `session start/end/list`
- **Topics**: subject labels for cross-session correlation
- **Markdown export**: `export-md` writes human/AI-readable `.md` per project
- **Compact embeddings**: binary float32 BLOB (legacy JSON auto-migrated)
- **Trust levels + sanitization** (prompt-injection defense)
- **CLAUDE.md bridge**: `bridge export` + `bridge import`
- **Memory budget**: `project_context -budget N`
- Local embeddings (Ollama) or remote (OpenAI-compatible)
- Pure-Go SQLite: single static binary, no cgo, easy cross-compile
- CLI: `remember`, `capture`, `session`, `timeline`, `project_context`, `maintain`, `setup`, `bridge`, `export-md`, `export/import`, `stats`, `list`, `eval`
- Eval harness with recall@k (20 queries), JSON output for CI

## Cross-session topic correlation 🔀

Sessions group memories, topics label them. Together they answer "how did this
subject evolve over time?":

```bash
forgetmenot session start -project demo      # hooks do this automatically
forgetmenot remember -content "chose JWT" -type decision -project demo -topics auth
forgetmenot session end -project demo        # hooks do this automatically

# next session, days later:
forgetmenot remember -content "switched refresh tokens to 60m" -type decision -project demo -topics auth

forgetmenot timeline -project demo -topic auth
# - [2026-08-11] we chose JWT for sessions (session a1b2c3d4) [decision]
# - [2026-08-15] we switched refresh tokens to 60m (session e5f6a7b8) [decision]
```

The MCP tool `memory.timeline` exposes the same correlation to agents.

## Automatic operation (no manual steps) 🤖

`forgetmenot` is designed to run on its own. The user does not execute memory
commands; hooks, agent instructions and background maintenance do:

- **SessionStart hook** → `forgetmenot project_context` injects the project summary automatically
- **Stop hook** → `forgetmenot capture` saves a session summary as an episode automatically
- **Agent skill** (`.claude/skills/forgetmenot/SKILL.md`) teaches the agent to recall/remember on its own
- **`forgetmenot maintain`** (cron/daemon) applies decay automatically

One-time setup in a project:

```bash
forgetmenot setup     # writes .claude/settings.json with the hooks
```

## CLAUDE.md bridge

The agent's native memory (CLAUDE.md) stays in sync without manual work:

```bash
forgetmenot bridge export -path CLAUDE.md -project demo   # write project context into CLAUDE.md
forgetmenot bridge import -path CLAUDE.md -project demo   # ingest facts section into memory
```

The export writes a managed `<!-- forgetmenot:context -->` section. The
import reads bullets from a `<!-- forgetmenot:facts -->` section.

## CLI

```bash
forgetmenot session start|end -project demo            # session lifecycle (hooks do this)
forgetmenot timeline -project demo -topic auth         # topic evolution across sessions
forgetmenot remember -content "chose JWT" -type decision -project demo -topics auth
forgetmenot project_context -project demo -budget 4000 # session-start context injection (used by hooks)
forgetmenot capture -project demo                      # session-end capture, reads summary from stdin (used by hooks)
forgetmenot maintain                                   # decay + future compression (cron-friendly)
forgetmenot setup                                      # write Claude Code hooks config
forgetmenot bridge export|import -path CLAUDE.md       # CLAUDE.md sync
forgetmenot export-md -project demo                    # human/AI-readable markdown
forgetmenot stats                                      # memory + project counts
forgetmenot list -project demo                         # list memories
forgetmenot export -project demo > mem.json            # portable backup (with embeddings)
forgetmenot import < mem.json                          # restore
forgetmenot eval [-json]                               # seed + eval against real embeddings (Ollama)
```

## Benchmark

`forgetmenot eval` runs a fixed 20-query dataset and reports recall@k. The
dataset and runner live in `internal/eval` so the benchmark is reproducible:

```bash
forgetmenot eval -embed ollama          # against local Ollama embeddings
forgetmenot eval -embed openai -embed-url https://api.openai.com/v1 -embed-api-key $KEY
forgetmenot eval -json                  # machine-readable for CI
```

The dataset is verified in CI (hermetic bag-of-words embedder): **recall@k =
100% (20/20)** on the default dataset. Real-model results vary by embedder;
the same command above produces yours.

## Install 🚀

Requires Go 1.26+:

```bash
go install github.com/iwanro/forgetmenot/cmd/forgetmenot@latest
```

Or build locally:

```bash
make build
./bin/forgetmenot -version
```

### Embeddings

**Ollama (recommended, local):**

```bash
ollama pull nomic-embed-text
ollama serve  # default: http://localhost:11434
```

**Alternative, remote (OpenAI-compatible):**

```bash
forgetmenot -embed openai -embed-url https://api.openai.com/v1 -embed-api-key $OPENAI_API_KEY
```

## Claude Code setup

Add the server to `~/.claude.json` or to `.mcp.json` in your project:

```json
{
  "mcpServers": {
    "forgetmenot": {
      "command": "forgetmenot",
      "args": []
    }
  }
}
```

If `forgetmenot` is not in `$PATH`, use the absolute path to the binary. The database is created automatically at `$XDG_DATA_HOME/forgetmenot/memory.db` (default `~/.local/share/forgetmenot/memory.db`). Override with `-db`.

## Usage 💬

Once connected, your agent has the `memory.*` tools. Examples:

```
Remember that the backend is FastAPI on Python 3.12, DB Postgres 16.
→ the agent calls memory.remember

Continuing feature #42. Do you know the context?
→ the agent calls memory.recall and retrieves the relevant memories

Forget the memory about the old SMTP client.
→ the agent calls memory.forget
```

## Development

```bash
make test    # unit tests
make build   # static binary in ./bin
make lint    # go vet
```

Structure:

```
cmd/forgetmenot/    entry point, CLI subcommands
internal/memory/    core: model, SQLite store, service (remember/recall/...)
internal/embed/     embedding providers (Ollama, OpenAI-compat)
internal/mcpserver/ MCP layer (memory.* tools)
internal/eval/      eval harness (recall@k)
```

## Roadmap 🗺️

- M0 ✅: remember/recall/forget/update/stats, SQLite, embeddings
- M1 ✅: relations (link/supersedes), conflicts + resolution, CLI, eval harness
- M2 ✅: automatic operation (project_context, capture, hooks, agent skill), decay + maintain
- M3 ✅: trust levels + sanitization, CLAUDE.md bridge, memory budget, public benchmark
- M4 ✅ (this release): sessions, topics, timeline correlation, markdown export, compact embeddings
- M5: HTTP/SSE transport, plugins, Web UI, telemetry

## License

MIT.
