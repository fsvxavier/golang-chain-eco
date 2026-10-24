# async_subagents

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A deep agent launches a long task in the background and checks on it later.

The agent runs through `middleware.Compaction()`: old turns are caveman-compressed, a Ponytail note is added when the conversation holds a real share of code, JSON tool results become TOON, and all of it is decided by rules, with no model needed.

## What it does

- `AsyncSubagents` middleware gives the main agent `start_async_task` (and check/cancel tools).
- The `researcher` runs in the background through an in-process `LocalRunner` while the main agent keeps answering.
- The example polls the handle until the task finishes and prints the report.

## How to apply it

```go
runner := &dmw.LocalRunner{Agent: researcher}
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{{Name: "researcher", Description: "long-running research", Runner: runner}}}
```

The `AsyncRunner` interface (`Start`, `Poll`, `Send`, `Cancel`) is what you implement or swap: `LocalRunner` stays in-process, `RemoteRunner` talks to an Agent Server (see `async_subagent_server`).

## Run it

```bash
go run ./examples/deepagents/async_subagents
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Use real models; real research jobs take minutes, so poll with backoff rather than a tight loop.
2. Bound concurrent background tasks and give every task a deadline; cancel with `Cancel` when the user leaves.
3. Persist task handles (the `async_tasks` state) with a durable checkpointer so a restart does not orphan running work.
4. Move heavy workers to separate processes with `RemoteRunner` when they need isolation or scale.

## See also

- [Manual, chapter 10: Deep Agents](../../../docs/en-US/manual/10-deep-agents.md)
- [Manual, chapter 11: MCP, server and remote graphs](../../../docs/en-US/manual/11-mcp-remote-servidor.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
