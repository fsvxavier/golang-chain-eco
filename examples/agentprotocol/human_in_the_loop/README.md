# human_in_the_loop

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

The Agent Streaming Protocol: subscribe to a thread, watch tokens and tool activity, answer an approval request and inspect checkpoints.

The agent runs through `middleware.Compaction()`: old turns are caveman-compressed, a Ponytail note is added when the conversation holds a real share of code, JSON tool results become TOON, and all of it is decided by rules, with no model needed.

## What it does

- The agent must ask before calling `pay` (`HumanInTheLoop`). A client subscribes to the `lifecycle`, `messages`, `tools` and `input` channels, starts a run with `run.start`, and prints the ordered events (`seq`).
- When the server emits `input.requested`, the client answers with `input.respond` (`approve`), the run completes, and the example reads `state.get`, `state.listCheckpoints` and `agent.getTree`.

## How to apply it

```go
for ev, err := range c.Events(ctx, thread, agentprotocol.Filter{Channels: []string{"lifecycle", "messages", "tools", "input"}}) { ... }
c.Command(ctx, thread, "run.start", map[string]any{"assistantId": "payer", "input": ...})
c.Command(ctx, thread, "input.respond", map[string]any{"interruptId": id, "response": []map[string]any{{"type": "approve"}}})
```

Build approval UIs on this: show `input.requested` payloads, send back the decision. Events carry a `seq`, so a client that reconnects can replay what it missed.

## Run it

```bash
go run ./examples/agentprotocol/human_in_the_loop
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Put the server behind auth and TLS; the approval endpoint is a privileged action, so authorize who may answer which thread.
2. Use a durable checkpointer; the interrupt must survive a restart.
3. Reconnect a client mid-run and confirm it replays from its last `seq` without duplicates.
4. Add an expiry to pending approvals and test the timeout path.
5. Use a real model and a real `pay` implementation that is idempotent.

## See also

- [Manual, chapter 17: Agent Protocol](../../../docs/en-US/manual/17-agent-protocol.md)
- [AGENT-PROTOCOL.md](../../../docs/en-US/AGENT-PROTOCOL.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
