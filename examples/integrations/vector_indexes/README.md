# vector_indexes

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

The two in-process vector indexes, exact (flat) and approximate (Annoy-style forest), with recall measured against the exact answer.

## What it does

- Builds 5,000 random 32-d vectors in `vectorstore.NewFlat()` and `vectorstore.Annoy{Trees: 20}`.
- Measures recall@10 of the forest against the exact index at the default `SearchK` and at 1500 (more candidates, better recall, slower queries), then saves and reloads the flat index and checks the same nearest neighbor comes back.

## How to apply it

```go
flat := vectorstore.NewFlat()          // exact, simplest
ann  := &vectorstore.Annoy{Trees: 20}  // approximate, faster on large sets
flat.Upsert(ctx, records); hits, _ := flat.Query(ctx, vec, 10)
flat.Save(path); flat, _ = vectorstore.LoadFlat(path)
```

Start with `Flat`; move to the forest when exact search becomes too slow. Always measure recall on **your** vectors: random Gaussian vectors, as here, are a hard case.

## Run it

```bash
go run ./examples/integrations/vector_indexes
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Use your real embeddings and a representative query sample, and pick `Trees` and `SearchK` for the recall you need (for example 95%) at an acceptable latency.
2. Rebuild or reload the index on startup from a persisted file; keep it consistent with the source of truth.
3. For very large or shared corpora, use a networked store from `integrations/vectorstore` instead of an in-process index.
4. Re-measure recall after changing the embedding model.

## See also

- [INTEGRATIONS.md](../../../docs/en-US/INTEGRATIONS.md)
- [Manual, chapter 6: RAG](../../../docs/en-US/manual/06-rag.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
