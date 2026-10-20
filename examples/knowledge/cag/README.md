# cag

Cache-augmented generation: the whole corpus goes into one byte-stable system prefix that the provider caches between calls.

## What it does

- `cag.NewCorpus` hashes the documents and estimates tokens; `cag.Engine` places them in the system prompt.
- Question 1 reports `CacheCreationTokens`, question 2 reports `CacheReadTokens` (the `cache_hit` trace step), the way a provider with prefix caching does.
- With a deliberately small `Window`, the engine cannot fit the corpus and falls back to a keyword answerer (the `fallback` trace step).

## How to apply it

```go
corp := cag.NewCorpus(docs)
engine := &cag.Engine{Model: model, Corpus: corp, Fallback: ragOrKeywordAnswerer}
ans, _ := engine.Answer(ctx, knowledge.Question{Text: q})
```

Use CAG when the corpus is small (it must fit the context window), stable, and asked about often: no retrieval step, and cached input tokens are far cheaper. Keep the prefix byte-identical between calls (same order, same text); the engine marks it with `cache_control` for Anthropic.

## Run it

```bash
go run ./examples/knowledge/cag
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use Anthropic (`ANTHROPIC_API_KEY`) with a real corpus; ask the same two questions twice and confirm `cache_read` is non-zero on the second call (that is the saving).
2. Mind the cache lifetime: a cold prefix after the TTL pays full price. Measure cost per question over a realistic traffic pattern.
3. Rebuild the corpus (new hash) only when documents change; a changed byte invalidates the cache.
4. Set `Window`/`Reserve` for the model you use and provide a real `Fallback` (usually the RAG pipeline) for when the corpus outgrows the window.

## See also

- [Manual, chapter 20: RAG, CAG and KAG](../../../docs/en-US/manual/20-rag-cag-kag.md)
- [KNOWLEDGE.md](../../../docs/en-US/KNOWLEDGE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
