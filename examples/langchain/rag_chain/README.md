# rag_chain

A classic retrieval chain built from runnables: split, embed, retrieve, prompt and answer.

## What it does

- Splits a small corpus with `textsplitters.NewRecursive`, embeds the chunks into an in-memory vector store and builds a retriever with `K: 2`.
- `runnables.Assign` adds a `context` field (the retrieved passages) to the `{"question": ...}` map, then prompt, model and parser finish the chain.
- Asks "What are goroutines?" and prints an answer grounded in the retrieved text.

## How to apply it

Use it when the knowledge fits a plain retriever and you want the whole flow as one composable `Runnable`.

```go
store, _ := vectorstores.FromDocuments(ctx, embedder, docs)
retr := retrievers.FromVectorStore(store, retrievers.VectorStoreOptions{K: 4})
withCtx := runnables.Assign(map[string]runnables.Runnable[map[string]any, any]{"context": retrieve})
```

For incremental indexing, hybrid search or a router between RAG, CAG and KAG, use the `rag` and `knowledge` packages instead (chapter 20).

## Run it

```bash
go run ./examples/langchain/rag_chain
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Replace `embeddings.Fake` with a real embedder (for example `providers/ollama.Embeddings` or an OpenAI-compatible one) and `FromDocuments` with a durable vector store from `integrations/vectorstore`.
2. Index your real documents once, outside the request path, and keep the chunk size and overlap under version control.
3. Measure retrieval before generation: ask 20 questions with known answers and check the right chunk is in the top `K`.
4. Keep the "answer only from this context" instruction and test a question the corpus cannot answer: the model should say so.
5. Log the retrieved chunks with each answer so a wrong answer can be traced to retrieval or to generation.

## See also

- [Manual, chapter 6: RAG](../../../docs/en-US/manual/06-rag.md)
- [Manual, chapter 20: RAG, CAG and KAG](../../../docs/en-US/manual/20-rag-cag-kag.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
