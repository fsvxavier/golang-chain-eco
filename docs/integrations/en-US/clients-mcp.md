# Connecting editors and consoles

**English** | [Português](../pt-BR/clients-mcp.md) | [Español](../es-ES/clients-mcp.md)

This project ships two MCP servers over stdio. Any MCP client can launch them as a subprocess:

| Server | Command | Needs |
|---|---|---|
| `ciam` (project context: search, ADR/PRD/plans, memory; tools named `ciam_*`) | `ciam mcp` | nothing; run `ciam index` once per project for useful answers. See [CIAM.md](../../en-US/CIAM.md). |
| `knowledge` (RAG, CAG and KAG answers over your documents) | `knowledge mcp --dir .knowledge` | an index made with `knowledge index <path> --out .knowledge`. Without it the server exits with `no index in ...`. See [KNOWLEDGE.md](../../en-US/KNOWLEDGE.md). |

Both exit cleanly when stdin closes (checked: `ciam mcp < /dev/null` and `knowledge mcp --dir <indexed dir> < /dev/null` return 0; a `tools/list` request over stdin returns the tool list).

## 1. Build the binaries

Editors launch the command themselves, with their own working directory. `go run` compiles on every start (slow, and `ciam init` would record a temporary path), so build once and use the absolute path:

```bash
go build -o ~/bin/ciam ./cmd/ciam
go build -o ~/bin/knowledge ./cmd/knowledge

cd /path/to/your/project
ciam index .
knowledge index docs --glob "*.md" --out .knowledge
```

Environment the servers read: `CIAM_PROJECT_PATH`, `CIAM_DB_PATH` (default `~/.local/share/ciam/ciam.json`), `CIAM_EMBEDDER=fake` (offline), `CIAM_OLLAMA_URL`, `KNOWLEDGE_MODEL` (`provider:model`, accepts `router:<model>`, see [gateway-router.md](gateway-router.md)) and `KNOWLEDGE_EMBEDDINGS`. Without an API key or `KNOWLEDGE_MODEL`, `knowledge` answers with an offline fake that echoes the top source and prints a warning on stderr.

## 2. The `ciam init` helper

`ciam init [--editor vscode|cursor|claude] [--print]` writes the `ciam` entry for you, merging with the servers already in the file (invalid JSON in the file is an error, not overwritten). Without `--editor` it picks Cursor if `.cursor/` exists, else VS Code.

| `--editor` | File written (project root) | Top-level key |
|---|---|---|
| `vscode` | `.vscode/mcp.json` | `servers`, entry has `"type": "stdio"` and `CIAM_PROJECT_PATH=${workspaceFolder}` |
| `cursor` | `.cursor/mcp.json` | `mcpServers` |
| `claude` | `.mcp.json` | `mcpServers` |

```bash
cd /path/to/your/project
ciam init --editor vscode --print   # preview
ciam init --editor claude           # write .mcp.json
```

It writes the path of the running executable as `command`, which is why you build first (a `go run` binary lives in a temporary directory). It only registers `ciam`; add `knowledge` by hand with the entries below. The Cursor entry it writes has no `"type"`, while Cursor's docs (https://cursor.com/docs/mcp) list `type` as required for stdio servers: if Cursor does not load the entry, add `"type": "stdio"`.

## 3. Clients

Config formats below are from each product's documentation, read on 2026-10-21. They change between versions; where the page offered two formats, both are noted.

### VS Code with GitHub Copilot Chat

Source: https://code.visualstudio.com/docs/agents/reference/mcp-configuration and https://docs.github.com/en/copilot/customizing-copilot/extending-copilot-chat-with-mcp

File `.vscode/mcp.json` (workspace) or the user file opened with the command **MCP: Open User Configuration**. Top-level key `servers`; stdio entries use `type`, `command`, `args`, `env`; `${workspaceFolder}` is available.

```json
{
  "servers": {
    "ciam": {
      "type": "stdio",
      "command": "/home/you/bin/ciam",
      "args": ["mcp"],
      "env": { "CIAM_PROJECT_PATH": "${workspaceFolder}" }
    },
    "knowledge": {
      "type": "stdio",
      "command": "/home/you/bin/knowledge",
      "args": ["mcp", "--dir", "${workspaceFolder}/.knowledge"]
    }
  }
}
```

GitHub's page (https://docs.github.com/en/copilot/customizing-copilot/extending-copilot-chat-with-mcp) says a "Start" button appears at the top of the server list in `.vscode/mcp.json`, and its steps tell you to select Agent in the Copilot Chat box before using the tools. Organization policy can restrict MCP and bring-your-own-model use: check the Copilot docs for your plan.

### Claude Code (VS Code extension and console)

Source: https://code.claude.com/docs/en/mcp

Same configuration for the extension and the `claude` console. Project scope is `.mcp.json` at the project root, top-level key `mcpServers`; `type` is optional for stdio. Claude Code asks for approval before using a project-scoped server.

```bash
claude mcp add --env CIAM_PROJECT_PATH=$PWD --transport stdio --scope project ciam -- ~/bin/ciam mcp
claude mcp add --transport stdio --scope project knowledge -- ~/bin/knowledge mcp --dir $PWD/.knowledge
claude mcp list
```

The CLI parses everything before `--` (a server name placed right after `--env` is read as another variable, hence `--transport` in between). Or `ciam init --editor claude` plus a `knowledge` entry by hand:

```json
{
  "mcpServers": {
    "ciam": { "command": "/home/you/bin/ciam", "args": ["mcp"], "env": { "CIAM_PROJECT_PATH": "/path/to/project" } },
    "knowledge": { "command": "/home/you/bin/knowledge", "args": ["mcp", "--dir", "/path/to/project/.knowledge"] }
  }
}
```

The docs say that without `--scope` the server is stored for you only (`~/.claude.json`, local scope); use `--scope user` for all projects. Inside a session `/mcp` shows status.

### Kilo Code (VS Code extension)

Source: https://kilo.ai/docs/automate/mcp/using-in-kilo-code

Kilo's documentation has two generations. The current page stores MCP servers in the Kilo config file under the key `mcp` (global `~/.config/kilo/kilo.jsonc`; project `kilo.jsonc` in the root or `.kilo/kilo.jsonc`; project wins), with `command` as an array and `environment` for variables. The VS Code extension tab describes adding servers in Settings, MCP, Add Server. Older pages (https://kilocode.ai/docs/features/mcp/using-mcp-in-kilo-code) describe `mcp_settings.json` and `.kilocode/mcp.json` with `mcpServers`. Check which one your installed version reads (its MCP settings view opens the right file).

```json
{
  "mcp": {
    "ciam": {
      "type": "local",
      "command": ["/home/you/bin/ciam", "mcp"],
      "environment": { "CIAM_PROJECT_PATH": "/path/to/project" },
      "enabled": true
    },
    "knowledge": {
      "type": "local",
      "command": ["/home/you/bin/knowledge", "mcp", "--dir", "/path/to/project/.knowledge"],
      "enabled": true
    }
  }
}
```

### GitHub Copilot CLI

Source: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers

File `~/.copilot/mcp-config.json`, key `mcpServers`, local servers use `"type": "local"` and `tools: ["*"]` to enable all tools. Or `copilot mcp add NAME -- COMMAND [ARGS...]` with `--env KEY=VALUE`.

```json
{
  "mcpServers": {
    "ciam": {
      "type": "local",
      "command": "/home/you/bin/ciam",
      "args": ["mcp"],
      "env": { "CIAM_PROJECT_PATH": "/path/to/project" },
      "tools": ["*"]
    },
    "knowledge": {
      "type": "local",
      "command": "/home/you/bin/knowledge",
      "args": ["mcp", "--dir", "/path/to/project/.knowledge"],
      "tools": ["*"]
    }
  }
}
```

```bash
copilot mcp add --env CIAM_PROJECT_PATH=$PWD ciam -- ~/bin/ciam mcp
```

### Opencode

Source: https://opencode.ai/docs/mcp-servers

`opencode.json` or `opencode.jsonc`, key `mcp`, `"type": "local"`, `command` as an array, variables in `environment`, `enabled`. The page does not state the global config path: check Opencode docs. Opencode's docs also show a v2 schema (`mcp.servers`, https://opencode.ai/v2/docs/mcp-servers/); use the page that matches your installed version.

```jsonc
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "ciam": {
      "type": "local",
      "command": ["/home/you/bin/ciam", "mcp"],
      "environment": { "CIAM_PROJECT_PATH": "/path/to/project" },
      "enabled": true
    },
    "knowledge": {
      "type": "local",
      "command": ["/home/you/bin/knowledge", "mcp", "--dir", "/path/to/project/.knowledge"],
      "enabled": true
    }
  }
}
```

### Cursor

Source: https://cursor.com/docs/mcp

`.cursor/mcp.json` (project) or `~/.cursor/mcp.json` (global), key `mcpServers`; the page lists `type`, `command`, `args`, `env`, and the variables `${workspaceFolder}`, `${env:NAME}`.

```json
{
  "mcpServers": {
    "ciam": {
      "type": "stdio",
      "command": "/home/you/bin/ciam",
      "args": ["mcp"],
      "env": { "CIAM_PROJECT_PATH": "${workspaceFolder}" }
    },
    "knowledge": {
      "type": "stdio",
      "command": "/home/you/bin/knowledge",
      "args": ["mcp", "--dir", "${workspaceFolder}/.knowledge"]
    }
  }
}
```

## Pointing a client at a gateway

This is the client's own setting, separate from MCP. The router is not involved: a client talks to one gateway endpoint, and the gateway (freellmapi, omniroute, bifrost) does its own routing. Use the base URLs and ports from the [gateway table](gateway-router.md#the-gateways); `PORT` below stands for the port you run it on.

| Client | How it accepts a custom endpoint (source) | Notes |
|---|---|---|
| Claude Code (console and VS Code extension) | `ANTHROPIC_BASE_URL` plus `ANTHROPIC_AUTH_TOKEN` or `ANTHROPIC_API_KEY`, in the shell or in the `env` block of `~/.claude/settings.json`; in the VS Code extension, `claudeCode.environmentVariables` (https://code.claude.com/docs/en/llm-gateway-connect) | The gateway must expose an Anthropic-format endpoint (`/v1/messages`). Anthropic's page (https://code.claude.com/docs/en/llm-gateway) states it does not support routing Claude Code to non-Claude models through a gateway. The freellmapi README documents `npx freellmapi setup-claude`; whether that fits this limit: check Claude Code and freellmapi docs. |
| VS Code Copilot Chat | provider `Custom Endpoint` (`vendor: customendpoint`) with API type Chat Completions, Responses or Messages; the URL may include the API path, otherwise VS Code appends it and inserts `/v1` (https://code.visualstudio.com/docs/copilot/customization/language-models) | Models must support tool calling to appear for agent use. Business and Enterprise administrators can disable BYOK. |
| Copilot CLI | `COPILOT_PROVIDER_BASE_URL`, `COPILOT_MODEL`, optional `COPILOT_PROVIDER_TYPE` (`openai` default, `azure`, `anthropic`), `COPILOT_PROVIDER_API_KEY` (https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/use-byok-models) | Model must support tool calling and streaming. Example: `COPILOT_PROVIDER_BASE_URL=http://localhost:PORT/v1 COPILOT_MODEL=auto copilot`. Whether a gateway wants the `/v1` suffix here: check the gateway docs. |
| Opencode | `provider` entry with `npm: "@ai-sdk/openai-compatible"`, `options.baseURL`, `options.apiKey` (`{env:VAR}`), and a `models` map (https://opencode.ai/docs/providers) | Model ids are the ones the gateway expects. |
| Kilo Code | Settings, Providers, Custom provider: Provider ID, Display name, Provider API "OpenAI Compatible", Base URL (a base or a full endpoint URL), API key (marked optional), Models (https://kilo.ai/docs/providers/openai-compatible) | Older versions of Kilo show an "OpenAI Compatible" API provider with Base URL, API Key and Model ID; check Kilo docs for the one your version has. |
| Cursor | Third-party gateway guides describe Settings, Models, "Override OpenAI Base URL" with an OpenAI key and custom model names (for example https://docs.litellm.ai/docs/tutorials/cursor_integration). Cursor's own docs were not found for this: check Cursor docs | Those guides say only some Cursor features use the override and that a paid plan may be needed; not verified here. |

Opencode example for omniroute (key in the environment):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "omniroute": {
      "npm": "@ai-sdk/openai-compatible",
      "name": "OmniRoute",
      "options": { "baseURL": "http://localhost:20128/v1", "apiKey": "{env:OMNIROUTE_API_KEY}" },
      "models": { "auto": { "name": "auto" } }
    }
  }
}
```

Several gateways at once: a client holds one endpoint per provider entry (Opencode, VS Code and Kilo let you define several and switch in the model picker), but none of these clients fails over between them by itself. Automatic failover across freellmapi, omniroute and bifrost with context kept is what `router:<model>` does for Go code, such as the examples and the `knowledge` CLI.

## Planned: chat control of the engineering team

Not implemented. Today `independent_engineering_team` is driven from its own client: the manager exposes the Agent Server API (`remote/server`, graph `engineering_team`) and the workers speak Agent Protocol to the hub. There is no MCP server that wraps those, so a chat client cannot start or watch a run yet. A bridge would be a new MCP server (using `mcp/server`) that calls the manager's API. Nothing else in these manuals depends on it.
