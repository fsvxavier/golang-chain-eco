# self_rag

Retrieve, generate, then grade the answer for grounding and regenerate when it is not supported by the documents.

## What it does

- `retrieve` and `generate` run once; a conditional edge asks a judge model whether the answer is grounded in the documents.
- The scripted first answer is a hallucination ("faster than light"), the judge says no, `generate` runs again (`graph.Sum()` caps tries at 3) and the grounded answer passes.

## How to apply it

```go
AddConditionalEdges("generate", func(ctx context.Context, s graph.State) (any, error) {
    if !grounded(ctx, s) && s["tries"].(int) < 3 { return "generate", nil }
    return graph.End, nil
})
```

Add this check when wrong answers are costly. A fuller version also grades document relevance (see `corrective_rag`) and rewrites the query when retrieval itself failed.

## Run it

```bash
go run ./examples/langgraph/self_rag
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real embedder, vector store and models; use a different model as the judge when possible.
2. Evaluate the judge on answers you labeled as grounded or not; track false positives (hallucinations let through).
3. Decide what happens after the last try: answer with a caveat, return "I don't know", or escalate.
4. Log grounding results; a falling grounded rate signals retrieval drift.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 6: RAG](../../../docs/en-US/manual/06-rag.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
