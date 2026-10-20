# background

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Memories formed "subconsciously" after the conversation goes quiet: a debounced extractor runs once over the full conversation.

## What it does

- `ReflectionExecutor` wraps a `StoreManager`. Each turn calls `Submit` with a 300 ms delay on the same thread; every submit cancels the pending run and reschedules.
- Three quick turns trigger **one** extraction over the whole conversation, writing three memories. Then it searches them semantically.

## How to apply it

```go
manager := langmem.NewStoreManager(model, langmem.Namespace{"memories", "{langgraph_user_id}"}, langmem.WithStore(st))
executor := &langmem.ReflectionExecutor{Manager: manager}
fut, _ := executor.Submit(ctx, langmem.Payload{Messages: convo}, langmem.SubmitOptions{After: 30 * time.Second})
defer executor.Shutdown(true, false) // flush pending work on exit
```

Use it so memory formation never slows a reply. Pick the delay from how long a conversation pauses before it is "over" (seconds to minutes).

## Run it

```bash
go run ./examples/langmem/background
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real extraction model and a durable store; pick `After` from your real conversation rhythm.
2. The timer is in-process: on shutdown call `Shutdown(true, false)` to flush, and accept that a crash loses pending extractions (it is a debounce, not a queue). For a remote executor, see `remote/server` and the gap noted in LANGMEM.md.
3. Run an extraction cost check: one extraction over the full conversation per quiet period, not per turn.
4. Review extracted memories for quality on real conversations before enabling for all users.

## See also

- [Manual, chapter 16: Long-term memory](../../../docs/en-US/manual/16-memoria-langmem.md)
- [LANGMEM.md](../../../docs/en-US/LANGMEM.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
