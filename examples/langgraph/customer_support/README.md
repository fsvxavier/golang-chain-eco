# customer_support

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

An assistant whose read-only tools run freely but whose sensitive tool needs human approval.

## What it does

- `lookup_booking` is free; `change_booking` is configured in `middleware.HumanInTheLoop` with the allowed decisions `Approve` and `Reject`.
- The model asks to look up and change a booking; the run pauses with an interrupt. The example runs two threads: one approves (booking changes) and one rejects (booking untouched).

## How to apply it

```go
middleware.HumanInTheLoop{Tools: map[string]middleware.ToolPolicy{
    "change_booking": {Allowed: []string{middleware.Approve, middleware.Reject}},
}}
// pause: out := agent.Invoke(ctx, input); req := graph.Interrupts(out)[0].Value.(middleware.HITLRequest)
// resume: agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: ...}})
```

The same `thread_id` must be used to resume. Classify tools by side effect: reads free, writes and payments gated.

## Run it

```bash
go run ./examples/langgraph/customer_support
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use a **durable** checkpointer (`sqlsaver`, Redis, ...) instead of `checkpoint.NewMemory()`: an approval can arrive hours later, after a restart.
2. Derive `thread_id` from the authenticated session, never from client input.
3. Build the approval UI from `HITLRequest` (tool name and arguments) and record who approved what and when.
4. Make `change_booking` idempotent and re-validate permissions at execution time, not only at approval.
5. Test the rejection path: the agent must tell the customer nothing changed.

## See also

- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
