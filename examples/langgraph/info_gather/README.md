# info_gather

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A chatbot that collects requirements over several turns, then switches to generating the final prompt.

## What it does

- `gather` asks questions until the collector replies `READY: ...`, which fills `spec`; then a conditional edge goes to `write_prompt`.
- Each user message is a separate `Invoke` on the same `thread_id`; a checkpointer keeps the conversation between turns.

## How to apply it

Use it for any guided intake: forms, onboarding, requirement capture.

```go
AddConditionalEdges("gather", func(_ context.Context, s graph.State) (any, error) {
    if s["spec"] != nil { return "write_prompt", nil }
    return graph.End, nil // wait for the next user message
})
```

Ending the graph on "need more input" and resuming with the next message on the same thread is the pattern; no long-lived process is needed.

## Run it

```bash
go run ./examples/langgraph/info_gather
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a real model and a durable checkpointer so the conversation survives restarts; derive `thread_id` from the session.
2. Make the "ready" signal structured (JSON or a tool call) rather than a text prefix.
3. Validate the collected spec before generating, and cap the number of turns.
4. Expire abandoned threads with `saver.DeleteThread`.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
