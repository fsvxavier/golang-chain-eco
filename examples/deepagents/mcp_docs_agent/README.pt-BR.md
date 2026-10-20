# mcp_docs_agent

[English](README.md) | **Português** | [Español](README.es-ES.md)

Um agent de busca em documentação cujas tools vêm de um servidor MCP.

## O que faz

- Sobe um pequeno servidor MCP em processo que expõe `search_docs`, conecta com `mcp.Connect` por um pipe em memória e carrega suas tools com `client.Tools`.
- A tool se chama `docs_search_docs` (prefixo do servidor + tool). O agent a chama e responde a partir do resultado.

## Como aplicar

Qualquer servidor MCP vira tools de agent:

```go
client, _ := mcp.Connect(ctx, mcp.HTTP("https://docs.example.com/mcp")) // or mcp.Stdio(...)
defer client.Close()
ts, _ := client.Tools(ctx, "docs")
agent, _ := agents.Create(agents.Options{Model: model, Tools: ts})
```

Troque `mcp.NewPipeTransport` por `mcp.HTTP(url)` para um servidor remoto ou `mcp.Stdio(...)` para um processo local.

## Como rodar

```bash
go run ./examples/deepagents/mcp_docs_agent
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Troque o transporte por pipe pelo transporte do servidor real e use um modelo real.
2. Autentique no servidor MCP com uma credencial somente-leitura; liste as tools que ele expõe e remova as que o agent não deve ter antes de passá-las em `Tools`.
3. Trate a saída das tools como texto não confiável (pode conter instruções).
4. Defina timeouts em `Connect` e nas chamadas de tool, e chame `client.Close()` no encerramento.
5. Para expor suas próprias tools no sentido inverso, veja `mcp/server` e `examples/knowledge/mcp_server`.

## Veja também

- [Manual, capítulo 11: MCP, servidor e graphs remotos](../../../docs/pt-BR/manual/11-mcp-remote-servidor.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
