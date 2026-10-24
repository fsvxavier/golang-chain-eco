# async_subagents

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un deep agent lanza una tarea larga en segundo plano y la consulta más tarde.

El agent se ejecuta con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, los resultados JSON de tools pasan a TOON, y todo se decide por reglas, sin necesidad de modelo.

## Qué hace

- El middleware `AsyncSubagents` da al agent principal `start_async_task` (y las tools de comprobar/cancelar).
- El `researcher` corre en segundo plano mediante un `LocalRunner` en proceso mientras el agent principal sigue respondiendo.
- El ejemplo consulta el handle hasta que la tarea termina e imprime el informe.

## Cómo aplicarlo

```go
runner := &dmw.LocalRunner{Agent: researcher}
dmw.AsyncSubagents{Agents: []dmw.AsyncSubAgent{{Name: "researcher", Description: "long-running research", Runner: runner}}}
```

La interfaz `AsyncRunner` (`Start`, `Poll`, `Send`, `Cancel`) es lo que implementas o cambias: `LocalRunner` se queda en proceso, `RemoteRunner` habla con un Agent Server (ver `async_subagent_server`).

## Cómo ejecutarlo

```bash
go run ./examples/deepagents/async_subagents
```

El modelo aquí está guionizado (`models.NewFake`) e ignora las claves de API, por lo que la ejecución es determinista y offline. Para probar un modelo real, créalo con `chatmodels.Init("anthropic:claude-sonnet-4-5")` (o `"openai:gpt-4o-mini"`) y pásalo como `Model`; un modelo real decide sus propias llamadas a tools, así que la conversación diferirá de la guionizada.

## Cómo probarlo en modo productivo

1. Usa modelos reales; las investigaciones reales tardan minutos, así que consulta con backoff en lugar de un bucle apretado.
2. Limita las tareas en segundo plano simultáneas y da un plazo a cada una; cancela con `Cancel` cuando el usuario se vaya.
3. Persiste los handles de las tareas (el estado `async_tasks`) con un checkpointer duradero para que un reinicio no deje trabajo huérfano.
4. Mueve los workers pesados a procesos separados con `RemoteRunner` cuando necesiten aislamiento o escala.

## Ver también

- [Manual, capítulo 10: Deep Agents](../../../docs/es-ES/manual/10-deep-agents.md)
- [Manual, capítulo 11: MCP, servidor y grafos remotos](../../../docs/es-ES/manual/11-mcp-remote-servidor.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
