# corrective_rag

Retrieve, grade each document, fall back to web search when nothing is relevant, then answer.

## What it does

- `retrieve` fetches the top documents; `grade` asks a grader model "relevant? yes/no" per document and keeps only the yes.
- If none survive, `web_search` supplies a replacement; otherwise `generate` answers from the kept documents.
- Two questions show both paths.

## How to apply it

```go
AddConditionalEdges("grade", func(_ context.Context, s graph.State) (any, error) {
    if s["web"] == true { return "web_search", nil }
    return "generate", nil
})
```

Use it when retrieval quality varies and a confidently wrong answer costs more than an extra model call per document. Grade in parallel if latency matters.

## Run it

```bash
go run ./examples/langgraph/corrective_rag
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real embedder and vector store, real grader and answer models, and a real search tool for the fallback.
2. Measure the grader: precision and recall on documents you labeled by hand; a grader that rejects everything sends every query to the web.
3. Cap the number of documents graded per query to bound cost and latency.
4. Record how often the fallback fires; a high rate means retrieval or chunking needs work, not a better grader.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 6: RAG](../../../docs/en-US/manual/06-rag.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
