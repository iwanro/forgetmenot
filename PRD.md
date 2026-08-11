# PRD: Agent Memory - Memorie Persistentă pentru Agenți AI

| | |
|---|---|
| **Versiune** | 0.1 (draft) |
| **Data** | 2026-08-10 |
| **Autor** | iwan |
| **Status** | În lucru |
| **Nume de lucru** | `forgetmenot` (ales 2026-08-10; alternativ: `agent-memory`) |
| **Ideea** | #4 din lista discutată: "Memorie persistentă pentru agenți" |

---

## 1. Rezumat executiv

Agenții AI actuali (Claude Code, Cursor, asistenți MCP) uită totul între sesiuni. Fiecare sesiune începe de la zero, iar utilizatorul repetă aceleași instrucțiuni, context și decizii de fiecare dată.

**Agent Memory** rezolvă asta printr-un **MCP server open source** care oferă oricărui agent o memorie persistentă, structurată și căutabilă semantic: fapte, entități, decizii, context de proiect, cu compresie automată și uitare inteligentă.

Principii de design:

- **Local-first**: toate datele rămân pe mașina utilizatorului. Fără cloud, fără cont.
- **Zero config**: instalare cu o comandă, funcționează imediat cu agenții populari.
- **Standarde deschise**: se integrează prin MCP (Model Context Protocol), deci funcționează cu orice agent care suportă MCP.
- **Hibrid**: reține automat lucrurile importante, dar lasă controlul explicit utilizatorului.

---

## 2. Problema

### 2.1 Declarația problemei

Agenții AI nu au memorie. Consecințe concrete, resimțite zilnic de dezvoltatori:

1. **Context repetat**: utilizatorul rescrie același context (stack, convenții, decizii) la fiecare sesiune nouă.
2. **Decizii pierdute**: alegerile arhitecturale și "de ce"-urile din săptămâna trecută sunt uitate; agentul propune soluții deja respinse.
3. **Fără învățare**: agentul nu învață preferințele utilizatorului (stil de cod, limbaj, tooling).
4. **Eroare repetată**: bug-uri diagnosticate anterior reapar pentru că agentul nu își amintește diagnosticele trecute.
5. **Proiecte multiple, confuzie totală**: fără separare pe proiect, memoria ar amesteca totul. Astăzi nu există nicio memorie de separat.

### 2.2 De ce acum

- Ecosistemul MCP e încă tânăr (2025-2026) și crește rapid; descoperirea e ușoară, competiția fragilă.
- Agenții de cod au intrat în mainstream; durerea memoriei e resimțită de milioane de utilizatori zilnic.
- Soluțiile existente (vezi §10) sunt fie SaaS închise, fie immature, fie orientate pe un singur agent.
- Modelele locale de embeddings (Ollama, etc.) au ajuns suficient de bune încât memoria poate fi 100% locală fără pierderi mari de calitate.

---

## 3. Scop

### 3.1 Goals (la ce ne angajăm)

- G1. Oferi oricărui agent MCP o memorie persistentă locală, cu search semantic.
- G2. Stoca tipuri structurate de memorie: fapte, entități, decizii, contexte de proiect, preferințe.
- G3. Compresie automată a memoriilor vechi/nefolosite, cu păstrarea esenței.
- G4. Uitare inteligentă (decay) bazată pe utilizare: ce nu e reamintit, își pierde prioritatea.
- G5. Detecție de conflicte (fapte contradictorii) cu flux de rezolvare.
- G6. Zero config pentru agentul principal (Claude Code), instalare într-o comandă.
- **G7 (decizie produs, 2026-08-10): funcționare automată.** Memoria se captează și se injectează AUTOMAT (hooks + instrucțiuni agent + întreținere în fundal), fără ca utilizatorul să ruleze comenzi manual. Manual rămâne doar ca override/oprire explicită.

### 3.2 Non-goals (la ce NU ne angajăm în v1)

- NG1. Nu construim un agent. Doar memorie pentru alți agenți.
- NG2. Nu facem sync cloud în v1 (posibil mai târziu, opțional și criptat).
- NG3. Nu înlocuim sistemul de fișiere al proiectului (nu e un storage de cod).
- NG4. Nu facem fine-tuning sau antrenare pe memoria utilizatorului.
- NG5. Nu suportăm multi-utilizator / echipe în v1.
- NG6. Nu capturăm automat *tot* ce se întâmplă nefiltrat (privacy). Captura automată se face pe **reguli** (început/sfârșit de sesiune, sumar de sesiune, activitate de tool-uri), cu opt-out granular și redactare de secrete.

---

## 4. Utilizatori țintă și personae

### P1. Dezvoltator individual (target principal)
- Folosește Claude Code / Cursor / asistenți MCP zilnic.
- Lucrează la 1-5 proiecte simultan.
- Se enervează când repetă același context de fiecare dată.
- Preferă local-first, zero config, fără cont.

### P2. Power user / self-hoster
- Rulează Ollama local, vrea totul offline.
- Testează mai mulți agenți, vrea memorie comună între ei.
- Îi pasă de control fin: ce se reține, ce se uită, export.

### P3. Contribuitor open source (secundar)
- Vrea să extindă memoria cu tipuri noi de memorie sau sink-uri (Notion, Jira).
- Atras de arhitectură curată și extensibilitate prin plugin-uri.

---

## 5. Cazuri de utilizare

| # | Caz | Scenariu |
|---|-----|----------|
| UC1 | Reținere de fapt | "Reține că backend-ul folosește FastAPI, Python 3.12, iar baza de date e Postgres 16." Agentul folosește asta în sesiunile următoare. |
| UC2 | Decizie arhitecturală | "Am ales SQLite în loc de Postgres pentru modulul X din cauza Y." Agentul nu mai propune Postgres pentru X. |
| UC3 | Preferință de stil | "Scrie commit messages în format Conventional Commits, imperative." Se aplică global sau per proiect. |
| UC4 | Context reluat | Sesiune nouă: "Continuăm lucrul la #42." Agentul aduce automat contextul relevant al proiectului. |
| UC5 | Căutare semantică | "Când am debugat problema de timeout anul trecut?" Răspuns: memoria relevantă cu data, proiectul și rezultatul. |
| UC6 | Conflicte | Se reține "API key e în .env" apoi "API key e în config.py". Memoria detectează conflictul și cere clarificare. |
| UC7 | Compresie | 200 de memorii despre proiect X devin, după 3 luni, un rezumat structurat de 10 intrări cheie. |
| UC8 | Uitare | O preferință folosită o dată acum 6 luni scade în relevanță; search-ul o aduce doar la cerere explicită. |
| UC9 | Multi-agent | Același utilizator, același proiect: Claude Code și Cursor citesc/scriu aceeași memorie. |
| UC10 | Export/portabilitate | `memory export` produce JSON/SQLite portabil pentru backup sau migrare. |

---

## 6. Modelul memoriei

### 6.1 Tipuri de memorie (v1)

| Tip | Descriere | Exemple | Expirare implicită |
|-----|-----------|---------|--------------------|
| `fact` | Enunț verificabil, mic | "DB e Postgres 16", "auth folosește JWT" | permanent (sau la conflict) |
| `preference` | Preferință a utilizatorului | "Commit-uri Conventional Commits", "fără tabs" | permanent |
| `decision` | Decizie cu rațiune | "Am ales X pentru că Y" | permanent |
| `entity` | Entitate cu proprietăți | persoană, serviciu, proiect, tool | permanent |
| `context` | Stare de lucru per proiect | "feature #42: în progres, blocat pe Z" | volatil, reîmprospătat |
| `episode` | Eveniment/experiență trecută | "bug timeout rezolvat prin W" | decay la N luni |

### 6.2 Atribute comune

- `id`, `type`, `content` (text), `embedding` (vector)
- `project` (namespace: `global` sau nume de proiect)
- `importance` (0-1, inițial din instrucțiune sau dedus)
- `last_accessed_at`, `access_count` (pentru decay)
- `created_at`, `source` (care agent/tool a scris)
- `metadata` (JSON liber: tags, links către alte memorii)
- `conflict_group` (dacă e parte dintr-un conflict)

### 6.3 Relații între memorii

- `links`: `entity -> fact` ("proiectul X folosește Postgres")
- `supersedes`: o decizie înlocuiește alta (se păstrează istoricul)
- `conflicts_with`: pentru detecția de contradicții

---

## 7. Cerințe funcționale

### 7.1 Instrumente MCP (P0)

| Tool | Descriere | Params cheie |
|------|-----------|--------------|
| `memory.remember` | Stochează o memorie | `content`, `type`, `project`, `importance`, `metadata` |
| `memory.recall` | Search semantic + filtru | `query`, `project`, `type`, `limit`, `min_score` |
| `memory.update` | Modifică / întărește o memorie | `id`, `content`, `importance_delta` |
| `memory.forget` | Șterge explicit | `id` |
| `memory.entities` | Listează entitățile cunoscute | `project` |
| `memory.conflicts` | Listează conflictele nerezolvate | `project` |
| `memory.resolve_conflict` | Alege câștigătorul unui conflict | `conflict_group`, `winning_id` |
| `memory.stats` | Sănătatea memoriei | - |
| `memory.export` / `memory.import` | Portabilitate | `format`, `path` |

### 7.2 Instrumente MCP (P1)

| Tool | Descriere |
|------|-----------|
| `memory.summarize_project` | Compresie manuală a unui proiect |
| `memory.project_context` | Rezumat de intrare pentru sesiune nouă ("ce știm despre X") |
| `memory.suggest` | Ce ar trebui reținut din conversația curentă (apelat de agent) |
| `memory.hook_auto_remember` | Config pentru reținere automată pe reguli (ex: toate deciziile) |

### 7.3 Comportament

1. **Recall**: returnează top-K rezultate, scor ≥ prag, filtrate pe `project` (dacă e specificat). Include `supersedes` (rezultatul înlocuit nu mai e sugerat, dar e vizibil la cerere).
2. **Remember**: dedupează (similaritate ≥ prag) înainte de insert; dacă există o memorare similară, actualizează `access_count`/`last_accessed_at` în loc să dubleze.
3. **Conflict**: la `remember`, dacă noua memorare contrazice una existentă (semantic + tip), creează `conflict_group` și marchează ambele. Nu șterge nimic automat.
4. **Auto-context**: la cerere, `project_context` compune un rezumat (top memorii + entity summary) pentru a fi injectat în prompt de agent.

---

## 8. Cerințe non-funcționale

### 8.1 Performanță

| Metrică | Țintă |
|---------|-------|
| `recall` cu < 10k memorii | < 100 ms (SQLite + index vector brut / HNSW) |
| `remember` | < 50 ms + timpul de embedding |
| Storage SQLite | < 5 MB la 10k memorii (fără embeddings mari) |
| Embedding local (Ollama) | `nomic-embed-text` sau similar, batch async |

### 8.2 Privacy și securitate

- Local-first: fără telemetrie, fără cloud, fără cont. (Decizie fermă de produs, nu doar tehnică.)
- Fișierul DB are permisiuni 0600; `metadata` nu conține niciodată secrete.
- **Risc prompt injection**: memoria e conținut potențial neîncrezător. Recall-ul returnează surse și scoruri; agentul decide. Documentăm acest pattern pentru utilizatori.
- Opțional (P1): criptare la repaus cu cheie locală (`SQLCipher` sau simetric pe fișier).

### 8.3 Portabilitate și integrare

- MCP transport: `stdio` (v1), `HTTP/SSE` (P1 pentru remote).
- SDK: bibliotecă de bază în **Go** (ales 2026-08-10: binary static unic, zero dependențe, SDK MCP oficial matur) + client MCP.
- Instalare: `go install github.com/iwanro/forgetmenot/cmd/forgetmenot@latest` (binary static, zero dependențe), plus un config exemplu pentru `claude_desktop_config.json` și `.mcp.json`.

### 8.4 Calitate

- Teste unitare pe: dedupe, conflict, decay, compresie, export/import.
- Teste de integrare cu un agent real (Claude Code) pe un repo de test.
- Eval harness (P1): set de interogări cu răspuns așteptat pentru a măsura recall quality.

---

## 9. Arhitectură (schiță)

```
┌─────────────────────────────┐
│  Agent (Claude Code, etc.)  │
└──────────────┬──────────────┘
               │ MCP (stdio)
┌──────────────▼──────────────┐
│     MCP Server Layer        │  ← tools, validare, auth local
│   (forgetmenot/mcp)         │
└──────────────┬──────────────┘
┌──────────────▼──────────────┐
│      Core Library           │  ← remember/recall/conflict/decay
│   (forgetmenot/core)        │
└──────────────┬──────────────┘
┌──────────────▼──────────────┐
│   Storage Engine (SQLite)   │  ← tabele memorii + index vector
└──────────────┬──────────────┘
┌──────────────▼──────────────┐
│   Embedding Provider        │  ← Ollama local / remote (config)
└─────────────────────────────┘
```

Componente:

1. **Core** - logica pură, fără dependențe MCP; testabilă unitar; poate fi înglobată în alte tool-uri.
2. **MCP Server Layer** - expune instrumentele; subțire, doar translatează.
3. **Storage** - SQLite cu extensie vectorială (ex. `sqlite-vec` sau fallback brute-force cu numpy); schema migrabilă.
4. **Embeddings** - provider abstract: `OllamaEmbedder`, `OpenAIEmbedder`, `HuggingFaceEmbedder` (P1). Default: Ollama local.
5. **CLI** (P1) - `memory export/import/stats/project_context` pentru scripturi și debugging.
6. **Web UI** (P2) - browser pe memorii, editare manuală, rezolvare conflicte vizual.

### 9.1 Funcționare automată (G7) - cum se întâmplă fără utilizator

MCP pe stdio e pasiv: răspunde doar la apeluri. "Automat" se realizează prin trei straturi, toate folosind același binary și același DB:

```
┌────────────────────────────────────────────────────────────┐
│ 1. Hooks la agent (Claude Code)                              │
│    SessionStart  → forgetmenot project_context  → injectează │
│                     contextul proiectului în CLAUDE.md /     │
│                     prompt (auto-recall la pornire)          │
│    Stop/SessionEnd → forgetmenot capture --summary  →        │
│                     salvează automat ce s-a întâmplat        │
│    UserPromptSubmit → (opt-in) forgetmenot recall pre-query  │
├────────────────────────────────────────────────────────────┤
│ 2. Instrucțiuni agent (plugin/SKILL)                         │
│    Agentul e instruit să cheme mereu memory.recall când      │
│    începe o sarcină și memory.remember la decizii/fapte noi  │
│    → se comportă automat, fără ca utilizatorul să ceară      │
├────────────────────────────────────────────────────────────┤
│ 3. Întreținere în fundal                                     │
│    forgetmenot daemon (sau cron: forgetmenot maintain)       │
│    → decay + compresie + consolidare pe un timer, singur     │
└────────────────────────────────────────────────────────────┘
```

Reguli de captură automată (opt-out granular, niciodată conținut nefiltrat):
- La `Stop`/`SessionEnd`: sumar al sesiunii → memorii `episode` + `decision`.
- La `SessionStart`: `project_context` → memorii `context` + `fact` relevante injectate.
- Fapte marcate de utilizator/agent explicit (`remember` cu `auto=true` din instrucțiune).
- Redactare de secrete: niciodată chei, token-uri, parole în memorii (M3: anti-injection + redactare).

Manual rămâne doar ca: override (`forgetmenot remember` / `list` / `export`), debugging, backup.

---

## 10. Competiție și poziționare

### 10.1 Harta pieței (august 2026, date GitHub API)

| Proiect | Stars | Ce face bine | Ce îi lipsește |
|---------|-------|--------------|----------------|
| **mem0** | 63k | Universal memory layer, ecosistem matur | Orientat pe app builders; config greoaie; cloud-first |
| **codebase-memory-mcp** | 38k | Code intelligence, indexează codul în knowledge graph | E despre cod, nu despre memorie personală/conversațională |
| **Graphiti (Zep)** | 29.7k | Knowledge graph temporal, contradicții prin LLM | Complex (Neo4j/FalkorDB), orientat enterprise; fără UX de conflict explicit |
| **context-mode** | 19.7k | Comprimă output-ul tool-urilor, persistență sesiune | Se concentrează pe context window, nu pe memorie de lungă durată |
| **memU** | 14.2k | Personal memory across agents | Bazat pe platforma lor; nu self-host complet |
| **EverOS** | 11.9k | Local-first, Markdown-native, self-evolving | Fără decay/uitare serioasă; fără anti-injection |
| **MemOS** | ~10k+ | Self-evolving, hybrid retrieval, -35% tokens | Nu publică uitare/decay; storage aditiv |
| **engram** | 5.9k | Go single binary: MCP + HTTP + CLI + TUI, SQLite+FTS5 | Fără conflict/proveniență/decay ca primă clasă |
| **basic-memory** | 3.6k | Markdown + Obsidian, citibil de om, knowledge graph | AGPL, fără decay/compresie structurată, fără anti-injection |
| **nocturne_memory** | 1.3k | Rollbackable, vizual, graph-like, drop-in pentru agenți | Mic, fără benchmark public, fără memory budget |
| **token-savior** | 1.1k | Benchmark public concret (97.9% la -80% tokens) | Orientat pe code navigation, nu memorie generală |
| **mcp-knowledge-graph** | 883 | Knowledge graph local, standard MCP reference | Baza, fără igienă/proveniență |
| **iwe** | 1.4k | Markdown knowledge graph + LSP + MCP | Note-taking, nu memorie agent |

### 10.2 Gap-uri reale pe care nimeni nu le acoperă

1. **Igienă: decay + compresie + uitare inteligentă**. Toată lumea adună memorii la infinit (vector bloat). Nimeni nu are uitare ca feature de bază, cu prioritate scăzută pe măsură ce ceva devine irelevant.
2. **Conflict detection + rezolvare ca primă clasă**. `memory.conflicts` + `resolve_conflict` ca UX explicit, nu doar edge-uri LLM în knowledge graph.
3. **Proveniență + audit trail**. Fiecare memorie știe de unde a venit (ce sesiune, ce agent, ce fișier). Fără asta, memoria e un zvon.
4. **Anti prompt injection în memorie**. Memoria e un vector de atac neatins. Trust levels, sanitizare, surse în rezultate.
5. **Memory budget per sesiune**. Memoriile injectate automat sunt comprimate și prioritizate ca să nu umfle contextul.
6. **Bridge cu memoria nativă** (CLAUDE.md, `.claude`, skills): sincronizare bidirecțională.

### 10.3 Poziționare

*"Memoria de lungă durată, locală și structurată, cu igienă automată (decay, compresie, conflict) și încredere (proveniență, anti-injection), care funcționează cu orice agent MCP, validată de un benchmark public."*

Diferențiatorii noștri, în ordinea importanței:
1. **Igienă + încredere** (decay, conflict, provenance, anti-injection) ca funcții de bază, nu add-on-uri.
2. **Benchmark public** de calitate a memoriei (eval harness), ca dovadă, nu slogan.
3. **Local-first ferm** + bridge bidirecțional cu memoria nativă a agenților.

---

## 11. Succes și metrici

### 11.1 Obiective (măsurabile)

- **M1 (luna 1)**: MVP funcțional, 5+ utilizatori reali care îl folosesc săptămânal.
- **M3**: 300+ stele, 20+ issues rezolvate, 3+ contribuitori externi, 1 articol/recenzie în comunitate.
- **M6**: 1k+ stele, adoptat în 2+ tool-uri/articole externe, eval harness public.

### 11.2 Metrici de produs

- Stele, fork-uri, contributors.
- Instalări (`uvx`/`npx`/npm downloads).
- Issue cycle time, PR-uri acceptate.
- **Calitate memorie** (cheia): accuracy la recall pe eval set ≥ 90%, fără false positives dominante; rata de conflicte rezolvate.
- Adoption semnal: `memory.remember` calls / agent activ / săptămână (telemetrie opțională, off by default).

### 11.3 Semnale de eșec (când oprim)

- După 6 luni, < 100 stele și fără utilizatori recurenți.
- Recall considerat inutil de utilizatori (feedback negativ consistent pe calitate).
- Un competitor domină exact aceeași nișă local-first cu feature parity.

---

## 12. Roadmap

### M0 - MVP ✅ (2026-08-10)
- [x] Repo + structură + CI de bază (GitHub Actions: vet, unit, integrare, build static).
- [x] Core: `remember`, `recall` (dedupe), `forget`, `update`.
- [x] Storage SQLite (modernc.org/sqlite, pure-Go) + cosine brute-force.
- [x] Embedding Ollama default + fallback OpenAI-compatibil.
- [x] MCP server cu transport stdio (SDK oficial MCP Go, 5 tool-uri).
- [x] Config pentru Claude Code (`.mcp.json`) + README.
- [x] Teste unitare + test de integrare MCP end-to-end.

### M1 - Model de date complet ✅ (2026-08-10)
- [x] Tipuri `decision`, `preference`, `entity`, `context`, `episode` (validare în core).
- [x] `links` + `supersedes` (relații între memorii: related, supersedes, part_of).
- [x] `memory.conflicts` + `resolve_conflict` (detecție automată la remember + rezolvare cu supersede).
- [x] CLI `export/import/stats/list` + subcomanda `eval`.
- [x] Eval harness (20 de interogări, recall@k, idempotent seed).
- [x] Recall ascunde memoriile superseded.
- [x] Stats corect: count + project_count distinct.
- [x] Fix review M0: validare importanță [0,1], validare tip la update, scan NULL resolved_at, importanță/conflict praguri documentate.

### M2 - Funcționare automată (G7) + igienă (parțial, 2026-08-10)
- [x] `project_context`: sumar de intrare pentru sesiuni noi (recall automat per proiect, fără embedder).
- [x] `capture --summary`: subcomandă hook pentru `Stop`/`SessionEnd` (memorii episode/decision; funcționează fără embeddings).
- [x] Config hooks Claude Code (`forgetmenot setup` scrie `.claude/settings.json`, merge cu fișiere existente): SessionStart + Stop.
- [x] Instrucțiuni agent (`.claude/skills/forgetmenot/SKILL.md`): recall la început de sarcină, remember la decizii noi.
- [x] Decay pe `episode`/`context` (bazat pe access, half-life 30 zile, floor configurabil).
- [x] `forgetmenot maintain` - decay pe timer, fără utilizator (rulabil din cron).
- [ ] Compresie: `summarize_project` + trigger periodic (necesită LLM pentru rezumare semantică; P1).
- [ ] Proveniență + audit trail complet (sursă/sesiune/agent există parțial în `source` + `metadata`; tabel de audit în P1).
- [ ] Web UI minimal (browse + edit + conflicte).
- [ ] SEO/launch: HN, Reddit, X, Product Hunt. Răspuns la toate issues.

### M3 - Încredere + extindere (parțial, 2026-08-11)
- [x] Anti prompt injection: trust levels (high/low), sanitizare la write (caractere de control, cap de lungime), surse+trust în recall, flag `[UNTRUSTED]` în project_context, migrare DB idempotentă.
- [x] Bridge bidirecțional cu CLAUDE.md: `bridge export` (context în CLAUDE.md) + `bridge import` (fapte din secțiunea facts), ambele fără embedder.
- [x] Memory budget per sesiune: `project_context -budget N` (drop de grupuri de prioritate joasă).
- [x] Benchmark public: `eval -json`, rezultat 100% recall@k (20/20) verificat în CI, secțiune Benchmark în README.
- [ ] HTTP/SSE transport, sync opțional criptat (P1).
- [ ] Plugin-uri pentru tipuri de memorie / sink-uri (Notion, Jira).
- [ ] Integrare Cursor + alte agenți MCP documentate.
- [ ] Telemetrie opt-in pentru metrici.

---


### M4 - Corelare între sesiuni + format lizibil (2026-08-11)
- [x] Sesiuni ca entitate: tabel `sessions`, `session start/end/list`, state file pentru hooks, `session_id` pe memorii.
- [x] Topic-uri ca entitate: tabel `topics` + `memory_topics`, asignare la remember (`-topics`), dedupe pe (name, project).
- [x] `memory.timeline` (MCP) + `timeline` (CLI): evoluția unui subiect între sesiuni, cu context de sesiune.
- [x] `export-md`: fișier Markdown lizibil de om și AI per proiect.
- [x] Embeddings binare float32 (BLOB) cu magic byte; migrare transparentă de la JSON legacy.
- [x] Setup generează hooks cu session start/end (automatizare completă).


### M5 - Web UI + recall îmbogățit (2026-08-11)
- [x] `forgetmenot web`: dashboard local embedded (go:embed) - memories, timeline, conflicte, sesiuni.
- [x] JSON API: GET/PATCH/DELETE memories, timeline, conflicte + resolve, sesiuni, stats.
- [x] Topics în recall output (MCP + Web).
- [ ] HTTP/SSE transport pentru agenți remote (P1).
- [ ] Plugin-uri / sink-uri (Notion, Jira) (P1).
- [ ] Telemetrie opt-in (P1).


### v0.3 - LLM features + tooling (2026-08-11)
- [x] internal/llm: client Ollama + OpenAI-compat, ChatJSON (fence-tolerant), teste.
- [x] Auto-topics la remember (-auto-topics, LLM): etichete extrase automat.
- [x] SummarizeProject: compresie episoade vechi → summary context (LLM).
- [x] `forgetmenot doctor`: diagnostic DB, embeddings, hooks, sesiune.
- [x] Release automation: goreleaser (4 platforme, static) + workflow pe tag.
- [ ] Sync criptat opțional (P1).
- [ ] Plugin-uri / sink-uri (Notion, Jira) (P1).
- [ ] Telemetrie opt-in (P1).

### v0.4 - Zero-config embeddings, orice mediu (2026-08-11)
- [x] `LexicalEmbedder`: embeddings deterministe offline (unigrams+bigrams hashate, 768d), fără serviciu extern. recall@k = 100% (20/20) pe eval set.
- [x] `AutoEmbedder`: primary (Ollama) + fallback lexical cu cooldown + probe; comutare transparentă și logată.
- [x] `-embed auto|ollama|openai|lexical`; default `auto` → MCP funcționează out-of-the-box fără Ollama (scenariu raportat de opencode: remember/recall nu mai eșuează cu `connection refused`).
- [x] Provenanță `embedding_mode` în metadata + re-embed la recall: vectorii scriși în timpul unei pene sunt auto-vindecați când Ollama revine; vectorii semantici nu sunt suprascriși de cei lexicali.
- [x] Threshold-uri calibrate pe scala lexicală (dedupe/conflict/recall floor).
- [x] `forgetmenot recall` (CLI): mirror al tool-ului MCP `memory.recall`, funcționează offline.
- [x] `doctor` aware de mod: auto → warn (fallback activ), strict → FAIL, lexical → ok fără endpoint.
- [x] Client LLM Anthropic-compatibil (`-llm anthropic`): Messages API `/v1/messages` pentru auto-topics + summarize (alături de Ollama și OpenAI-compat).
- [x] Teste: 100 (lexical determinism/sim, auto fallback+recovery, recall heal, CLI dispatch regression, MCP integrare fără embeddings, Anthropic client).

## 13. Decizii deschise (TBD)

1. ~~Limbaj de implementare~~: **ales: Go** (binary static unic, zero dependențe, SDK MCP oficial `modelcontextprotocol/go-sdk`, SQLite pure-Go fără cgo; embeddings prin HTTP către Ollama local sau API remote).
2. **Index vectorial**: `sqlite-vec` vs faiss vs brute-force numpy. Depinde de volumul țintă (target: 10k-100k memorii).
3. ~~Automat vs manual~~: **ales: automat (G7).** Reținerea și injectarea sunt automate (hooks + instrucțiuni agent + întreținere în fundal); manual = override. Reguli de captură: sumar de sesiune, fapte marcate, activitate de tool-uri; niciodată conținut nefiltrat.
4. ~~Nume de produs~~: **ales: `forgetmenot`** (repo: github.com/iwanro/forgetmenot).
5. **Model embedding implicit**: `nomic-embed-text` (Ollama) vs `bge-small` vs remote default. Trebuie testat pe eval set.

---

## 14. Riscuri și mitigări

| Risc | Impact | Mitigare |
|------|--------|----------|
| Competitori OSS ajung primii la feature parity | Mare | Viteză + diferențiere local-first + igienă automată; comunitate activă de la ziua 1 |
| Recall de calitate slabă (embeddings locale) | Mare | Eval harness devreme, testare pe modele diferite, fallback hybrid (BM25 + vector) |
| Prompt injection prin memorie | Mediu | Sursă + scor în rezultate, documentație de pattern sigur, opțiune "trust level" |
| Scope creep (vrem și UI, și sync, și plugins) | Mediu | Non-goals clare (§3.2), roadmap strict pe M0→M3 |
| Utilizatorul nu înțelege ce reține sistemul | Mediu | `memory.stats`, UI de browse, export transparent; totul e inspectabil |
| Burnout solo dev | Mediu | Scope mic la M0, stele + comunitate ca feedback loop motivațional |

---

## 15. Feedback loop (cum știm că merge)

1. **Zilnic**: folosesc eu produsul pe proiectele reale; orice fricțiune = issue.
2. **Săptămânal**: metrici (stele, instalări, issues deschise/închise, utilizatori în Discord/issue-uri).
3. **La fiecare release**: eval harness pe recall accuracy; dacă scade, nu release.
4. **Lunar**: interviuri informale cu 3-5 utilizatori activi (ce rețin ei, ce nu).

---

## 16. Anexă: Exemple de interacțiune (UX)

**Sesiune 1:**
```
User:  Reține că backend-ul e FastAPI pe Python 3.12, DB Postgres 16.
Agent: ✓ Am reținut: fact "backend FastAPI / Python 3.12 / Postgres 16" (proiect: acest repo).
```

**Sesiune 2 (repo nou, aceeași mașină):**
```
User:  Continuăm cu feature #42.
Agent: [auto] Context proiect: "feature #42 în progres, blocat pe validare Z (decizie din 12 aug)". 
       Știu și că backend-ul e FastAPI + Postgres 16. Ce facem mai departe?
```

**Conflict:**
```
User:  De fapt, am migrat DB-ul pe SQLite pentru testele locale.
Agent: ⚠ Conflict detectat: "DB Postgres 16" vs "DB SQLite (teste locale)". 
       Confirmi că SQLite o înlocuiește pe Postgres? (da / nu)
```
