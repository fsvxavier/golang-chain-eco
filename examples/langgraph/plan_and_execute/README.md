# plan_and_execute

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A planner writes the steps, an executor runs them one by one and a replanner decides to continue or answer.

## What it does

- `plan` turns the question into a numbered list; `execute` takes the first step, runs it and appends `(step, result)` to `past_steps`; `replan` answers when no steps are left.
- The scripted run answers "hometown of the 2024 Australian Open winner" in two steps.

## How to apply it

```go
AddEdge("execute", "replan").
AddConditionalEdges("replan", func(_ context.Context, s graph.State) (any, error) {
    if s["response"] != nil { return graph.End, nil }
    return "execute", nil
})
```

Use it for multi-step questions where a plan helps more than a free-form loop. In production the replanner should also revise the remaining plan from new evidence, not only stop when the list is empty.

## Run it

```bash
go run ./examples/langgraph/plan_and_execute
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Use real models and give the executor real tools (search, calculators); today it only calls a model.
2. Cap the number of steps and validate the plan format before executing.
3. Persist the plan and `past_steps` with a checkpointer so long tasks can resume.
4. Evaluate plan quality separately from answer quality.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
