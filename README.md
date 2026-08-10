# forgetmenot

Memorie persistentă, structurată și căutabilă semantic pentru agenți AI, printr-un **MCP server** local, scris în Go. Un singur binary, zero dependențe de runtime, datele rămân pe mașina ta.

Funcționează cu orice agent care suportă MCP: Claude Code, Cursor, Codex, OpenClaw și altele.

## De ce

Agenții uită totul între sesiuni. Repeti același context, deciziile arhitecturale se pierd, preferințele tale sunt ignorate. `forgetmenot` dă agenților tăi o memorie de lungă durată: fapte, decizii, preferințe, entități, context de proiect și episoade, stocate local și găsite semantic.

Poziționare: **igienă + încredere** (dedupe, provenance, conflicte, uitare inteligentă) ca funcții de bază, nu add-on-uri. Detalii în [PRD.md](./PRD.md).

## Features (M1)

- `memory.remember` - stochează o memorie; dedupe automat + detecție de conflicte
- `memory.recall` - search semantic cu scor de similaritate, filtru pe proiect/tip, ascunde memoriile superseded
- `memory.forget` - șterge o memorie
- `memory.update` - modifică conținut/tip/proiect/importanță/metadata
- `memory.link` - relații între memorii: `related`, `supersedes`, `part_of`
- `memory.conflicts` - listează conflictele deschise
- `memory.resolve_conflict` - alege câștigătorul; perdedorul devine superseded
- `memory.stats` - număr de memorii și proiecte
- Tipuri de memorie: `fact`, `preference`, `decision`, `entity`, `context`, `episode`
- Embeddings locale (Ollama) sau remote (OpenAI-compatibil)
- SQLite pure-Go: un singur binary static, fără cgo, cross-compile ușor
- CLI: `export`, `import`, `stats`, `list`, `eval`
- Eval harness cu recall@k (20 de interogări)

## CLI

```bash
forgetmenot stats                       # număr memorii + proiecte
forgetmenot list -project demo          # listează memoriile
forgetmenot export -project demo > mem.json   # backup portabil (cu embeddings)
forgetmenot import < mem.json           # restore
forgetmenot eval                        # seed + eval pe embeddings reale (Ollama)
```

## Instalare

Ai nevoie de Go 1.26+:

```bash
go install github.com/iwanro/forgetmenot/cmd/forgetmenot@latest
```

Sau build local:

```bash
make build
./bin/forgetmenot -version
```

### Embeddings

**Ollama (recomandat, local):**

```bash
ollama pull nomic-embed-text
ollama serve  # implicit: http://localhost:11434
```

**Alternativ, remote (OpenAI-compatibil):**

```bash
forgetmenot -embed openai -embed-url https://api.openai.com/v1 -embed-api-key $OPENAI_API_KEY
```

## Configurare pentru Claude Code

Adaugă serverul în `~/.claude.json` sau în `.mcp.json` din proiectul tău:

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

Dacă `forgetmenot` nu e în `$PATH`, folosește calea absolută către binary. Baza de date e creată automat în `$XDG_DATA_HOME/forgetmenot/memory.db` (implicit `~/.local/share/forgetmenot/memory.db`). Poți suprascrie cu `-db`.

## Cum îl folosești

Odată conectat, agentul tău are instrumentele `memory.*`. Exemple:

```
Reține că backend-ul e FastAPI pe Python 3.12, DB Postgres 16.
→ agentul cheamă memory.remember

Continuăm feature #42. Știi contextul?
→ agentul cheamă memory.recall și aduce memoriile relevante

Uită memoria despre vechiul client SMTP.
→ agentul cheamă memory.forget
```

## Dezvoltare

```bash
make test    # teste unitare
make build   # binary static în ./bin
make lint    # go vet
```

Structura:

```
cmd/forgetmenot/    entry point + flag-uri
internal/memory/    core: model, SQLite store, service (remember/recall/...)
internal/embed/     embedding providers (Ollama, OpenAI-compat)
internal/mcpserver/ stratul MCP (tool-urile memory.*)
```

## Roadmap

- M0 ✅: remember/recall/forget/update/stats, SQLite, embeddings
- M1 ✅ (acest release): relații (link/supersedes), conflicte + rezolvare, CLI, eval harness
- M2: decay + compresie automată, provenance, `project_context`, Web UI
- M3: anti prompt injection, bridge cu CLAUDE.md, benchmark public, memory budget

## Licență

MIT.
