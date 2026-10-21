# Conectando editores e consoles

[English](../en-US/clients-mcp.md) | **Português** | [Español](../es-ES/clients-mcp.md)

Este projeto traz dois servidores MCP via stdio. Qualquer cliente MCP pode iniciá-los como subprocesso:

| Servidor | Comando | Requer |
|---|---|---|
| `ciam` (contexto do projeto: busca, ADR/PRD/planos, memória; ferramentas `ciam_*`) | `ciam mcp` | nada; rode `ciam index` uma vez por projeto para respostas úteis. Veja [CIAM.md](../../pt-BR/CIAM.md). |
| `knowledge` (respostas RAG, CAG e KAG sobre seus documentos) | `knowledge mcp --dir .knowledge` | um índice criado com `knowledge index <caminho> --out .knowledge`. Sem ele o servidor sai com `no index in ...`. Veja [KNOWLEDGE.md](../../pt-BR/KNOWLEDGE.md). |

Ambos saem limpos quando o stdin fecha (verificado: `ciam mcp < /dev/null` e `knowledge mcp --dir <dir indexado> < /dev/null` devolvem 0; uma requisição `tools/list` pelo stdin devolve a lista de ferramentas).

## 1. Compile os binários

Os editores iniciam o comando sozinhos, com o diretório de trabalho deles. `go run` compila a cada início (lento, e o `ciam init` gravaria um caminho temporário), então compile uma vez e use o caminho absoluto:

```bash
go build -o ~/bin/ciam ./cmd/ciam
go build -o ~/bin/knowledge ./cmd/knowledge

cd /path/to/your/project
ciam index .
knowledge index docs --glob "*.md" --out .knowledge
```

Variáveis de ambiente lidas pelos servidores: `CIAM_PROJECT_PATH`, `CIAM_DB_PATH` (padrão `~/.local/share/ciam/ciam.json`), `CIAM_EMBEDDER=fake` (offline), `CIAM_OLLAMA_URL`, `KNOWLEDGE_MODEL` (`provider:model`, aceita `router:<model>`, veja [gateway-router.md](gateway-router.md)) e `KNOWLEDGE_EMBEDDINGS`. Sem chave de API nem `KNOWLEDGE_MODEL`, o `knowledge` responde com um falso offline que repete a principal fonte e imprime um aviso no stderr.

## 2. O auxiliar `ciam init`

`ciam init [--editor vscode|cursor|claude] [--print]` grava a entrada `ciam` para você, mesclando com os servidores já presentes no arquivo (JSON inválido no arquivo é erro, não é sobrescrito). Sem `--editor` escolhe Cursor se existir `.cursor/`, senão VS Code.

| `--editor` | Arquivo gravado (raiz do projeto) | Chave de topo |
|---|---|---|
| `vscode` | `.vscode/mcp.json` | `servers`, a entrada tem `"type": "stdio"` e `CIAM_PROJECT_PATH=${workspaceFolder}` |
| `cursor` | `.cursor/mcp.json` | `mcpServers` |
| `claude` | `.mcp.json` | `mcpServers` |

```bash
cd /path/to/your/project
ciam init --editor vscode --print   # preview
ciam init --editor claude           # write .mcp.json
```

Ele grava o caminho do executável em execução como `command`, por isso você compila antes (um binário de `go run` fica num diretório temporário). Registra só o `ciam`; adicione o `knowledge` à mão com as entradas abaixo. A entrada do Cursor que ele grava não tem `"type"`, enquanto a doc do Cursor lista `type` como obrigatório para servidores stdio: se o Cursor não carregar a entrada, adicione `"type": "stdio"`.

## 3. Clientes

Os formatos de configuração abaixo vêm da documentação de cada produto, lida em 2026-10-21. Mudam entre versões; onde a página trazia dois formatos, ambos são citados.

### VS Code com GitHub Copilot Chat

Fonte: https://code.visualstudio.com/docs/agents/reference/mcp-configuration e https://docs.github.com/en/copilot/customizing-copilot/extending-copilot-chat-with-mcp

Arquivo `.vscode/mcp.json` (workspace) ou o arquivo de usuário aberto pelo comando **MCP: Open User Configuration**. Chave de topo `servers`; entradas stdio usam `type`, `command`, `args`, `env`; `${workspaceFolder}` está disponível.

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

A página do GitHub diz que um botão Start aparece acima da entrada e que as ferramentas MCP são usadas no Copilot Chat em modo Agent. A política da organização pode restringir MCP e o uso de modelo próprio (bring your own model): confira as docs do Copilot para o seu plano.

### Claude Code (extensão do VS Code e console)

Fonte: https://code.claude.com/docs/en/mcp

Mesma configuração para a extensão e para o console `claude`. O escopo de projeto é `.mcp.json` na raiz do projeto, chave de topo `mcpServers`; `type` é opcional para stdio. O Claude Code pede aprovação antes de usar um servidor de escopo de projeto.

```bash
claude mcp add --env CIAM_PROJECT_PATH=$PWD --transport stdio --scope project ciam -- ~/bin/ciam mcp
claude mcp add --transport stdio --scope project knowledge -- ~/bin/knowledge mcp --dir $PWD/.knowledge
claude mcp list
```

A CLI interpreta tudo antes de `--` (um nome de servidor logo após `--env` é lido como outra variável, por isso o `--transport` no meio). Ou `ciam init --editor claude` mais uma entrada `knowledge` à mão:

```json
{
  "mcpServers": {
    "ciam": { "command": "/home/you/bin/ciam", "args": ["mcp"], "env": { "CIAM_PROJECT_PATH": "/path/to/project" } },
    "knowledge": { "command": "/home/you/bin/knowledge", "args": ["mcp", "--dir", "/path/to/project/.knowledge"] }
  }
}
```

As docs dizem que, sem `--scope`, o servidor é guardado só para você (`~/.claude.json`, escopo local); use `--scope user` para todos os projetos. Dentro de uma sessão, `/mcp` mostra o status.

### Kilo Code (extensão do VS Code)

Fonte: https://kilo.ai/docs/automate/mcp/using-in-kilo-code

A documentação do Kilo tem duas gerações. A página atual guarda os servidores MCP no arquivo de configuração do Kilo sob a chave `mcp` (global `~/.config/kilo/kilo.jsonc`; projeto `kilo.jsonc` na raiz ou `.kilo/kilo.jsonc`; o projeto vence), com `command` como array e `environment` para variáveis. A aba da extensão do VS Code descreve adicionar servidores em Settings, MCP, Add Server. Páginas antigas (https://kilocode.ai/docs/features/mcp/using-mcp-in-kilo-code) descrevem `mcp_settings.json` e `.kilocode/mcp.json` com `mcpServers`. Confira qual a sua versão instalada lê (a tela de configurações de MCP abre o arquivo certo).

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

Fonte: https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/add-mcp-servers

Arquivo `~/.copilot/mcp-config.json`, chave `mcpServers`; servidores locais usam `"type": "local"` e `tools: ["*"]` para habilitar todas as ferramentas. Ou `copilot mcp add NOME -- COMANDO [ARGS...]` com `--env CHAVE=VALOR`.

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

Fonte: https://opencode.ai/docs/mcp-servers

`opencode.json` ou `opencode.jsonc`, chave `mcp`, `"type": "local"`, `command` como array, variáveis em `environment`, `enabled`. A página não informa o caminho da configuração global: check Opencode docs. As docs do Opencode também mostram um esquema v2 (`mcp.servers`, https://opencode.ai/v2/docs/mcp-servers/); use a página que corresponde à sua versão instalada.

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

Fonte: https://cursor.com/docs/mcp

`.cursor/mcp.json` (projeto) ou `~/.cursor/mcp.json` (global), chave `mcpServers`; a página lista `type`, `command`, `args`, `env` e as variáveis `${workspaceFolder}`, `${env:NAME}`.

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

## Apontando um cliente para um gateway

Esta é uma configuração do próprio cliente, separada do MCP. O router não participa: o cliente fala com um único endpoint de gateway, e o gateway (freellmapi, omniroute, bifrost) faz o próprio roteamento. Use as base URLs e portas da [tabela de gateways](gateway-router.md#os-gateways); `PORT` abaixo é a porta em que você o executa.

| Cliente | Como aceita um endpoint próprio (fonte) | Notas |
|---|---|---|
| Claude Code (console e extensão do VS Code) | `ANTHROPIC_BASE_URL` mais `ANTHROPIC_AUTH_TOKEN` ou `ANTHROPIC_API_KEY`, no shell ou no bloco `env` de `~/.claude/settings.json`; na extensão do VS Code, `claudeCode.environmentVariables` (https://code.claude.com/docs/en/llm-gateway-connect) | O gateway deve expor um endpoint em formato Anthropic (`/v1/messages`). A página da Anthropic (https://code.claude.com/docs/en/llm-gateway) afirma que não suporta rotear o Claude Code para modelos que não sejam Claude por um gateway. O README do freellmapi documenta `npx freellmapi setup-claude`; se isso se encaixa nesse limite: check Claude Code and freellmapi docs. |
| VS Code Copilot Chat | provedor `Custom Endpoint` (`vendor: customendpoint`) com tipo de API Chat Completions, Responses ou Messages; a URL pode incluir o caminho da API, senão o VS Code o acrescenta e insere `/v1` (https://code.visualstudio.com/docs/copilot/customization/language-models) | Os modelos precisam suportar tool calling para aparecer no uso com agente. Administradores Business e Enterprise podem desativar o BYOK. |
| Copilot CLI | `COPILOT_PROVIDER_BASE_URL`, `COPILOT_MODEL`, opcionais `COPILOT_PROVIDER_TYPE` (`openai` padrão, `azure`, `anthropic`), `COPILOT_PROVIDER_API_KEY` (https://docs.github.com/en/copilot/how-tos/copilot-cli/customize-copilot/use-byok-models) | O modelo precisa suportar tool calling e streaming. Exemplo: `COPILOT_PROVIDER_BASE_URL=http://localhost:PORT/v1 COPILOT_MODEL=auto copilot`. Se um gateway quer o sufixo `/v1` aqui: check the gateway docs. |
| Opencode | entrada `provider` com `npm: "@ai-sdk/openai-compatible"`, `options.baseURL`, `options.apiKey` (`{env:VAR}`) e um mapa `models` (https://opencode.ai/docs/providers) | Os ids de modelo são os que o gateway espera. |
| Kilo Code | provedor de API "OpenAI Compatible": Base URL, API Key, Model ID (https://kilo.ai/docs/providers/openai-compatible) | Versões novas podem mostrar Settings, Providers, Add custom provider; os mesmos três valores. |
| Cursor | Guias de gateways de terceiros descrevem Settings, Models, "Override OpenAI Base URL" com uma chave OpenAI e nomes de modelo próprios (por exemplo https://docs.litellm.ai/docs/tutorials/cursor_integration). As docs do próprio Cursor não foram encontradas para isso: check Cursor docs | Esses guias dizem que só alguns recursos do Cursor usam o override e que pode ser preciso um plano pago; não verificado aqui. |

Exemplo de Opencode para o omniroute (chave no ambiente):

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

Vários gateways ao mesmo tempo: um cliente guarda um endpoint por entrada de provedor (Opencode, VS Code e Kilo permitem definir vários e alternar no seletor de modelos), mas nenhum desses clientes faz failover entre eles sozinho. O failover automático entre freellmapi, omniroute e bifrost mantendo o contexto é o que `router:<model>` faz para código Go, como os exemplos e a CLI `knowledge`.

## Planejado: controlar o time de engenharia por chat

Não implementado. Hoje o `independent_engineering_team` é conduzido pelo próprio cliente: o manager expõe a Agent Server API (`remote/server`, grafo `engineering_team`) e os workers falam Agent Protocol com o hub. Não existe servidor MCP que envolva isso, então um cliente de chat ainda não consegue iniciar nem acompanhar uma execução. Uma ponte seria um novo servidor MCP (usando `mcp/server`) que chama a API do manager. Nada mais nestes manuais depende disso.
