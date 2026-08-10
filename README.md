# forgetmenot

Persistent, structured, semantically searchable memory for AI agents, delivered as a local **MCP server** written in Go. One static binary, zero runtime dependencies, data stays on your machine.

Works with any MCP-capable agent: Claude Code, Cursor, Codex, OpenClaw and others.

## Why

Agents forget everything between sessions. You re-explain the same context, architectural decisions get lost, your preferences are ignored. `forgetmenot` gives your agents long-term memory: facts, decisions, preferences, entities, project context and episodes, stored locally and found semantically.

Positioning: **hygiene + trust** (dedupe, provenance, conflicts, intelligent forgetting) as first-class features, not add-ons. Details in [PRD.md](./PRD.md).

## Features (M1)

- `memory.remember` - store a memory; automatic dedupe + conflict detection
- `memory.recall` - semantic search with similarity score, project/type filters, hides superseded memories
- `memory.forget` - delete a memory
- `memory.update` - change content/type/project/importance/metadata
- `memory.link` - relations between memories: `related`, `supersedes`, `part_of`
- `memory.conflicts` - list open conflicts
- `memory.resolve_conflict` - pick the winner; the loser becomes superseded
- `memory.stats` - memory and project counts
- Memory types: `fact`, `preference`, `decision`, `entity`, `context`, `episode`
- Local embeddings (Ollama) or remote (OpenAI-compatible)
- Pure-Go SQLite: single static binary, no cgo, easy cross-compile
- CLI: `export`, `import`, `stats`, `list`, `eval`
- Eval harness with recall@k (20 queries)

## CLI

```bash
forgetmenot stats                       # memory + project counts
forgetmenot list -project demo          # list memories
forgetmenot export -project demo > mem.json   # portable backup (with embeddings)
forgetmenot import < mem.json           # restore
forgetmenot eval                        # seed + eval against real embeddings (Ollama)
```

## Install

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

## Usage

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

## Roadmap

- M0 ✅: remember/recall/forget/update/stats, SQLite, embeddings
- M1 ✅ (this release): relations (link/supersedes), conflicts + resolution, CLI, eval harness
- M2: automatic decay + compression, provenance, `project_context`, Web UI
- M3: prompt-injection defense, CLAUDE.md bridge, public benchmark, memory budget

## License

MIT.
