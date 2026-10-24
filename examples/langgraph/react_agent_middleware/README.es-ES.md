# react_agent_middleware

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un agent con llamadas a tools armado con `agents.Create`, un middleware y updates en streaming. El agent se ejecuta con `middleware.Compaction()`: los turnos antiguos se comprimen al estilo caveman, se añade una nota Ponytail cuando la conversación tiene una parte real de código, y los resultados JSON de tools se recodifican en TOON, todo decidido por reglas, sin necesitar modelo.

## Qué hace

- Una tool (`get_weather`) y `middleware.ModelCallLimit{RunLimit: 5}`.
- `StreamEvents` con `graph.ModeUpdates` imprime el update de cada nodo a medida que ocurre (llamada al modelo, llamada a tool, respuesta final).

## Cómo aplicarlo

```go
agent, _ := agents.Create(agents.Options{
    Model: model, Tools: []tools.Tool{weather}, SystemPrompt: "...",
    Middleware: []agents.Middleware{middleware.ModelCallLimit{RunLimit: 5}},
})
for ev, err := range agent.StreamEvents(ctx, graph.State{"messages": q}, graph.ModeUpdates) { ... }
```

Este es el punto de partida por defecto de un agent. Haz streaming a la UI para que los usuarios vean el progreso; el schema de la tool sale de la struct de argumentos en Go (tags `json` y `description`).

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/react_agent_middleware
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Ejecuta con una clave real y una API de clima real detrás de la tool (timeout, mapeo de errores).
2. Añade `ToolRetry`, `ModelRetry` y `ModelFallback`; ver `langchain/middleware_stack`.
3. Añade un checkpointer y un `thread_id` si la conversación continúa entre peticiones.
4. Comprueba el schema de tool que recibe el modelo; las tags `description` vagas causan argumentos erróneos.

## Ver también

- [Manual, capítulo 9: Agents y middleware](../../../docs/es-ES/manual/09-agents-e-middleware.md)
- [Manual, capítulo 5: Tools](../../../docs/es-ES/manual/05-tools.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
