# wait_user_input

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Pause a graph to ask the user something, then resume with the answer.

## What it does

- `ask_location` calls `graph.Interrupt(ctx, "Where are you?")`; the first `Invoke` returns with the interrupt and `GetState` shows the next node.
- `Invoke(ctx, graph.Command{Resume: "Lisbon"})` on the same thread continues and `report` uses the answer.

## How to apply it

```go
loc := graph.Interrupt(ctx, "Where are you?") // returns the Resume value after the pause
app.Invoke(ctx, graph.Command{Resume: "Lisbon"})
```

Nothing stays running while it waits: the state is in the checkpointer. A web handler can return the question to the client and resume when the answer arrives.

## Run it

```bash
go run ./examples/langgraph/wait_user_input
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Use a durable checkpointer; with `NewMemory` a restart loses the pending question.
2. Expire stale threads (`DeleteThread`) and authenticate the resume request against the thread's owner.
3. Validate the answer before resuming.
4. Test double-resume (the same answer sent twice) and resume of an unknown thread.

## See also

- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
