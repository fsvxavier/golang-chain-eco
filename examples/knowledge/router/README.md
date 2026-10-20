# router

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

One router in front of RAG, CAG and KAG that picks the right technique per question, with forced routes and fallback.

## What it does

- The same three chunks feed a vector store (RAG), a cached corpus (CAG) and an extracted graph (KAG).
- Five scenarios: a multi-hop question goes to the graph; a simple question over a small stable corpus goes to CAG; the same question on a volatile corpus goes to RAG; `Force: "kag"` overrides the choice; and when KAG fails, `Fallback: true` makes RAG answer. The `route` and `fallback` trace steps show why.

## How to apply it

```go
r := &knowledge.Router{
    RAG: ragPipeline, CAG: cagEngine, KAG: kagEngine,
    Profile:  knowledge.Profile{HasGraph: true, CorpusTokens: n, ContextWindow: 200000, Volatile: false},
    Fallback: true,
}
ans, _ := r.Answer(ctx, knowledge.Question{Text: q})
```

Describe your data honestly in `Profile` (size, window, volatility, graph availability): that is what the routing rules read. Expose the router to agents as tools or MCP with `knowledge.Toolkit`.

## Run it

```bash
go run ./examples/knowledge/router
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use real models, a durable vector store and graph store, and set `Profile` from measurements (corpus tokens, how often documents change).
2. Build a labeled set of questions per technique and check the route chosen matches; review misroutes.
3. Keep `Fallback` on and test it by breaking the graph store (the answer should still arrive via RAG).
4. Log the `route` trace per request and watch the distribution; a surprising shift means your data profile changed.

## See also

- [Manual, chapter 20: RAG, CAG and KAG](../../../docs/en-US/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/en-US/KNOWLEDGE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
