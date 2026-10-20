# react_from_scratch

The ReAct loop wired by hand with a `StateGraph`, to show what an agent is made of.

## What it does

- Two nodes: `agent` (calls the model bound to the tools) and `tools` (`prebuilt.NewToolNode`); `prebuilt.ToolsCondition` routes to `tools` when the last message has tool calls, otherwise ends.
- Prints the graph as a Mermaid diagram, then streams the updates of one question.

## How to apply it

```go
g.AddNode("agent", callModel).
  AddNode("tools", prebuilt.NewToolNode(weather).Node()).
  AddEdge(graph.Start, "agent").
  AddConditionalEdges("agent", prebuilt.ToolsCondition).
  AddEdge("tools", "agent")
```

Build the loop yourself when you need a node that `agents.Create` does not offer (custom routing, extra state, a human step). Otherwise prefer `agents.Create`, which adds the middleware chain.

## Run it

```bash
go run ./examples/langgraph/react_from_scratch
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Run with a real model bound with `models.BindTools`.
2. Set a `RecursionLimit` in `runnables.Config`: a hand-built loop has no call limit unless you add one.
3. Add a checkpointer for conversations and error handling in the model node.
4. Use `app.Mermaid()` in your docs so the topology stays reviewable.

## See also

- [Manual, chapter 7: LangGraph: StateGraph](../../../docs/en-US/manual/07-langgraph.md)
- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
