# plan_and_execute

[English](README.md) | [Português](README.pt-BR.md) | **Español**

Un planner escribe los pasos, un executor los ejecuta uno a uno y un replanner decide continuar o responder.

## Qué hace

- `plan` convierte la pregunta en una lista numerada; `execute` toma el primer paso, lo ejecuta y añade `(paso, resultado)` a `past_steps`; `replan` responde cuando no quedan pasos.
- La ejecución guionizada responde "ciudad natal del ganador del Australian Open 2024" en dos pasos.

## Cómo aplicarlo

```go
AddEdge("execute", "replan").
AddConditionalEdges("replan", func(_ context.Context, s graph.State) (any, error) {
    if s["response"] != nil { return graph.End, nil }
    return "execute", nil
})
```

Úsalo en preguntas de varios pasos donde un plan ayuda más que un bucle libre. En producción el replanner también debe revisar el plan restante con la nueva evidencia, no solo parar cuando la lista se vacía.

## Cómo ejecutarlo

```bash
go run ./examples/langgraph/plan_and_execute
```

El ejemplo elige el modelo mediante `examples/internal/demo`: con `ANTHROPIC_API_KEY` usa `anthropic:claude-sonnet-4-5`, con `OPENAI_API_KEY` usa `openai:gpt-4o-mini` y, sin clave, recurre a un falso con respuestas guionizadas, por lo que corre offline.

## Cómo probarlo en modo productivo

1. Usa modelos reales y da al executor tools reales (búsqueda, calculadoras); hoy solo llama a un modelo.
2. Limita el número de pasos y valida el formato del plan antes de ejecutar.
3. Persiste el plan y `past_steps` con un checkpointer para que las tareas largas puedan reanudarse.
4. Evalúa la calidad del plan por separado de la calidad de la respuesta.

## Ver también

- [Manual, capítulo 7: LangGraph: StateGraph](../../../docs/es-ES/manual/07-langgraph.md)
- [Checklist de producción](../../../docs/es-ES/manual/14-producao-seguranca-troubleshooting.md)
