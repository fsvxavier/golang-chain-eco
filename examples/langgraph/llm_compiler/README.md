# llm_compiler

A planner emits a DAG of tool calls; every call whose dependencies are done runs in parallel with `Send`, then a joiner builds the answer.

## What it does

- The plan has three tasks: population of France, population of Germany (independent) and their sum (depends on both).
- `schedule` finds the tasks whose dependencies are complete and fans them out with `graph.Send`; results merge into `done` through a custom `BinOp` reducer.
- The two independent calls run in parallel (the example prints elapsed time), the sum runs after them, `join` formats the answer.

## How to apply it

Use it when a model can plan several independent tool calls up front: latency becomes the depth of the DAG, not the number of calls.

```go
sends = append(sends, graph.Send{Node: "exec", Arg: graph.State{"task": t, "deps": done}})
return sends, nil // a conditional edge returning []Send fans out
```

Here the planner is hard-coded; in a real system a model returns the plan as JSON that you validate (unique ids, deps exist, no cycles) before scheduling.

## Run it

```bash
go run ./examples/langgraph/llm_compiler
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Let a real model produce the plan and validate it strictly; reject cycles and unknown tools.
2. Replace the sleep with real tool calls, each with its own timeout, retry and result-size cap.
3. Bound parallelism (a semaphore) so a large plan cannot flood a downstream API.
4. Decide what a failed task means for its dependents (skip, replan or abort) and test it.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
