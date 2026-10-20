# agui_server

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Serve a compiled graph over the AG-UI protocol, the event stream that CopilotKit and assistant-ui consume.

## What it does

- A one-node graph emits three model tokens with `callbacks.Emit`; `agui.Handler(app, ...)` turns the run into AG-UI server-sent events (`TEXT_MESSAGE_CONTENT` and friends).
- The example posts a run to an `httptest` server and prints the event stream.

## How to apply it

```go
mux := http.NewServeMux()
mux.Handle("/agent", agui.Handler(app, func(in agui.RunInput) any { return graph.State{"messages": in.Messages} }))
http.ListenAndServe(":8080", mux)
```

Point a CopilotKit or assistant-ui frontend at `/agent`. The second argument maps the frontend's input to your graph's input; in a real node, call a streaming chat model so tokens flow to the UI as they are produced.

## Run it

```bash
go run ./examples/integrations/agui_server
```

This example does not call a model, so it needs no API key and is deterministic.

## Test it in production mode

1. Replace the fixed tokens with a streaming model call and check the frontend renders tokens incrementally.
2. Put authentication (and CORS, if the UI is on another origin) in front of the handler; derive the thread from the session, not from client-supplied ids.
3. Use `http.Server` with sensible timeouts and make sure your proxy does not buffer SSE.
4. Add a durable checkpointer for multi-turn threads and test a client reconnect.

## See also

- [INTEGRATIONS.md](../../../docs/en-US/INTEGRATIONS.md)
- [Manual, chapter 8: Persistence, HITL and streaming](../../../docs/en-US/manual/08-persistencia-hitl-streaming.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
