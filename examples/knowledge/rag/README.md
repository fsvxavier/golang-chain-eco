# rag

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Retrieval-augmented answers with citations, hybrid search (vector + BM25) and incremental indexing.

## What it does

- Indexes five short documents (in Portuguese, about a fictional router) with `rag.NewIndexer`.
- Compares plain vector search with hybrid search (`pipe.Hybrid = true`): the exact error code `KRX-9921` is found by BM25 even when embeddings blur it.
- Answers with `[n]` citations and prints the `Trace` steps and sources.
- Re-indexes: an unchanged corpus is skipped entirely (`Added=0 Skipped=5`), and editing one document removes and re-adds only its chunks.

## How to apply it

```go
indexer := rag.NewIndexer(store, nil)
stats, _ := indexer.Index(ctx, loader)          // incremental: Added / Skipped / Removed
pipe := &rag.Pipeline{Store: store, Model: model, K: 3, Hybrid: true, Docs: indexer.Documents()}
ans, _ := pipe.Answer(ctx, knowledge.Question{Text: q}) // ans.Text, ans.Sources, ans.Trace
```

Use RAG when the corpus is large or changes often. Turn on `Hybrid` when users search by codes, names or identifiers. The CLI does the same over files: `go run ./cmd/knowledge <index|ask|mcp|serve>`.

## Run it

```bash
go run ./examples/knowledge/rag
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real embedder and a durable vector store (`integrations/vectorstore`), and a real model; keep source metadata (`source`) on every document so citations are meaningful.
2. Index with the CLI or your own loader on a schedule; the second run must report mostly `Skipped` (that is what makes re-indexing cheap).
3. Build 20-30 questions with known answers, including codes/identifiers and unanswerable questions; check retrieval (right chunk in top K) separately from generation.
4. Keep the "not in the context" behavior: the model should say it does not know.
5. Log `Trace` and sources per answer for debugging and audit.

## See also

- [Manual, chapter 20: RAG, CAG and KAG](../../../docs/en-US/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/en-US/KNOWLEDGE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
