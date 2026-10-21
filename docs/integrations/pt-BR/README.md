# Manuais de integração

[English](../en-US/README.md) | **Português** | [Español](../es-ES/README.md)

Como conectar este projeto às ferramentas que você já usa: assistentes de código no editor, consoles de terminal e os gateways de LLM que ficam na frente dos modelos.

| Manual | O que cobre |
|---|---|
| [gateway-router.md](gateway-router.md) | Usar `router:<model>` para distribuir chamadas entre freellmapi, omniroute, bifrost, Anthropic e OpenAI a partir de um único `.env`, incluindo modelos por papel nos exemplos de time de engenharia. |
| [clients-mcp.md](clients-mcp.md) | Conectar `ciam mcp` e `knowledge mcp` (MCP via stdio) ao VS Code com Copilot Chat, Claude Code, Kilo Code, Copilot CLI, Opencode e Cursor, e apontar cada cliente para um gateway. |

Documentos de referência em que estes manuais se apoiam (não repetidos aqui): a [seção Router de PROVIDERS.md](../../pt-BR/PROVIDERS.md#router), o [capítulo 3.6.1 do manual](../../pt-BR/manual/03-modelos-e-provedores.md), [CIAM.md](../../pt-BR/CIAM.md) e [KNOWLEDGE.md](../../pt-BR/KNOWLEDGE.md).

## O que funciona hoje e o que está planejado

| Capacidade | Status |
|---|---|
| Código Go (e os exemplos) chamando vários gateways e provedores por `router:<model>`, com os modos failover, round robin, random e weighted | Funciona hoje |
| Um `.env` controlando o router do processo inteiro; um modelo por papel em `independent_engineering_team` | Funciona hoje |
| `ciam mcp` e `knowledge mcp` como servidores MCP via stdio para qualquer cliente MCP | Funciona hoje (verificado executando os servidores; veja as notas por cliente em [clients-mcp.md](clients-mcp.md) para o que foi ou não confirmado) |
| `ciam init --editor vscode\|cursor\|claude` grava a entrada MCP do VS Code, Cursor e Claude Code | Funciona hoje |
| Apontar um editor ou console para um gateway (freellmapi, omniroute, bifrost) | Feito pelas configurações de cada cliente, não por este projeto; veja [clients-mcp.md](clients-mcp.md) |
| O router como servidor HTTP que editores usariam como endpoint | Não existe: o router é uma biblioteca Go (`providers/router`), não um processo proxy. Os clientes falam com um gateway; o código Go fala com o router |
| Qual alvo respondeu a uma chamada, como linha de log ou campo da resposta | Não existe: o router só informa quando todos os alvos falharam (veja [gateway-router.md](gateway-router.md#qual-alvo-respondeu)) |
| Controlar o time de engenharia por um chat via MCP | Planejado; ainda não existe ponte MCP (veja a última seção de [clients-mcp.md](clients-mcp.md)) |

## Fatos que vêm de outros produtos

Nomes de arquivos de configuração dos clientes, portas dos gateways e nomes de variáveis pertencem a esses produtos e mudam entre versões. Cada fato desse tipo nestes manuais vem acompanhado da URL de onde foi lido (lido em 2026-10-21). Quando um fato não pôde ser confirmado, o manual diz "check <produto> docs" em vez de adivinhar.
