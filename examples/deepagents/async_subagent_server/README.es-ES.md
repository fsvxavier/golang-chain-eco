# async_subagent_server

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent worker servido por la API HTTP del Agent Server y controlado por un supervisor mediante un `RemoteRunner`.

El agent se ejecuta con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, los resultados JSON de tools pasan a TOON, y todo se decide por reglas, sin necesidad de modelo.

## Qué hace

- Sirve un grafo `researcher` con `remote/server` en un servidor `httptest` (150 ms de trabajo simulado).
- El supervisor inicia una tarea asíncrona mediante `dmw.RemoteRunner{Client: remote.NewClient(url), AssistantID: "researcher"}`; el cliente solo usa HTTP.
- El ejemplo consulta la ejecución hasta que termina con éxito e imprime los resultados.

## Cómo aplicarlo

Separa el worker en su propio servicio y mantén el supervisor ligero:

```go
// worker process
http.ListenAndServe(":8123", server.New(map[string]*graph.Compiled{"researcher": worker}))
// supervisor process
runner := dmw.RemoteRunner{Client: remote.NewClient("http://worker:8123"), AssistantID: "researcher"}
```

El mismo servidor habla el subconjunto de LangGraph Platform, así que un cliente Python o JS también puede llamarlo.

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/async_subagent_server
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Ejecuta worker y supervisor como dos procesos (o contenedores) y cambia la URL de `httptest` por la dirección del worker.
2. Pon autenticación y TLS delante del worker (proxy inverso o middleware); `remote/server` no autentica por sí mismo.
3. Da al worker un checkpointer duradero (aquí usa `checkpoint.NewMemory()`) para que ejecuciones y threads sobrevivan a los reinicios.
4. Define timeouts en el cliente, gestiona `remote.APIError` y añade un health check para el worker.
5. Haz una prueba de carga con varias tareas concurrentes y observa la memoria del worker.

## Ver también

- [Manual, capítulo 11: MCP, servidor y grafos remotos](../../../docs/es-ES/manual/11-mcp-remote-servidor.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
