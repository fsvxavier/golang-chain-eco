# mcp_docs_agent

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

A docs-search agent whose tools come from an MCP server.

## What it does

- Starts a tiny in-process MCP server that exposes `search_docs`, connects with `mcp.Connect` over an in-memory pipe, and loads its tools with `client.Tools`.
- The tool is named `docs_search_docs` (server prefix + tool). The agent calls it and answers from the result.

## How to apply it

Any MCP server becomes agent tools:

```go
client, _ := mcp.Connect(ctx, mcp.HTTP("https://docs.example.com/mcp")) // or mcp.Stdio(...)
defer client.Close()
ts, _ := client.Tools(ctx, "docs")
agent, _ := agents.Create(agents.Options{Model: model, Tools: ts})
```

Replace `mcp.NewPipeTransport` with `mcp.HTTP(url)` for a remote server or `mcp.Stdio(...)` for a local process.

## Run it

```bash
go run ./examples/deepagents/mcp_docs_agent
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Swap the pipe transport for the real server's transport and a real model.
2. Authenticate to the MCP server and use a read-only credential; list the tools it exposes and drop the ones the agent should not have before passing them to `Tools`.
3. Treat tool output as untrusted text (it can contain instructions).
4. Set timeouts on `Connect` and on tool calls, and call `client.Close()` on shutdown.
5. To expose your own tools the other way, see `mcp/server` and `examples/knowledge/mcp_server`.

## See also

- [Manual, chapter 11: MCP, server and remote graphs](../../../docs/en-US/manual/11-mcp-remote-servidor.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
