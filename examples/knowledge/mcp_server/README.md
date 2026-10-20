# mcp_server

**English** | [Português](README.pt-BR.md) | [Español](README.es-ES.md)

Serve `knowledge.Toolkit` over MCP so editors and desktop assistants can query your knowledge base.

## What it does

- Builds the same small RAG pipeline and exposes it with `kb.MCPServer(nil)`.
- With `-demo` it feeds one `tools/list` and one `tools/call` request through the server in-process and prints the JSON-RPC replies (an offline check). Without it, the server speaks MCP on stdio.

## How to apply it

```bash
go run ./examples/knowledge/mcp_server            # MCP over stdio
go run ./cmd/knowledge mcp                       # the CLI version, over your own index
```

Register the command in VS Code, Cursor or Claude Desktop as a stdio MCP server. The toolkit includes an `Ingest` hook so the assistant can add documents too; leave it unset for a read-only server.

## Run it

```bash
go run ./examples/knowledge/mcp_server
```

The model here is scripted (`models.NewFake`) and ignores API keys, so the run is deterministic and offline. To try a real model, build one with `chatmodels.Init("anthropic:claude-sonnet-4-5")` (or `"openai:gpt-4o-mini"`) and pass it as `Model`; a real model decides its own tool calls, so the transcript will differ from the scripted one.

## Test it in production mode

1. Run `go run ./examples/knowledge/mcp_server -demo` and check the `tools/list` and `tools/call` replies.
2. Register the server in your editor's MCP configuration (command `go`, args `run ./cmd/knowledge mcp`, or a built binary) and ask a question that needs your documents.
3. Use a real embedder and a persisted index; expose only the tools the client needs, and prefer read-only.
4. Remember MCP tool results are untrusted text for the client's model; do not put secrets in indexed documents.

## See also

- [Manual, chapter 11: MCP, server and remote graphs](../../../docs/en-US/manual/11-mcp-remote-servidor.md)
- [KNOWLEDGE.md](../../../docs/en-US/KNOWLEDGE.md)
- [Production checklist](../../../docs/en-US/manual/14-producao-seguranca-troubleshooting.md)
