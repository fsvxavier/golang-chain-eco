# subgraph

A compiled graph used as a node of a parent graph, with state mapped in and out.

## What it does

- The child graph cleans and counts words; the parent's `analyse` node calls `child.Invoke` with the fields it needs and copies back only `words`.
- `summarise` then formats the result.

## How to apply it

```go
AddNode("analyse", func(ctx context.Context, s graph.State) (any, error) {
    out, err := child.Invoke(ctx, graph.State{"text": s["text"]})
    return graph.State{"words": out["words"]}, err
})
```

Use subgraphs to keep schemas separate and to reuse a workflow. An `Interrupt` inside the child propagates to the parent and resumes correctly; use `graph.Subgraphs()` to stream the child's events.

## Run it

```bash
go run ./examples/langgraph/subgraph
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Give the child and parent their own state schemas and map explicitly; do not leak internal child fields.
2. With a checkpointer, subgraph checkpoints live in the namespace `node:index`; test resume across a restart.
3. Version a reusable subgraph like a library.
4. Name graphs with `graph.WithName` so traces are readable.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
