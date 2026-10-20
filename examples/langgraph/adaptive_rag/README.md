# adaptive_rag

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A router decides, per question, whether to answer from the vector store or from web search.

## What it does

- A conditional edge from `Start` asks a router model for `vectorstore` or `web_search`.
- Each branch is a node that fills `docs`; both flow into `generate`.
- Runs two questions: one about LLM agents (vector store) and one about a match result (web).

## How to apply it

```go
g.AddConditionalEdges(graph.Start, func(ctx context.Context, s graph.State) (any, error) {
    out, _ := router.Generate(ctx, []messages.Message{messages.System("Route to vectorstore or web_search."), messages.Human(q)})
    return strings.TrimSpace(out.Text()), nil // the returned string is the next node
})
```

Use it when you have a private knowledge base plus an open-ended source. Keep the router's choices a closed set and validate its output; unknown routes should fall back to a default branch.

## Run it

```bash
go run ./examples/langgraph/adaptive_rag
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Replace `embeddings.Fake` and the in-memory store with a real embedder and a store from `integrations/vectorstore`; replace the `web` retriever with a real search API client (timeout, result cap).
2. Use a small, fast model for routing and a stronger one for the answer.
3. Evaluate the router on 30-50 labeled questions; track routing accuracy and the cost of wrong routes.
4. Log the chosen route and sources with each answer.
5. Treat web results as untrusted text (prompt injection) before they reach `generate`.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 6: RAG](../../../docs/en-US/manual/06-rag.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
