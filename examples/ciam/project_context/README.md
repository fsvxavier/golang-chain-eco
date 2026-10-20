# project_context

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Cheap, precise project context for an AI assistant: incremental index, hybrid search, compression, decisions (ADR/PRD/Plan), memory and an MCP server.

## What it does

- Indexes a small Django-style project plus an ADR, a PRD and a plan, incrementally (second run re-embeds nothing; editing one file re-embeds one).
- Hybrid search (vector + BM25, ranked with RRF) filtered by chunk type, compression that keeps signatures and drops docs, `DecisionContext` (code plus the ADRs/PRDs behind it), `Remember`/`Recall`, ADR supersession, and the same capabilities served as an MCP server.
- Prints estimated tokens saved compared with reading the files.

## How to apply it

```go
m := ciam.New(embedder, nil)
m.Index(ctx, root, ciam.IndexOptions{})
res, _ := m.Search(ctx, ciam.SearchRequest{Project: "shop", Query: "send receipt", Types: []string{"task"}, Limit: 3})
dc, _ := m.DecisionContext(ctx, "shop", "invoice total cents stored", 3)
srv := ciam.Toolkit{M: m, Project: root}.MCPServer(nil) // for VS Code / Cursor / Claude
```

Or use the CLI: `go run ./cmd/ciam index|search|decision|mcp`. Write the WHY down as ADRs so the next assistant (or person) finds it.

## Run it

```bash
go run ./examples/ciam/project_context
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Replace `embeddings.Fake{}` with a real embedder (the comment points to `providers/ollama.Embeddings`) and re-index; check search quality on 20 real questions about your codebase.
2. Index your real repository and keep the index outside the repo; re-run `Index` in CI or a git hook (it is incremental).
3. Register the MCP server with your editor (`go run ./cmd/ciam mcp`) and confirm the assistant calls `ciam_*` tools instead of reading whole files.
4. Review ADR/PRD documents like code; ADRs are superseded, never edited.
5. Watch `Status` (searches, cache hits, tokens saved) to confirm it pays for itself.

## See also

- [Manual, chapter 18: ciam](../../../docs/en-US/manual/18-ciam-contexto.md)
- [CIAM.md](../../../docs/en-US/CIAM.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
