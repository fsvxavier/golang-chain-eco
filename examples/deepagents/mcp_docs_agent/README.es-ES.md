# mcp_docs_agent

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent de búsqueda en documentación cuyas tools provienen de un servidor MCP.

## Qué hace

- Levanta un pequeño servidor MCP en proceso que expone `search_docs`, conecta con `mcp.Connect` por un pipe en memoria y carga sus tools con `client.Tools`.
- La tool se llama `docs_search_docs` (prefijo del servidor + tool). El agent la llama y responde a partir del resultado.

## Cómo aplicarlo

Cualquier servidor MCP se convierte en tools de agent:

```go
client, _ := mcp.Connect(ctx, mcp.HTTP("https://docs.example.com/mcp")) // or mcp.Stdio(...)
defer client.Close()
ts, _ := client.Tools(ctx, "docs")
agent, _ := agents.Create(agents.Options{Model: model, Tools: ts})
```

Cambia `mcp.NewPipeTransport` por `mcp.HTTP(url)` para un servidor remoto o `mcp.Stdio(...)` para un proceso local.

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/mcp_docs_agent
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Cambia el transporte por pipe por el transporte del servidor real y usa un modelo real.
2. Autentícate en el servidor MCP con una credencial de solo lectura; lista las tools que expone y elimina las que el agent no debe tener antes de pasarlas en `Tools`.
3. Trata la salida de las tools como texto no confiable (puede contener instrucciones).
4. Define timeouts en `Connect` y en las llamadas a tools, y llama a `client.Close()` al apagar.
5. Para exponer tus propias tools en sentido inverso, mira `mcp/server` y `examples/knowledge/mcp_server`.

## Ver también

- [Manual, capítulo 11: MCP, servidor y grafos remotos](../../../docs/es-ES/manual/11-mcp-remote-servidor.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
