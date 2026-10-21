# Conectando editores y consolas

[English](../en-US/clients-mcp.md) | [Português](../pt-BR/clients-mcp.md) | **Español**

Este proyecto incluye dos servidores MCP por stdio. Cualquier cliente MCP puede iniciarlos como subproceso:

| Servidor | Comando | Requiere |
|---|---|---|
| `ciam` (contexto del proyecto: búsqueda, ADR/PRD/planes, memoria; herramientas `ciam_*`) | `ciam mcp` | nada; ejecute `ciam index` una vez por proyecto para obtener respuestas útiles. Vea [CIAM.md](../../es-ES/CIAM.md). |
| `knowledge` (respuestas RAG, CAG y KAG sobre sus documentos) | `knowledge mcp --dir .knowledge` | un índice creado con `knowledge index <ruta> --out .knowledge`. Sin él el servidor termina con `no index in ...`. Vea [KNOWLEDGE.md](../../es-ES/KNOWLEDGE.md). |

Ambos terminan limpiamente cuando stdin se cierra (verificado: `ciam mcp < /dev/null` y `knowledge mcp --dir <dir indexado> < /dev/null` devuelven 0; una petición `tools/list` por stdin devuelve la lista de herramientas).

## 1. Compile los binarios

Los editores inician el comando por su cuenta, con su propio directorio de trabajo. `go run` compila en cada inicio (lento, y `ciam init` registraría una ruta temporal), así que compile una vez y use la ruta absoluta:

```bash
go build -o ~/bin/ciam ./cmd/ciam
go build -o ~/bin/knowledge ./cmd/knowledge

cd /path/to/your/project
ciam index .
knowledge index docs --glob "*.md" --out .knowledge
```

Variables de entorno que leen los servidores: `CIAM_PROJECT_PATH`, `CIAM_DB_PATH` (por defecto `~/.local/share/ciam/ciam.json`), `CIAM_EMBEDDER=fake` (offline), `CIAM_OLLAMA_URL`, `KNOWLEDGE_MODEL` (`provider:model`, acepta `router:<model>`, vea [gateway-router.md](gateway-router.md)) y `KNOWLEDGE_EMBEDDINGS`. Sin clave de API ni `KNOWLEDGE_MODEL`, `knowledge` responde con un falso offline que repite la fuente principal e imprime un aviso en stderr.

## 2. El auxiliar `ciam init`

`ciam init [--editor vscode|cursor|claude] [--print]` escribe la entrada `ciam` por usted, combinándola con los servidores que ya estén en el archivo (un JSON inválido en el archivo es un error, no se sobrescribe). Sin `--editor` elige Cursor si existe `.cursor/`, si no VS Code.

| `--editor` | Archivo escrito (raíz del proyecto) | Clave de nivel superior |
|---|---|---|
| `vscode` | `.vscode/mcp.json` | `servers`, la entrada tiene `"type": "stdio"` y `CIAM_PROJECT_PATH=${workspaceFolder}` |
| `cursor` | `.cursor/mcp.json` | `mcpServers` |
| `claude` | `.mcp.json` | `mcpServers` |

```bash
cd /path/to/your/project
ciam init --editor vscode --print   # preview
ciam init --editor claude           # write .mcp.json
```

Escribe la ruta del ejecutable en curso como `command`, por eso se compila antes (un binario de `go run` vive en un directorio temporal). Solo registra `ciam`; agregue `knowledge` a mano con las entradas de abajo. La entrada de Cursor que escribe no tiene `"type"`, mientras que la doc de Cursor lista `type` como obligatorio para servidores stdio: si Cursor no carga la entrada, agregue `"type": "stdio"`.

## 3. Clientes

Los formatos de configuración de abajo provienen de la documentación de cada producto, leída el 2026-10-21. Cambian entre versiones; donde la página ofrecía dos formatos, se citan ambos.

### VS Code con GitHub Copilot Chat

Fuente: https://code.visualstudio.com/docs/agents/reference/mcp-configuration y https://docs.github.com/en/copilot/customizing-copilot/extending-copilot-chat-with-mcp

Archivo `.vscode/mcp.json` (workspace) o el archivo de usuario que abre el comando **MCP: Open User Configuration**. Clave de nivel superior `servers`; las entradas stdio usan `type`, `command`, `args`, `env`; `${workspaceFolder}` está disponible.

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

La página de GitHub dice que aparece un botón Start sobre la entrada y que las herramientas MCP se usan en Copilot Chat en modo Agent. La política de la organización puede restringir MCP y el uso de modelo propio (bring your own model): revise las docs de Copilot para su plan.

### Claude Code (extensión de VS Code y consola)

Fuente: https://code.claude.com/docs/en/mcp

Misma configuración para la extensión y para la consola `claude`. El alcance de proyecto es `.mcp.json` en la raíz del proyecto, clave de nivel superior `mcpServers`; `type` es opcional para stdio. Claude Code pide aprobación antes de usar un servidor de alcance de proyecto.

```bash
claude mcp add --env CIAM_PROJECT_PATH=$PWD --transport stdio --scope project ciam -- ~/bin/ciam mcp
claude mcp add --transport stdio --scope project knowledge -- ~/bin/knowledge mcp --dir $PWD/.knowledge
claude mcp list
```

La CLI interpreta todo lo que está antes de `--` (un nombre de servidor justo después de `--env` se lee como otra variable, de ahí el `--transport` en medio). O `ciam init --editor claude` más una entrada `knowledge` a mano:

```json
{
  "mcpServers": {
    "ciam": { "command": "/home/you/bin/ciam", "args": ["mcp"], "env": { "CIAM_PROJECT_PATH": "/path/to/project" } },
    "knowledge": { "command": "/home/you/bin/knowledge", "args": ["mcp", "--dir", "/path/to/project/.knowledge"] }
  }
}
```

Las docs dicen que, sin `--scope`, el servidor se guarda solo para usted (`~/.claude.json`, alcance local); use `--scope user` para todos los proyectos. Dentro de una sesión, `/mcp` muestra el estado.

### Kilo Code (extensión de VS Code)

Fuente: https://kilo.ai/docs/automate/mcp/using-in-kilo-code

La documentación de Kilo tiene dos generaciones. La página actual guarda los servidores MCP en el archivo de configuración de Kilo bajo la clave `mcp` (global `~/.config/kilo/kilo.jsonc`; proyecto `kilo.jsonc` en la raíz o `.kilo/kilo.jsonc`; gana el proyecto), con `command` como array y `environment` para variables. La pestaña de la extensión de VS Code describe agregar servidores en Settings, MCP, Add Server. Páginas antiguas (https://kilocode.ai/docs/features/mcp/using-mcp-in-kilo-code) describen `mcp_settings.json` y `.kilocode/mcp.json` con `mcpServers`. Compruebe cuál lee su versión instalada (la vista de configuración de MCP abre el archivo correcto).

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

Fuente: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers

Archivo `~/.copilot/mcp-config.json`, clave `mcpServers`; los servidores locales usan `"type": "local"` y `tools: ["*"]` para habilitar todas las herramientas. O `copilot mcp add NOMBRE -- COMANDO [ARGS...]` con `--env CLAVE=VALOR`.

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

Fuente: https://opencode.ai/docs/mcp-servers

`opencode.json` o `opencode.jsonc`, clave `mcp`, `"type": "local"`, `command` como array, variables en `environment`, `enabled`. La página no indica la ruta de la configuración global: check Opencode docs. Las docs de Opencode también muestran un esquema v2 (`mcp.servers`, https://opencode.ai/v2/docs/mcp-servers/); use la página que corresponda a su versión instalada.

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

Fuente: https://cursor.com/docs/mcp

`.cursor/mcp.json` (proyecto) o `~/.cursor/mcp.json` (global), clave `mcpServers`; la página lista `type`, `command`, `args`, `env` y las variables `${workspaceFolder}`, `${env:NAME}`.

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

## Apuntando un cliente a un gateway

Es un ajuste del propio cliente, separado de MCP. El router no interviene: el cliente habla con un único endpoint de gateway, y el gateway (freellmapi, omniroute, bifrost) hace su propio enrutamiento. Use las base URL y puertos de la [tabla de gateways](gateway-router.md#los-gateways); `PORT` abajo es el puerto en el que lo ejecuta.

| Cliente | Cómo acepta un endpoint propio (fuente) | Notas |
|---|---|---|
| Claude Code (consola y extensión de VS Code) | `ANTHROPIC_BASE_URL` más `ANTHROPIC_AUTH_TOKEN` o `ANTHROPIC_API_KEY`, en el shell o en el bloque `env` de `~/.claude/settings.json`; en la extensión de VS Code, `claudeCode.environmentVariables` (https://code.claude.com/docs/en/llm-gateway-connect) | El gateway debe exponer un endpoint en formato Anthropic (`/v1/messages`). La página de Anthropic (https://code.claude.com/docs/en/llm-gateway) afirma que no soporta enrutar Claude Code a modelos que no sean Claude mediante un gateway. El README de freellmapi documenta `npx freellmapi setup-claude`; si eso encaja en ese límite: check Claude Code and freellmapi docs. |
| VS Code Copilot Chat | proveedor `Custom Endpoint` (`vendor: customendpoint`) con tipo de API Chat Completions, Responses o Messages; la URL puede incluir la ruta de la API, si no VS Code la agrega e inserta `/v1` (https://code.visualstudio.com/docs/copilot/customization/language-models) | Los modelos deben soportar tool calling para aparecer en el uso con agente. Los administradores Business y Enterprise pueden desactivar BYOK. |
| Copilot CLI | `COPILOT_PROVIDER_BASE_URL`, `COPILOT_MODEL`, opcionales `COPILOT_PROVIDER_TYPE` (`openai` por defecto, `azure`, `anthropic`), `COPILOT_PROVIDER_API_KEY` (https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/use-byok-models) | El modelo debe soportar tool calling y streaming. Ejemplo: `COPILOT_PROVIDER_BASE_URL=http://localhost:PORT/v1 COPILOT_MODEL=auto copilot`. Si un gateway quiere el sufijo `/v1` aquí: check the gateway docs. |
| Opencode | entrada `provider` con `npm: "@ai-sdk/openai-compatible"`, `options.baseURL`, `options.apiKey` (`{env:VAR}`) y un mapa `models` (https://opencode.ai/docs/providers) | Los ids de modelo son los que espera el gateway. |
| Kilo Code | proveedor de API "OpenAI Compatible": Base URL, API Key, Model ID (https://kilo.ai/docs/providers/openai-compatible) | Las versiones nuevas pueden mostrar Settings, Providers, Add custom provider; los mismos tres valores. |
| Cursor | Guías de gateways de terceros describen Settings, Models, "Override OpenAI Base URL" con una clave OpenAI y nombres de modelo propios (por ejemplo https://docs.litellm.ai/docs/tutorials/cursor_integration). No se encontraron las docs del propio Cursor para esto: check Cursor docs | Esas guías dicen que solo algunas funciones de Cursor usan el override y que puede hacer falta un plan de pago; no verificado aquí. |

Ejemplo de Opencode para omniroute (clave en el entorno):

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

Varios gateways a la vez: un cliente guarda un endpoint por entrada de proveedor (Opencode, VS Code y Kilo permiten definir varios y alternar en el selector de modelos), pero ninguno de estos clientes hace failover entre ellos por sí solo. El failover automático entre freellmapi, omniroute y bifrost conservando el contexto es lo que hace `router:<model>` para código Go, como los ejemplos y la CLI `knowledge`.

## Planificado: controlar el equipo de ingeniería por chat

No implementado. Hoy `independent_engineering_team` se maneja desde su propio cliente: el manager expone la Agent Server API (`remote/server`, grafo `engineering_team`) y los workers hablan Agent Protocol con el hub. No existe un servidor MCP que lo envuelva, así que un cliente de chat todavía no puede iniciar ni seguir una ejecución. Un puente sería un nuevo servidor MCP (usando `mcp/server`) que llame a la API del manager. Nada más en estos manuales depende de ello.
