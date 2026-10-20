# react_agent_middleware

A tool-calling agent built with `agents.Create`, a middleware and streamed updates.

## What it does

- One tool (`get_weather`) and `middleware.ModelCallLimit{RunLimit: 5}`.
- `StreamEvents` with `graph.ModeUpdates` prints each node's update as it happens (model call, tool call, final answer).

## How to apply it

```go
agent, _ := agents.Create(agents.Options{
    Model: model, Tools: []tools.Tool{weather}, SystemPrompt: "...",
    Middleware: []agents.Middleware{middleware.ModelCallLimit{RunLimit: 5}},
})
for ev, err := range agent.StreamEvents(ctx, graph.State{"messages": q}, graph.ModeUpdates) { ... }
```

This is the default starting point for an agent. Stream to the UI so users see progress; the tool schema comes from the Go args struct (`json` and `description` tags).

## Run it

```bash
go run ./examples/langgraph/react_agent_middleware
```

The example picks its model through `examples/internal/demo`: with `ANTHROPIC_API_KEY` it uses `anthropic:claude-sonnet-4-5`, with `OPENAI_API_KEY` it uses `openai:gpt-4o-mini`, and without a key it falls back to a scripted fake so it runs offline.

## Test it in production mode

1. Run with a real key and a real weather API behind the tool (timeout, error mapping).
2. Add `ToolRetry`, `ModelRetry` and `ModelFallback`; see `langchain/middleware_stack`.
3. Add a checkpointer and a `thread_id` if the conversation continues across requests.
4. Check the tool schema the model receives; vague `description` tags cause wrong arguments.

## See also

- [Manual, chapter 9: Agents and middleware](../../../docs/en-US/manual/09-agents-e-middleware.md)
- [Manual, chapter 5: Tools](../../../docs/en-US/manual/05-tools.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
