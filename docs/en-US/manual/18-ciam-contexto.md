# 18. ciam: cheap and precise project context for assistants

**English** | [Português](../../pt-BR/manual/18-ciam-contexto.md) | [Español](../../es-ES/manual/18-ciam-contexto.md)

AI assistants spend tokens rereading whole files and still get things wrong because they do not know the **why** behind the code. The `ciam` package solves both sides: a **local index with hybrid search** (it returns only the relevant passages) and a `docs/` folder with versioned, searchable **ADRs, PRDs and plans**. Reference and limits: [docs/en-US/CIAM.md](../CIAM.md).

Prerequisites: chapter 6 (embeddings and RAG) and, for the agents part, chapters 5 and 9.

## 18.1 The flow on one screen

```bash
go install ./cmd/ciam
cd my-project
ciam adr new "Use JWT for sessions"     # 1. write the WHY before the code
ciam index                               # 2. index (incremental)
ciam search "authentication" --type view # 3. search passages, not files
ciam decision "add refresh token"        # 4. code + ADR + PRD + plans in one call
ciam remember "tokens expire in 15 min" --type decision
ciam init --editor claude                # 5. connect the editor (MCP)
```

Without Ollama running, the CLI uses hashing embeddings and warns you; for real semantic search: `ollama pull nomic-embed-text` (100% local).

## 18.2 Indexing and searching through the API

```go
emb := &ollama.Embeddings{Model: "nomic-embed-text"}
fs, _ := ciam.OpenFileStore("ciam.json")        // or any store.Store (sqlstore, redis, mongo)
m := ciam.New(emb, fs)

res, _ := m.Index(ctx, "/project", ciam.IndexOptions{})
fmt.Println(res.Changed, "files reprocessed,", res.Unchanged, "untouched")

hits, _ := m.Search(ctx, ciam.SearchRequest{
	Project: "project", Query: "JWT token validation",
	Types: []string{"view", "handler"}, Limit: 5, Compress: true,
})
```

- **Incremental**: only files with a new hash are cut and embedded; removed files leave the index. Reindexing costs almost nothing (`ciam watch` does it every 30 s).
- **Hybrid**: `Result.Vector` (cosine) and `Result.BM25` show each half; the final score is the reciprocal-rank fusion. `snake_case`/`camelCase` identifiers are split: "stripe customer" finds `stripe_customer_id`.
- **Chunk types**: `model`, `view`, `url`, `serializer`, `task`, `test`, `handler`, `config`, `doc`, `schema`, `generic`, and the documents `adr`, `prd`, `plan`, `research`. For other patterns, pass `Manager.Classifiers`.
- **Compression**: `Compress: true` removes docstrings/comments; it saves tokens in the response.

`m.Status(ctx)` shows the tokens served and the **estimate** of what reading the whole files would have cost extra.

## 18.3 ADRs, PRDs and plans

```go
docs := ciam.NewDocs("/project")
docs.New(ciam.PRD, "Billing via Stripe", "")
docs.New(ciam.Plan, "Stripe integration", "PRD-001")   // links the plan to the PRD
docs.New(ciam.ADR, "Fix race in the email worker", "")  // root cause BEFORE the fix
docs.SupersedeADR(3, "Use ULID as primary key")        // ADR-003 becomes "superseded"; nothing is rewritten
list, _ := docs.List(ciam.ADR)                         // number, title, status
```

Files in `docs/en-US/adr|prd|plans|research/PREFIX-NNN-slug.md` with front matter (`status`, `date`, `title`). After writing, run `ciam index`: they become searchable chunks. Golden rule: **new feature → PRD + plan; relevant bug → ADR with the root cause; changed your mind → supersede, do not edit**.

`m.DecisionContext(ctx, project, "what I am going to do", 3)` returns in Markdown: code (compressed), ADRs, PRDs, plans and relevant research — it is what an agent should consult **before implementing**.

## 18.4 Memory across sessions

`Remember`/`Recall` store decisions and notes that matter beyond a single project ("deploy only on Tuesdays"). They are `langmem` memories (chap. 16) in the namespace `["ciam","memories"]`: a `langmem.Manager` or the `manage_memory` tool can curate them too.

## 18.5 Giving the context to an agent

```go
kit := ciam.Toolkit{M: m, Project: "/project"}
agent, _ := agents.Create(agents.Options{Model: model, Tools: kit.All(), SystemPrompt: ciam.Instructions})
```

Tools (`ciam_*`): `route`, `index`, `search`, `context` (search + compression), `decision_context`, `project_map`, `adr_search`, `prd_search`, `plan_search`, `research_search`, `remember`, `recall`, `compress`, `doc_new` and, if `Manager.Model` exists, `draft`. `ciam.Instructions` teaches the order: **decision_context before implementing; search before reading files; record decisions**. Example: `go run ./examples/ciam/agent_with_context` — the agent discovers, from the ADR, that money is `int64` in cents, something the request did not say.

In the Open SWE engineer (chap. 19): `openswe.Config{Context: &kit}`.

## 18.6 Serving the editor (MCP)

```bash
ciam init --editor vscode     # or cursor / claude; merges with servers already configured
```

This writes `{"command": ".../ciam", "args": ["mcp"], "env": {"CIAM_PROJECT_PATH": ...}}`. In code: `kit.MCPServer(onCall).ServeStdio(ctx, os.Stdin, os.Stdout)` or `.Handler()` for HTTP (`ciam serve`). The server also publishes *prompts* (`implement-feature`, `investigate-bug`) and the instructions above. The `mcp/server` package is generic: it serves any `[]tools.Tool` of yours.

## 18.7 Drafts with a local model (optional)

`Manager.Model = ollama.New("qwen2.5-coder:1.5b")` enables `ciam_draft`: a code draft grounded in the context and in a plan phase (`PlanID`, `Phase: "Phase 1"`), marked as **speculative**. Use it to save paid tokens on boilerplate; validate it against the plan and the ADRs.

## 18.8 Cautions

- **Secrets**: the index contains the code. Keep `.env`, keys and dumps out of the indexed project (or out of the indexable extensions), and keep the index file with the same permissions as the code.
- **Scale**: search loads the project's corpus into RAM and scans it by cosine. It works well for normal-sized projects; for huge monorepos, index per subproject (`IndexOptions.Project`).
- **Team**: `FileStore` belongs to one user. For a team, use a shared store, and version `docs/` in git (that is the part that really matters to everyone).

Examples: `go run ./examples/ciam/project_context` and `.../agent_with_context`.

---

Previous: [17. Agent Protocol](17-agent-protocol.md) · Next: [19. Open SWE](19-open-swe.md) · [Manual index](README.md)
