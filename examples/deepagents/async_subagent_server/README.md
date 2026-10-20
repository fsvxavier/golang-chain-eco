# async_subagent_server

A worker agent served over the Agent Server HTTP API and driven by a supervisor through a `RemoteRunner`.

## What it does

- Serves a `researcher` graph with `remote/server` on an `httptest` server (150 ms of simulated work).
- The supervisor starts an async task via `dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "researcher"}`; the client only uses HTTP.
- The example polls the run until it succeeds and prints the findings.

## How to apply it

Split the worker into its own service, keep the supervisor thin:

```go
// worker process
http.ListenAndServe(":8123", server.New(map[string]*graph.Compiled{"researcher": worker}))
// supervisor process
runner := dmw.RemoteRunner{Client: remote.NewClient("http://worker:8123"), AssistantID: "researcher"}
```

The same server speaks the LangGraph Platform subset, so a Python or JS client can call it too.

## Run it

```bash
go run ./examples/deepagents/async_subagent_server
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Run worker and supervisor as two processes (or containers) and replace the `httptest` URL with the worker's address.
2. Put authentication and TLS in front of the worker (reverse proxy or middleware); `remote/server` does not authenticate by itself.
3. Give the worker a durable checkpointer (it uses `checkpoint.NewMemory()` here) so runs and threads survive restarts.
4. Set client timeouts, handle `remote.APIError`, and add a health check for the worker.
5. Load-test with several concurrent tasks and watch worker memory.

## See also

- [Manual, chapter 11: MCP, server and remote graphs](../../../docs/en-US/manual/11-mcp-remote-servidor.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
