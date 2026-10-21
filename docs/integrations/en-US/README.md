# Integration manuals

**English** | [Português](../pt-BR/README.md) | [Español](../es-ES/README.md)

How to connect this project to the tools you already use: coding assistants in the editor, terminal consoles, and the LLM gateways that sit in front of the models.

| Manual | What it covers |
|---|---|
| [gateway-router.md](gateway-router.md) | Using `router:<model>` to spread calls over freellmapi, omniroute, bifrost, Anthropic and OpenAI from one `.env`, including per-role models for the engineering-team examples. |
| [clients-mcp.md](clients-mcp.md) | Connecting `ciam mcp` and `knowledge mcp` (MCP over stdio) to VS Code with Copilot Chat, Claude Code, Kilo Code, Copilot CLI, Opencode and Cursor, and pointing each client at a gateway. |

Reference docs these manuals build on (not repeated here): the [Router section of PROVIDERS.md](../../en-US/PROVIDERS.md#router), [manual chapter 3.6.1](../../en-US/manual/03-modelos-e-provedores.md), [CIAM.md](../../en-US/CIAM.md) and [KNOWLEDGE.md](../../en-US/KNOWLEDGE.md).

## What works today and what is planned

| Capability | Status |
|---|---|
| Go code (and the examples) calling several gateways and providers through `router:<model>`, with failover, round robin, random and weighted modes | Works today |
| One `.env` driving the router for the whole process; one model per role in `independent_engineering_team` | Works today |
| `ciam mcp` and `knowledge mcp` as MCP servers over stdio for any MCP client | Works today (checked by running the servers; see the client notes in [clients-mcp.md](clients-mcp.md) for what was and was not confirmed per client) |
| `ciam init --editor vscode\|cursor\|claude` writes the MCP entry for VS Code, Cursor and Claude Code | Works today |
| Pointing an editor or console client at a gateway (freellmapi, omniroute, bifrost) | Done by each client's own settings, not by this project; see [clients-mcp.md](clients-mcp.md) |
| The router as an HTTP server that editors could use as their endpoint | Not available: the router is a Go library (`providers/router`), not a proxy process. Clients talk to a gateway, and Go code talks to the router |
| Which target answered a call, as a log line or a response field | Not available: the router reports it only when every target failed (see [gateway-router.md](gateway-router.md#which-target-answered)) |
| Controlling the engineering team from a chat client over MCP | Planned; no MCP bridge exists yet (see the last section of [clients-mcp.md](clients-mcp.md)) |

## Facts that come from other products

Client config file names, gateway ports and environment names belong to those products and change between releases. Every such fact in these manuals is followed by the URL it was read from (read on 2026-10-21). Where a fact could not be confirmed the manual says "check <product> docs" instead of guessing.
