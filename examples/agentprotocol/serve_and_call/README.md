# serve_and_call

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Serve an agent over the Agent Protocol and drive it with the protocol client: threads, runs, streaming, time travel and the long-term store.

The agent runs through `middleware.Compaction()`: old turns are caveman-compressed, a Ponytail note is added when the conversation holds a real share of code, JSON tool results become TOON, and all of it is decided by rules, with no model needed.

## What it does

- Serves a weather agent with `agentprotocol.New` on an `httptest` server and talks to it with `agentprotocol.NewClient`.
- Covers agent discovery and schemas, a multi-turn conversation on one thread, history and time travel (`PatchThread` from an earlier checkpoint, `CopyThread`), an SSE-streamed stateless run, and the key-value store (`PutItem`, `SearchItems`, `ListNamespaces`).

## How to apply it

```go
srv, _ := agentprotocol.New(agentprotocol.Options{Agents: []agentprotocol.AgentSpec{{ID: "weather-bot", Graph: agent}}})
http.ListenAndServe(":8080", srv)
c := agentprotocol.NewClient("http://localhost:8080", apiKey)
out, _ := c.CreateAndWait(ctx, agentprotocol.RunCreate{ThreadID: id, AgentID: "weather-bot", Messages: msgs})
```

Any compliant client (the Python and JS SDKs, UIs) can then use your Go agent. Give the agent a checkpointer: threads, history and time travel rely on it.

## Run it

```bash
go run ./examples/agentprotocol/serve_and_call
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Serve on a real address behind TLS and an authenticating reverse proxy; the second argument of `NewClient` is the API key your proxy checks.
2. Replace `checkpoint.NewMemory()` and the in-memory run store with durable ones (`agentprotocol.NewStoreRunStore` on a durable `store.Store`) so threads and runs survive restarts and multiple replicas.
3. Run two server replicas and verify a thread started on one can be continued on the other.
4. Use a real model; keep tool-call ids unique per conversation.
5. Load-test streaming connections and set server timeouts.

## See also

- [Manual, chapter 17: Agent Protocol](../../../docs/en-US/manual/17-agent-protocol.md)
- [AGENT-PROTOCOL.md](../../../docs/en-US/AGENT-PROTOCOL.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
