# typed_state

A graph whose state is a Go struct with reducer tags and validation, with a human approval step.

## What it does

- `OrderState` uses `graph` tags to name channels and reducers (`sum`, `append`, `messages`) and implements `Validate()`.
- `price` computes a total, `approve` pauses with `graph.Interrupt` for a human, `confirm` finishes.
- The example resumes the thread with `Command{Resume: true}` and shows that a negative quantity is rejected by `Validate` before any node runs.

## How to apply it

```go
type OrderState struct {
    Messages []messages.Message `graph:"messages,reducer=messages"`
    Total    int                `graph:"total,reducer=sum"`
    Log      []string           `graph:"log,reducer=append"`
}
g := graph.NewTyped[OrderState]().AddNode("price", func(ctx context.Context, s OrderState) (graph.Update, error) { ... })
```

Prefer typed state for larger graphs: the compiler catches field typos that `map[string]any` hides.

## Run it

```bash
go run ./examples/langgraph/typed_state
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Use a durable checkpointer, and register custom state types with `checkpoint.Register` so they survive the JSON round trip.
2. Put business invariants in `Validate()`: it runs before every node.
3. Derive `thread_id` from the authenticated user.
4. Test resume after a process restart.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
