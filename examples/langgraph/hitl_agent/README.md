# hitl_agent

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Pause a tool call for human approval, then resume the same run.

## What it does

- `wire_money` is gated with `middleware.HumanInTheLoop`. The first `Invoke` returns with an interrupt (`graph.Interrupts(out)`).
- A `graph.Command{Resume: HITLResponse{Decisions: [Approve]}}` on the same thread continues the run and the tool executes.

## How to apply it

The smallest approval flow. Use it as the template for any irreversible action: payments, emails, deletes, deploys.

```go
ctx := runnables.WithConfig(ctx, graph.Thread("demo"))
out, _ := agent.Invoke(ctx, graph.State{"messages": "Wire $500 to Bob"})
// show graph.Interrupts(out) to a human ...
out, _ = agent.Invoke(ctx, graph.Command{Resume: middleware.HITLResponse{Decisions: []middleware.HITLDecision{{Type: middleware.Approve}}}})
```

## Run it

```bash
go run ./examples/langgraph/hitl_agent
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Replace `checkpoint.NewMemory()` with a durable saver and deliver the approval request through your real channel (UI, Slack, ticket).
2. Add a timeout/expiry policy: a request nobody answers should not execute later by surprise.
3. Allow `Edit` only where editing arguments is safe, and re-validate edited arguments.
4. Audit-log request, decision, approver and result.
5. Test resume after a process restart to prove the pause is really durable.

## See also

- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
