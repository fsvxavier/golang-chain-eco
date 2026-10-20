# ciam: surgical context for AI assistants (`ciam/`)

**English** | [Português](../pt-BR/CIAM.md) | [Español](../es-ES/CIAM.md)

A Go port, integrated into this ecosystem, of the ideas behind [Context IA Manager](https://github.com/smkbarbosa/context-ia-manager): instead of sending whole files to the model, **index the project**, **search only what matters** (semantics + keywords), **keep the why** (ADR, PRD, plans) and **remember decisions** across sessions. Everything is exposed as `tools.Tool` (for agents in this project) and as an **MCP server** (VS Code, Cursor, Claude Code...).

Tutorial: [manual/18-ciam-contexto.md](manual/18-ciam-contexto.md). Examples: [`examples/ciam/`](../../examples/ciam/). Generic MCP server: package [`mcp/server`](../../mcp/server).

> **About the origin.** The original repository has no license file; this port is therefore a reimplementation based on the behavior described in its README and on reading its code, **without copying text**: templates, prompts and tables are original. The tool names (`ciam_*`) were kept for compatibility of use.

## Original → Go map

| Original (ciam) | Here |
|---|---|
| `ciam index` (SQLite, hash per file) | `Manager.Index` over any `store.Store`; incremental by SHA-256 |
| hybrid search (vector + simplified BM25, RRF) | `Manager.Search`: cosine + **BM25 with real corpus IDF** + RRF (k=60) |
| Django chunk types (`model`, `view`, ...) | Pluggable `Classifier`: Django, Go, JS/TS/Python, generic (`test`, `handler`, `config`, `doc`, `schema`) |
| `ciam_compress` | `Compress` (Python `#`/docstrings; Go/JS `//` and `/* */`) |
| `ciam_remember` / `ciam_recall` | `Remember`/`Recall`, memories from **langmem** (`IndexedStore`) in namespace `["ciam","memories"]` |
| `adr`/`prd`/`plan` `new`/`list`/`supersede` | `Docs.New/List/SupersedeADR/SetStatus` (+ `research`) |
| `ciam_adr/prd/plan/research_search` | tools with a filter by chunk type |
| `ciam_decision_context` | `Manager.DecisionContext`: code + ADR + PRD + plans + research |
| `ciam_django_map` | `ProjectMap` (Django: apps; Go: packages; others: directories) — tool `ciam_project_map` |
| `ciam_route` | `Route` (table of intents → tool) |
| `ciam_draft` (local Ollama) | `Manager.Draft` with any `models.ChatModel` (`Manager.Model`) |
| L1/L2 cache, metrics | in-memory cache (LRU by age + TTL) and `Status` (tokens served / estimated saved) |
| MCP server (mcp-go) | `Toolkit.MCPServer()` over `mcp/server` (stdio and HTTP) |
| `ciam watch` | `Manager.Watch` (incremental polling; no fsnotify, stdlib only) |
| `ciam init` (6 editors) | `ciam init --editor vscode\|cursor\|claude` (or `--print`) |

## API

```go
m := ciam.New(embedder, store)                 // store nil = memory; ciam.OpenFileStore(path) persists to JSON
res, _ := m.Index(ctx, "/proj", ciam.IndexOptions{})  // incremental: only re-embeds what changed
hits, _ := m.Search(ctx, ciam.SearchRequest{Project: "proj", Query: "jwt auth", Types: []string{"view"}, Limit: 5, Compress: true})
dc, _ := m.DecisionContext(ctx, "proj", "add refresh token", 3)
m.Remember(ctx, "decision", "...") ; m.Recall(ctx, "...", 3)

docs := ciam.NewDocs("/proj")
docs.New(ciam.ADR, "Use UUID as PK", "") ; docs.New(ciam.Plan, "Stripe integration", "PRD-001")
docs.SupersedeADR(3, "Use ULID as PK")      // the old one becomes "superseded": history is not rewritten

kit := ciam.Toolkit{M: m, Project: "/proj"}
kit.All()                                      // []tools.Tool for an agent
kit.MCPServer(nil).ServeStdio(ctx, os.Stdin, os.Stdout)
```

**Indexing.** Walks the project ignoring hidden entries and `node_modules`, `vendor`, `venv`, `dist`... (files > 1 MB too). Windows of 50 lines with 5 lines of overlap, cut at 1800 bytes (the context limit of small embedding models); documents under `docs/{adr,prd,plans,research}` get their own type (`adr`, `prd`, `plan`, `research`) in windows of 80 lines. The file hash decides whether it is re-embedded; removed files leave the index. The project id is the name of the root directory.

**Search.** Two rankings over the candidates (after type filtering): cosine similarity and BM25 (`snake_case`/`camelCase` identifiers are split, so `stripe customer` finds `stripe_customer_id`). **RRF** fusion (`Σ 1/(60+position)`); a passage with no word in common does not get a position in the keyword ranking. `Compress` removes docstrings and comments before returning. Repeated results come from the cache (default 256 entries, 10 min), invalidated on reindex.

**Embedder.** Any `embeddings.Embedder`: `providers/ollama.Embeddings{Model: "nomic-embed-text"}` for 100% local. The CLI tests Ollama and, if it does not answer, uses the hashing embedder (`CIAM_EMBEDDER=fake` forces it) — search then relies more on BM25.

**Storage.** `store.Store`: chunks in `["ciam","chunks",project]`, hashes in `["ciam","files",project]`, memories in `["ciam","memories"]`. `FileStore` keeps everything in one JSON file (atomic write) — fine for one developer; for teams, large projects or several processes use `sqlstore`, Redis or Mongo. Search loads the project corpus into RAM (invalidated on reindex), so the cost per search is O(chunks).

**Documents.** ADR (`status`, context, decision, consequences, alternatives), PRD (problem, goal, scope, requirements, acceptance criteria), Plan (phases with automatable criteria and manual verification, pause between phases, `prd:` referencing the PRD), Research. Files `docs/<type>/<PREFIX>-NNN-slug.md` with YAML front matter (`status`, `date`, `title`); sequential numbering. `Docs.PlanExcerpt(n, "Phase 2")` extracts a phase for prompts.

**`ciam_draft`.** Exists only if `Manager.Model` is configured (CLI: `CIAM_CODE_MODEL`, via Ollama). It builds the prompt with the decision context + the plan excerpt and returns a draft **labeled as speculative** — it is meant to save paid tokens, not to be accepted without review.

## CLI

```bash
go install ./cmd/ciam
ciam adr new "Use JWT for sessions"     # write the why BEFORE the code
ciam index                               # incremental
ciam search "authentication" --type view --limit 3
ciam decision "add refresh token"        # code + ADR + PRD + plans
ciam remember "we decided to use UUID as PK" --type decision ; ciam recall "primary keys"
ciam mcp                                 # MCP server over stdio
ciam init --editor claude                # writes .mcp.json (merges with existing servers)
CIAM_API_KEY=secret ciam api --addr :8080   # JSON API: POST /tools/{name}, GET /tools, GET /status, GET /healthz
```

`ciam api` exposes the same tools as plain JSON, without MCP: `POST /tools/ciam_search` with the body `{"query": "..."}` returns the result; an unknown name responds 404, invalid arguments 400. With `CIAM_API_KEY` set, every route (except `/healthz`) requires the `X-Api-Key` header, otherwise 401. In code: `ciam.Toolkit{M: m, Project: root}.Handler(apiKey)`.

Variables: `CIAM_DB_PATH` (default `~/.local/share/ciam/ciam.json`), `CIAM_PROJECT_PATH`, `CIAM_OLLAMA_URL`/`OLLAMA_HOST`, `CIAM_OLLAMA_MODEL`, `CIAM_CODE_MODEL`, `CIAM_EMBEDDER`, `CIAM_API_KEY`.

## Integration with agents

- `openswe.Config{Context: &ciam.Toolkit{...}}` gives the engineer the tools and the instruction "consult before reading files and before implementing" (`ciam.Instructions`).
- In any `agents.Create`/`deepagents.Create`: `Tools: kit.All()` and `SystemPrompt: ciam.Instructions`.
- Serving to editors: `Toolkit.MCPServer(onCall)`; `onCall` receives the name, arguments, duration and error of each call (metrics).

## Differences from the original (no sugarcoating)

- **No persistent L2 cache** of searches (in memory only) and no sqlite-vec: the index lives in the `store.Store`. Cosine is a scan by default; with `Manager.IndexFactory` (e.g. `func() langmem.VectorIndex { return vectorstore.NewHNSW() }`) it uses one HNSW per project, rebuilt when the project's index changes. BM25 remains exact.
- **Metrics** are process counters (`Status`), not a persistent per-tool table.
- **Generic editing**: the original is Django-focused; here the Django types remain, and there are rules for Go/JS/Python/config, but no syntax analysis (classification by file name and content hints).
- ADR/PRD/Plan templates are **original and in English** (the original uses Portuguese); edit `ciam/templates.go` for your team's language.
- `watch` is polling (default 30 s), not file system events.
- `ciam init` covers VS Code, Cursor and Claude Code; for Windsurf, Cline, OpenCode, Antigravity use `--print` and paste the JSON.

## Tests

`go test -race ./ciam/`: classification and incremental indexing (changed/removed), hybrid search (identifiers, filters, compression, cache), ADR/PRD/Plan cycle (supersede, status, plan→PRD link, phase excerpt), `DecisionContext` without document leakage into the code section, `FileStore` persistence (reopen → same search, same hashes; corrupted file = error), memory, end-to-end `Toolkit` over MCP (real `mcp` client over pipes), `Draft`, `Compress`/`Tokenize` and `ProjectMap`.
