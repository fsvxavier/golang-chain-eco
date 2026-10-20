# graph_rag

Retrieval that follows links: the best match pulls in related pages that plain similarity search would miss.

## What it does

- Five pages with `links` metadata form a small graph (intro → channels, scheduler; channels → select; plus an unrelated CSS page).
- Plain top-2 similarity returns the intro and one neighbor; `graphretriever.Retriever` (start 1, depth 2, edge `links` → `id`) walks the links and returns the connected pages.

## How to apply it

```go
&graphretriever.Retriever{
    Store: vs, K: 6, StartK: 1, Depth: 2,
    Edges: []graphretriever.Edge{{From: "links", To: "id"}},
}
```

Use it for documentation, wikis and codebases where relevant context is linked rather than similar. Store the link targets in document metadata at indexing time. For a full knowledge graph with multi-hop reasoning, see `examples/knowledge/kag`.

## Run it

```bash
go run ./examples/integrations/graph_rag
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Index real documents with a real embedder into a durable store (`integrations/vectorstore`), extracting links from the source (hrefs, imports, wiki links).
2. Tune `StartK`, `Depth` and `K` on real questions; deeper traversal adds recall and noise.
3. Cap the total documents returned to protect the model's context budget.
4. Compare answer quality with plain similarity on 20 questions before adopting it.

## See also

- [INTEGRATIONS.md](../../../docs/en-US/INTEGRATIONS.md)
- [Manual, chapter 6: RAG](../../../docs/en-US/manual/06-rag.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
