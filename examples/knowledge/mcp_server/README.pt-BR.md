# mcp_server

[English](README.md) | **Português** | [Español](README.es-ES.md)

Sirva `knowledge.Toolkit` por MCP para que editores e assistentes desktop consultem a sua base de conhecimento.

## O que faz

- Monta o mesmo pipeline RAG pequeno e o expõe com `kb.MCPServer(nil)`.
- Com `-demo`, alimenta um `tools/list` e um `tools/call` pelo servidor em processo e imprime as respostas JSON-RPC (uma checagem offline). Sem ele, o servidor fala MCP por stdio.

## Como aplicar

```bash
go run ./examples/knowledge/mcp_server            # MCP over stdio
go run ./cmd/knowledge mcp                       # the CLI version, over your own index
```

Registre o comando no VS Code, no Cursor ou no Claude Desktop como servidor MCP stdio. O toolkit inclui um hook `Ingest` para que o assistente também adicione documentos; deixe-o sem definir para um servidor somente-leitura.

## Como rodar

```bash
go run ./examples/knowledge/mcp_server
```

O modelo aqui é roteirizado (`models.NewFake`) e ignora chaves de API, então a execução é determinística e offline. Para testar um modelo real, crie um com `chatmodels.Init("anthropic:claude-sonnet-4-5")` (ou `"openai:gpt-4o-mini"`) e passe como `Model`; um modelo real decide as próprias chamadas de tool, então a conversa será diferente da roteirizada.

## Como testar em modo produtivo

1. Rode `go run ./examples/knowledge/mcp_server -demo` e confira as respostas de `tools/list` e `tools/call`.
2. Registre o servidor na configuração MCP do seu editor (comando `go`, args `run ./cmd/knowledge mcp`, ou um binário compilado) e faça uma pergunta que precise dos seus documentos.
3. Use um embedder real e um índice persistido; exponha só as tools de que o cliente precisa e prefira somente-leitura.
4. Lembre que resultados de tools MCP são texto não confiável para o modelo do cliente; não coloque segredos em documentos indexados.

## Veja também

- [Manual, capítulo 11: MCP, servidor e graphs remotos](../../../docs/pt-BR/manual/11-mcp-remote-servidor.md)
- [KNOWLEDGE.md](../../../docs/pt-BR/KNOWLEDGE.md)
- [Checklist de produção](../../../docs/pt-BR/manual/14-producao-seguranca-troubleshooting.md)
