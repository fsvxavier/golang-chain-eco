# info_gather

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un chatbot que recoge requisitos a lo largo de la conversación y luego pasa a generar el prompt final.

## Qué hace

- `gather` hace preguntas hasta que el recolector responde `READY: ...`, lo que rellena `spec`; entonces una arista condicional va a `write_prompt`.
- Cada mensaje del usuario es un `Invoke` separado en el mismo `thread_id`; un checkpointer mantiene la conversación entre los turnos.

## Cómo aplicarlo

Úsalo para cualquier recogida guiada: formularios, onboarding, captura de requisitos.

```go
AddConditionalEdges("gather", func(_ context.Context, s graph.State) (any, error) {
    if s["spec"] != nil { return "write_prompt", nil }
    return graph.End, nil // wait for the next user message
})
```

Terminar el grafo en "necesito más información" y reanudar con el siguiente mensaje en el mismo thread es el patrón; no hace falta un proceso de larga duración.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/info_gather
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa un modelo real y un checkpointer duradero para que la conversación sobreviva a los reinicios; deriva el `thread_id` de la sesión.
2. Haz que la señal de "listo" sea estructurada (JSON o una llamada a tool) en lugar de un prefijo de texto.
3. Valida la spec recogida antes de generar y limita el número de turnos.
4. Haz expirar los threads abandonados con `saver.DeleteThread`.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Manual, capítulo 8: Persistencia, human-in-the-loop y streaming](../../../docs/es-ES/manual/08-persistencia-hitl-streaming.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
