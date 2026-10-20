# hierarchical_teams

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A top supervisor delegates to team subgraphs, each with its own supervisor and workers.

## What it does

- Two teams (research, writing) are compiled graphs with a supervisor that runs their workers in order.
- The top supervisor routes to a team, the team runs as a node and returns only the messages it added, then control goes back to the top supervisor until it says `FINISH`.
- No model is used; the supervisors are rules so the structure is the focus.

## How to apply it

Compose teams as subgraphs and make each supervisor an LLM call when decisions need judgment:

```go
g.AddNode(name, func(ctx context.Context, s graph.State) (any, error) {
    out, _ := sub.Invoke(ctx, graph.State{"messages": s["messages"]})
    return graph.State{"messages": ms[len(prev):]}, nil // only what the team added
})
```

Returning only the delta keeps parent state free of duplicates. For a single level see `multi_agent_supervisor`.

## Run it

```bash
go run ./examples/langgraph/hierarchical_teams
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Replace the rule-based supervisors with model calls constrained to a closed set of routes; add loop limits at both levels.
2. Use real agents (`agents.Create`) as workers, each with its own tools.
3. Add a checkpointer so long runs resume; stream with `graph.Subgraphs()` to follow inner teams.
4. Trace by team name (`graph.WithName`) and set per-team budgets.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
