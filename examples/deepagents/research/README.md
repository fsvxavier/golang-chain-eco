# research

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A lead agent plans, delegates two research questions to subagents in parallel and writes a report file.

## What it does

- The lead calls `write_todos`, then two `task` calls to the `researcher` subagent at once, then `write_file` for `/report.md`.
- The subagent has its own model and the `internet_search` tool (a stub here).
- Prints every file in the virtual filesystem, the todo list and the final message.

## How to apply it

```go
deepagents.Create(deepagents.Options{
    Model: lead, Tools: []tools.Tool{search},
    SubAgents: []deepagents.SubAgentSpec{{Name: "researcher", Description: "Focused researcher.", Model: sub}},
})
```

Give each subagent a narrow description (the lead uses it to decide who gets the work), its own model if cheaper, and only the tools it needs. Parallel `task` calls in one turn run concurrently.

## Run it

```bash
go run ./examples/deepagents/research
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Replace the stub `internet_search` with a real search tool (HTTP client with timeout, result cap and URL allow-list) and use real models for lead and subagent.
2. Use a stronger model for the lead and a cheaper one for researchers; compare cost per report.
3. Bound the run: `ModelCallLimit`, `ToolCallLimit` and a `RecursionLimit`, because a research agent can loop on searches.
4. Treat fetched web text as untrusted input (prompt injection): keep the report-writing step free of tools that can act on the outside world.
5. Persist reports with `backends.Filesystem` or an object store rather than reading them from state.

## See also

- [Manual, chapter 10: Deep Agents](../../../docs/en-US/manual/10-deep-agents.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
