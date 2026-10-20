# mcp_server

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Sirve `knowledge.Toolkit` por MCP para que editores y asistentes de escritorio consulten tu base de conocimiento.

## Qué hace

- Monta el mismo pipeline RAG pequeño y lo expone con `kb.MCPServer(nil)`.
- Con `-demo`, alimenta un `tools/list` y un `tools/call` a través del servidor en proceso e imprime las respuestas JSON-RPC (una comprobación offline). Sin él, el servidor habla MCP por stdio.

## Cómo aplicarlo

```bash
go run ./examples/knowledge/mcp_server            # MCP over stdio
go run ./cmd/knowledge mcp                       # the CLI version, over your own index
```

Registra el comando en VS Code, Cursor o Claude Desktop como servidor MCP stdio. El toolkit incluye un hook `Ingest` para que el asistente también añada documentos; déjalo sin definir para un servidor de solo lectura.

## Cómo ejecutarlo

```bash
go run ./examples/knowledge/mcp_server
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Ejecuta `go run ./examples/knowledge/mcp_server -demo` y comprueba las respuestas de `tools/list` y `tools/call`.
2. Registra el servidor en la configuración MCP de tu editor (comando `go`, args `run ./cmd/knowledge mcp`, o un binario compilado) y haz una pregunta que necesite tus documentos.
3. Usa un embedder real y un índice persistido; expón solo las tools que el cliente necesita y prefiere solo lectura.
4. Recuerda que los resultados de las tools MCP son texto no confiable para el modelo del cliente; no pongas secretos en documentos indexados.

## Ver también

- [Manual, capítulo 11: MCP, servidor y grafos remotos](../../../docs/es-ES/manual/11-mcp-remote-servidor.md)
- [KNOWLEDGE.md](../../../docs/es-ES/KNOWLEDGE.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
