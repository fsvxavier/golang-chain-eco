# kag

Multi-hop questions answered by walking a knowledge graph instead of retrieving the single best chunk.

## What it does

- Three chunks are turned into a graph by `kag.Extractor` (entities and typed relations; the fake returns structured extractions).
- The question "who prescribed a drug that interacts with the aspirin prescribed to João?" needs three hops across chunks. Vector search with `K=1` returns one chunk and cannot answer it.
- `kag.Engine{Verify: true}` plans steps, walks the relations, verifies the answer against the used triples and returns them as sources. A last case shows a cyclic graph terminating (visited edges are not repeated).

## How to apply it

```go
g := kag.NewMemory()                                   // or integrations/graphstore for Neo4j/Memgraph
(&kag.Extractor{Model: m, Embedder: emb, Graph: g}).Extract(ctx, chunks)
ans, _ := (&kag.Engine{Model: m, Graph: g, Verify: true, MaxHops: 4}).Answer(ctx, knowledge.Question{Text: q})
```

Use KAG for questions about relationships ("who ... related to ...", chains of dependencies). Relation types and labels are validated (`^[A-Z][A-Z0-9_]*$`) before they reach a graph store; never concatenate model text into a query.

## Run it

```bash
go run ./examples/knowledge/kag
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real model with tool/function calling (extraction, planning and verification are structured calls) and a real embedder.
2. Swap `kag.NewMemory()` for a graph store from `integrations/graphstore` (Neo4j or Memgraph); the conformance suite in `internal/conformance` shows what each store must satisfy.
3. Audit extraction quality on a sample (entities merged correctly, relation types sensible) before trusting answers; check `Dropped` counts.
4. Bound traversal (`MaxHops`) and check that unanswerable questions return "I don't know".
5. Keep `Verify` on for anything user-facing.

## See also

- [Manual, chapter 20: RAG, CAG and KAG](../../../docs/en-US/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/en-US/KNOWLEDGE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
