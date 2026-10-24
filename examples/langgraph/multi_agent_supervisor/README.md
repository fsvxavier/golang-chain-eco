# multi_agent_supervisor

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A supervisor routes work to specialist agents until it decides the task is finished. The agent runs through `middleware.Compaction()`: old turns are caveman-compressed, a Ponytail note is added when the conversation has a real share of code, and JSON tool results are re-encoded as TOON, all decided by rules with no model needed.

## What it does

- Two specialists (`researcher`, `writer`) are compiled agents (`agents.Create`) called inside graph nodes.
- The supervisor model answers with the next role or `FINISH`; a conditional edge follows its choice and each worker loops back to the supervisor.
- Contributions are appended to a shared message list so each agent sees what the others did.

## How to apply it

Each specialist is a full agent with its own prompt and tools, wrapped in a node:

```go
g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
    out, err := w.Invoke(ctx, graph.State{"messages": s["messages"]})
    // append the specialist's last message to the shared list
})
```

Constrain the supervisor to a closed set of next steps and treat anything else as an error. For deeper structures see `hierarchical_teams`; for a full pipeline with parallelism and review gates see `engineering_team`.

## Run it

```bash
go run ./examples/langgraph/multi_agent_supervisor
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use real models; give each specialist only the tools its role needs.
2. Add a step limit to the supervisor loop and a `RecursionLimit`; a supervisor that never says `FINISH` is the classic failure.
3. Log the routing decisions; evaluate on tasks where you know which specialist should act.
4. Use a durable checkpointer if a run can take long or needs human review between steps.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
