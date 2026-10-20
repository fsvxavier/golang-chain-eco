# map_reduce

Fan out with `Send`, run workers in parallel and reduce the results.

## What it does

- A conditional edge from `start` returns one `graph.Send` per subject, each carrying its own input.
- Every `worker` runs in parallel and appends to `jokes` (`graph.Append()`); `pick_best` runs once all workers are done and reduces the list.

## How to apply it

```go
AddConditionalEdges("start", func(_ context.Context, s graph.State) (any, error) {
    var sends []graph.Send
    for _, subj := range s["subjects"].([]string) {
        sends = append(sends, graph.Send{Node: "worker", Arg: graph.State{"subject": subj}})
    }
    return sends, nil
})
```

The pattern for summarizing many documents, scoring many candidates or calling a tool per item. Parallel workers finish in any order, so the reducer must not depend on arrival order (the example sorts).

## Run it

```bash
go run ./examples/langgraph/map_reduce
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Replace the worker with a real model or tool call and cap how many run at once.
2. Put timeouts and retries on workers; one slow item should not stall the batch indefinitely.
3. Make the reduce step order-independent and handle empty and partial results.
4. For large fan-outs, use a checkpointer so a crash does not redo finished work.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
